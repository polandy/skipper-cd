package health

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeOutputter records the commands it runs and answers each with fn.
type fakeOutputter struct {
	mu    sync.Mutex
	calls [][]string // each: name + args
	dirs  []string
	fn    func(args []string) ([]byte, error)
}

func (f *fakeOutputter) Output(_ context.Context, dir, name string, args ...string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, append([]string{name}, args...))
	f.dirs = append(f.dirs, dir)
	return f.fn(args)
}

// fakeContainer is one container as the docker daemon would describe it.
type fakeContainer struct {
	ID, WorkingDir, Service, Name, Image, State, Health string
	ExitCode                                            int
}

// dockerHost answers the poller's `docker ps` with every container and its
// `docker inspect` with the ones whose IDs it is asked for, in the shapes
// listArgs/inspectArgs request.
func dockerHost(containers ...fakeContainer) *fakeOutputter {
	return &fakeOutputter{fn: func(args []string) ([]byte, error) {
		var b strings.Builder
		switch args[0] {
		case "ps":
			for _, c := range containers {
				fmt.Fprintf(&b, "%s\t%s\n", c.ID, c.Image)
			}
		case "inspect":
			for _, id := range args[len(inspectArgs):] {
				for _, c := range containers {
					if c.ID == id {
						fmt.Fprintf(&b, "%s\t%s\t%s\t/%s\t%s\t%s\t%d\n", c.ID, c.WorkingDir, c.Service, c.Name, c.State, c.Health, c.ExitCode)
					}
				}
			}
		}
		return []byte(b.String()), nil
	}}
}

func healthyIn(dir, svc string) fakeContainer {
	return fakeContainer{ID: dir + "/" + svc, WorkingDir: dir, Service: svc, Name: svc, State: "running", Health: "healthy"}
}

func newTestPoller(out Outputter, stacks []StackRef, publish func(Snapshot), hasSubs func() bool) *Poller {
	return New(Config{
		Outputter:      out,
		Stacks:         func() []StackRef { return stacks },
		Publish:        publish,
		Interval:       time.Hour,
		HasSubscribers: hasSubs,
	})
}

func pollStacks(out Outputter, stacks ...StackRef) Snapshot {
	var got Snapshot
	newTestPoller(out, stacks, func(s Snapshot) { got = s }, nil).pollOnce(context.Background())
	return got
}

var appStack = StackRef{Name: "app", ComposePath: "/repo/app/docker-compose.yml"}

func TestPoller_PollOnceReportsRollupPerStack(t *testing.T) {
	out := dockerHost(
		healthyIn("/srv/gitea", "gitea"),
		fakeContainer{ID: "g", WorkingDir: "/srv/grafana", Service: "grafana", Name: "grafana", State: "restarting"},
	)

	got := pollStacks(out,
		StackRef{Name: "gitea", ComposePath: "/repo/gitea/docker-compose.yml", ProjectDir: "/srv/gitea"},
		StackRef{Name: "grafana", ComposePath: "/repo/grafana/docker-compose.yml", ProjectDir: "/srv/grafana"},
	)

	if got.Stacks["gitea"].Status != Healthy {
		t.Errorf("gitea: got %q, want healthy", got.Stacks["gitea"].Status)
	}
	if got.Stacks["grafana"].Status != Unhealthy {
		t.Errorf("grafana: got %q, want unhealthy", got.Stacks["grafana"].Status)
	}
	if len(got.Stacks["gitea"].Services) != 1 {
		t.Errorf("expected gitea's service list populated, got %+v", got.Stacks["gitea"].Services)
	}
}

func TestPoller_PollOnceRunsTwoDockerCallsWhateverTheStackCount(t *testing.T) {
	var containers []fakeContainer
	var stacks []StackRef
	for _, name := range []string{"a", "b", "c", "d"} {
		containers = append(containers, healthyIn("/repo/"+name, "app"))
		stacks = append(stacks, StackRef{Name: name, ComposePath: "/repo/" + name + "/docker-compose.yml"})
	}
	out := dockerHost(containers...)

	got := pollStacks(out, stacks...)

	if len(out.calls) != 2 {
		t.Fatalf("expected one ps + one inspect for 4 stacks, got %d calls: %v", len(out.calls), out.calls)
	}
	wantPS := append([]string{"docker"}, listArgs...)
	if !reflect.DeepEqual(out.calls[0], wantPS) {
		t.Errorf("ps argv:\n got %v\nwant %v", out.calls[0], wantPS)
	}
	wantInspect := append(append([]string{"docker"}, inspectArgs...), "/repo/a/app", "/repo/b/app", "/repo/c/app", "/repo/d/app")
	if !reflect.DeepEqual(out.calls[1], wantInspect) {
		t.Errorf("inspect argv:\n got %v\nwant %v", out.calls[1], wantInspect)
	}
	for _, s := range stacks {
		if got.Stacks[s.Name].Status != Healthy {
			t.Errorf("%s: got %q, want healthy", s.Name, got.Stacks[s.Name].Status)
		}
	}
}

