package healthwatch

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/polandy/skipper-cd/internal/events"
	"github.com/polandy/skipper-cd/internal/health"
)

// fakeAlerter records fired alerts.
type fakeAlerter struct {
	mu     sync.Mutex
	alerts []Alert
}

func (f *fakeAlerter) Fire(a Alert) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.alerts = append(f.alerts, a)
}

func (f *fakeAlerter) all() []Alert {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Alert(nil), f.alerts...)
}

// fakeClock is a manually advanced clock so phase timestamps are deterministic.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{t: time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// testEnv bundles a Watcher over one stack "app" whose snapshot content is
// swapped between Observe calls — the poller feed, minus the poller.
type testEnv struct {
	watcher   *Watcher
	alerter   *fakeAlerter
	clock     *fakeClock
	statePath string

	mu   sync.Mutex
	snap health.Snapshot
}

// set makes the next Observe see the single service "app" with the given status.
func (e *testEnv) set(status health.Status) {
	e.setServices(health.ServiceHealth{Name: "app", Status: status})
}

// setServices makes the next Observe see exactly the given services.
func (e *testEnv) setServices(svcs ...health.ServiceHealth) {
	e.mu.Lock()
	defer e.mu.Unlock()
	rollup := health.Healthy
	for _, s := range svcs {
		if s.Status == health.Unhealthy {
			rollup = health.Unhealthy
		}
	}
	e.snap = health.Snapshot{Stacks: map[string]health.StackHealth{
		"app": {Status: rollup, Services: svcs},
	}}
}

// failProbe makes the next Observe see the stack as unknown (probe failure).
func (e *testEnv) failProbe() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.snap = health.Snapshot{Stacks: map[string]health.StackHealth{
		"app": {Status: health.Unknown},
	}}
}

// tick delivers the current snapshot, as one poller interval would.
func (e *testEnv) tick() {
	e.mu.Lock()
	snap := e.snap
	e.mu.Unlock()
	e.watcher.Observe(snap)
}

// newTestEnv builds a Watcher with debounce 2 and a 5-minute attribution
// window, persisting to a temp state file.
func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	env := &testEnv{
		alerter:   &fakeAlerter{},
		clock:     newFakeClock(),
		statePath: filepath.Join(t.TempDir(), "healthwatch.yaml"),
	}
	env.set(health.Healthy)
	env.watcher = newWatcherFor(env, 2)
	return env
}

// newWatcherFor builds a fresh Watcher over env's fakes — used both at setup
// and to simulate a skipper restart against the same state file.
func newWatcherFor(env *testEnv, debounce int) *Watcher {
	return New(Config{
		Alerter:           env.alerter,
		StatePath:         env.statePath,
		DebouncePolls:     debounce,
		AttributionWindow: 5 * time.Minute,
		Now:               env.clock.now,
	})
}

// newWatcherWithCooldown builds a Watcher whose per-service alert cooldown is
// active (ADR-0031 amendment); debounce 1 keeps the flap sequences short.
func newWatcherWithCooldown(env *testEnv, cooldown time.Duration) *Watcher {
	return New(Config{
		Alerter:           env.alerter,
		StatePath:         env.statePath,
		DebouncePolls:     1,
		AttributionWindow: 5 * time.Minute,
		AlertCooldown:     cooldown,
		Now:               env.clock.now,
	})
}

// flapOnce drives one accepted unhealthy→healthy flap cycle a minute apart.
func flapOnce(env *testEnv) {
	env.clock.advance(time.Minute)
	env.set(health.Unhealthy)
	env.tick()
	env.clock.advance(time.Minute)
	env.set(health.Healthy)
	env.tick()
}

func successEvent(stack, sha string, at time.Time) events.DeployEvent {
	return events.DeployEvent{
		Stack:     stack,
		Status:    events.StatusSuccess,
		Timestamp: at,
		Commits:   []events.CommitInfo{{SHA: sha, Subject: "change"}},
	}
}

