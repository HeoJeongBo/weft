package domain

// Project identifies a repository weft operates on.
type Project struct {
	Name          string // display name
	Slug          string // sanitized; used in labels and paths
	Root          string // repository top-level directory
	DefaultBranch string // base ref new worktrees branch from
	ConfigPath    string // devcontainer config path, relative to a worktree
	TmuxSession   string // resolved tmux session name for this project
}

// WindowTarget returns the tmux target ("session:window") for a session name.
func (p Project) WindowTarget(sessionName string) string {
	return p.TmuxSession + ":" + sessionName
}