func TestPoller_PollOnceSkipsInspectWhenNoContainerExists(t *testing.T) {
	out := dockerHost()

	got := pollStacks(out, appStack)

	if len(out.calls) != 1 {
		t.Errorf("expected only the ps call, got %v", out.calls)
	}
	if got.Stacks["app"].Status != Stopped {
		t.Errorf("got %q, want stopped", got.Stacks["app"].Status)
	}
}

func TestPoller_PollOnceRunsNothingWithoutStacks(t *testing.T) {
	out := dockerHost(healthyIn("/repo/app", "app"))

	pollStacks(out)

	if len(out.calls) != 0 {
		t.Errorf("expected no docker calls without stacks, got %v", out.calls)
	}
}

func TestPoller_MatchesContainersByProjectDir(t *testing.T) {
	out := dockerHost(
		healthyIn("/srv/app", "app"),
		healthyIn("/repo/plain", "web"),
		healthyIn("/elsewhere/unmanaged", "x"),
	)

	got := pollStacks(out,
		// project_directory set: compose labels the container with it.
		StackRef{Name: "app", ComposePath: "/repo/app/docker-compose.yml", ProjectDir: "/srv/app/"},
		// No project_directory: the compose file's own dir is the identity.
		StackRef{Name: "plain", ComposePath: "/repo/plain/docker-compose.yml"},
		StackRef{Name: "idle", ComposePath: "/repo/idle/docker-compose.yml"},
	)

	if s := got.Stacks["app"]; s.Status != Healthy || len(s.Services) != 1 || s.Services[0].Name != "app" {
		t.Errorf("app: got %+v", s)
	}
	if s := got.Stacks["plain"]; s.Status != Healthy || len(s.Services) != 1 || s.Services[0].Name != "web" {
		t.Errorf("plain: got %+v", s)
	}
	if s := got.Stacks["idle"]; s.Status != Stopped || len(s.Services) != 0 {
		t.Errorf("idle: a stack with no containers must be stopped with no services, got %+v", s)
	}
}

func TestPoller_ReportsRunningImagePerServiceSortedByContainerName(t *testing.T) {
	out := dockerHost(
		fakeContainer{ID: "2", WorkingDir: "/repo/nextcloud", Service: "db", Name: "nextcloud-db-1", Image: "postgres:16", State: "running"},
		fakeContainer{ID: "1", WorkingDir: "/repo/nextcloud", Service: "app", Name: "nextcloud-app-1", Image: "nextcloud:30.0.2", State: "running"},
	)

	got := pollStacks(out, StackRef{Name: "nextcloud", ComposePath: "/repo/nextcloud/docker-compose.yml"})

	svcs := got.Stacks["nextcloud"].Services
	if len(svcs) != 2 {
		t.Fatalf("expected 2 services, got %+v", svcs)
	}
	if svcs[0].Name != "app" || svcs[0].Image != "nextcloud:30.0.2" || svcs[1].Name != "db" || svcs[1].Image != "postgres:16" {
		t.Errorf("expected services in container-name order with their images, got %+v", svcs)
	}
}

