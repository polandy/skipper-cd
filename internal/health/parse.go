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
// on-demand container as stopped, not unhealthy. unread marks a listed
// container inspect printed nothing for, so its state is unknown.
type psLine struct {
	Service  string
	Name     string
	Image    string
	State    string
	Health   string
	ExitCode int

	onDemand bool
	unread   bool
}

// listed is one container as listArgs reports it.
type listed struct {
	Image, WorkingDir, Service, Name string
}

// Docker container states (State.Status) the poller tells apart.
const (
	stateRunning = "running"
	stateExited  = "exited"
	stateDead    = "dead"
)

// parseList reads listArgs output (columns ID, image, working_dir, service,
// name) into the container IDs in listing order and what the listing says
// about each.
func parseList(out []byte) ([]string, map[string]listed) {
	var ids []string
	listing := map[string]listed{}
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Split(strings.TrimRight(line, "\r"), "\t")
		if f[0] == "" {
			continue
		}
		f = append(f, make([]string, max(len(listColumns)-len(f), 0))...)
		ids = append(ids, f[0])
		listing[f[0]] = listed{Image: f[1], WorkingDir: f[2], Service: f[3], Name: f[4]}
	}
	return ids, listing
}

// parseInspect reads inspectArgs output (columns per inspectColumns) into
// per-project lines keyed by the cleaned working_dir, each sorted by container
// name as compose ps sorts them. Health is kept only for a running container
// and ExitCode only for an exited or dead one, as compose ps does. A listed
// container with no inspect line is kept as unread rather than dropped, so a
// failed read can never look like a stopped project.
func parseInspect(out []byte, listing map[string]listed) (map[string][]psLine, error) {
	byDir := map[string][]psLine{}
	inspected := map[string]bool{}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != len(inspectColumns) {
			return nil, fmt.Errorf("docker inspect: want %d columns, got %d in %q", len(inspectColumns), len(f), line)
		}
		exitCode, err := strconv.Atoi(f[colExitCode])
		if err != nil {
			return nil, fmt.Errorf("docker inspect: exit code %q: %w", f[colExitCode], err)
		}
		inspected[f[colID]] = true
		l := psLine{
			Service: f[colService],
			Name:    strings.TrimPrefix(f[colName], "/"),
			Image:   listing[f[colID]].Image,
			State:   f[colState],
		}
		switch l.State {
		case stateRunning:
			l.Health = f[colHealth]
		case stateExited, stateDead:
			l.ExitCode = exitCode
		}
		dir := filepath.Clean(f[colWorkingDir])
		byDir[dir] = append(byDir[dir], l)
	}
	for id, c := range listing {
		if !inspected[id] {
			dir := filepath.Clean(c.WorkingDir)
			byDir[dir] = append(byDir[dir], psLine{Service: c.Service, Name: c.Name, Image: c.Image, unread: true})
		}
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
	if l.unread {
		return Unknown
	}
	switch l.Health {
	case string(Healthy):
		return Healthy
	case string(Unhealthy):
		return Unhealthy
	case string(Starting):
		return Starting
	}

	switch l.State {
	case stateRunning:
		return Healthy
	case "restarting", stateDead:
		return Unhealthy
	case "created", "starting":
		return Starting
	case stateExited, "removing":
		if l.ExitCode != 0 && !l.onDemand {
			return Unhealthy
		}
		return Stopped
	default:
		return Stopped
	}
}

// rollup reduces per-service statuses to one stack status. Precedence:
// any unread container makes the whole stack unknown, as a failed read does;
// then any unhealthy dominates, then any starting, then any healthy (running);
// a project with no running/starting/unhealthy container is stopped. No lines
// at all (no containers for the project) is stopped.
func rollup(lines []psLine) Status {
	if len(lines) == 0 {
		return Stopped
	}
	var anyUnhealthy, anyStarting, anyHealthy bool
	for _, l := range lines {
		switch serviceStatus(l) {
		case Unknown:
			return Unknown
		case Unhealthy:
			anyUnhealthy = true
		case Starting:
			anyStarting = true
		case Healthy:
			anyHealthy = true
		}
	}
	switch {
	case anyUnhealthy:
		return Unhealthy
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
