package deploy

import (
	"errors"
	"path/filepath"
	"slices"
	"testing"

	"github.com/polandy/skipper-cd/internal/config"
	"github.com/polandy/skipper-cd/internal/events"
)

// psHeldVersion is a `compose ps --all --format json` snapshot of the signal
// stack still running a held signal:3: left by `rollback: false`, or applied
// by hand.
const psHeldVersion = `{"ID":"id-app","Name":"signal-app-1","Service":"app","State":"running","Image":"signal:3"}
`

// psWithOrphan is the snapshot after a hand-applied held change that also
// added a service the reverted compose file no longer has.
const psWithOrphan = `{"ID":"id-app","Name":"signal-app-1","Service":"app","State":"running","Image":"signal:2"}
{"ID":"id-runner","Name":"gitea_act_runner","Service":"act_runner","State":"running","Image":"gitea/act_runner:0.2.11"}
`

// psRecorded lists only the declared service at the recorded version.
const psRecorded = `{"ID":"id-app","Name":"signal-app-1","Service":"app","State":"running","Image":"signal:2"}
`

// holdThenRevert deploys signal:2, holds a failed signal:3 and writes signal:2
// back, so the next run finds the recorded inputs with a hold standing.
func holdThenRevert(t *testing.T, env *heldEnv) {
	t.Helper()
	env.run("signal")
	writeFile(t, env.composePath, composeWithImage("signal:3"))
	env.failStart = true
	env.run("signal")
	if _, ok := env.state().Held["signal"]; !ok {
		t.Fatal("setup: the failed signal:3 must be held")
	}
	env.failStart = false
	writeFile(t, env.composePath, composeWithImage("signal:2"))
}

// serveContainers answers `ps` reads with before until an `up` runs from now
// on, and with after once one has; every other read answers empty.
func serveContainers(env *heldEnv, before, after string, err error) {
	from := len(env.runner.calls)
	env.runner.outputFn = func(_ int, args []string) ([]byte, error) {
		if !slices.Contains(args, "ps") {
			return nil, nil
		}
		if len(upsSince(env, from)) > 0 {
			return []byte(after), err
		}
		return []byte(before), err
	}
}

// upsSince returns the argv of every `up` the runner has seen since call index
// from.
func upsSince(env *heldEnv, from int) [][]string {
	var out [][]string
	for _, c := range env.runner.calls[from:] {
		if slices.Contains(c.args, "up") {
			out = append(out, c.args)
		}
	}
	return out
}

// removals returns the argv of every `docker rm` the runner has seen.
func removals(env *heldEnv) [][]string {
	var out [][]string
	for _, c := range env.runner.calls {
		if c.name == "docker" && len(c.args) > 0 && c.args[0] == "rm" {
			out = append(out, c.args)
		}
	}
	return out
}

// psReads counts the `ps` reads made since call index from.
func psReads(env *heldEnv, from int) int {
	n := 0
	for _, c := range env.runner.outputCalls[from:] {
		if slices.Contains(c.args, "ps") {
			n++
		}
	}
	return n
}

// convergeUp is the release's one compose call: a plain up of the recorded
// version, with no health gate.
var convergeUp = []string{"compose", "up", "-d", "--remove-orphans"}