func TestPoller_MarksOnDemandContainersByName(t *testing.T) {
	// monica-style stack: the on-demand app container was stopped by skipper
	// after the deploy (exit 137), a sibling keeps running. The stack must read
	// stopped-not-unhealthy for the app and healthy overall.
	out := dockerHost(
		fakeContainer{ID: "1", WorkingDir: "/repo/monica", Service: "app", Name: "monica-app", State: "exited", ExitCode: 137},
		fakeContainer{ID: "2", WorkingDir: "/repo/monica", Service: "db", Name: "monica-db", State: "running", Health: "healthy"},
	)
	monica := StackRef{Name: "monica", ComposePath: "/repo/monica/docker-compose.yml"}
	onDemand := monica
	onDemand.OnDemand = []string{"monica-app"}

	got := pollStacks(out, onDemand).Stacks["monica"]

	if got.Status != Healthy {
		t.Errorf("stack rollup: got %q, want healthy (idle on-demand app must not degrade it)", got.Status)
	}
	if len(got.Services) != 2 || got.Services[0].Status != Stopped {
		t.Errorf("expected the on-demand app classified stopped, got %+v", got.Services)
	}
	// The snapshot labels the service so the UI can say *why* it is stopped.
	if !got.Services[0].OnDemand || got.Services[1].OnDemand {
		t.Errorf("expected only the app marked on-demand, got %+v", got.Services)
	}

	// Without the on-demand marking the same containers are a real failure.
	if plain := pollStacks(out, monica).Stacks["monica"]; plain.Status != Unhealthy {
		t.Errorf("non-on-demand exit 137 must stay unhealthy, got %q", plain.Status)
	}
}

func TestPoller_OnDemandMarkingStaysWithItsStack(t *testing.T) {
	// Two stacks sharing one project dir see the same containers; one stack's
	// on_demand_containers must not leak into the other's classification.
	out := dockerHost(fakeContainer{ID: "1", WorkingDir: "/srv/shared", Service: "job", Name: "job", State: "exited", ExitCode: 1})

	got := pollStacks(out,
		StackRef{Name: "a", ComposePath: "/repo/a/docker-compose.yml", ProjectDir: "/srv/shared", OnDemand: []string{"job"}},
		StackRef{Name: "b", ComposePath: "/repo/b/docker-compose.yml", ProjectDir: "/srv/shared"},
	)

	if got.Stacks["a"].Status != Stopped || got.Stacks["b"].Status != Unhealthy {
		t.Errorf("got a=%q b=%q, want a=stopped b=unhealthy", got.Stacks["a"].Status, got.Stacks["b"].Status)
	}
}

func TestPoller_UnknownForEveryStackWhenListingFails(t *testing.T) {
	out := &fakeOutputter{fn: func([]string) ([]byte, error) { return nil, errors.New("boom") }}

	got := pollStacks(out, appStack, StackRef{Name: "web", ComposePath: "/repo/web/docker-compose.yml"})

	if got.Stacks["app"].Status != Unknown || got.Stacks["web"].Status != Unknown {
		t.Errorf("got %+v, want every stack unknown", got.Stacks)
	}
}

func TestPoller_UnknownWhenInspectFailsWithoutOutput(t *testing.T) {
	ok := dockerHost(healthyIn("/repo/app", "app"))
	out := &fakeOutputter{fn: func(args []string) ([]byte, error) {
		if args[0] == "inspect" {
			return nil, errors.New("daemon gone")
		}
		return ok.fn(args)
	}}

	if got := pollStacks(out, appStack).Stacks["app"]; got.Status != Unknown {
		t.Errorf("got %q, want unknown", got.Status)
	}
}

func TestPoller_ContainerRemovedBeforeInspectIsDroppedNotUnknown(t *testing.T) {
	// A container removed between ps and inspect (a deploy recreating it) fails
	// inspect for that ID alone; the others are still printed.
	host := dockerHost(healthyIn("/repo/app", "app"))
	out := &fakeOutputter{fn: func(args []string) ([]byte, error) {
		if args[0] == "ps" {
			return []byte("/repo/app/app\t\ngone\t\n"), nil
		}
		b, _ := host.fn(args)
		return b, errors.New("Error: No such object: gone")
	}}

	got := pollStacks(out, appStack).Stacks["app"]
	if got.Status != Healthy || len(got.Services) != 1 {
		t.Errorf("got %+v, want the surviving container healthy", got)
	}
}

func TestPoller_UnknownOnMalformedOutput(t *testing.T) {
	out := &fakeOutputter{fn: func(args []string) ([]byte, error) {
		if args[0] == "ps" {
			return []byte("id1\timg\n"), nil
		}
		return []byte("id1\tnot enough columns\n"), nil
	}}

	if got := pollStacks(out, appStack).Stacks["app"]; got.Status != Unknown {
		t.Errorf("got %q, want unknown", got.Status)
	}
}