func TestWatcher_FirstObservationIsSilentBaseline(t *testing.T) {
	env := newTestEnv(t)

	env.tick()

	if got := env.alerter.all(); len(got) != 0 {
		t.Fatalf("baseline must not alert, got %+v", got)
	}
	phases := env.watcher.phases("app", "app")
	if len(phases) != 1 || phases[0].Status != health.Healthy {
		t.Fatalf("expected one healthy baseline phase, got %+v", phases)
	}
}

func TestWatcher_AlertsOnDebouncedUnhealthyTransition(t *testing.T) {
	env := newTestEnv(t)
	env.tick() // healthy baseline

	env.set(health.Unhealthy)
	firstSeen := env.clock.now()
	env.tick()
	if got := env.alerter.all(); len(got) != 0 {
		t.Fatalf("must not alert before debounce confirms, got %+v", got)
	}

	env.clock.advance(time.Minute)
	env.tick() // second consecutive unhealthy snapshot confirms

	got := env.alerter.all()
	if len(got) != 1 {
		t.Fatalf("expected exactly one alert, got %+v", got)
	}
	a := got[0]
	if a.Stack != "app" || a.Service != "app" || a.From != health.Healthy || a.To != health.Unhealthy {
		t.Errorf("unexpected alert: %+v", a)
	}
	if !a.Since.Equal(firstSeen) {
		t.Errorf("since must be the first snapshot of the phase, got %v want %v", a.Since, firstSeen)
	}

	phases := env.watcher.phases("app", "app")
	if len(phases) != 2 || phases[0].Status != health.Unhealthy || phases[1].Status != health.Healthy {
		t.Fatalf("expected [unhealthy healthy] history, got %+v", phases)
	}
}

func TestWatcher_TransientBlipNeverAlerts(t *testing.T) {
	env := newTestEnv(t)
	env.tick() // baseline

	env.set(health.Unhealthy)
	env.tick() // one bad snapshot
	env.set(health.Healthy)
	env.tick()
	env.tick()

	if got := env.alerter.all(); len(got) != 0 {
		t.Fatalf("a single bad snapshot must not alert, got %+v", got)
	}
	if phases := env.watcher.phases("app", "app"); len(phases) != 1 {
		t.Fatalf("a blip must not enter the history, got %+v", phases)
	}
}

func TestWatcher_RecoveryAlertCarriesUnhealthyDuration(t *testing.T) {
	env := newTestEnv(t)
	env.tick() // baseline healthy

	env.set(health.Unhealthy)
	env.tick()
	env.tick() // accepted → alert #1
	unhealthySince := env.watcher.phases("app", "app")[0].Since

	env.clock.advance(10 * time.Minute)
	env.set(health.Healthy)
	recoverySeen := env.clock.now()
	env.tick()
	env.tick() // accepted → recovery alert

	got := env.alerter.all()
	if len(got) != 2 {
		t.Fatalf("expected fail + recovery alerts, got %+v", got)
	}
	rec := got[1]
	if rec.From != health.Unhealthy || rec.To != health.Healthy {
		t.Errorf("unexpected recovery alert: %+v", rec)
	}
	if want := recoverySeen.Sub(unhealthySince); rec.PrevDuration != want {
		t.Errorf("PrevDuration: got %v, want %v", rec.PrevDuration, want)
	}
}

func TestWatcher_StartingAndStoppedTransitionsAreSilent(t *testing.T) {
	env := newTestEnv(t)
	env.tick() // baseline healthy

	env.set(health.Starting)
	env.tick()
	env.tick() // accepted
	env.set(health.Stopped)
	env.tick()
	env.tick() // accepted

	if got := env.alerter.all(); len(got) != 0 {
		t.Fatalf("starting/stopped must not alert, got %+v", got)
	}
	phases := env.watcher.phases("app", "app")
	if len(phases) != 3 || phases[0].Status != health.Stopped || phases[1].Status != health.Starting {
		t.Fatalf("transitions must still be recorded, got %+v", phases)
	}
}