// With rollback off the failed version keeps running while held. Reverting it
// must bring the containers back to the recorded version, not just release the
// hold over a stack that still runs the failed one.
func TestDeployAllStacks_RevertOfHeldChangeConvergesToRecordedVersion(t *testing.T) {
	env := newHeldEnv(t, config.Stack{Name: "signal", Rollback: off()})
	holdThenRevert(t, env)
	from := len(env.runner.calls)
	serveContainers(env, psHeldVersion, psRecorded, nil)

	statuses, _ := env.run("signal")

	if got := upsSince(env, from); !slices.EqualFunc(got, [][]string{convergeUp}, slices.Equal) {
		t.Fatalf("up calls = %v, want exactly %v", got, convergeUp)
	}
	if !slices.Equal(statuses, []events.Status{events.StatusSuccess}) {
		t.Fatalf("statuses %v, want exactly [success] naming the restored version", statuses)
	}
	want := []events.ServiceImageChange{{Service: "app", Old: "signal:3", New: "signal:2"}}
	if got := env.emitted[len(env.emitted)-1].ImageChanges; !slices.Equal(got, want) {
		t.Errorf("image changes = %+v, want %+v", got, want)
	}
	if _, ok := env.state().Held["signal"]; ok {
		t.Error("the revert must release the hold once the stack converged")
	}
	if got := env.state().RunningImages["signal"]["app"]; got != "signal:2" {
		t.Errorf("recorded running image = %q, want signal:2 as the next deploy's baseline", got)
	}
}

// A hand-applied held change can leave a service the reverted compose file no
// longer declares; the converge's --remove-orphans removes it and the event
// names it as removed.
func TestDeployAllStacks_RevertOfHeldChangeRemovesOrphanedService(t *testing.T) {
	env := newHeldEnv(t)
	holdThenRevert(t, env)
	from := len(env.runner.calls)
	serveContainers(env, psWithOrphan, psRecorded, nil)

	statuses, _ := env.run("signal")

	if got := upsSince(env, from); !slices.EqualFunc(got, [][]string{convergeUp}, slices.Equal) {
		t.Fatalf("up calls = %v, want exactly %v", got, convergeUp)
	}
	if got := removals(env); len(got) != 0 {
		t.Errorf("docker rm calls = %v, want none: the up removes orphans", got)
	}
	if !slices.Equal(statuses, []events.Status{events.StatusSuccess}) {
		t.Fatalf("statuses %v, want exactly [success] naming the removal", statuses)
	}
	want := []events.ServiceImageChange{{Service: "act_runner", Old: "gitea/act_runner:0.2.11"}}
	if got := env.emitted[len(env.emitted)-1].ImageChanges; !slices.Equal(got, want) {
		t.Errorf("image changes = %+v, want %+v", got, want)
	}
}

// The rollback already restored the recorded version: the converge's up
// changes nothing and the release reports the plain skip it always did.
func TestDeployAllStacks_RevertOfRolledBackChangeIsSkipped(t *testing.T) {
	env := newHeldEnv(t)
	holdThenRevert(t, env)
	from := len(env.runner.calls)
	serveContainers(env, psRecorded, psRecorded, nil)

	statuses, _ := env.run("signal")

	if got := upsSince(env, from); len(got) != 1 {
		t.Fatalf("up calls = %v, want the one converge", got)
	}
	if !slices.Equal(statuses, []events.Status{events.StatusSkipped}) {
		t.Errorf("statuses %v, want [skipped]", statuses)
	}
	if _, ok := env.state().Held["signal"]; ok {
		t.Error("the revert must release the hold")
	}
}

// An unchanged stack with no hold is skipped without an up or a container
// read: the converge is tied to releasing a hold, not paid every tick.
func TestDeployAllStacks_UnheldSkipMakesNoUp(t *testing.T) {
	env := newHeldEnv(t)
	env.run("signal")
	from, fromReads := len(env.runner.calls), len(env.runner.outputCalls)
	serveContainers(env, psWithOrphan, psRecorded, nil)

	statuses, _ := env.run("signal")

	if !slices.Equal(statuses, []events.Status{events.StatusSkipped}) {
		t.Fatalf("statuses %v, want [skipped]", statuses)
	}
	if got := upsSince(env, from); len(got) != 0 {
		t.Errorf("up calls = %v, want none for an unheld skip", got)
	}
	if n := psReads(env, fromReads); n != 0 {
		t.Errorf("ps reads = %d, want none for an unheld skip", n)
	}
}

