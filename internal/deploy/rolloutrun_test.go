package deploy

import (
	"context"
	"reflect"
	"testing"
)

// twoRolledCompose rolls web and api; db is brought up in place first.
const twoRolledCompose = `services:
  web:
    image: nginx:1.25
    healthcheck:
      test: ["CMD", "true"]
  api:
    image: api:1
    healthcheck:
      test: ["CMD", "true"]
  db:
    image: postgres:16
`

// The rolling indicator names each rolled service as its cutover starts — after
// the in-place up of the rest — and clears to the zero value when the rollout ends.
func TestRollout_PublishesRolloutRunPerService(t *testing.T) {
	var runs []RolloutRun
	var callsAtPublish []int
	var runner *recordingRunner
	d, runner, run := rolloutSetup(t, twoRolledCompose, func(c *Config) {
		c.RolloutRunSink = func(r RolloutRun) {
			runs = append(runs, r)
			callsAtPublish = append(callsAtPublish, len(runner.calls))
		}
	})
	run.stack.Rollout.Services = []string{"web", "api"}
	compose, err := parseComposeFile(run.composePath)
	if err != nil {
		t.Fatal(err)
	}

	if err := d.rollout(context.Background(), run, compose, newEmptyState()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := []RolloutRun{
		{Stack: "app", Service: "web", Index: 1, Total: 2},
		{Stack: "app", Service: "api", Index: 2, Total: 2},
		{},
	}
	if !reflect.DeepEqual(runs, want) {
		t.Errorf("rolloutrun sequence:\n got %+v\nwant %+v", runs, want)
	}
	if len(callsAtPublish) == 0 || callsAtPublish[0] == 0 {
		t.Errorf("the first service's phase must follow the in-place up of db; calls before it: %v", callsAtPublish)
	}
	if d.CurrentRolloutRun() != (RolloutRun{}) {
		t.Errorf("rolling state must be cleared after the rollout, got %+v", d.CurrentRolloutRun())
	}
}

// A failed cutover clears the indicator too, so the row never sticks on "rolling".
func TestRollout_ClearsRolloutRunOnCanaryFailure(t *testing.T) {
	var runs []RolloutRun
	d, runner, run := rolloutSetup(t, rolloutCompose, func(c *Config) {
		c.RolloutRunSink = func(r RolloutRun) { runs = append(runs, r) }
	})
	compose, err := parseComposeFile(run.composePath)
	if err != nil {
		t.Fatal(err)
	}
	runner.outputFn = func(call int, _ []string) ([]byte, error) {
		if call == 0 {
			return psArray(cl("old1", "web", "healthy")), nil
		}
		return psArray(cl("old1", "web", "healthy"), cl("new1", "web", "starting")), nil
	}

	if err := d.rollout(context.Background(), run, compose, newEmptyState()); err == nil {
		t.Fatal("expected error when the canary never becomes healthy")
	}

	want := []RolloutRun{{Stack: "app", Service: "web", Index: 1, Total: 1}, {}}
	if !reflect.DeepEqual(runs, want) {
		t.Errorf("rolloutrun sequence:\n got %+v\nwant %+v", runs, want)
	}
	if d.CurrentRolloutRun() != (RolloutRun{}) {
		t.Errorf("rolling state must be cleared after a failed rollout, got %+v", d.CurrentRolloutRun())
	}
}

// Without a UI sink nothing is recorded: headless deploys never compute the state.
func TestRollout_NoSinkRecordsNoRolloutRun(t *testing.T) {
	d, _, _ := rolloutSetup(t, rolloutCompose)
	d.publishRolloutRun(RolloutRun{Stack: "app", Service: "web", Index: 1, Total: 1})
	if d.CurrentRolloutRun() != (RolloutRun{}) {
		t.Errorf("no sink: state must stay zero, got %+v", d.CurrentRolloutRun())
	}
}

// probeCommitReader records the rolling state the moment the rollback reads the
// restored compose file — the positive signal that the rollback leg ran.
type probeCommitReader struct {
	fakeCommitReader
	d      *Deployer
	atRead []RolloutRun
}

func (p *probeCommitReader) FileAtCommit(ctx context.Context, sha, path string) ([]byte, error) {
	p.atRead = append(p.atRead, p.d.CurrentRolloutRun())
	return p.fakeCommitReader.FileAtCommit(ctx, sha, path)
}

// A cutover failure that falls back to the git-restore rollback clears the
// indicator first, so the rollback leg does not read as "rolling".
func TestRollout_ClearsRolloutRunBeforeGitRestoreRollback(t *testing.T) {
	cr := &probeCommitReader{}
	d, runner, run := rolloutSetup(t, rolloutCompose, func(c *Config) {
		c.RolloutRunSink = func(RolloutRun) {}
		c.CommitReader = cr
	})
	cr.d = d
	runner.errOnCommand = "--no-recreate" // the canary start fails → git-restore path
	runner.outputFn = func(int, []string) ([]byte, error) {
		return psArray(cl("old1", "web", "healthy")), nil
	}
	compose, err := parseComposeFile(run.composePath)
	if err != nil {
		t.Fatal(err)
	}
	state := newEmptyState()
	state.LastDeployedCommit = "base1"

	if err := d.rollout(context.Background(), run, compose, state); err == nil {
		t.Fatal("expected the canary start failure to fail the rollout")
	}
	if len(cr.atRead) == 0 {
		t.Fatal("the git-restore rollback never read the restored compose file")
	}
	if cr.atRead[0] != (RolloutRun{}) {
		t.Errorf("rollback ran while the indicator still read %+v", cr.atRead[0])
	}
}