func TestWatcher_UnknownHoldsLastStatus(t *testing.T) {
	env := newTestEnv(t)
	env.tick() // baseline healthy
	baseline := env.watcher.phases("app", "app")

	env.failProbe()
	env.tick()
	env.tick()

	if got := env.alerter.all(); len(got) != 0 {
		t.Fatalf("a failed probe must not alert, got %+v", got)
	}
	phases := env.watcher.phases("app", "app")
	if len(phases) != 1 || !phases[0].Since.Equal(baseline[0].Since) {
		t.Fatalf("unknown must hold the last phase untouched, got %+v", phases)
	}

	// The real transition is still detected once the probe works again.
	env.set(health.Unhealthy)
	env.tick()
	env.tick()
	if got := env.alerter.all(); len(got) != 1 || got[0].To != health.Unhealthy {
		t.Fatalf("expected the unhealthy alert after probe recovery, got %+v", got)
	}
}

func TestWatcher_HistoryIsBoundedToTenPhases(t *testing.T) {
	env := newTestEnv(t)
	env.tick() // baseline

	statuses := []health.Status{health.Unhealthy, health.Healthy}
	for i := range 12 {
		env.clock.advance(time.Minute)
		env.set(statuses[i%2])
		env.tick()
		env.tick() // accept each flip
	}

	phases := env.watcher.phases("app", "app")
	if len(phases) != 10 {
		t.Fatalf("history must cap at 10 phases, got %d", len(phases))
	}
	for i := 1; i < len(phases); i++ {
		if !phases[i].Since.Before(phases[i-1].Since) {
			t.Fatalf("history must be newest first, got %+v", phases)
		}
	}
}

func TestWatcher_RestartAlertsOnDowntimeTransition(t *testing.T) {
	env := newTestEnv(t)
	env.tick() // baseline healthy, persisted

	// Simulate: skipper restarts, the service failed while it was down.
	env.watcher = newWatcherFor(env, 2)
	env.set(health.Unhealthy)
	env.tick()
	env.tick()

	got := env.alerter.all()
	if len(got) != 1 || got[0].From != health.Healthy || got[0].To != health.Unhealthy {
		t.Fatalf("expected the downtime transition to alert after restart, got %+v", got)
	}
}

func TestWatcher_RestartStaysSilentOnUnchangedStatus(t *testing.T) {
	env := newTestEnv(t)
	env.tick() // baseline healthy
	env.set(health.Unhealthy)
	env.tick()
	env.tick() // accepted unhealthy → 1 alert
	if len(env.alerter.all()) != 1 {
		t.Fatal("test setup: expected the initial unhealthy alert")
	}

	// Restart: still unhealthy — a known failure must not re-page.
	env.watcher = newWatcherFor(env, 2)
	env.tick()
	env.tick()
	env.tick()

	if got := env.alerter.all(); len(got) != 1 {
		t.Fatalf("restart must not re-alert an unchanged status, got %+v", got)
	}
}

func TestWatcher_CorruptStateFileIsACleanSlate(t *testing.T) {
	env := newTestEnv(t)
	if err := os.WriteFile(env.statePath, []byte("{not yaml"), 0o644); err != nil {
		t.Fatal(err)
	}

	env.watcher = newWatcherFor(env, 2)
	env.set(health.Unhealthy) // already unhealthy on a fresh slate
	env.tick()

	if got := env.alerter.all(); len(got) != 0 {
		t.Fatalf("a corrupt state file must baseline silently, got %+v", got)
	}
	phases := env.watcher.phases("app", "app")
	if len(phases) != 1 || phases[0].Status != health.Unhealthy {
		t.Fatalf("expected an unhealthy baseline phase, got %+v", phases)
	}
}