// A failed converge keeps the hold, so the next tick tries again — releasing
// it would leave the held version running for good.
func TestDeployAllStacks_FailedConvergeKeepsHold(t *testing.T) {
	env := newHeldEnv(t)
	holdThenRevert(t, env)
	from := len(env.runner.calls)
	serveContainers(env, psHeldVersion, psRecorded, nil)
	env.runner.failFn = func(_ string, args []string) error {
		if slices.Contains(args, "up") {
			return errors.New("port is already allocated")
		}
		return nil
	}

	statuses, _ := env.run("signal")

	if got := upsSince(env, from); len(got) != 1 {
		t.Fatalf("up calls = %v, want the one failed attempt", got)
	}
	if !slices.Equal(statuses, []events.Status{events.StatusFailed}) {
		t.Fatalf("statuses %v, want [failed]", statuses)
	}
	if _, ok := env.state().Held["signal"]; !ok {
		t.Error("a failed converge must keep the hold so the next tick retries")
	}
}

// A failed container read cannot tell what the up changed, but the up itself
// still runs and the hold is released; the release reports a plain skip.
func TestDeployAllStacks_FailedContainerReadStillConverges(t *testing.T) {
	env := newHeldEnv(t)
	holdThenRevert(t, env)
	from := len(env.runner.calls)
	serveContainers(env, "", "", errors.New("required variable TOKEN is missing a value"))

	statuses, _ := env.run("signal")

	if got := upsSince(env, from); !slices.EqualFunc(got, [][]string{convergeUp}, slices.Equal) {
		t.Fatalf("up calls = %v, want exactly %v", got, convergeUp)
	}
	if !slices.Equal(statuses, []events.Status{events.StatusSkipped}) {
		t.Fatalf("statuses %v, want [skipped]", statuses)
	}
	if _, ok := env.state().Held["signal"]; ok {
		t.Error("a failed read must not keep the hold once the up converged")
	}
}

// The converge's up starts stopped on-demand containers like any up; they are
// stopped again right after it.
func TestDeployAllStacks_ConvergeStopsOnDemandContainersAgain(t *testing.T) {
	env := newHeldEnv(t, config.Stack{Name: "signal", OnDemandContainers: []string{"signal-worker"}})
	holdThenRevert(t, env)
	from := len(env.runner.calls)
	serveContainers(env, psHeldVersion, psRecorded, nil)

	env.run("signal")

	var tail [][]string
	for _, c := range env.runner.calls[from:] {
		tail = append(tail, c.args)
	}
	want := [][]string{convergeUp, {"stop", "signal-worker"}}
	if !slices.EqualFunc(tail, want, slices.Equal) {
		t.Errorf("calls = %v, want %v", tail, want)
	}
}

// A stack that builds its images rebuilds them in the converge: the held
// version's build carries the same tag, so a plain up would keep running it.
// No --pull, so no base image moves.
func TestDeployAllStacks_ConvergeRebuildsBuiltImages(t *testing.T) {
	env := newHeldEnv(t)
	dir := filepath.Dir(env.composePath)
	writeFile(t, env.composePath, "services:\n  app:\n    build: .\n")
	dockerfile := filepath.Join(dir, "Dockerfile")
	writeFile(t, dockerfile, "FROM nginx:1.27\n")
	env.run("signal")
	writeFile(t, dockerfile, "FROM nginx:1.29\n")
	env.failStart = true
	env.run("signal")
	if _, ok := env.state().Held["signal"]; !ok {
		t.Fatal("setup: the failed Dockerfile change must be held")
	}
	env.failStart = false
	writeFile(t, dockerfile, "FROM nginx:1.27\n")
	from := len(env.runner.calls)

	env.run("signal")

	ups := upsSince(env, from)
	if len(ups) != 1 {
		t.Fatalf("up calls = %v, want the one converge", ups)
	}
	if !slices.Contains(ups[0], "--build") || slices.Contains(ups[0], "--pull") {
		t.Errorf("converge up = %v, want --build without --pull", ups[0])
	}
}
