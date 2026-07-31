package updatecheck

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

// fakeRegistry answers Tags/ManifestDigest from canned maps keyed by the image
// reference as the checker passes it, and records every call.
type fakeRegistry struct {
	tags        map[string][]string
	digests     map[string]string
	err         error
	tagCalls    []string
	digestCalls []string
}

func (f *fakeRegistry) Tags(_ context.Context, ref string) ([]string, error) {
	f.tagCalls = append(f.tagCalls, ref)
	if f.err != nil {
		return nil, f.err
	}
	t, ok := f.tags[ref]
	if !ok {
		return nil, fmt.Errorf("no tags for %s", ref)
	}
	return t, nil
}

func (f *fakeRegistry) ManifestDigest(_ context.Context, ref string) (string, error) {
	f.digestCalls = append(f.digestCalls, ref)
	if f.err != nil {
		return "", f.err
	}
	d, ok := f.digests[ref]
	if !ok {
		return "", fmt.Errorf("no digest for %s", ref)
	}
	return d, nil
}

// fakeInspector answers `docker image inspect` reads from a canned map keyed
// by the image name argument (the last argv element).
type fakeInspector struct {
	repoDigests map[string]string // image name → raw JSON output
	calls       [][]string
}

func (f *fakeInspector) Output(_ context.Context, dir, name string, args ...string) ([]byte, error) {
	f.calls = append(f.calls, append([]string{name}, args...))
	image := args[len(args)-1]
	out, ok := f.repoDigests[image]
	if !ok {
		return nil, errors.New("no such image: " + image)
	}
	return []byte(out + "\n"), nil
}

// testChecker builds a Checker over fixed running images with everything else
// fake; override fields on the returned Config before New for special cases.
func testConfig(running map[string]map[string]string, reg *fakeRegistry, out *fakeInspector) Config {
	return Config{
		Registry:  reg,
		Outputter: out,
		Running:   func() map[string]map[string]string { return running },
		Include:   func(string) bool { return true },
		Now:       func() time.Time { return time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC) },
	}
}

func TestRunOnce_NewerTagAdvertised(t *testing.T) {
	reg := &fakeRegistry{tags: map[string][]string{
		"gitea/gitea:1.22.3@40c2d6f1d8f0": {"1.21.0", "1.22.3", "1.22.6", "latest"},
	}}
	c := New(testConfig(map[string]map[string]string{
		"gitea": {"server": "gitea/gitea:1.22.3@40c2d6f1d8f0"},
	}, reg, &fakeInspector{}))

	c.RunOnce(context.Background())

	snap := c.Snapshot()
	if snap == nil {
		t.Fatal("no snapshot after a run")
	}
	got := snap.Stacks["gitea"]["server"]
	want := ServiceUpdate{Running: "1.22.3", Latest: "1.22.6"}
	if got != want {
		t.Errorf("update = %+v, want %+v", got, want)
	}
	// A found tag answers the question; the digest is not also fetched.
	if len(reg.digestCalls) != 0 {
		t.Errorf("digest was fetched despite a newer tag: %v", reg.digestCalls)
	}
	if snap.CheckedAt.IsZero() {
		t.Error("CheckedAt not stamped")
	}
}

func TestRunOnce_RebuiltViaDigestForFloatingTag(t *testing.T) {
	reg := &fakeRegistry{
		tags:    map[string][]string{"traefik:v3.1@40c2d6f1d8f0": {"v3.1", "v3.0"}},
		digests: map[string]string{"traefik:v3.1@40c2d6f1d8f0": "sha256:new"},
	}
	out := &fakeInspector{repoDigests: map[string]string{
		"traefik:v3.1": `["traefik@sha256:old"]`,
	}}
	c := New(testConfig(map[string]map[string]string{
		"proxy": {"traefik": "traefik:v3.1@40c2d6f1d8f0"},
	}, reg, out))

	c.RunOnce(context.Background())

	got := c.Snapshot().Stacks["proxy"]["traefik"]
	want := ServiceUpdate{Running: "v3.1", Rebuilt: true}
	if got != want {
		t.Errorf("update = %+v, want %+v", got, want)
	}
	// The local digest comes from docker image inspect on the tag (without the
	// short-id suffix).
	if len(out.calls) != 1 || out.calls[0][len(out.calls[0])-1] != "traefik:v3.1" {
		t.Errorf("inspect calls = %v", out.calls)
	}
}

