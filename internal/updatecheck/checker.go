package updatecheck

import (
	"context"
	"encoding/json"
	"log/slog"
	"maps"
	"slices"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/polandy/skipper-cd/internal/registry"
)

// ServiceUpdate is what the check concluded for one service that has an
// update. Exactly one of Latest/Rebuilt is set: a newer same-shape tag, or the
// running tag republished upstream (its digest moved).
type ServiceUpdate struct {
	// Running is the tag the service currently runs, for messages and the UI
	// tooltip.
	Running string `json:"running"`
	// Latest is the newest same-shape tag upstream, when newer than Running.
	Latest string `json:"latest,omitempty"`
	// Rebuilt reports that the running tag itself points at a different digest
	// upstream than the one running locally.
	Rebuilt bool `json:"rebuilt,omitempty"`
}

// Snapshot is one completed check run: per stack, the services with an
// available update (services without one are absent). It rides the `stacks`
// SSE snapshot into the UI and the peer fan-in (ADR-0054).
type Snapshot struct {
	Stacks    map[string]map[string]ServiceUpdate `json:"stacks,omitempty"`
	CheckedAt time.Time                           `json:"checked_at"`
}

// Alert is one update's first appearance, handed to Config.Notify.
type Alert struct {
	Stack   string
	Service string
	Running string
	Latest  string
	Rebuilt bool
}

// Registry answers the two questions the check asks upstream; implemented by
// internal/registry.Client, faked in tests.
type Registry interface {
	// Tags lists the tags of the reference's repository.
	Tags(ctx context.Context, imageRef string) ([]string, error)
	// ManifestDigest resolves the digest the reference's tag points at.
	ManifestDigest(ctx context.Context, imageRef string) (string, error)
}

// Outputter runs a command and returns its captured stdout — the local half of
// the digest comparison (docker image inspect). command.ShellRunner satisfies it.
type Outputter interface {
	Output(ctx context.Context, dir string, name string, args ...string) ([]byte, error)
}

// Config wires a Checker. Registry and Running are required; everything else
// degrades gracefully when absent.
type Config struct {
	// Interval is the check cadence for Run. RunOnce ignores it.
	Interval time.Duration
	// Registry answers tag and digest questions.
	Registry Registry
	// Outputter reads local image digests; nil skips digest comparisons for
	// references that do not carry their digest themselves.
	Outputter Outputter
	// Running returns the stack→service→running-image map to check
	// (Deployer.CurrentRunningImages).
	Running func() map[string]map[string]string
	// Include reports whether a stack takes part (effective set membership +
	// per-stack update_check override); nil includes every stack.
	Include func(stack string) bool
	// Notify receives each update's first appearance; nil disables
	// notifications. Dedup is the Checker's job, not the receiver's.
	Notify func(Alert)
	// OnChange runs after every completed check, so the wiring can republish
	// the stacks snapshot. nil disables it.
	OnChange func()
	// StatePath persists the notification dedup across restarts (skipper
	// self-updates routinely); "" keeps it in memory only.
	StatePath string
	// Now is the clock used to stamp snapshots; nil uses time.Now.
	Now func() time.Time
}

// Checker periodically compares running images against their registries and
// publishes the result. Snapshot is safe for concurrent use; runMu serializes
// the check itself, so the interval ticker (Run) and the post-deploy nudge
// (RunOnceIfChanged) may fire concurrently.
type Checker struct {
	cfg      Config
	runMu    sync.Mutex
	snapshot atomic.Pointer[Snapshot]
	notified map[string]string // "stack/service" → advertised identity already alerted

	// lastRunning is the running-images map the previous check ran over, so
	// RunOnceIfChanged can tell a deploy that moved something from a no-op run.
	lastRunning map[string]map[string]string
}

// New builds a Checker and loads the persisted notification dedup, if any.
func New(cfg Config) *Checker {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &Checker{cfg: cfg, notified: loadNotified(cfg.StatePath)}
}

// Snapshot returns the latest completed check (nil before the first). The
// returned value is shared and must be treated as read-only.
func (c *Checker) Snapshot() *Snapshot { return c.snapshot.Load() }

// Run checks once immediately, then on every Interval tick until ctx is
// cancelled. Headless by design — the notification must fire with no UI open.
func (c *Checker) Run(ctx context.Context) {
	c.RunOnce(ctx)
	if c.cfg.Interval <= 0 {
		return
	}
	t := time.NewTicker(c.cfg.Interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			c.RunOnce(ctx)
		}
	}
}

// RunOnceIfChanged checks only when the running images differ from what the
// previous check ran over, and reports whether it ran. Called after every
// deploy run: a deploy that applied an update clears its marker now — not at
// the next tick, up to a full interval later, claiming an update the host
// already runs — while the frequent no-op reconcile runs ask the registry
// nothing.
func (c *Checker) RunOnceIfChanged(ctx context.Context) bool {
	c.runMu.Lock()
	unchanged := c.lastRunning != nil && runningEqual(c.lastRunning, c.cfg.Running())
	c.runMu.Unlock()
	if unchanged {
		return false
	}
	c.RunOnce(ctx)
	return true
}

