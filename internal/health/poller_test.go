package health

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"
)

// fakeOutputter records the commands it runs and returns canned output keyed by
// the --project-directory / run dir, so a test can hand each stack its own ps.
type fakeOutputter struct {
	mu    sync.Mutex
	calls [][]string // each: name + args
	dirs  []string
	fn    func(dir string, args []string) ([]byte, error)
}

func (f *fakeOutputter) Output(_ context.Context, dir, name string, args ...string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, append([]string{name}, args...))
	f.dirs = append(f.dirs, dir)
	return f.fn(dir, args)
}

func healthyJSON(svc string) []byte {
	return []byte(`{"Service":"` + svc + `","State":"running","Health":"healthy"}`)
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

func TestPoller_PollOnceReportsRollupPerStack(t *testing.T) {
	stacks := []StackRef{
		{Name: "gitea", ComposePath: "/repo/gitea/docker-compose.yml", ProjectDir: "/srv/gitea"},
		{Name: "grafana", ComposePath: "/repo/grafana/docker-compose.yml", ProjectDir: "/srv/grafana"},
	}
	out := &fakeOutputter{fn: func(dir string, _ []string) ([]byte, error) {
		if dir == "/srv/grafana" {
			return []byte(`{"Service":"grafana","State":"restarting","Health":""}`), nil
		}
		return healthyJSON("gitea"), nil
	}}

	var got Snapshot
	p := newTestPoller(out, stacks, func(s Snapshot) { got = s }, nil)
	p.pollOnce(context.Background())

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

func TestPoller_ProbeUsesComposeIdentityWithWorkingDir(t *testing.T) {
	out := &fakeOutputter{fn: func(string, []string) ([]byte, error) { return healthyJSON("app"), nil }}
	p := newTestPoller(out, nil, nil, nil)

	p.probe(context.Background(), StackRef{
		Name: "app", ComposePath: "/repo/app/docker-compose.yml", ProjectDir: "/srv/app",
	})

	want := []string{"docker", "compose", "-f", "/repo/app/docker-compose.yml", "--project-directory", "/srv/app", "ps", "--format", "json", "--all"}
	if !reflect.DeepEqual(out.calls[0], want) {
		t.Errorf("argv:\n got %v\nwant %v", out.calls[0], want)
	}
	if out.dirs[0] != "/srv/app" {
		t.Errorf("run dir: got %q, want /srv/app", out.dirs[0])
	}
}

func TestPoller_ProbeUsesComposeIdentityWithoutWorkingDir(t *testing.T) {
	out := &fakeOutputter{fn: func(string, []string) ([]byte, error) { return healthyJSON("app"), nil }}
	p := newTestPoller(out, nil, nil, nil)

	compose := "/repo/app/docker-compose.yml"
	p.probe(context.Background(), StackRef{Name: "app", ComposePath: compose})

	// No working_dir: no -f/--project-directory, run from the compose dir — mirrors
	// how the deploy path invokes compose in that case.
	want := []string{"docker", "compose", "ps", "--format", "json", "--all"}
	if !reflect.DeepEqual(out.calls[0], want) {
		t.Errorf("argv:\n got %v\nwant %v", out.calls[0], want)
	}
	if out.dirs[0] != filepath.Dir(compose) {
		t.Errorf("run dir: got %q, want %q", out.dirs[0], filepath.Dir(compose))
	}
}

func TestPoller_ProbeUnknownOnCommandError(t *testing.T) {
	out := &fakeOutputter{fn: func(string, []string) ([]byte, error) { return nil, errors.New("boom") }}
	p := newTestPoller(out, nil, nil, nil)

	got := p.probe(context.Background(), StackRef{Name: "x", ComposePath: "/repo/x/docker-compose.yml"})
	if got.Status != Unknown {
		t.Errorf("got %q, want unknown", got.Status)
	}
}

func TestPoller_ProbeUnknownOnMalformedOutput(t *testing.T) {
	out := &fakeOutputter{fn: func(string, []string) ([]byte, error) { return []byte(`{bad`), nil }}
	p := newTestPoller(out, nil, nil, nil)

	got := p.probe(context.Background(), StackRef{Name: "x", ComposePath: "/repo/x/docker-compose.yml"})
	if got.Status != Unknown {
		t.Errorf("got %q, want unknown", got.Status)
	}
}

func TestPoller_TickPollsWhenSubscribed(t *testing.T) {
	out := &fakeOutputter{fn: func(string, []string) ([]byte, error) { return healthyJSON("app"), nil }}
	var polls int
	p := newTestPoller(out, []StackRef{{Name: "app", ComposePath: "/repo/app/docker-compose.yml"}},
		func(Snapshot) { polls++ }, func() bool { return true })

	p.tick(context.Background())
	if polls != 1 {
		t.Errorf("expected 1 publish, got %d", polls)
	}
}

func TestPoller_TickSkipsWhenNoSubscribers(t *testing.T) {
	out := &fakeOutputter{fn: func(string, []string) ([]byte, error) { return healthyJSON("app"), nil }}
	var polls int
	p := newTestPoller(out, []StackRef{{Name: "app", ComposePath: "/repo/app/docker-compose.yml"}},
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
	out := &fakeOutputter{fn: func(string, []string) ([]byte, error) { return healthyJSON("app"), nil }}
	var snaps int
	// No subscribers, but AlwaysPoll (self-heal) forces the poll anyway.
	p := New(Config{
		Outputter:      out,
		Stacks:         func() []StackRef { return []StackRef{{Name: "app", ComposePath: "/repo/app/docker-compose.yml"}} },
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
	out := &fakeOutputter{fn: func(string, []string) ([]byte, error) { return healthyJSON("app"), nil }}
	var got Snapshot
	p := New(Config{
		Outputter:  out,
		Stacks:     func() []StackRef { return []StackRef{{Name: "app", ComposePath: "/repo/app/docker-compose.yml"}} },
		Interval:   time.Hour,
		OnSnapshot: func(s Snapshot) { got = s },
	})

	p.pollOnce(context.Background())
	if got.Stacks["app"].Status != Healthy {
		t.Errorf("OnSnapshot did not receive the fresh snapshot, got %+v", got)
	}
}

func TestPoller_CurrentReturnsLatestSnapshot(t *testing.T) {
	out := &fakeOutputter{fn: func(string, []string) ([]byte, error) { return healthyJSON("app"), nil }}
	p := newTestPoller(out, []StackRef{{Name: "app", ComposePath: "/repo/app/docker-compose.yml"}}, func(Snapshot) {}, nil)

	if got := p.Current(); len(got.Stacks) != 0 {
		t.Errorf("expected empty snapshot before first poll, got %+v", got)
	}
	p.pollOnce(context.Background())
	if got := p.Current(); got.Stacks["app"].Status != Healthy {
		t.Errorf("Current after poll: got %+v", got)
	}
}

func TestPoller_PollTriggersImmediateRefresh(t *testing.T) {
	out := &fakeOutputter{fn: func(string, []string) ([]byte, error) { return healthyJSON("app"), nil }}
	published := make(chan Snapshot, 1)
	p := newTestPoller(out, []StackRef{{Name: "app", ComposePath: "/repo/app/docker-compose.yml"}},
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
