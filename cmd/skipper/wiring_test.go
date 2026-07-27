package main

import (
	"bytes"
	"log/slog"
	"slices"
	"testing"

	"github.com/polandy/skipper-cd/internal/config"
	"github.com/polandy/skipper-cd/internal/deploy"
	"github.com/polandy/skipper-cd/internal/events"
)

func ptr[T any](v T) *T { return &v }

func TestDeployerRef_GetPanicsBeforeWiringCompletes(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected get before set to panic")
		}
	}()
	(&deployerRef{}).get()
}

func TestDeployerRef_GetReturnsTheWiredDeployer(t *testing.T) {
	ref := &deployerRef{}
	d := deploy.New(deploy.Config{})
	ref.set(d)

	if got := ref.get(); got != d {
		t.Errorf("get() = %p, want the deployer set via set() (%p)", got, d)
	}
}

func TestStackViews_EffectiveUsesHostListWithoutDiscovery(t *testing.T) {
	cfg := &config.Config{Stacks: []config.Stack{{Name: "gitea"}, {Name: "web"}}}
	// The ref is deliberately unset: static mode must never consult the deployer.
	views := stackViews{cfg: cfg, deployer: &deployerRef{}}

	got := views.effective()

	if len(got) != 2 || got[0].Name != "gitea" || got[1].Name != "web" {
		t.Errorf("effective() = %v, want the host config's stacks", got)
	}
}

func TestStackViews_EffectiveReadsDiscoveredSetUnderDiscovery(t *testing.T) {
	// In discovery mode cfg.Stacks holds raw overrides, not the effective set;
	// a fresh deployer has discovered nothing yet, so an empty result proves
	// the view read through to the deployer instead of the config.
	cfg := &config.Config{StackDiscovery: true, Stacks: []config.Stack{{Name: "override-only"}}}
	ref := &deployerRef{}
	ref.set(deploy.New(deploy.Config{}))
	views := stackViews{cfg: cfg, deployer: ref}

	if got := views.effective(); len(got) != 0 {
		t.Errorf("effective() = %v, want the (empty) discovered set, not cfg.Stacks", got)
	}
}

func TestStackViews_ManagedMarksActiveProjectDirs(t *testing.T) {
	cfg := &config.Config{
		StacksBaseDir: "/srv/stacks",
		Stacks: []config.Stack{
			{Name: "gitea"},
			{Name: "web", ProjectDirectory: "/opt/web"},
		},
	}
	ref := &deployerRef{}
	ref.set(deploy.New(deploy.Config{}))
	views := stackViews{cfg: cfg, deployer: ref}

	m := views.managed()

	if m.BaseDir != "/srv/stacks" {
		t.Errorf("BaseDir = %q, want %q", m.BaseDir, "/srv/stacks")
	}
	// The default project dir is stacks_base_dir/<name>; an explicit
	// project_directory wins (matching the working_dir label a running
	// project carries).
	for _, dir := range []string{"/srv/stacks/gitea", "/opt/web"} {
		if !m.ActiveDirs[dir] {
			t.Errorf("ActiveDirs missing %q: %v", dir, m.ActiveDirs)
		}
	}
	if len(m.ActiveDirs) != 2 {
		t.Errorf("ActiveDirs = %v, want exactly the two active stacks", m.ActiveDirs)
	}
}

func TestStackViews_AppLinkDirsMapsStackToProjectDir(t *testing.T) {
	cfg := &config.Config{
		StacksBaseDir: "/srv/stacks",
		Stacks: []config.Stack{
			{Name: "gitea"},
			{Name: "web", ProjectDirectory: "/opt/web"},
		},
	}
	views := stackViews{cfg: cfg, deployer: &deployerRef{}}

	got := views.appLinkDirs()

	want := map[string]string{"gitea": "/srv/stacks/gitea", "web": "/opt/web"}
	if len(got) != len(want) {
		t.Fatalf("appLinkDirs() = %v, want %v", got, want)
	}
	for name, dir := range want {
		if got[name] != dir {
			t.Errorf("appLinkDirs()[%q] = %q, want %q", name, got[name], dir)
		}
	}
}