func TestWatcher_AlertCarriesCommitAndDeployCorrelation(t *testing.T) {
	env := newTestEnv(t)
	env.tick() // baseline healthy

	env.watcher.ObserveDeploy(successEvent("app", "a1b2c3d4e5f6", env.clock.now()))
	env.clock.advance(time.Minute) // within the 5m window
	env.set(health.Unhealthy)
	env.tick()
	env.tick()

	got := env.alerter.all()
	if len(got) != 1 {
		t.Fatalf("expected one alert, got %+v", got)
	}
	if got[0].Commit != "a1b2c3d4e5f6" || !got[0].DeployCorrelated {
		t.Errorf("expected deploy-correlated alert with commit, got %+v", got[0])
	}
	if p := env.watcher.phases("app", "app")[0]; p.Commit != "a1b2c3d4e5f6" {
		t.Errorf("phase must record the commit, got %+v", p)
	}
}

func TestWatcher_SteadyStateTransitionIsNotDeployCorrelated(t *testing.T) {
	env := newTestEnv(t)
	env.tick() // baseline healthy

	env.watcher.ObserveDeploy(successEvent("app", "a1b2c3d4e5f6", env.clock.now()))
	env.clock.advance(2 * time.Hour) // far outside the window
	env.set(health.Unhealthy)
	env.tick()
	env.tick()

	got := env.alerter.all()
	if len(got) != 1 {
		t.Fatalf("expected one alert, got %+v", got)
	}
	if got[0].DeployCorrelated {
		t.Errorf("a steady-state failure must not be deploy-correlated: %+v", got[0])
	}
	if got[0].Commit != "a1b2c3d4e5f6" {
		t.Errorf("the commit context is still carried, got %+v", got[0])
	}
}

func TestWatcher_DeployRecordSurvivesRestart(t *testing.T) {
	env := newTestEnv(t)
	env.tick() // baseline healthy
	env.watcher.ObserveDeploy(successEvent("app", "a1b2c3d4e5f6", env.clock.now()))
	env.tick() // persists the deploy record

	env.watcher = newWatcherFor(env, 2) // restart
	env.clock.advance(time.Minute)
	env.set(health.Unhealthy)
	env.tick()
	env.tick()

	got := env.alerter.all()
	if len(got) != 1 || got[0].Commit != "a1b2c3d4e5f6" || !got[0].DeployCorrelated {
		t.Fatalf("deploy record must survive a restart, got %+v", got)
	}
}

func TestWatcher_NonSuccessAndCommitlessEventsAreHandled(t *testing.T) {
	env := newTestEnv(t)
	env.tick() // baseline

	env.watcher.ObserveDeploy(events.DeployEvent{
		Stack: "app", Status: events.StatusFailed, Timestamp: env.clock.now(),
		Commits: []events.CommitInfo{{SHA: "deadbeef"}},
	})
	env.watcher.ObserveDeploy(events.DeployEvent{
		Stack: "app", Status: events.StatusSuccess, Timestamp: env.clock.now(),
	})

	env.clock.advance(time.Minute)
	env.set(health.Unhealthy)
	env.tick()
	env.tick()

	got := env.alerter.all()
	if len(got) != 1 {
		t.Fatalf("expected one alert, got %+v", got)
	}
	// The failed event is ignored; the commitless success still dates the
	// deploy for correlation but carries no SHA.
	if got[0].Commit != "" || !got[0].DeployCorrelated {
		t.Errorf("expected correlated alert without commit, got %+v", got[0])
	}
}

