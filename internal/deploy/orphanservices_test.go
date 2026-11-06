package deploy

import (
	"errors"
	"slices"
	"testing"

	"github.com/polandy/skipper-cd/internal/events"
)

// psWithOrphan is a `compose ps --all --format json` snapshot of the signal
// stack after a manual apply of a held change: its declared service plus one
// the reverted compose file no longer has.
const psWithOrphan = `{"ID":"id-app","Name":"signal-app-1","Service":"app","State":"running","Image":"signal:2"}
{"ID":"id-runner","Name":"gitea_act_runner","Service":"act_runner","State":"running","Image":"gitea/act_runner:0.2.11"}
`

// psWithoutOrphan lists only the declared service.
const psWithoutOrphan = `{"ID":"id-app","Name":"signal-app-1","Service":"app","State":"running","Image":"signal:2"}
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

// serveps makes every `ps` read answer with out and every other read empty.
func serveps(env *heldEnv, out string, err error) {
	env.runner.outputFn = func(_ int, args []string) ([]byte, error) {
		if slices.Contains(args, "ps") {
			return []byte(out), err
		}
		return nil, nil
	}
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

// A held change applied by hand and then reverted leaves the containers ahead
// of the recorded inputs. Releasing the hold removes the services the compose
// file no longer declares — exactly those, nothing else.
func TestDeployAllStacks_RevertOfHeldChangeRemovesOrphanedService(t *testing.T) {
	env := newHeldEnv(t)
	holdThenRevert(t, env)
	serveps(env, psWithOrphan, nil)

	statuses, ups := env.run("signal")

	if got, want := removals(env), [][]string{{"rm", "-f", "id-runner"}}; !slices.EqualFunc(got, want, slices.Equal) {
		t.Fatalf("docker rm calls = %v, want %v", got, want)
	}
	if ups != 0 {
		t.Errorf("deploy ups = %d, want none: the stack is at its recorded inputs", ups)
	}
	if !slices.Equal(statuses, []events.Status{events.StatusSuccess}) {
		t.Fatalf("statuses %v, want exactly [success] naming the removal", statuses)
	}
	want := []events.ServiceImageChange{{Service: "act_runner", Old: "gitea/act_runner:0.2.11"}}
	if got := env.emitted[len(env.emitted)-1].ImageChanges; !slices.Equal(got, want) {
		t.Errorf("image changes = %+v, want %+v", got, want)
	}
	if _, ok := env.state().Held["signal"]; ok {
		t.Error("the revert must release the hold once the orphans are gone")
	}
}

// Nothing to remove: the release reads the containers and reports the plain
// skip it always did.
func TestDeployAllStacks_RevertOfHeldChangeWithoutOrphanIsSkipped(t *testing.T) {
	env := newHeldEnv(t)
	holdThenRevert(t, env)
	serveps(env, psWithoutOrphan, nil)
	from := len(env.runner.outputCalls)

	statuses, _ := env.run("signal")

	if psReads(env, from) != 1 {
		t.Fatalf("ps reads = %d, want 1: the release must compare the containers", psReads(env, from))
	}
	if got := removals(env); len(got) != 0 {
		t.Errorf("docker rm calls = %v, want none", got)
	}
	if !slices.Equal(statuses, []events.Status{events.StatusSkipped}) {
		t.Errorf("statuses %v, want [skipped]", statuses)
	}
	if _, ok := env.state().Held["signal"]; ok {
		t.Error("the revert must release the hold")
	}
}

// An unchanged stack with no hold is skipped without reading its containers:
// the comparison is tied to releasing a hold, not paid every tick.
func TestDeployAllStacks_UnheldSkipReadsNoContainers(t *testing.T) {
	env := newHeldEnv(t)
	env.run("signal")
	serveps(env, psWithOrphan, nil)
	from := len(env.runner.outputCalls)

	statuses, _ := env.run("signal")

	if !slices.Equal(statuses, []events.Status{events.StatusSkipped}) {
		t.Fatalf("statuses %v, want [skipped]", statuses)
	}
	if n := psReads(env, from); n != 0 {
		t.Errorf("ps reads = %d, want none for an unheld skip", n)
	}
	if got := removals(env); len(got) != 0 {
		t.Errorf("docker rm calls = %v, want none", got)
	}
}

// A failed read or removal keeps the hold, so the next tick tries again —
// releasing it would leave the orphan for good.
func TestDeployAllStacks_OrphanCleanupFailureKeepsHold(t *testing.T) {
	cases := map[string]func(env *heldEnv){
		"ps fails": func(env *heldEnv) { serveps(env, "", errors.New("docker daemon unreachable")) },
		"rm fails": func(env *heldEnv) {
			serveps(env, psWithOrphan, nil)
			env.runner.failFn = func(_ string, args []string) error {
				if len(args) > 0 && args[0] == "rm" {
					return errors.New("container is busy")
				}
				return nil
			}
		},
	}
	for name, arrange := range cases {
		t.Run(name, func(t *testing.T) {
			env := newHeldEnv(t)
			holdThenRevert(t, env)
			arrange(env)

			statuses, _ := env.run("signal")

			if !slices.Equal(statuses, []events.Status{events.StatusFailed}) {
				t.Fatalf("statuses %v, want [failed]", statuses)
			}
			if _, ok := env.state().Held["signal"]; !ok {
				t.Error("a failed cleanup must keep the hold so the next tick retries")
			}
		})
	}
}
