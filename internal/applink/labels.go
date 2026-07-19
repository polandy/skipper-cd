package applink

import (
	"context"
	"encoding/json"
	"regexp"
	"sort"
	"strings"
)

// containerRef is one running compose container, identified by the project
// working_dir label — the same identity orphan detection matches on, rather
// than the container name or the compose project name.
type containerRef struct {
	ID         string
	WorkingDir string
}

// psColumns are the tab-separated fields psArgs emits per running container.
var psColumns = []string{
	`{{.ID}}`,
	`{{.Label "com.docker.compose.project.working_dir"}}`,
}

// psArgs lists every running compose-managed container, one line each. Only
// running containers are listed (no -a): a stopped stack has nothing Traefik
// is actually routing to, so it simply yields no hosts — never an error.
var psArgs = []string{
	"ps",
	"--filter", "label=com.docker.compose.project",
	"--format", strings.Join(psColumns, "\t"),
}

// enableLabel is Traefik's own opt-in convention: homelab setups typically run
// with exposedByDefault=false, so a missing label means "not exposed", not
// "unset".
const enableLabel = "traefik.enable"

// routerRuleKey matches a router's rule label key, e.g.
// "traefik.http.routers.media.rule".
var routerRuleKey = regexp.MustCompile(`^traefik\.http\.routers\.[^.]+\.rule$`)

// hostCall matches one Host(...) call within a rule; hostLiteral then pulls
// every backtick-quoted literal out of its argument list. Traefik v2 combines
// multiple hosts via "Host(`a`) || Host(`b`)"; v3 also accepts them
// variadically in one call ("Host(`a`,`b`)") — both shapes are handled the
// same way. HostRegexp(...) rules are deliberately not matched: a regexp is
// not a single clickable hostname.
var (
	hostCall    = regexp.MustCompile(`Host\(([^)]*)\)`)
	hostLiteral = regexp.MustCompile("`([^`]*)`")
)

// listContainers runs psArgs and parses the result.
func (d *Detector) listContainers(ctx context.Context) ([]containerRef, error) {
	out, err := d.out.Output(ctx, "", "docker", psArgs...)
	if err != nil {
		return nil, err
	}
	return parsePS(out), nil
}

// parsePS folds `docker ps` output (one line per container: ID, working_dir)
// into container refs, skipping lines with no ID or no working_dir label.
func parsePS(out []byte) []containerRef {
	var refs []containerRef
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		id, dir, _ := strings.Cut(line, "\t")
		if id == "" || dir == "" {
			continue
		}
		refs = append(refs, containerRef{ID: id, WorkingDir: dir})
	}
	return refs
}

// inspectLabels batch-inspects every ref's container labels in one call.
func (d *Detector) inspectLabels(ctx context.Context, refs []containerRef) ([]map[string]string, error) {
	args := make([]string, 0, len(refs)+2)
	args = append(args, "inspect", "--format", `{{json .Config.Labels}}`)
	for _, r := range refs {
		args = append(args, r.ID)
	}
	out, err := d.out.Output(ctx, "", "docker", args...)
	if err != nil {
		return nil, err
	}
	return parseLabelLines(out), nil
}

// parseLabelLines parses one JSON label map per line, in docker inspect's
// argument order. A container with no labels prints "null" and yields a nil
// map, not an error; an unparsable line likewise yields a nil map rather than
// failing the whole batch.
func parseLabelLines(out []byte) []map[string]string {
	lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	maps := make([]map[string]string, len(lines))
	for i, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var m map[string]string
		if json.Unmarshal([]byte(line), &m) == nil {
			maps[i] = m
		}
	}
	return maps
}

// hostsByWorkingDir aggregates every Host() literal from each container's
// Traefik router rules, grouped by the container's project working_dir. A
// container is only scanned when it opts in via traefik.enable=true. Results
// are deduped and sorted per working_dir. Fewer label maps than refs (a
// container vanished between ps and inspect) is tolerated: the remainder are
// simply skipped.
func hostsByWorkingDir(refs []containerRef, labelMaps []map[string]string) map[string][]string {
	sets := map[string]map[string]struct{}{}
	for i, ref := range refs {
		if i >= len(labelMaps) {
			break
		}
		labels := labelMaps[i]
		if labels[enableLabel] != "true" {
			continue
		}
		for key, val := range labels {
			if !routerRuleKey.MatchString(key) {
				continue
			}
			for _, host := range extractHosts(val) {
				set, ok := sets[ref.WorkingDir]
				if !ok {
					set = map[string]struct{}{}
					sets[ref.WorkingDir] = set
				}
				set[host] = struct{}{}
			}
		}
	}

	result := make(map[string][]string, len(sets))
	for dir, set := range sets {
		hosts := make([]string, 0, len(set))
		for h := range set {
			hosts = append(hosts, h)
		}
		sort.Strings(hosts)
		result[dir] = hosts
	}
	return result
}

// extractHosts pulls every backtick-quoted hostname out of a Traefik rule's
// Host(...) call(s), ignoring any other matcher (PathPrefix, Headers, ...) the
// rule combines it with.
func extractHosts(rule string) []string {
	var hosts []string
	for _, call := range hostCall.FindAllStringSubmatch(rule, -1) {
		for _, m := range hostLiteral.FindAllStringSubmatch(call[1], -1) {
			if h := strings.TrimSpace(m[1]); h != "" {
				hosts = append(hosts, h)
			}
		}
	}
	return hosts
}
