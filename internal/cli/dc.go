package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/HeoJeongBo/weft/internal/devcontainer"
	"github.com/HeoJeongBo/weft/internal/dockerx"
	"github.com/HeoJeongBo/weft/internal/logx"
	"github.com/HeoJeongBo/weft/internal/sysexec"
	"github.com/HeoJeongBo/weft/internal/tmux"
	"github.com/HeoJeongBo/weft/internal/tui"
)

// runDcPicker is a seam so tests can drive selection without a terminal.
var runDcPicker = tui.PickDc

// dcTmuxSession is the machine-global tmux session that holds one window per
// attached devcontainer — the single terminal that orchestrates them all.
const dcTmuxSession = "weft/dc"

// dcShellFallback picks the best interactive shell available in the container.
const dcShellFallback = `exec zsh -l 2>/dev/null || exec bash -l 2>/dev/null || exec sh -l`

// dcClaudeChain resumes the container's last claude conversation, falls back
// to a fresh claude, and to a plain shell (with an install hint) when claude
// is not installed. ~/.local/bin is prepended because the native installer
// puts claude there but only teaches interactive shells about it.
const dcClaudeChain = `export PATH="$HOME/.local/bin:$PATH"; ` +
	`if command -v claude >/dev/null 2>&1; then claude --continue || claude; else ` +
	`echo "weft: claude is not installed in this container."; ` +
	`echo "  install it with: curl -fsSL https://claude.ai/install.sh | bash"; ` +
	`echo "  (dropping to a shell)"; ` +
	dcShellFallback + `; fi`

// dcCandidate is one devcontainer discovered from docker's standard identity
// labels (devcontainer.local_folder / devcontainer.config_file), which the
// devcontainer CLI and VS Code stamp on every container they create.
type dcCandidate struct {
	Name          string // display name derived from the config location
	Folder        string // devcontainer.local_folder (workspace on the host)
	ConfigPath    string // devcontainer.config_file
	ContainerName string
	State         string // running, exited, ...
	HasWindow     bool   // a weft/dc tmux window with this name already exists
}

