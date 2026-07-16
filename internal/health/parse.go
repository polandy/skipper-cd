package health

import (
	"bufio"
	"bytes"
	"encoding/json"
)

// psLine mirrors the fields skipper needs from one object of
// `docker compose ps --format json` output.
type psLine struct {
	Service  string `json:"Service"`
	State    string `json:"State"`
	Health   string `json:"Health"`
	ExitCode int    `json:"ExitCode"`
}

// parsePS parses `docker compose ps --format json` output into per-service
// lines. Compose emits either a JSON array or newline-delimited objects
// depending on its version, so both are accepted. Empty output (no containers)
// yields no lines and no error.
func parsePS(out []byte) ([]psLine, error) {
	trimmed := bytes.TrimSpace(out)
	if len(trimmed) == 0 {
		return nil, nil
	}
	if trimmed[0] == '[' {
		var lines []psLine
		if err := json.Unmarshal(trimmed, &lines); err != nil {
			return nil, err
		}
		return lines, nil
	}

	var lines []psLine
	sc := bufio.NewScanner(bytes.NewReader(trimmed))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		b := bytes.TrimSpace(sc.Bytes())
		if len(b) == 0 {
			continue
		}
		var l psLine
		if err := json.Unmarshal(b, &l); err != nil {
			return nil, err
		}
		lines = append(lines, l)
	}
	return lines, sc.Err()
}

// serviceStatus classifies one container. The compose Health field wins when
// present (the service has a healthcheck); otherwise the container State is
// used. A container that exited cleanly (code 0) counts as stopped, not
// unhealthy — it is treated as a completed one-shot rather than a crash.
func serviceStatus(l psLine) Status {
	switch l.Health {
	case "healthy":
		return Healthy
	case "unhealthy":
		return Unhealthy
	case "starting":
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
		if l.ExitCode != 0 {
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
		svcs = append(svcs, ServiceHealth{Name: l.Service, State: l.State, Health: l.Health, Status: serviceStatus(l)})
	}
	return svcs
}
