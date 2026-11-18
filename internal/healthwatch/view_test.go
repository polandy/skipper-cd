package healthwatch

import (
	"testing"
	"time"

	"github.com/polandy/skipper-cd/internal/health"
)

func TestWatcher_CurrentViewCarriesPhasesAndDerivedCorrelation(t *testing.T) {
	env := newTestEnv(t)
	env.tick() // baseline healthy at T0

	env.clock.advance(time.Minute)
	env.watcher.ObserveDeploy(successEvent("app", "a1b2c3d4e5f6", env.clock.now())) // deploy at T1

	env.clock.advance(time.Minute)
	env.set(health.Unhealthy)
	env.tick()
	env.tick() // accepted at T2, within the 5m window of T1

	v := env.watcher.Current()
	phases := v.Stacks["app"]["app"]
	if len(phases) != 2 {
		t.Fatalf("expected 2 phases in the view, got %+v", phases)
	}
	cur := phases[0]
	if cur.Status != health.Unhealthy || cur.Commit != "a1b2c3d4e5f6" || !cur.DeployCorrelated {
		t.Errorf("newest phase must be unhealthy, commit-stamped and deploy-correlated, got %+v", cur)
	}
	// The baseline began before the deploy — the same commit context question
	// answered "no": it cannot be correlated with a later deploy.
	if phases[1].DeployCorrelated {
		t.Errorf("a phase beginning before the deploy must not be correlated, got %+v", phases[1])
	}
	if !phases[1].Since.Before(cur.Since) {
		t.Errorf("view must be newest first, got %+v", phases)
	}
}

func TestWatcher_CurrentViewIsEmptyBeforeFirstObservation(t *testing.T) {
	env := newTestEnv(t)
	v := env.watcher.Current()
	if v.Stacks == nil || len(v.Stacks) != 0 {
		t.Fatalf("expected an empty, non-nil view, got %+v", v)
	}
}

func TestWatcher_PublishesViewOnAcceptedChangesOnly(t *testing.T) {
	env := newTestEnv(t)
	var published []View
	env.watcher = New(Config{
		Alerter:           env.alerter,
		StatePath:         env.statePath,
		DebouncePolls:     2,
		AttributionWindow: 5 * time.Minute,
		Now:               env.clock.now,
		Publish:           func(v View) { published = append(published, v) },
	})

	env.tick() // baseline is an accepted change → published
	if len(published) != 1 {
		t.Fatalf("expected the baseline to publish a view, got %d", len(published))
	}

	env.tick() // steady state → nothing accepted → no publish
	if len(published) != 1 {
		t.Fatalf("steady state must not publish, got %d", len(published))
	}

	env.set(health.Unhealthy)
	env.tick() // pending only → no publish
	if len(published) != 1 {
		t.Fatalf("a pending (undebounced) change must not publish, got %d", len(published))
	}
	env.tick() // accepted → published
	if len(published) != 2 {
		t.Fatalf("an accepted transition must publish, got %d", len(published))
	}
	if got := published[1].Stacks["app"]["app"][0].Status; got != health.Unhealthy {
		t.Errorf("published view must carry the new phase, got %v", got)
	}
}
