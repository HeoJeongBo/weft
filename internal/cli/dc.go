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
	"github.com/HeoJeongBo/weft/internal/tui"
)

// runDcPicker is a seam so tests can drive selection without a terminal.
var runDcPicker = tui.PickDc

// dcShellFallback picks the best interactive shell available in the container.
const dcShellFallback = `exec zsh -l 2>/dev/null || exec bash -l 2>/dev/null || exec sh -l`

// dcCandidate is one devcontainer discovered from docker's standard identity
// labels (devcontainer.local_folder / devcontainer.config_file), which the
// devcontainer CLI and VS Code stamp on every container they create.
type dcCandidate struct {
	Name          string // display name derived from the config location
	Folder        string // devcontainer.local_folder (workspace on the host)
	ConfigPath    string // devcontainer.config_file
	ContainerName string
	State         string // running, exited, ...
}

func newDcCmd() *cobra.Command {
	var start bool
	cmd := &cobra.Command{
		Use:   "dc [query] [-- cmd...]",
		Short: "Scan every devcontainer on this machine and attach to one",
		Long: `Scan the machine for devcontainers — including ones VS Code started — via the
standard devcontainer.local_folder docker label, then attach a shell.

On a terminal, "weft dc" opens a picker: move with the arrow keys, press enter
to attach (a stopped devcontainer is brought up first), r to rescan, q to quit.
A query ("weft dc oasys") narrows the scan; a unique match attaches directly.
Append "-- cmd..." to run a command instead of a shell. Works outside git
repositories; piped output falls back to a plain table.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDc(cmd, args, start)
		},
	}
	cmd.Flags().BoolVar(&start, "start", false, "bring a stopped devcontainer up before attaching")
	return cmd
}

func runDc(cmd *cobra.Command, args []string, start bool) error {
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
		cands := dcCandidates(cs)
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
		return dcAttach(cmd, r, chosen, autoStart, runCmd, verbosity, dryRun)
	}
}

// dcAttach brings the devcontainer up if needed and hands the terminal to a
// shell (or the given command) inside it.
func dcAttach(cmd *cobra.Command, r sysexec.Runner, c dcCandidate, autoStart bool, runCmd []string, verbosity int, dryRun bool) error {
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

	shellCmd := runCmd
	if len(shellCmd) == 0 {
		shellCmd = []string{"sh", "-lc", dcShellFallback}
	}
	argv := devcontainer.ExecArgs(devcontainer.ExecOpts{
		WorkspaceFolder: c.Folder,
		ConfigPath:      c.ConfigPath,
	}, shellCmd...)
	if dryRun {
		fmt.Fprintln(cmd.OutOrStdout(), strings.Join(argv, " "))
		return nil
	}
	ex := execCommand(cmd.Context(), argv[0], argv[1:]...)
	ex.Stdin, ex.Stdout, ex.Stderr = os.Stdin, os.Stdout, os.Stderr
	return ex.Run()
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
func dcCandidates(cs []dockerx.Container) []dcCandidate {
	byKey := map[string]dcCandidate{}
	var keys []string
	for _, c := range cs {
		folder := c.Labels["devcontainer.local_folder"]
		cand := dcCandidate{
			Name:          dcName(folder, c.Labels["devcontainer.config_file"]),
			Folder:        folder,
			ConfigPath:    c.Labels["devcontainer.config_file"],
			ContainerName: c.Names,
			State:         c.State,
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
		items[i] = tui.DcItem{Name: c.Name, Container: c.ContainerName, Workspace: c.Folder, State: c.State}
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
		fmt.Fprintf(w, "%s %-8s %-18s %-28s %s\n",
			colorize(glyph, code, color),
			c.State,
			truncate(c.Name, 18),
			truncate(c.ContainerName, 28),
			c.Folder,
		)
	}
}
