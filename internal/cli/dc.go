package cli

import (
	"context"
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

// Seams so tests can drive selection and pane commands without a terminal.
var (
	runDcPicker    = tui.PickDc
	executablePath = os.Executable
	userHomeDir    = os.UserHomeDir
)

// dcTmuxSession is the machine-global tmux session that orchestrates attached
// devcontainers, and dcGridWindow is its single window: every attached
// devcontainer is a pane in it, auto-tiled — one terminal shows them all.
const (
	dcTmuxSession  = "weft/dc"
	dcGridWindow   = "grid"
	dcSidebarWidth = 30
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
	// A long-lived token minted by `weft dc token` lives in the shared mount
	// and spares the per-container OAuth login.
	`[ -f "$HOME/.claude/weft-oauth-token" ] && ` +
	`export CLAUDE_CODE_OAUTH_TOKEN="$(cat "$HOME/.claude/weft-oauth-token")"; ` +
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
	HasWindow     bool   // an attached pane exists somewhere in weft/dc
	Shown         bool   // that pane is the one displayed in the main window
	PaneDead      bool   // the pane exists but its process has died
}

func newDcCmd() *cobra.Command {
	var start, shell, noSidebar bool
	cmd := &cobra.Command{
		Use:   "dc [query] [-- cmd...]",
		Short: "Scan every devcontainer on this machine and attach to one",
		Long: `Scan the machine for devcontainers — including ones VS Code started — via the
standard devcontainer.local_folder docker label, then orchestrate them from a
single terminal.

On a terminal, "weft dc" opens a picker: enter attaches a pane in the "grid"
window of tmux session weft/dc, running claude inside that container and
resuming its last conversation (a stopped devcontainer is brought up first; a
missing claude is installed). Every picked devcontainer becomes another pane,
auto-tiled from the second one on, with a sidebar listing containers and
usage on the left. Rerunning weft dc focuses the pane you pick. --shell opens
a shell pane instead of claude; append "-- cmd..." for a one-shot command
without tmux. Works outside git repositories; piped output prints a table.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDc(cmd, args, start, shell, noSidebar)
		},
	}
	cmd.Flags().BoolVar(&start, "start", false, "bring a stopped devcontainer up before attaching")
	cmd.Flags().BoolVar(&shell, "shell", false, "open a shell pane instead of claude")
	cmd.Flags().BoolVar(&noSidebar, "no-sidebar", false, "do not add the sidebar pane to the grid")
	cmd.AddCommand(newDcSidebarCmd(), newDcTokenCmd())
	return cmd
}

// dcRunner builds the runner for dc commands (which never open a weft project).
func dcRunner(cmd *cobra.Command) (sysexec.Runner, int, bool) {
	verbosity, _ := cmd.Flags().GetCount("verbose")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	log := logx.New(os.Stderr, verbosity, false)
	return newRunner(dryRun, log), verbosity, dryRun
}

func runDc(cmd *cobra.Command, args []string, start, shell, noSidebar bool) error {
	query, runCmd := splitAtDash(args, cmd.ArgsLenAtDash())
	if len(query) > 1 {
		return fmt.Errorf("at most one query expected, got %d", len(query))
	}
	q := strings.Join(query, "")

	r, verbosity, dryRun := dcRunner(cmd)
	interactive := len(runCmd) == 0 && isTTY(cmd.OutOrStdout())

	for {
		cands, err := dcScan(cmd.Context(), r)
		if err != nil {
			return err
		}
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
		return dcAttach(cmd, r, chosen, autoStart, runCmd, verbosity, dryRun, shell, noSidebar)
	}
}

// dcUp brings a non-running devcontainer up, streaming build output at -v.
func dcUp(cmd *cobra.Command, r sysexec.Runner, c dcCandidate, verbosity int) error {
	fmt.Fprintf(cmd.ErrOrStderr(), "%s devcontainer up (%s)\n", colorize("▶", ansiCyan, colorEnabled(cmd)), c.Name)
	sink := func(l sysexec.Line) {
		if verbosity > 0 {
			fmt.Fprintln(cmd.ErrOrStderr(), l.Text)
		}
	}
	_, err := devcontainer.New(r).Up(cmd.Context(), sink, devcontainer.UpOpts{
		WorkspaceFolder: c.Folder,
		ConfigPath:      c.ConfigPath,
	})
	return err
}

// dcLaunchArgs builds the pane's foreground command for a candidate.
func dcLaunchArgs(c dcCandidate, shell bool) []string {
	chain := dcClaudeChain
	if shell {
		chain = dcShellFallback
	}
	return devcontainer.ExecArgs(devcontainer.ExecOpts{
		WorkspaceFolder: c.Folder,
		ConfigPath:      c.ConfigPath,
	}, "sh", "-lc", chain)
}

// dcAttach brings the devcontainer up if needed, then either runs a one-shot
// command inside it or ensures its grid pane (plus the sidebar) and hands the
// terminal to the weft/dc session.
func dcAttach(cmd *cobra.Command, r sysexec.Runner, c dcCandidate, autoStart bool, runCmd []string, verbosity int, dryRun, shell, noSidebar bool) error {
	if c.State != "running" {
		if !autoStart {
			return fmt.Errorf("%s is %s — rerun with --start to bring it up", c.Name, c.State)
		}
		if err := dcUp(cmd, r, c, verbosity); err != nil {
			return err
		}
	}

	// One-shot command: no tmux, stream in the current terminal.
	if len(runCmd) > 0 {
		argv := devcontainer.ExecArgs(devcontainer.ExecOpts{
			WorkspaceFolder: c.Folder,
			ConfigPath:      c.ConfigPath,
		}, runCmd...)
		if dryRun {
			fmt.Fprintln(cmd.OutOrStdout(), strings.Join(argv, " "))
			return nil
		}
		ex := execCommand(cmd.Context(), argv[0], argv[1:]...)
		ex.Stdin, ex.Stdout, ex.Stderr = os.Stdin, os.Stdout, os.Stderr
		return ex.Run()
	}

	launch := dcLaunchArgs(c, shell)
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
	// Let claude's "c to copy" reach the terminal clipboard: tmux's default
	// set-clipboard=external drops OSC 52 sequences coming from applications.
	_ = tm.SetServerOption(ctx, "set-clipboard", "on")
	// Show a reversed ^B badge in the status line while the prefix is armed —
	// feedback for users who don't live in tmux.
	_ = tm.SetSessionOption(ctx, dcTmuxSession, "status-left", "#{?client_prefix,#[reverse] ^B #[default] ,}[weft] ")
	_ = tm.SetSessionOption(ctx, dcTmuxSession, "status-left-length", "20")

	grid := dcTmuxSession + ":" + dcGridWindow
	paneID, err := dcShow(ctx, tm, c, launch, !noSidebar)
	if err != nil {
		return err
	}
	// Make the focused pane obvious.
	_ = tm.SetWindowOption(ctx, grid, "pane-active-border-style", "fg=cyan,bold")
	_ = tm.SetWindowOption(ctx, grid, "pane-border-style", "fg=colour240")

	_ = tm.SelectWindow(ctx, grid)
	if paneID != "" {
		_ = tm.SelectPane(ctx, paneID)
	}

	if tmux.InTmux() {
		return tm.SwitchClient(ctx, grid)
	}
	ex := execCommand(ctx, "tmux", tmux.AttachArgs(dcTmuxSession)...)
	ex.Stdin, ex.Stdout, ex.Stderr = os.Stdin, os.Stdout, os.Stderr
	return ex.Run()
}

// dcShow makes the candidate's pane the one displayed in the main window's
// right slot (Orca-style master-detail): the selected claude fills the right
// side while the others keep running in parked background windows, and picking
// another one swap-panes it into view. Legacy layouts need no special-casing —
// pre-grid windows and grid leftovers are just panes found session-wide.
func dcShow(ctx context.Context, tm tmux.Tmux, c dcCandidate, launch []string, withSidebar bool) (string, error) {
	main := dcTmuxSession + ":" + dcGridWindow
	all, err := tm.ListAllPanes(ctx, dcTmuxSession)
	if err != nil {
		return "", err
	}
	target := dcFindPane(all, c)
	mainPanes, err := tm.ListPanes(ctx, main)
	if err != nil {
		return "", err
	}

	if len(mainPanes) == 0 {
		// No main window yet.
		if target != "" {
			// Promote the pane's window (a parked or legacy one) to main. Panes
			// sharing that window are carved out first so it holds exactly one.
			if len(windowMates(all, target)) > 1 {
				if err := tm.BreakPane(ctx, target); err != nil {
					return "", err
				}
			}
			if err := tm.RenameWindow(ctx, target, dcGridWindow); err != nil {
				return "", err
			}
		} else if _, err := tm.NewWindow(ctx, dcTmuxSession, dcGridWindow, c.Folder, launch); err != nil {
			return "", err
		}
	} else {
		shown := dcShownPane(mainPanes)
		switch {
		case target == "" && shown == "":
			if _, err := tm.SplitWindowRight(ctx, main, c.Folder, launch); err != nil {
				return "", err
			}
		case target == "":
			// Create parked, then swap into view — the old one parks itself.
			id, err := tm.NewBackgroundWindow(ctx, dcTmuxSession, "dc-"+c.Name, c.Folder, launch)
			if err != nil {
				return "", err
			}
			if err := tm.SwapPane(ctx, id, shown); err != nil {
				return "", err
			}
		case target == shown, dcPaneIn(mainPanes, target):
			// Already displayed (or a grid leftover in the main window — the
			// parking sweep below keeps it and parks the rest).
		case shown == "":
			if err := tm.JoinPaneRight(ctx, target, main); err != nil {
				return "", err
			}
		default:
			if err := tm.SwapPane(ctx, target, shown); err != nil {
				return "", err
			}
		}
	}

	// Park any extra claude panes still in the main window (grid leftovers).
	if mainPanes, err = tm.ListPanes(ctx, main); err != nil {
		return "", err
	}
	displayed := dcFindPane(mainPanes, c)
	for _, p := range mainPanes {
		if p.ID != displayed && strings.Contains(p.StartCommand, "--workspace-folder ") {
			_ = tm.BreakPane(ctx, p.ID)
		}
	}

	if withSidebar {
		sb := dcSidebarPane(mainPanes)
		if sb == "" {
			if exe, err := executablePath(); err == nil {
				// Best effort — a missing sidebar must not block the attach.
				sb, _ = tm.SplitWindowLeft(ctx, main, dcSidebarWidth, []string{exe, "dc", "sidebar"})
			}
		}
		if sb != "" {
			// Pane churn (a claude exiting, swaps) makes tmux redistribute
			// widths; pin the sidebar back to its column every time.
			_ = tm.ResizePane(ctx, sb, dcSidebarWidth)
		}
	}
	return displayed, nil
}

// windowMates returns the panes sharing a window with the given pane.
func windowMates(all []tmux.Pane, id string) []tmux.Pane {
	var win string
	for _, p := range all {
		if p.ID == id {
			win = p.WindowID
		}
	}
	var out []tmux.Pane
	for _, p := range all {
		if p.WindowID == win {
			out = append(out, p)
		}
	}
	return out
}

// dcShownPane returns the first devcontainer pane in the main window — the
// "displayed" slot of the master-detail layout.
func dcShownPane(mainPanes []tmux.Pane) string {
	for _, p := range mainPanes {
		if strings.Contains(p.StartCommand, "--workspace-folder ") {
			return p.ID
		}
	}
	return ""
}

func dcPaneIn(panes []tmux.Pane, id string) bool {
	for _, p := range panes {
		if p.ID == id {
			return true
		}
	}
	return false
}

// dcSidebarPane returns the sidebar pane's id, if present.
func dcSidebarPane(panes []tmux.Pane) string {
	for _, p := range panes {
		if strings.Contains(p.StartCommand, "dc sidebar") {
			return p.ID
		}
	}
	return ""
}

// dcPaneFor returns the pane belonging to the candidate, keyed by the pane's
// start command (stable — programs may rewrite pane titles). Panes from any
// weft version match: the start command always carries the same
// --workspace-folder/--config pair.
func dcPaneFor(panes []tmux.Pane, c dcCandidate) *tmux.Pane {
	for i, p := range panes {
		if strings.Contains(p.StartCommand, "--workspace-folder "+c.Folder) &&
			strings.Contains(p.StartCommand, "--config "+c.ConfigPath) {
			return &panes[i]
		}
	}
	return nil
}

// dcFindPane returns the candidate's pane id, or "".
func dcFindPane(panes []tmux.Pane, c dcCandidate) string {
	if p := dcPaneFor(panes, c); p != nil {
		return p.ID
	}
	return ""
}

// dcScan discovers every devcontainer on the machine and marks attachment
// state: pane anywhere in the session, displayed in the main window, or dead.
func dcScan(ctx context.Context, r sysexec.Runner) ([]dcCandidate, error) {
	cs, err := dockerx.New(r).Ps(ctx, "devcontainer.local_folder")
	if err != nil {
		return nil, err
	}
	tm := tmux.New(r)
	all, _ := tm.ListAllPanes(ctx, dcTmuxSession)
	mainPanes, _ := tm.ListPanes(ctx, dcTmuxSession+":"+dcGridWindow)
	return dcCandidates(cs, all, mainPanes), nil
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
func dcCandidates(cs []dockerx.Container, all, mainPanes []tmux.Pane) []dcCandidate {
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
		if p := dcPaneFor(all, cand); p != nil {
			cand.HasWindow = true
			cand.PaneDead = p.Dead
			cand.Shown = dcFindPane(mainPanes, cand) != ""
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
