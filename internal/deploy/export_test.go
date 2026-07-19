package deploy

import (
	"context"
	"time"

	"github.com/polandy/skipper-cd/internal/autosync"
	"github.com/polandy/skipper-cd/internal/events"
)

// Test-only wiring. Production code configures a Deployer exclusively through
// New(Config) — all collaborators are fixed at construction. Tests instead
// build a minimal deployer and swap in single seams afterwards, which is safe
// because tests drive the deployer synchronously from one goroutine.

// newDeployerWithRunner builds a Deployer with only a (fake) runner wired.
func newDeployerWithRunner(r Runner) *Deployer {
	return New(Config{Runner: r})
}

func (d *Deployer) SetEventSink(fn func(events.DeployEvent)) { d.eventSink = fn }

func (d *Deployer) SetAutosync(c *autosync.Controller, q *autosync.Queue) {
	d.autosync = c
	d.queue = q
}

func (d *Deployer) SetShutdownContext(ctx context.Context) { d.shutdownCtx = ctx }

func (d *Deployer) SetRunPlanSink(fn func(RunPlan)) { d.runPlanSink = fn }

func (d *Deployer) InitEventID(startID int64) { d.nextEventID.Store(startID) }

// SetOutputter wires the command outputter rollout uses to read `docker compose ps`.
func (d *Deployer) SetOutputter(o Outputter) { d.outputter = o }

// SetRolloutPollInterval shortens the canary health-wait poll so rollout tests
// do not sleep the production default between polls.
func (d *Deployer) SetRolloutPollInterval(iv time.Duration) { d.rolloutPollInterval = iv }