// RunOnce performs one full check: every included stack's services, then
// notification dedup maintenance, snapshot publication and OnChange.
func (c *Checker) RunOnce(ctx context.Context) {
	c.runMu.Lock()
	defer c.runMu.Unlock()
	snap := &Snapshot{Stacks: map[string]map[string]ServiceUpdate{}, CheckedAt: c.cfg.Now()}
	failed := map[string]bool{} // keys whose check errored — no claim either way
	changed := false

	running := c.cfg.Running()
	// Stored as a copy: the comparison must not depend on whether Running()
	// hands out fresh maps or a shared one.
	c.lastRunning = make(map[string]map[string]string, len(running))
	for stack, services := range running {
		c.lastRunning[stack] = maps.Clone(services)
	}
	for _, stack := range sortedKeys(running) {
		if c.cfg.Include != nil && !c.cfg.Include(stack) {
			continue
		}
		for _, service := range sortedKeys(running[stack]) {
			ref := running[stack][service]
			key := stack + "/" + service
			upd, advertised, err := c.checkService(ctx, ref)
			if err != nil {
				// Skipped, not claimed — and the dedup record survives, so a
				// registry outage does not re-page on recovery.
				slog.Warn("update check failed", "stack", stack, "service", service, "image", ref, "err", err)
				failed[key] = true
				continue
			}
			if upd == nil {
				continue
			}
			if snap.Stacks[stack] == nil {
				snap.Stacks[stack] = map[string]ServiceUpdate{}
			}
			snap.Stacks[stack][service] = *upd
			if c.notified[key] != advertised {
				c.notified[key] = advertised
				changed = true
				if c.cfg.Notify != nil {
					c.cfg.Notify(Alert{Stack: stack, Service: service, Running: upd.Running, Latest: upd.Latest, Rebuilt: upd.Rebuilt})
				}
			}
		}
	}

	// A dedup record without a standing update is done: the update was applied,
	// the advertised tag vanished, or the service is gone — and a future update,
	// even to the same version string, is news again. An errored check keeps its
	// record: it made no claim, and dropping it would re-page after every
	// registry outage.
	for key := range c.notified {
		if failed[key] {
			continue
		}
		if _, stillListed := c.updateFor(snap, key); !stillListed {
			delete(c.notified, key)
			changed = true
		}
	}

	if changed {
		saveNotified(c.cfg.StatePath, c.notified)
	}
	c.snapshot.Store(snap)
	if c.cfg.OnChange != nil {
		c.cfg.OnChange()
	}
}

// updateFor resolves a "stack/service" dedup key against a snapshot.
func (c *Checker) updateFor(snap *Snapshot, key string) (ServiceUpdate, bool) {
	stack, service, ok := strings.Cut(key, "/")
	if !ok {
		return ServiceUpdate{}, false
	}
	upd, ok := snap.Stacks[stack][service]
	return upd, ok
}

// checkService answers the two questions for one running reference: a newer
// same-shape tag first (which, when found, already answers the check), else
// whether the running tag's upstream digest moved. advertised identifies what
// an update offers, for notification dedup. A nil update with nil error is a
// clean "no update / no claim possible".
func (c *Checker) checkService(ctx context.Context, ref string) (upd *ServiceUpdate, advertised string, err error) {
	parsed, err := registry.ParseReference(ref)
	if err != nil {
		return nil, "", err
	}
	result := ServiceUpdate{Running: parsed.Tag}

	if _, versionShaped := parseTagShape(parsed.Tag); versionShaped {
		tags, err := c.cfg.Registry.Tags(ctx, ref)
		if err != nil {
			return nil, "", err
		}
		if latest := NewerTag(parsed.Tag, tags); latest != "" {
			result.Latest = latest
			return &result, "tag:" + latest, nil
		}
	}

	local, ok, err := c.localDigests(ctx, ref, parsed)
	if err != nil {
		return nil, "", err
	}
	if !ok {
		// No local digest to compare — a locally-built image, or no Outputter
		// wired. No truthful claim can be made.
		return nil, "", nil
	}
	upstream, err := c.cfg.Registry.ManifestDigest(ctx, ref)
	if err != nil {
		return nil, "", err
	}
	if !slices.Contains(local, upstream) {
		result.Rebuilt = true
		return &result, "digest:" + upstream, nil
	}
	return nil, "", nil
}

// localDigests resolves the digests the running image is known by locally: the
// reference's own digest when it carries one, else the image's RepoDigests via
// docker image inspect. ok=false means no digest exists to compare (a
// locally-built image, or no Outputter).
func (c *Checker) localDigests(ctx context.Context, ref string, parsed registry.Reference) ([]string, bool, error) {
	if parsed.Digest != "" {
		return []string{parsed.Digest}, true, nil
	}
	if c.cfg.Outputter == nil {
		return nil, false, nil
	}
	// The inspectable name is the reference without the short-image-id suffix
	// running_images appends to floating tags (ADR-0053).
	name, _, _ := strings.Cut(ref, "@")
	out, err := c.cfg.Outputter.Output(ctx, "", "docker", "image", "inspect", "--format", "{{json .RepoDigests}}", name)
	if err != nil {
		return nil, false, err
	}
	var repoDigests []string
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(out))), &repoDigests); err != nil {
		return nil, false, err
	}
	var digests []string
	for _, rd := range repoDigests {
		if _, digest, ok := strings.Cut(rd, "@"); ok {
			digests = append(digests, digest)
		}
	}
	return digests, len(digests) > 0, nil
}

// runningEqual compares two stack→service→image maps by value.
func runningEqual(a, b map[string]map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for stack, services := range a {
		if !maps.Equal(services, b[stack]) {
			return false
		}
	}
	return true
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