func newDcCmd() *cobra.Command {
	var start, shell bool
	cmd := &cobra.Command{
		Use:   "dc [query] [-- cmd...]",
		Short: "Scan every devcontainer on this machine and attach to one",
		Long: `Scan the machine for devcontainers — including ones VS Code started — via the
standard devcontainer.local_folder docker label, then orchestrate them from a
single terminal.

On a terminal, "weft dc" opens a picker: enter attaches a tmux window (session
"weft/dc") whose foreground runs claude inside the container, resuming its
last conversation (claude --continue; a stopped devcontainer is brought up
first, and a shell replaces claude when it is not installed). Rerunning weft
dc reuses the window, so picking is also how you switch between containers.
--shell opens a plain shell window instead of claude. Append "-- cmd..." to
run a one-shot command without tmux. Works outside git repositories; piped
output falls back to a plain table.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDc(cmd, args, start, shell)
		},
	}
	cmd.Flags().BoolVar(&start, "start", false, "bring a stopped devcontainer up before attaching")
	cmd.Flags().BoolVar(&shell, "shell", false, "open a shell window instead of claude")
	return cmd
}

func runDc(cmd *cobra.Command, args []string, start, shell bool) error {
	query, runCmd := splitAtDash(args, cmd.ArgsLenAtDash())
	if len(query) > 1 {
		return fmt.Errorf("at most one query expected, got %d", len(query))
	}
	q := strings.Join(query, "")

	verbosity, _ := cmd.Flags().GetCount("verbose")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	log := logx.New(os.Stderr, verbosity, false)
	r := newRunner(dryRun, log)

	interactive := len(runCmd) == 0 && isTTY(cmd.OutOrStdout())

	for {
		cs, err := dockerx.New(r).Ps(cmd.Context(), "devcontainer.local_folder")
		if err != nil {
			return err
		}
		cands := dcCandidates(cs, dcWindowNames(cmd, r))
		matches := matchDc(cands, q)

		if len(matches) == 0 {
			if len(cands) == 0 {
				return fmt.Errorf("no devcontainers found — open one in your editor or run `devcontainer up` first")
			}
			return fmt.Errorf("no devcontainer matches %q (run `weft dc` to list)", q)
		}

		var chosen dcCandidate
		autoStart := start
		switch {
		case len(matches) == 1 && q != "":
			// An explicit, unique query goes straight in.
			chosen = matches[0]
		case !interactive:
			if q == "" && !start && len(runCmd) == 0 {
				printDcTable(cmd.OutOrStdout(), matches, colorEnabled(cmd))
				return nil
			}
			if len(matches) > 1 {
				printDcTable(cmd.OutOrStdout(), matches, colorEnabled(cmd))
				return fmt.Errorf("%d devcontainers match %q — be more specific", len(matches), q)
			}
			chosen = matches[0]
		default:
			idx, err := runDcPicker(cmd.Context(), dcItems(matches))
			if err != nil {
				return err
			}
			if idx == tui.DcRescan {
				continue
			}
			if idx == tui.DcCancelled {
				return nil
			}
			chosen = matches[idx]
			autoStart = true // picking a stopped one means "bring it up and attach"
		}
		return dcAttach(cmd, r, chosen, autoStart, runCmd, verbosity, dryRun, shell)
	}
}

// dcAttach brings the devcontainer up if needed, then either runs a one-shot
// command inside it or ensures a weft/dc tmux window running claude (or a
// shell) and hands the terminal to that session.
func dcAttach(cmd *cobra.Command, r sysexec.Runner, c dcCandidate, autoStart bool, runCmd []string, verbosity int, dryRun, shell bool) error {
	if c.State != "running" {
		if !autoStart {
			return fmt.Errorf("%s is %s — rerun with --start to bring it up", c.Name, c.State)
		}
		fmt.Fprintf(cmd.ErrOrStderr(), "%s devcontainer up (%s)\n", colorize("▶", ansiCyan, colorEnabled(cmd)), c.Name)
		sink := func(l sysexec.Line) {
			if verbosity > 0 {
				fmt.Fprintln(cmd.ErrOrStderr(), l.Text)
			}
		}
		if _, err := devcontainer.New(r).Up(cmd.Context(), sink, devcontainer.UpOpts{
			WorkspaceFolder: c.Folder,
			ConfigPath:      c.ConfigPath,
		}); err != nil {
			return err
		}
	}

	opts := devcontainer.ExecOpts{WorkspaceFolder: c.Folder, ConfigPath: c.ConfigPath}

	// One-shot command: no tmux, stream in the current terminal.
	if len(runCmd) > 0 {
		argv := devcontainer.ExecArgs(opts, runCmd...)
		if dryRun {
			fmt.Fprintln(cmd.OutOrStdout(), strings.Join(argv, " "))
			return nil
		}
		ex := execCommand(cmd.Context(), argv[0], argv[1:]...)
		ex.Stdin, ex.Stdout, ex.Stderr = os.Stdin, os.Stdout, os.Stderr
		return ex.Run()
	}

	chain := dcClaudeChain
	if shell {
		chain = dcShellFallback
	}
	launch := devcontainer.ExecArgs(opts, "sh", "-lc", chain)
	if dryRun {
		fmt.Fprintln(cmd.OutOrStdout(), strings.Join(launch, " "))
		return nil
	}

	ctx := cmd.Context()
	tm := tmux.New(r)
	has, err := tm.HasSession(ctx, dcTmuxSession)
	if err != nil {
		return err
	}
	if !has {
		if err := tm.NewSession(ctx, dcTmuxSession, c.Folder); err != nil {
			return err
		}
	}
	if !c.HasWindow {
		// Window names are the dc name; a duplicate name (two checkouts of the
		// same config layout) reuses the first window.
		if _, err := tm.NewWindow(ctx, dcTmuxSession, c.Name, c.Folder, launch); err != nil {
			return err
		}
	}
	target := dcTmuxSession + ":" + c.Name
	_ = tm.SelectWindow(ctx, target)

	if tmux.InTmux() {
		return tm.SwitchClient(ctx, target)
	}
	ex := execCommand(ctx, "tmux", tmux.AttachArgs(dcTmuxSession)...)
	ex.Stdin, ex.Stdout, ex.Stderr = os.Stdin, os.Stdout, os.Stderr
	return ex.Run()
}

// dcWindowNames returns the names of existing weft/dc tmux windows; tmux being
// down or the session missing simply yields none.
func dcWindowNames(cmd *cobra.Command, r sysexec.Runner) map[string]bool {
	ws, err := tmux.New(r).ListWindows(cmd.Context(), dcTmuxSession)
	if err != nil {
		return nil
	}
	names := make(map[string]bool, len(ws))
	for _, w := range ws {
		names[w.Name] = true
	}
	return names
}

// splitAtDash separates "[query] -- cmd..." using cobra's dash position.
func splitAtDash(args []string, dash int) (query, runCmd []string) {
	if dash == -1 {
		return args, nil
	}
	return args[:dash], args[dash:]
}

// dcCandidates converts labelled containers into display candidates, keeping
// one entry per (folder, config) pair and preferring a running container over
// a stopped leftover for the same devcontainer.
func dcCandidates(cs []dockerx.Container, windows map[string]bool) []dcCandidate {
	byKey := map[string]dcCandidate{}
	var keys []string
	for _, c := range cs {
		folder := c.Labels["devcontainer.local_folder"]
		name := dcName(folder, c.Labels["devcontainer.config_file"])
		cand := dcCandidate{
			Name:          name,
			Folder:        folder,
			ConfigPath:    c.Labels["devcontainer.config_file"],
			ContainerName: c.Names,
			State:         c.State,
			HasWindow:     windows[name],
		}
		key := cand.Folder + "\x00" + cand.ConfigPath
		prev, seen := byKey[key]
		if !seen {
			byKey[key] = cand
			keys = append(keys, key)
			continue
		}
		if prev.State != "running" && cand.State == "running" {
			byKey[key] = cand
		}
	}
	out := make([]dcCandidate, 0, len(keys))
	for _, k := range keys {
		out = append(out, byKey[k])
	}
	sort.SliceStable(out, func(i, j int) bool {
		if (out[i].State == "running") != (out[j].State == "running") {
			return out[i].State == "running"
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// dcName derives a human name: the config's directory name (e.g.
// .devcontainer/oasys-ui/devcontainer.json -> "oasys-ui"), falling back to the
// workspace folder's name for root-style configs.
func dcName(folder, config string) string {
	if dir := filepath.Base(filepath.Dir(config)); dir != ".devcontainer" && dir != "." {
		return dir
	}
	return filepath.Base(folder)
}

// matchDc filters candidates by a case-insensitive substring over name,
// workspace path, and container name. An empty query matches everything.
func matchDc(cands []dcCandidate, q string) []dcCandidate {
	q = strings.ToLower(q)
	var out []dcCandidate
	for _, c := range cands {
		if strings.Contains(strings.ToLower(c.Name), q) ||
			strings.Contains(strings.ToLower(c.Folder), q) ||
			strings.Contains(strings.ToLower(c.ContainerName), q) {
			out = append(out, c)
		}
	}
	return out
}

func dcItems(cands []dcCandidate) []tui.DcItem {
	items := make([]tui.DcItem, len(cands))
	for i, c := range cands {
		items[i] = tui.DcItem{
			Name:      c.Name,
			Container: c.ContainerName,
			Workspace: c.Folder,
			State:     c.State,
			HasWindow: c.HasWindow,
		}
	}
	return items
}

func printDcTable(w io.Writer, cands []dcCandidate, color bool) {
	fmt.Fprintf(w, "  %-8s %-18s %-28s %s\n", "STATE", "NAME", "CONTAINER", "WORKSPACE")
	for _, c := range cands {
		glyph, code := "○", ansiDim
		if c.State == "running" {
			glyph, code = "●", ansiGreen
		}
		name := c.Name
		if c.HasWindow {
			name += "*"
		}
		fmt.Fprintf(w, "%s %-8s %-18s %-28s %s\n",
			colorize(glyph, code, color),
			c.State,
			truncate(name, 18),
			truncate(c.ContainerName, 28),
			c.Folder,
		)
	}
}
