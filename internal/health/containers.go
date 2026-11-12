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
// is the field compose ps reports (the image ID once the tag has moved).
var listArgs = []string{
	"ps", "--all", "--no-trunc",
	"--filter", "label=" + workingDirLabel,
	"--filter", "label=com.docker.compose.oneoff=False",
	"--format", "{{.ID}}\t{{.Image}}",
}

// inspectColumns are the tab-separated fields inspectArgs emits per container,
// in the order parseInspect reads them.
var inspectColumns = []string{
	"{{.Id}}",
	`{{index .Config.Labels "` + workingDirLabel + `"}}`,
	`{{index .Config.Labels "` + serviceLabel + `"}}`,
	"{{.Name}}",
	"{{.State.Status}}",
	"{{if .State.Health}}{{.State.Health.Status}}{{end}}",
	"{{.State.ExitCode}}",
}

// inspectArgs precede the container IDs in the batched inspect.
var inspectArgs = []string{"inspect", "--format", strings.Join(inspectColumns, "\t")}

// listContainers reads every compose container's state in two docker calls,
// grouped by project working_dir (see ADR-0069). A container removed between
// the two calls fails inspect for its ID alone while the rest still print, so
// only an inspect that printed nothing counts as failed.
func (p *Poller) listContainers(ctx context.Context) (map[string][]psLine, error) {
	out, err := p.out.Output(ctx, "", "docker", listArgs...)
	if err != nil {
		return nil, err
	}
	ids, images := parseList(out)
	if len(ids) == 0 {
		return map[string][]psLine{}, nil
	}
	out, err = p.out.Output(ctx, "", "docker", append(inspectArgs, ids...)...)
	if err != nil && len(bytes.TrimSpace(out)) == 0 {
		return nil, err
	}
	return parseInspect(out, images)
}