func TestRunOnce_UpToDateYieldsNoEntry(t *testing.T) {
	reg := &fakeRegistry{
		tags:    map[string][]string{"traefik:v3.1@40c2d6f1d8f0": {"v3.1"}},
		digests: map[string]string{"traefik:v3.1@40c2d6f1d8f0": "sha256:same"},
	}
	out := &fakeInspector{repoDigests: map[string]string{
		"traefik:v3.1": `["traefik@sha256:same"]`,
	}}
	c := New(testConfig(map[string]map[string]string{
		"proxy": {"traefik": "traefik:v3.1@40c2d6f1d8f0"},
	}, reg, out))

	c.RunOnce(context.Background())

	if snap := c.Snapshot(); len(snap.Stacks) != 0 {
		t.Errorf("expected no entries, got %+v", snap.Stacks)
	}
}

func TestRunOnce_DigestPinnedComparesWithoutInspect(t *testing.T) {
	const pinned = "postgres:16@sha256:old"
	reg := &fakeRegistry{
		tags:    map[string][]string{pinned: {"16"}},
		digests: map[string]string{pinned: "sha256:new"},
	}
	out := &fakeInspector{}
	c := New(testConfig(map[string]map[string]string{
		"db": {"postgres": pinned},
	}, reg, out))

	c.RunOnce(context.Background())

	got := c.Snapshot().Stacks["db"]["postgres"]
	if !got.Rebuilt {
		t.Errorf("update = %+v, want rebuilt", got)
	}
	// The reference itself carries the digest; docker is not consulted.
	if len(out.calls) != 0 {
		t.Errorf("unexpected inspect calls: %v", out.calls)
	}
}

func TestRunOnce_NonVersionTagSkipsTagListing(t *testing.T) {
	reg := &fakeRegistry{
		digests: map[string]string{"redis:latest@40c2d6f1d8f0": "sha256:new"},
	}
	out := &fakeInspector{repoDigests: map[string]string{
		"redis:latest": `["redis@sha256:old"]`,
	}}
	c := New(testConfig(map[string]map[string]string{
		"cache": {"redis": "redis:latest@40c2d6f1d8f0"},
	}, reg, out))

	c.RunOnce(context.Background())

	if len(reg.tagCalls) != 0 {
		t.Errorf("tags were listed for a non-version tag: %v", reg.tagCalls)
	}
	if got := c.Snapshot().Stacks["cache"]["redis"]; !got.Rebuilt {
		t.Errorf("update = %+v, want rebuilt", got)
	}
}

func TestRunOnce_LocallyBuiltImageIsSkipped(t *testing.T) {
	// A build: service's image exists only locally: no RepoDigests, and the
	// registry knows nothing about it. No claim is made.
	reg := &fakeRegistry{}
	out := &fakeInspector{repoDigests: map[string]string{"myapp:latest": `[]`}}
	c := New(testConfig(map[string]map[string]string{
		"app": {"web": "myapp:latest@40c2d6f1d8f0"},
	}, reg, out))

	c.RunOnce(context.Background())

	if snap := c.Snapshot(); len(snap.Stacks) != 0 {
		t.Errorf("expected no entries, got %+v", snap.Stacks)
	}
}

