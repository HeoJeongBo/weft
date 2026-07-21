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

// dcTmuxSession is the machine-global tmux session that orchestrates attached
// devcontainers, and dcGridWindow is its single window: every attached
// devcontainer is a pane in it, auto-tiled — one terminal shows them all.
const (
	dcTmuxSession = "weft/dc"
	dcGridWindow  = "grid"
)

// dcShellFallback picks the best interactive shell available in the container.
const dcShellFallback = `exec zsh -l 2>/dev/null || exec bash -l 2>/dev/null || exec sh -l`

// dcClaudeChain resumes the container's last claude conversation. When claude
// is not installed (containers lose it on rebuild) it installs the native
// build into ~/.local/bin first — user-scoped, one-time per container — and
// only drops to a shell if that fails. ~/.local/bin is prepended because the
// installer only teaches interactive shells about it.
const dcClaudeChain = `export PATH="$HOME/.local/bin:$PATH"; ` +
	// Keep claude's global state (account, onboarding) inside ~/.claude: that
	// directory is typically a host mount, so login and setup survive container
	// rebuilds and are shared between containers. Migrate an existing
	// home-level ~/.claude.json in so a past login is not lost.
	`export CLAUDE_CONFIG_DIR="$HOME/.claude"; ` +
	`if [ ! -f "$HOME/.claude/.claude.json" ] && [ -f "$HOME/.claude.json" ]; then ` +
	`cp "$HOME/.claude.json" "$HOME/.claude/.claude.json" 2>/dev/null; fi; ` +
	`if ! command -v claude >/dev/null 2>&1; then ` +
	`echo "weft: claude not found in this container — installing (one-time)…"; ` +
	// Some images create ~/.local as root during build, which blocks the
	// user-scoped installer after every rebuild; reclaim it when sudo allows.
	`mkdir -p "$HOME/.local/share" 2>/dev/null; ` +
	`[ -w "$HOME/.local/share" ] || sudo -n chown -R "$(id -u)" "$HOME/.local" 2>/dev/null; ` +
	`curl -fsSL https://claude.ai/install.sh | bash; fi; ` +
	`if command -v claude >/dev/null 2>&1; then claude --continue || claude; else ` +
	`echo "weft: claude install failed — dropping to a shell."; ` +
	`echo "  try manually: curl -fsSL https://claude.ai/install.sh | bash"; ` +
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
		panes, winNames := dcAttached(cmd, r)
		cands := dcCandidates(cs, panes, winNames)
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

	grid := dcTmuxSession + ":" + dcGridWindow
	panes, err := tm.ListPanes(ctx, grid)
	if err != nil {
		return err
	}
	if dcFindPane(panes, c) == "" {
		windows, err := tm.ListWindows(ctx, dcTmuxSession)
		if err != nil {
			return err
		}
		gridExists, legacy := false, ""
		for _, w := range windows {
			if w.Name == dcGridWindow {
				gridExists = true
			}
			if w.Name == c.Name {
				legacy = dcTmuxSession + ":" + w.Name
			}
		}
		switch {
		case legacy != "" && gridExists:
			// A pre-grid window for this devcontainer: absorb it as a pane so
			// whatever runs in it (a live claude) survives the migration.
			err = tm.JoinPane(ctx, legacy, grid)
		case legacy != "":
			// The legacy window becomes the grid.
			err = tm.RenameWindow(ctx, legacy, dcGridWindow)
		case gridExists:
			_, err = tm.SplitWindow(ctx, grid, c.Folder, launch)
		default:
			_, err = tm.NewWindow(ctx, dcTmuxSession, dcGridWindow, c.Folder, launch)
		}
		if err != nil {
			return err
		}
		if panes, err = tm.ListPanes(ctx, grid); err != nil {
			return err
		}
		// Two or more panes tile automatically — the requested grid.
		_ = tm.SelectLayout(ctx, grid, "tiled")
	}

	_ = tm.SelectWindow(ctx, grid)
	if id := dcFindPane(panes, c); id != "" {
		_ = tm.SelectPane(ctx, id)
	}

	if tmux.InTmux() {
		return tm.SwitchClient(ctx, grid)
	}
	ex := execCommand(ctx, "tmux", tmux.AttachArgs(dcTmuxSession)...)
	ex.Stdin, ex.Stdout, ex.Stderr = os.Stdin, os.Stdout, os.Stderr
	return ex.Run()
}

// dcFindPane returns the id of the grid pane belonging to the candidate, keyed
// by the pane's start command (stable — programs may rewrite pane titles).
// Legacy panes adopted via join/rename match too: their start command carries
// the same --workspace-folder/--config pair.
func dcFindPane(panes []tmux.Pane, c dcCandidate) string {
	for _, p := range panes {
		if strings.Contains(p.StartCommand, "--workspace-folder "+c.Folder) &&
			strings.Contains(p.StartCommand, "--config "+c.ConfigPath) {
			return p.ID
		}
	}
	return ""
}

// dcAttached returns the grid's panes and the names of weft/dc windows (legacy
// pre-grid layout); tmux being down or the session missing simply yields none.
func dcAttached(cmd *cobra.Command, r sysexec.Runner) ([]tmux.Pane, map[string]bool) {
	tm := tmux.New(r)
	panes, _ := tm.ListPanes(cmd.Context(), dcTmuxSession+":"+dcGridWindow)
	ws, err := tm.ListWindows(cmd.Context(), dcTmuxSession)
	if err != nil {
		return panes, nil
	}
	names := make(map[string]bool, len(ws))
	for _, w := range ws {
		names[w.Name] = true
	}
	return panes, names
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
// a stopped leftover for the same devcontainer. A candidate is marked attached
// when it has a grid pane or a legacy (pre-grid) window.
func dcCandidates(cs []dockerx.Container, panes []tmux.Pane, windows map[string]bool) []dcCandidate {
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
		}
		cand.HasWindow = windows[name] || dcFindPane(panes, cand) != ""
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
