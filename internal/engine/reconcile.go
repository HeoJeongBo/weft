package engine

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/HeoJeongBo/weft/internal/dockerx"
	"github.com/HeoJeongBo/weft/internal/domain"
	"github.com/HeoJeongBo/weft/internal/tmux"
)

// Reconcile joins the three live sources (git worktrees, docker containers, tmux
// windows) into the current set of sessions. It never fails on a single missing
// source (e.g. docker down) — it degrades and logs. Worktrees define ownership;
// windows only enrich known sessions, so an unrelated tmux window can never
// masquerade as a session.
func (e *Engine) Reconcile(ctx context.Context) ([]domain.Session, error) {
	prefix := e.Cfg.Branch.Prefix

	wts, err := e.Git.Worktrees(ctx)
	if err != nil {
		return nil, err
	}
	containers := e.listContainers(ctx)
	windows := e.listWindows(ctx)

	byName := map[string]*domain.Session{}
	get := func(name string) *domain.Session {
		if s := byName[name]; s != nil {
			return s
		}
		s := &domain.Session{Name: name, Project: e.Project.Slug}
		byName[name] = s
		return s
	}

	// 1. Worktrees with our branch prefix are the authoritative session set.
	for _, wt := range wts {
		if wt.Branch == "" || !strings.HasPrefix(wt.Branch, prefix) {
			continue
		}
		name := strings.TrimPrefix(wt.Branch, prefix)
		s := get(name)
		s.Branch = wt.Branch
		s.Worktree = &domain.Worktree{
			Path:     wt.Path,
			Branch:   wt.Branch,
			Head:     wt.Head,
			Locked:   wt.Locked,
			Prunable: wt.Prunable,
		}
	}

	// 2. Containers labelled for this project (catches orphans with no worktree).
	for _, c := range containers {
		key := c.Labels["weft.session"]
		_, name, ok := domain.SplitSessionKey(key)
		if !ok || name == "" {
			continue
		}
		s := get(name)
		s.Container = &domain.Container{ID: c.ID, Image: c.Image, State: c.State}
		if s.Branch == "" {
			s.Branch = c.Labels["weft.branch"]
		}
		if b := c.Labels["weft.base_ref"]; b != "" {
			s.BaseRef = b
		}
		if ts := c.Labels["weft.created_at"]; ts != "" {
			if t, err := time.Parse(time.RFC3339, ts); err == nil {
				s.CreatedAt = t
			}
		}
	}

	// 3. Windows only enrich sessions we already know about.
	winByName := map[string]tmux.Window{}
	for _, w := range windows {
		winByName[w.Name] = w
	}
	claudeCmds := e.Cfg.ClaudeProcessNames()
	for name, s := range byName {
		if w, ok := winByName[name]; ok {
			s.Window = &domain.Window{
				ID:          w.ID,
				Index:       w.Index,
				Name:        w.Name,
				Active:      w.Active,
				PaneCommand: w.PaneCommand,
				PaneDead:    w.PaneDead,
				Activity:    w.Activity,
			}
		}
		s.DeriveStatus(claudeCmds...)
	}

	out := make([]domain.Session, 0, len(byName))
	for _, s := range byName {
		out = append(out, *s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// FindSession reconciles and returns the named session, enriched with worktree
// git detail (dirty, ahead/behind). ok is false when no such session exists.
func (e *Engine) FindSession(ctx context.Context, name string) (domain.Session, bool, error) {
	sessions, err := e.Reconcile(ctx)
	if err != nil {
		return domain.Session{}, false, err
	}
	for _, s := range sessions {
		if s.Name == name {
			e.enrichWorktree(ctx, &s)
			return s, true, nil
		}
	}
	return domain.Session{}, false, nil
}

// enrichWorktree fills the worktree's dirty and ahead/behind fields via git.
func (e *Engine) enrichWorktree(ctx context.Context, s *domain.Session) {
	if s.Worktree == nil {
		return
	}
	if dirty, err := e.Git.IsDirty(ctx, s.Worktree.Path); err == nil {
		s.Worktree.Dirty = dirty
	}
	upstream := e.Project.DefaultBranch
	if ahead, behind, err := e.Git.AheadBehind(ctx, s.Worktree.Path, upstream); err == nil {
		s.Worktree.Ahead, s.Worktree.Behind = ahead, behind
	}
}

func (e *Engine) listContainers(ctx context.Context) []dockerx.Container {
	cs, err := e.Docker.Ps(ctx, "weft.project="+e.Project.Slug)
	if err != nil {
		e.Log.Debug("reconcile: docker unavailable", "err", err)
		return nil
	}
	return cs
}

func (e *Engine) listWindows(ctx context.Context) []tmux.Window {
	ws, err := e.Tmux.ListWindows(ctx, e.Project.TmuxSession)
	if err != nil {
		e.Log.Debug("reconcile: tmux unavailable", "err", err)
		return nil
	}
	return ws
}