func TestRunOnce_OptedOutStackNeverTouchesTheRegistry(t *testing.T) {
	reg := &fakeRegistry{tags: map[string][]string{
		"gitea/gitea:1.22.3": {"1.22.6"},
	}}
	cfg := testConfig(map[string]map[string]string{
		"gitea": {"server": "gitea/gitea:1.22.3"},
	}, reg, &fakeInspector{})
	cfg.Include = func(stack string) bool { return stack != "gitea" }
	c := New(cfg)

	c.RunOnce(context.Background())

	if len(reg.tagCalls)+len(reg.digestCalls) != 0 {
		t.Errorf("registry was asked about an opted-out stack: %v %v", reg.tagCalls, reg.digestCalls)
	}
	if snap := c.Snapshot(); len(snap.Stacks) != 0 {
		t.Errorf("expected no entries, got %+v", snap.Stacks)
	}
}

func newerTagFixture() (map[string]map[string]string, *fakeRegistry) {
	reg := &fakeRegistry{tags: map[string][]string{
		"gitea/gitea:1.22.3": {"1.22.3", "1.22.6"},
	}}
	running := map[string]map[string]string{
		"gitea": {"server": "gitea/gitea:1.22.3"},
	}
	return running, reg
}

func TestNotify_OncePerAdvertisedVersion(t *testing.T) {
	running, reg := newerTagFixture()
	var alerts []Alert
	cfg := testConfig(running, reg, &fakeInspector{})
	cfg.Notify = func(a Alert) { alerts = append(alerts, a) }
	c := New(cfg)

	c.RunOnce(context.Background())
	c.RunOnce(context.Background())
	if len(alerts) != 1 {
		t.Fatalf("alerts = %+v, want exactly one", alerts)
	}
	want := Alert{Stack: "gitea", Service: "server", Running: "1.22.3", Latest: "1.22.6"}
	if alerts[0] != want {
		t.Errorf("alert = %+v, want %+v", alerts[0], want)
	}

	// A newer advertised version alerts again.
	reg.tags["gitea/gitea:1.22.3"] = []string{"1.22.3", "1.22.7"}
	c.RunOnce(context.Background())
	if len(alerts) != 2 || alerts[1].Latest != "1.22.7" {
		t.Errorf("alerts = %+v, want a second one for 1.22.7", alerts)
	}
}

func TestNotify_DedupSurvivesRestart(t *testing.T) {
	running, reg := newerTagFixture()
	statePath := filepath.Join(t.TempDir(), "update-check.yaml")

	var alerts []Alert
	cfg := testConfig(running, reg, &fakeInspector{})
	cfg.Notify = func(a Alert) { alerts = append(alerts, a) }
	cfg.StatePath = statePath
	New(cfg).RunOnce(context.Background())
	if len(alerts) != 1 {
		t.Fatalf("first process: alerts = %+v", alerts)
	}

	// A new Checker over the same state file must not re-send the standing update.
	New(cfg).RunOnce(context.Background())
	if len(alerts) != 1 {
		t.Errorf("restart re-sent a standing update: %+v", alerts)
	}
}

func TestNotify_ClearedUpdateReAlertsWhenItReturns(t *testing.T) {
	running, reg := newerTagFixture()
	out := &fakeInspector{}
	var alerts []Alert
	cfg := testConfig(running, reg, out)
	cfg.Notify = func(a Alert) { alerts = append(alerts, a) }
	c := New(cfg)

	c.RunOnce(context.Background())

	// The operator applies the update: the running tag moves, nothing newer,
	// and the running digest matches the registry — a clean no-update check.
	running["gitea"]["server"] = "gitea/gitea:1.22.6"
	reg.tags["gitea/gitea:1.22.6"] = []string{"1.22.3", "1.22.6"}
	reg.digests = map[string]string{"gitea/gitea:1.22.6": "sha256:x"}
	out.repoDigests = map[string]string{"gitea/gitea:1.22.6": `["gitea/gitea@sha256:x"]`}
	c.RunOnce(context.Background())

	// A later update to the same version string as before alerts again.
	running["gitea"]["server"] = "gitea/gitea:1.22.3"
	c.RunOnce(context.Background())
	if len(alerts) != 2 {
		t.Errorf("alerts = %+v, want re-alert after the update had cleared", alerts)
	}
}

