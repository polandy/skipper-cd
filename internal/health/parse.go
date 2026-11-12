package health

import (
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// psLine is one container in the shape `docker compose ps` reports it. Name
// is the container name — the identifier on_demand_containers uses. Image is
// the reference the container actually runs, which is what the UI shows as the
// service's live version. onDemand is not docker's: the poller marks it from
// the stack's config so classification can treat an intentionally stopped
// on-demand container as stopped, not unhealthy.
type psLine struct {
	Service  string
	Name     string
	Image    string
	State    string
	Health   string
	ExitCode int

	onDemand bool
}

// parseList reads listArgs output (ID<TAB>image per line) into the container
// IDs in listing order and their images.
func parseList(out []byte) ([]string, map[string]string) {
	var ids []string
	images := map[string]string{}
	for _, line := range strings.Split(string(out), "\n") {
		id, image, _ := strings.Cut(strings.TrimRight(line, "\r"), "\t")
		if id == "" {
			continue
		}
		ids = append(ids, id)
		images[id] = image
	}
	return ids, images
}

// parseInspect reads inspectArgs output (columns per inspectColumns) into
// per-project lines keyed by the cleaned working_dir, each sorted by container
// name as compose ps sorts them. Health is kept only for a running container
// and ExitCode only for an exited or dead one, as compose ps does.
func parseInspect(out []byte, images map[string]string) (map[string][]psLine, error) {
	byDir := map[string][]psLine{}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != len(inspectColumns) {
			return nil, fmt.Errorf("docker inspect: want %d columns, got %d in %q", len(inspectColumns), len(f), line)
		}
		exitCode, err := strconv.Atoi(f[6])
		if err != nil {
			return nil, fmt.Errorf("docker inspect: exit code %q: %w", f[6], err)
		}
		l := psLine{Service: f[2], Name: strings.TrimPrefix(f[3], "/"), Image: images[f[0]], State: f[4]}
		switch l.State {
		case "running":
			l.Health = f[5]
		case "exited", "dead":
			l.ExitCode = exitCode
		}
		dir := filepath.Clean(f[1])
		byDir[dir] = append(byDir[dir], l)
	}
	for _, lines := range byDir {
		sort.SliceStable(lines, func(i, j int) bool { return lines[i].Name < lines[j].Name })
	}
	return byDir, nil
}

// serviceStatus classifies one container. The compose Health field wins when
// present (the service has a healthcheck); otherwise the container State is
// used. A container that exited cleanly (code 0) counts as stopped, not
// unhealthy — it is treated as a completed one-shot rather than a crash. An
// exited on-demand container is stopped whatever its exit code: skipper itself
// stops it after the deploy (often via SIGKILL → 137) and the scheduler starts
// it on request, so idle is its intended state. Any other state of an
// on-demand container (restarting, unhealthy) classifies as usual.
func serviceStatus(l psLine) Status {
	switch l.Health {
	case string(Healthy):
		return Healthy
	case string(Unhealthy):
		return Unhealthy
	case string(Starting):
		return Starting
	}

	switch l.State {
	case "running":
		return Healthy
	case "restarting", "dead":
		return Unhealthy
	case "created", "starting":
		return Starting
	case "exited", "removing":
		if l.ExitCode != 0 && !l.onDemand {
			return Unhealthy
		}
		return Stopped
	default:
		return Stopped
	}
}

// rollup reduces per-service statuses to one stack status. Precedence:
// any unhealthy dominates, then any starting, then any healthy (running); a
// project with no running/starting/unhealthy container is stopped. No lines at
// all (no containers for the project) is stopped.
func rollup(lines []psLine) Status {
	if len(lines) == 0 {
		return Stopped
	}
	var anyStarting, anyHealthy bool
	for _, l := range lines {
		switch serviceStatus(l) {
		case Unhealthy:
			return Unhealthy
		case Starting:
			anyStarting = true
		case Healthy:
			anyHealthy = true
		}
	}
	switch {
	case anyStarting:
		return Starting
	case anyHealthy:
		return Healthy
	default:
		return Stopped
	}
}

// servicesOf maps parsed lines to the public per-service health for the UI panel.
func servicesOf(lines []psLine) []ServiceHealth {
	if len(lines) == 0 {
		return nil
	}
	svcs := make([]ServiceHealth, 0, len(lines))
	for _, l := range lines {
		svcs = append(svcs, ServiceHealth{Name: l.Service, Image: l.Image, State: l.State, Health: l.Health, Status: serviceStatus(l), OnDemand: l.onDemand})
	}
	return svcs
}