func TestStackViews_OrderPrependsNixosWhenRebuildEnabled(t *testing.T) {
	cfg := &config.Config{
		NixOSRebuild: &config.NixOSRebuild{Enabled: ptr(true)},
		Stacks:       []config.Stack{{Name: "gitea"}, {Name: "web"}},
	}
	views := stackViews{cfg: cfg, deployer: &deployerRef{}}

	got := views.order()

	want := []string{deploy.NixosStateKey, "gitea", "web"}
	if !slices.Equal(got, want) {
		t.Errorf("order() = %v, want %v", got, want)
	}
}

func TestBuildEventFanout_FansOutInOrderSkippingAbsentConsumers(t *testing.T) {
	var calls []string
	record := func(name string) func(events.DeployEvent) {
		return func(events.DeployEvent) { calls = append(calls, name) }
	}

	sink := buildEventFanout(record("tally"), nil, record("audit"), nil, record("notify"))
	if sink == nil {
		t.Fatal("expected a composed sink, got nil")
	}
	sink(events.DeployEvent{})

	want := []string{"tally", "audit", "notify"}
	if !slices.Equal(calls, want) {
		t.Errorf("fan-out order = %v, want %v", calls, want)
	}
}

func TestBuildEventFanout_NilWhenNoConsumerIsConfigured(t *testing.T) {
	if sink := buildEventFanout(nil, nil); sink != nil {
		t.Error("expected nil sink when every consumer is absent")
	}
}

func TestSetupLogging_UIDisabledYieldsNoRingAndNilSink(t *testing.T) {
	t.Cleanup(func(l *slog.Logger) func() { return func() { slog.SetDefault(l) } }(slog.Default()))
	cfg := &config.Config{UIEnabled: ptr(false)}

	ring, sink := setupLogging(cfg, &bytes.Buffer{})

	if ring != nil {
		t.Error("expected no log ring with the UI disabled")
	}
	// A typed-nil *logbuf.Log in the interface would defeat the runner's
	// sink != nil check — the interface itself must be nil.
	if sink != nil {
		t.Errorf("expected a nil sink interface, got %T", sink)
	}
}

func TestSetupLogging_UIEnabledTeesSlogIntoTheRing(t *testing.T) {
	t.Cleanup(func(l *slog.Logger) func() { return func() { slog.SetDefault(l) } }(slog.Default()))
	var out bytes.Buffer
	cfg := &config.Config{UIEnabled: ptr(true), LogFormat: config.LogFormatText}

	ring, sink := setupLogging(cfg, &out)

	if ring == nil || sink == nil {
		t.Fatal("expected a log ring and a child-output sink with the UI enabled")
	}
	slog.Info("hello from setup")
	if !bytes.Contains(out.Bytes(), []byte("hello from setup")) {
		t.Errorf("expected the message on the primary writer, got %q", out.String())
	}
	entries := ring.Entries()
	if len(entries) == 0 || entries[len(entries)-1].Msg != "hello from setup" {
		t.Errorf("expected the message teed into the ring, got %v", entries)
	}
}

func TestSetupLogging_LogsConfigWarningsFirst(t *testing.T) {
	t.Cleanup(func(l *slog.Logger) func() { return func() { slog.SetDefault(l) } }(slog.Default()))
	var out bytes.Buffer
	cfg := &config.Config{
		UIEnabled: ptr(false),
		LogFormat: config.LogFormatText,
		Warnings:  []string{"suspicious but valid setup"},
	}

	setupLogging(cfg, &out)

	if !bytes.Contains(out.Bytes(), []byte("suspicious but valid setup")) {
		t.Errorf("expected the config warning logged, got %q", out.String())
	}
}