func TestNotify_RegistryErrorKeepsDedup(t *testing.T) {
	running, reg := newerTagFixture()
	var alerts []Alert
	cfg := testConfig(running, reg, &fakeInspector{})
	cfg.Notify = func(a Alert) { alerts = append(alerts, a) }
	c := New(cfg)

	c.RunOnce(context.Background())
	if len(alerts) != 1 {
		t.Fatalf("alerts = %+v", alerts)
	}

	// Registry down: the service is skipped, but the dedup record survives …
	reg.err = errors.New("registry unreachable")
	c.RunOnce(context.Background())

	// … so recovery with the same standing update does not page again.
	reg.err = nil
	c.RunOnce(context.Background())
	if len(alerts) != 1 {
		t.Errorf("a registry outage caused a duplicate alert: %+v", alerts)
	}
}

func TestRunOnce_RegistryErrorLeavesServiceUnclaimed(t *testing.T) {
	running, reg := newerTagFixture()
	reg.err = errors.New("boom")
	c := New(testConfig(running, reg, &fakeInspector{}))

	c.RunOnce(context.Background())

	if snap := c.Snapshot(); len(snap.Stacks) != 0 {
		t.Errorf("an errored check must not claim anything, got %+v", snap.Stacks)
	}
}

func TestSnapshot_NilBeforeFirstRun(t *testing.T) {
	running, reg := newerTagFixture()
	if snap := New(testConfig(running, reg, &fakeInspector{})).Snapshot(); snap != nil {
		t.Errorf("snapshot before any run = %+v, want nil", snap)
	}
}

func TestRun_TicksAndStopsOnCancel(t *testing.T) {
	running, reg := newerTagFixture()
	cfg := testConfig(running, reg, &fakeInspector{})
	cfg.Interval = time.Millisecond
	ran := make(chan struct{}, 16)
	cfg.OnChange = func() { ran <- struct{}{} }
	c := New(cfg)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { c.Run(ctx); close(done) }()

	// One immediate run plus at least one tick — blocking receives, no sleeps.
	<-ran
	<-ran
	cancel()
	<-done
}

func TestRunOnceIfChanged_SkipsWhileRunningImagesAreUnchanged(t *testing.T) {
	running, reg := newerTagFixture()
	c := New(testConfig(running, reg, &fakeInspector{}))

	c.RunOnce(context.Background())
	calls := len(reg.tagCalls)

	// Same running images: the nudge reports false and asks the registry nothing.
	if c.RunOnceIfChanged(context.Background()) {
		t.Error("RunOnceIfChanged ran despite unchanged running images")
	}
	if len(reg.tagCalls) != calls {
		t.Errorf("registry was consulted on a skipped nudge: %v", reg.tagCalls)
	}

	// A deploy moved a service: the nudge re-checks, so an applied update's
	// marker clears now, not at the next tick up to a full interval later.
	running["gitea"]["server"] = "gitea/gitea:1.22.6"
	reg.tags["gitea/gitea:1.22.6"] = []string{"1.22.3", "1.22.6"}
	reg.digests = map[string]string{"gitea/gitea:1.22.6": "sha256:x"}
	if !c.RunOnceIfChanged(context.Background()) {
		t.Fatal("RunOnceIfChanged skipped despite changed running images")
	}
	if len(c.Snapshot().Stacks) != 0 {
		t.Errorf("applied update still advertised: %+v", c.Snapshot().Stacks)
	}
}

func TestRunOnceIfChanged_FirstCallRuns(t *testing.T) {
	running, reg := newerTagFixture()
	c := New(testConfig(running, reg, &fakeInspector{}))
	if !c.RunOnceIfChanged(context.Background()) {
		t.Fatal("first RunOnceIfChanged must run (nothing was checked yet)")
	}
}
