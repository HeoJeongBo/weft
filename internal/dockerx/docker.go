// Package dockerx wraps the docker CLI for the reads and teardown weft needs:
// listing containers by label (reconciliation) and removing them (since the
// devcontainer CLI has no "down").
package dockerx

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/HeoJeongBo/weft/internal/sysexec"
)

// Container is a subset of `docker ps --format '{{json .}}'`.
type Container struct {
	ID     string
	Names  string
	Image  string
	State  string // running, exited, created, paused, ...
	Status string
	Labels map[string]string
}

// Docker is the subset of docker operations weft depends on.
type Docker interface {
	Ps(ctx context.Context, labelFilters ...string) ([]Container, error)
	Remove(ctx context.Context, force bool, ids ...string) error
	RemoveByLabel(ctx context.Context, label string, force bool) (int, error)
	Stop(ctx context.Context, ids ...string) error
	Start(ctx context.Context, ids ...string) error
	DaemonUp(ctx context.Context) bool
}

// Exec is the real Docker backed by a sysexec.Runner.
type Exec struct {
	r sysexec.Runner
}

// New returns a Docker backed by r.
func New(r sysexec.Runner) *Exec { return &Exec{r: r} }

// Ps lists containers (including stopped) matching all label filters, e.g.
// "weft.project=app".
func (e *Exec) Ps(ctx context.Context, labelFilters ...string) ([]Container, error) {
	args := []string{"ps", "-a", "--no-trunc", "--format", "{{json .}}"}
	for _, f := range labelFilters {
		args = append(args, "--filter", "label="+f)
	}
	res, err := e.r.Run(ctx, "docker", args...)
	if err != nil {
		return nil, err
	}
	return parsePs(res.Stdout), nil
}

// Remove removes the given containers. force adds -f. No-op with no ids.
func (e *Exec) Remove(ctx context.Context, force bool, ids ...string) error {
	if len(ids) == 0 {
		return nil
	}
	args := []string{"rm"}
	if force {
		args = append(args, "-f")
	}
	args = append(args, ids...)
	_, err := e.r.Mutate(ctx, "docker", args...)
	return err
}

// RemoveByLabel removes every container carrying label (e.g.
// "weft.session=app/x") and returns how many were removed.
func (e *Exec) RemoveByLabel(ctx context.Context, label string, force bool) (int, error) {
	cs, err := e.Ps(ctx, label)
	if err != nil {
		return 0, err
	}
	ids := make([]string, len(cs))
	for i, c := range cs {
		ids[i] = c.ID
	}
	if err := e.Remove(ctx, force, ids...); err != nil {
		return 0, err
	}
	return len(ids), nil
}

// Stop stops the given containers.
func (e *Exec) Stop(ctx context.Context, ids ...string) error {
	if len(ids) == 0 {
		return nil
	}
	_, err := e.r.Mutate(ctx, "docker", append([]string{"stop"}, ids...)...)
	return err
}

// Start starts the given (stopped) containers.
func (e *Exec) Start(ctx context.Context, ids ...string) error {
	if len(ids) == 0 {
		return nil
	}
	_, err := e.r.Mutate(ctx, "docker", append([]string{"start"}, ids...)...)
	return err
}

// DaemonUp reports whether a Docker daemon is reachable.
func (e *Exec) DaemonUp(ctx context.Context) bool {
	_, err := e.r.Run(ctx, "docker", "info", "--format", "{{.ServerVersion}}")
	return err == nil
}

type psJSON struct {
	ID     string `json:"ID"`
	Names  string `json:"Names"`
	Image  string `json:"Image"`
	State  string `json:"State"`
	Status string `json:"Status"`
	Labels string `json:"Labels"`
}

func parsePs(stdout string) []Container {
	var out []Container
	for _, line := range strings.Split(stdout, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var p psJSON
		if err := json.Unmarshal([]byte(line), &p); err != nil {
			continue
		}
		out = append(out, Container{
			ID:     p.ID,
			Names:  p.Names,
			Image:  p.Image,
			State:  p.State,
			Status: p.Status,
			Labels: parseLabels(p.Labels),
		})
	}
	return out
}

// parseLabels turns docker's comma-separated "k=v,k2=v2" label string into a map.
func parseLabels(s string) map[string]string {
	m := map[string]string{}
	for _, pair := range strings.Split(s, ",") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		k, v, _ := strings.Cut(pair, "=")
		m[k] = v
	}
	return m
}