func TestPoller_TickPollsWhenSubscribed(t *testing.T) {
	out := dockerHost(healthyIn("/repo/app", "app"))
	var polls int
	p := newTestPoller(out, []StackRef{appStack},
		func(Snapshot) { polls++ }, func() bool { return true })

	p.tick(context.Background())
	if polls != 1 {
		t.Errorf("expected 1 publish, got %d", polls)
	}
}

func TestPoller_TickSkipsWhenNoSubscribers(t *testing.T) {
	out := dockerHost(healthyIn("/repo/app", "app"))
	var polls int
	p := newTestPoller(out, []StackRef{appStack},
		func(Snapshot) { polls++ }, func() bool { return false })

	p.tick(context.Background())
	if polls != 0 {
		t.Errorf("expected no publish when unsubscribed, got %d", polls)
	}
	out.mu.Lock()
	defer out.mu.Unlock()
	if len(out.calls) != 0 {
		t.Errorf("expected no ps calls when unsubscribed, got %d", len(out.calls))
	}
}

func TestPoller_TickAlwaysPollsWhenAlwaysPollSet(t *testing.T) {
	out := dockerHost(healthyIn("/repo/app", "app"))
	var snaps int
	// No subscribers, but AlwaysPoll (self-heal) forces the poll anyway.
	p := New(Config{
		Outputter:      out,
		Stacks:         func() []StackRef { return []StackRef{appStack} },
		Interval:       time.Hour,
		HasSubscribers: func() bool { return false },
		AlwaysPoll:     true,
		OnSnapshot:     func(Snapshot) { snaps++ },
	})

	p.tick(context.Background())
	if snaps != 1 {
		t.Errorf("expected AlwaysPoll to poll despite no subscribers, got %d snapshots", snaps)
	}
}

func TestPoller_OnSnapshotReceivesEveryPoll(t *testing.T) {
	out := dockerHost(healthyIn("/repo/app", "app"))
	var got Snapshot
	p := New(Config{
		Outputter:  out,
		Stacks:     func() []StackRef { return []StackRef{appStack} },
		Interval:   time.Hour,
		OnSnapshot: func(s Snapshot) { got = s },
	})

	p.pollOnce(context.Background())
	if got.Stacks["app"].Status != Healthy {
		t.Errorf("OnSnapshot did not receive the fresh snapshot, got %+v", got)
	}
}

func TestPoller_CurrentReturnsLatestSnapshot(t *testing.T) {
	out := dockerHost(healthyIn("/repo/app", "app"))
	p := newTestPoller(out, []StackRef{appStack}, func(Snapshot) {}, nil)

	if got := p.Current(); len(got.Stacks) != 0 {
		t.Errorf("expected empty snapshot before first poll, got %+v", got)
	}
	p.pollOnce(context.Background())
	if got := p.Current(); got.Stacks["app"].Status != Healthy {
		t.Errorf("Current after poll: got %+v", got)
	}
}

func TestPoller_PollTriggersImmediateRefresh(t *testing.T) {
	out := dockerHost(healthyIn("/repo/app", "app"))
	published := make(chan Snapshot, 1)
	p := newTestPoller(out, []StackRef{appStack},
		func(s Snapshot) { published <- s }, func() bool { return true })

	go p.Run(t.Context()) // interval is 1h, so only Poll can trigger this within the test

	p.Poll()
	select {
	case s := <-published:
		if s.Stacks["app"].Status != Healthy {
			t.Errorf("unexpected snapshot: %+v", s)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Poll did not trigger a refresh")
	}
}

func TestPoller_PollIfStaleSkipsWhileTheLastPollIsFresh(t *testing.T) {
	out := dockerHost(healthyIn("/repo/app", "app"))
	now := time.Unix(1_000_000, 0)
	p := New(Config{
		Outputter: out,
		Stacks:    func() []StackRef { return []StackRef{appStack} },
		Interval:  time.Hour,
		Now:       func() time.Time { return now },
	})

	p.PollIfStale()
	if len(p.trigger) != 1 {
		t.Fatal("first PollIfStale must request a poll: nothing has polled yet")
	}
	<-p.trigger
	p.pollOnce(t.Context())

	now = now.Add(clientPollMinInterval - time.Second)
	p.PollIfStale()
	if len(p.trigger) != 0 {
		t.Error("PollIfStale requested a poll although the last one is fresh")
	}

	now = now.Add(time.Second)
	p.PollIfStale()
	if len(p.trigger) != 1 {
		t.Error("PollIfStale must request a poll once the last one is clientPollMinInterval old")
	}
}
