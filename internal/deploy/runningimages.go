// Running images: the image identity a stack's containers actually run, as
// opposed to the image *reference* its compose file asks for. A floating tag
// (`:latest`, `:stable`) never changes that reference, so a version delta built
// from the compose file alone reports nothing on the very deploys that move it.
// This is what the terminal deploy events — and the notifications built from
// them — compare instead, falling back to the compose references when the read
// is unavailable.

package deploy

import (
	"context"
	"log/slog"
	"strings"

	"github.com/polandy/skipper-cd/internal/events"
)

// runningImageIDLen is how many hex characters of a container's image ID are
// kept in the recorded identity. Twelve is docker's own short-ID length, so a
// docker that reports the short form and one that reports the full sha256
// normalize to the same value — a docker upgrade must not read as a rebuild.
const runningImageIDLen = 12

// untaggedImageTag is what compose reports as the tag of an image that has none.
const untaggedImageTag = "<none>"

// imageLine is the subset of `docker compose images --format json` skipper
// needs. ID is the local image ID the service's container was created from —
// the value that moves when a floating tag is re-pulled. `compose ps` reports
// only the reference, which is why it cannot see that move.
type imageLine struct {
	Service    string `json:"Service"`
	Repository string `json:"Repository"`
	Tag        string `json:"Tag"`
	ID         string `json:"ID"`
}

// identity renders the line as one comparable image identity,
// "<repository>:<tag>@<short id>". An untagged image drops the tag, an image
// whose ID compose did not report drops the "@<id>" suffix — which degrades to
// exactly the compose-reference form, so the same comparison still works.
// Returns "" when the line names no image at all.
func (l imageLine) identity() string {
	ref := l.Repository
	if l.Tag != "" && l.Tag != untaggedImageTag {
		ref += ":" + l.Tag
	}
	id := strings.TrimPrefix(l.ID, "sha256:")
	if len(id) > runningImageIDLen {
		id = id[:runningImageIDLen]
	}
	if ref == "" {
		return id
	}
	if id == "" {
		return ref
	}
	return ref + "@" + id
}

// runningImages reads the image identity of every service of the stack's
// compose project via `docker compose images --format json`, keyed by service
// name. It is called after a successful deploy, so the containers exist and
// report the version the stack now runs.
//
// Returns nil — never a partial map — when the read is unavailable: no
// Outputter wired, the command failed, or its output did not parse. Callers
// treat that as "no running-image knowledge" and fall back to the compose
// references, so the read is strictly additive and can never fail a deploy.
//
// Like rollout's `compose ps` read this goes through the Outputter, which
// passes no env: a compose file interpolating an env_file variable resolves it
// to empty here. That only affects this read (the deploy itself runs through
// runDockerCompose with the full env), and a compose file that errors on the
// missing value degrades to the fallback above.
func (d *Deployer) runningImages(ctx context.Context, run stackRun) serviceImageByName {
	if d.outputter == nil {
		return nil
	}
	dir, args := run.composeInvocation()
	args = append(args, "images", "--format", "json")
	out, err := d.outputter.Output(ctx, dir, "docker", args...)
	if err != nil {
		slog.Warn("could not read running images, version delta falls back to compose references",
			"stack", run.stack.Name, "err", err)
		return nil
	}
	lines, err := parseComposeJSON[imageLine](out)
	if err != nil {
		slog.Warn("could not parse docker compose images output, version delta falls back to compose references",
			"stack", run.stack.Name, "err", err)
		return nil
	}
	images := make(serviceImageByName, len(lines))
	for _, l := range lines {
		if l.Service == "" {
			continue
		}
		if id := l.identity(); id != "" {
			images[l.Service] = id
		}
	}
	return images
}

// runningImageDelta returns the per-service version changes to report for a
// deploy, and whether the running images could answer at all.
//
// It answers only when both sides are known: a previous deploy recorded what
// was running, and this one could read what runs now. Without a baseline —
// a stack's first deploy, or the first deploy after upgrading to a skipper that
// records this — every service would otherwise read as newly added, which says
// nothing about what the deploy changed. The caller then keeps the compose-
// reference delta, and this deploy's read becomes the next one's baseline.
func runningImageDelta(current, previous serviceImageByName) ([]events.ServiceImageChange, bool) {
	if len(current) == 0 || len(previous) == 0 {
		return nil, false
	}
	return imageChanges(current, previous), true
}
