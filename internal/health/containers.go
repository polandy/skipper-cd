package health

import (
	"bytes"
	"context"
	"strings"
)

// Compose labels each service container with its project's working_dir (the
// --project-directory the deploy path passes) and its service name.
const (
	workingDirLabel = "com.docker.compose.project.working_dir"
	serviceLabel    = "com.docker.compose.service"
)

// listArgs lists the containers `compose ps --all` would show for every
// project at once: service containers, stopped ones included, one-off
// `compose run` containers excluded. Image comes from this listing because it
// is the field compose ps reports (the image ID once the tag has moved); the
// labels and name place a container that inspect then fails to print.
var listArgs = []string{
	"ps", "--all", "--no-trunc",
	"--filter", "label=" + workingDirLabel,
	"--filter", "label=com.docker.compose.oneoff=False",
	"--format", `{{.ID}}\t{{.Image}}\t{{.Label "` + workingDirLabel + `"}}\t{{.Label "` + serviceLabel + `"}}\t{{.Names}}`,
}

// Indexes into inspectColumns, as parseInspect reads them.
const (
	colID = iota
	colWorkingDir
	colService
	colName
	colState
	colHealth
	colExitCode
)

// inspectColumns are the tab-separated fields inspectArgs emits per container.
// Every field must exist on docker's typed inspect struct (ID, not Id), or the
// CLI falls back to the raw JSON map, where `.State.Health` errors for a
// container without a healthcheck and that container prints nothing.
var inspectColumns = []string{
	colID:         "{{.ID}}",
	colWorkingDir: `{{index .Config.Labels "` + workingDirLabel + `"}}`,
	colService:    `{{index .Config.Labels "` + serviceLabel + `"}}`,
	colName:       "{{.Name}}",
	colState:      "{{.State.Status}}",
	colHealth:     "{{if .State.Health}}{{.State.Health.Status}}{{end}}",
	colExitCode:   "{{.State.ExitCode}}",
}

// inspectArgs precede the container IDs in the batched inspect.
var inspectArgs = []string{"inspect", "--format", strings.Join(inspectColumns, "\t")}

// listContainers reads every compose container's state in two docker calls,
// grouped by project working_dir (see ADR-0069). A container removed between
// the two calls fails inspect for its ID alone while the rest still print, so
// only an inspect that printed nothing counts as failed; parseInspect reports
// the missing one as unread.
func (p *Poller) listContainers(ctx context.Context) (map[string][]psLine, error) {
	out, err := p.out.Output(ctx, "", "docker", listArgs...)
	if err != nil {
		return nil, err
	}
	ids, listing := parseList(out)
	if len(ids) == 0 {
		return map[string][]psLine{}, nil
	}
	out, err = p.out.Output(ctx, "", "docker", append(inspectArgs, ids...)...)
	if err != nil && len(bytes.TrimSpace(out)) == 0 {
		return nil, err
	}
	return parseInspect(out, listing)
}
