// Running images: the image identity a stack's containers actually run, as
// opposed to the image *reference* its compose file asks for. A reference that
// carries no digest — a floating tag like `:latest` or `:34-ghostscript` —
// stays the same string across a re-pull, so a version delta built from the
// compose file alone reports nothing on the very deploys that move it. This is
// what the terminal deploy events — and the notifications built from them —
// compare instead, falling back to the compose references when the read is
// unavailable.
//
// Reading it takes two compose calls because compose splits the answer: `ps`
// knows which service a container belongs to and which reference it runs, and
// `images` knows the image id behind it. Neither alone is enough (ADR-0053).

package deploy

import (
	"context"
	"log/slog"
	"slices"
	"strings"

	"github.com/polandy/skipper-cd/internal/events"
)

// runningImageIDLen is how many hex characters of a container's image ID are
// kept in the recorded identity. Twelve is docker's own short-ID length, so a
// docker that reports the short form and one that reports the full sha256
// normalize to the same value — a docker upgrade must not read as a rebuild.
const runningImageIDLen = 12

// imageLine is the subset of `docker compose images --format json` this read
// needs. Compose reports **no** service on this output — only the container
// name, which is what ties it back to a containerLine — and ID is the local
// image id, the value that moves when a floating tag is re-pulled.
type imageLine struct {
	ContainerName string `json:"ContainerName"`
	ID            string `json:"ID"`
}

// runningImage renders one service's running identity from what its container
// reports: the image reference itself, plus the short image id when — and only
// when — that reference carries no digest.
//
// A digest-pinned reference (`traefik:v3.7.9@sha256:6529…`) already identifies
// the image exactly, and keeping it verbatim means a tag bump still reports as
// the tag bump it is. A reference without one (`nextcloud:34-ghostscript`) is
// blind to a re-pull, so the id is appended as the part that moves. Returns ""
// when there is no reference to report.
func runningImage(ref, id string) string {
	if ref == "" || strings.Contains(ref, "@") {
		return ref
	}
	short := strings.TrimPrefix(id, "sha256:")
	if len(short) > runningImageIDLen {
		short = short[:runningImageIDLen]
	}
	if short == "" {
		return ref
	}
	return ref + "@" + short
}

// runningImages reads what every service of the stack's compose project
// actually runs, keyed by service name. It is called after a successful deploy,
// so the containers exist and report the version the stack now runs.
//
// Returns nil — never a partial map — when the read is unavailable: no
// Outputter wired, either command failed, or its output did not parse. Callers
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
	psLines, err := composeJSONRead[containerLine](ctx, d, run, "ps", "--format", "json", "--all")
	if err != nil {
		slog.Warn("could not read running services, version delta falls back to compose references",
			"stack", run.stack.Name, "err", err)
		return nil
	}
	imgLines, err := composeJSONRead[imageLine](ctx, d, run, "images", "--format", "json")
	if err != nil {
		slog.Warn("could not read running images, version delta falls back to compose references",
			"stack", run.stack.Name, "err", err)
		return nil
	}

	// Container name → image id, the half `ps` does not report.
	idByContainer := make(map[string]string, len(imgLines))
	for _, l := range imgLines {
		if l.ContainerName != "" {
			idByContainer[l.ContainerName] = l.ID
		}
	}

	images := make(serviceImageByName, len(psLines))
	for _, l := range psLines {
		if l.Service == "" {
			continue
		}
		// A service with several containers (a rollout canary mid-cutover) yields
		// several lines; they run the same image, so last-one-wins is stable.
		if ref := runningImage(l.Image, idByContainer[l.Name]); ref != "" {
			images[l.Service] = ref
		}
	}
	return images
}

// composeJSONRead runs a `docker compose … --format json` read for the stack
// and parses it into T, using the same project identity the deploy path uses.
func composeJSONRead[T any](ctx context.Context, d *Deployer, run stackRun, args ...string) ([]T, error) {
	dir, composeArgs := run.composeInvocation()
	out, err := d.outputter.Output(ctx, dir, "docker", append(composeArgs, args...)...)
	if err != nil {
		return nil, err
	}
	return parseComposeJSON[T](out)
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
//
// A service in the baseline that `ps` no longer lists is reported as removed
// only when it is also gone from the compose file (cf) — a declared service
// without a container (an inactive profile, a scale of zero) is not a removal.
// A nil cf (compose parse unavailable) keeps the raw delta: suppressing
// removals blindly would hide the real ones.
func runningImageDelta(current, previous serviceImageByName, cf *composeFile) ([]events.ServiceImageChange, bool) {
	if len(current) == 0 || len(previous) == 0 {
		return nil, false
	}
	changes := imageChanges(current, previous)
	if cf != nil {
		changes = slices.DeleteFunc(changes, func(c events.ServiceImageChange) bool {
			_, declared := cf.Services[c.Service]
			return c.New == "" && declared
		})
	}
	return changes, true
}