func TestWatcher_NewServiceBaselinesSilentlyAndRemovedServiceStops(t *testing.T) {
	env := newTestEnv(t)
	env.tick() // baseline: only "app"

	// A second service appears (e.g. added to the compose file): silent baseline.
	env.setServices(
		health.ServiceHealth{Name: "app", Status: health.Healthy},
		health.ServiceHealth{Name: "db", Status: health.Unhealthy},
	)
	env.tick()
	if got := env.alerter.all(); len(got) != 0 {
		t.Fatalf("a newly appearing service must baseline silently, got %+v", got)
	}

	// "db" disappears from the snapshot entirely: it is observed as stopped.
	env.setServices(health.ServiceHealth{Name: "app", Status: health.Healthy})
	env.tick()
	env.tick()
	if got := env.alerter.all(); len(got) != 0 {
		t.Fatalf("a removed service must not alert, got %+v", got)
	}
	phases := env.watcher.phases("app", "db")
	if len(phases) != 2 || phases[0].Status != health.Stopped {
		t.Fatalf("expected the removed service recorded as stopped, got %+v", phases)
	}
}

func TestWatcher_DebounceOfOneAcceptsImmediately(t *testing.T) {
	env := newTestEnv(t)
	env.watcher = newWatcherFor(env, 1)
	env.tick() // baseline

	env.set(health.Unhealthy)
	env.tick()

	if got := env.alerter.all(); len(got) != 1 {
		t.Fatalf("debounce 1 must accept on the first snapshot, got %+v", got)
	}
}

func TestWatcher_NoCooldownAlertsEveryFlap(t *testing.T) {
	env := newTestEnv(t)
	env.watcher = newWatcherFor(env, 1) // cooldown disabled: the default
	env.tick()                          // baseline healthy

	flapOnce(env)
	flapOnce(env)

	if got := env.alerter.all(); len(got) != 4 {
		t.Fatalf("without a cooldown every flap must page, got %d alerts: %+v", len(got), got)
	}
}

func TestWatcher_CooldownStillDeliversFirstFailAndRecovery(t *testing.T) {
	env := newTestEnv(t)
	env.watcher = newWatcherWithCooldown(env, 30*time.Minute)
	env.tick() // baseline healthy

	// One ordinary incident: fail + recover two minutes apart. The cooldown is
	// per direction, so the all-clear is never delayed by the unhealthy alert.
	flapOnce(env)

	got := env.alerter.all()
	if len(got) != 2 || got[0].To != health.Unhealthy || got[1].To != health.Healthy {
		t.Fatalf("expected the ordinary fail+recovery pair untouched, got %+v", got)
	}
}

func TestWatcher_CooldownSuppressesRepeatedFlapAlerts(t *testing.T) {
	env := newTestEnv(t)
	env.watcher = newWatcherWithCooldown(env, 30*time.Minute)
	env.tick() // baseline healthy

	flapOnce(env) // first cycle pages both directions
	flapOnce(env) // second cycle within the cooldown: fully suppressed

	if got := env.alerter.all(); len(got) != 2 {
		t.Fatalf("the repeat flap must be suppressed, got %d alerts: %+v", len(got), got)
	}
	// Suppression never loses history: all five phases are recorded.
	if phases := env.watcher.phases("app", "app"); len(phases) != 5 {
		t.Fatalf("suppressed transitions must still be recorded, got %+v", phases)
	}
}

func TestWatcher_CooldownCatchesUpWhenFlapSettlesUnhealthy(t *testing.T) {
	env := newTestEnv(t)
	env.watcher = newWatcherWithCooldown(env, 30*time.Minute)
	env.tick()    // baseline healthy
	flapOnce(env) // pages fail + recovery

	// The flap's last transition lands unhealthy — suppressed by the cooldown.
	env.clock.advance(time.Minute)
	env.set(health.Unhealthy)
	settledSince := env.clock.now()
	env.tick()
	if got := env.alerter.all(); len(got) != 2 {
		t.Fatalf("the repeat unhealthy within the cooldown must not page, got %+v", got)
	}

	// Once the cooldown expires the owed alert is delivered late: the operator
	// last heard "recovered" but the service is still down.
	env.clock.advance(31 * time.Minute)
	env.tick()
	got := env.alerter.all()
	if len(got) != 3 {
		t.Fatalf("expected the catch-up alert after cooldown expiry, got %+v", got)
	}
	a := got[2]
	if a.To != health.Unhealthy || a.From != health.Healthy {
		t.Errorf("unexpected catch-up alert: %+v", a)
	}
	if !a.Since.Equal(settledSince) {
		t.Errorf("catch-up since must be when the phase began, got %v want %v", a.Since, settledSince)
	}

	// It is delivered exactly once.
	env.clock.advance(time.Minute)
	env.tick()
	if got := env.alerter.all(); len(got) != 3 {
		t.Fatalf("the catch-up must fire only once, got %+v", got)
	}
}

func TestWatcher_CooldownCatchUpStaysSilentWhenConverged(t *testing.T) {
	env := newTestEnv(t)
	env.watcher = newWatcherWithCooldown(env, 30*time.Minute)
	env.tick() // baseline healthy

	flapOnce(env) // pages both directions
	flapOnce(env) // suppressed, ends healthy — matching what was last alerted

	env.clock.advance(31 * time.Minute)
	env.tick()
	if got := env.alerter.all(); len(got) != 2 {
		t.Fatalf("a converged flap owes no catch-up, got %+v", got)
	}

	// A fresh failure after the cooldown pages normally again.
	env.clock.advance(time.Minute)
	env.set(health.Unhealthy)
	env.tick()
	if got := env.alerter.all(); len(got) != 3 || got[2].To != health.Unhealthy {
		t.Fatalf("a failure after cooldown expiry must page, got %+v", got)
	}
}

func TestWatcher_CooldownSuppressionSurvivesRestart(t *testing.T) {
	env := newTestEnv(t)
	env.watcher = newWatcherWithCooldown(env, 30*time.Minute)
	env.tick()    // baseline healthy
	flapOnce(env) // pages fail + recovery
	env.clock.advance(time.Minute)
	env.set(health.Unhealthy) // suppressed; service stays down
	env.tick()

	// Restart within the cooldown: the suppression state is persisted, so the
	// unchanged unhealthy status must not re-page early…
	env.watcher = newWatcherWithCooldown(env, 30*time.Minute)
	env.tick()
	if got := env.alerter.all(); len(got) != 2 {
		t.Fatalf("restart must not bypass the cooldown, got %+v", got)
	}

	// …but the owed catch-up still arrives once the cooldown expires.
	env.clock.advance(31 * time.Minute)
	env.tick()
	got := env.alerter.all()
	if len(got) != 3 || got[2].To != health.Unhealthy {
		t.Fatalf("the catch-up must survive a restart, got %+v", got)
	}
}

func TestWatcher_CooldownSettledStoppedResolvesSilently(t *testing.T) {
	env := newTestEnv(t)
	env.watcher = newWatcherWithCooldown(env, 30*time.Minute)
	env.tick()    // baseline healthy
	flapOnce(env) // pages fail + recovery
	env.clock.advance(time.Minute)
	env.set(health.Unhealthy) // suppressed
	env.tick()

	// The service is then taken down intentionally: stopped never alerts, so
	// the pending catch-up resolves silently at expiry.
	env.clock.advance(time.Minute)
	env.set(health.Stopped)
	env.tick()
	env.clock.advance(31 * time.Minute)
	env.tick()

	if got := env.alerter.all(); len(got) != 2 {
		t.Fatalf("a stopped service owes no catch-up, got %+v", got)
	}
}

func TestWatcher_NilAlerterOnlyRecords(t *testing.T) {
	env := newTestEnv(t)
	env.watcher = New(Config{
		StatePath:         env.statePath,
		DebouncePolls:     2,
		AttributionWindow: 5 * time.Minute,
		Now:               env.clock.now,
	})
	env.tick() // baseline
	env.set(health.Unhealthy)
	env.tick()
	env.tick() // must not panic without an alerter

	phases := env.watcher.phases("app", "app")
	if len(phases) != 2 || phases[0].Status != health.Unhealthy {
		t.Fatalf("transitions must still be recorded without targets, got %+v", phases)
	}
}
