package deploy

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/polandy/skipper-cd/internal/config"
	"github.com/polandy/skipper-cd/internal/events"
)

const (
	digestA = "@sha256:aaaa"
	digestB = "@sha256:bbbb"
)

var (
	deployUpArgs   = []string{"up", "-d", "--remove-orphans"}
	rollbackUpArgs = []string{"up", "-d"}
)

// composeWithImages renders a compose file with one image: service per entry.
func composeWithImages(images map[string]string) string {
	var b strings.Builder
	b.WriteString("services:\n")
	for _, name := range sortedKeysOf(images) {
		b.WriteString("  " + name + ":\n    image: " + images[name] + "\n")
	}
	return b.String()
}

func sortedKeysOf(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// upArgs returns the compose arguments from "up" on of every up call.
func upArgs(calls []runCall) [][]string {
	var ups [][]string
	for _, c := range calls {
		if i := slices.Index(c.args, "up"); i >= 0 {
			ups = append(ups, c.args[i:])
		}
	}
	return ups
}

// TestDeployAllStacks_RollbackPatchOnly drives a failed start through a full
// run per update level: only a digest or patch step may restore the previous
// version; anything larger or unclassifiable leaves the failed one running and
// held, with the reason in the event error (ADR-0074).
func TestDeployAllStacks_RollbackPatchOnly(t *testing.T) {
	tests := []struct {
		name         string
		policy       config.RollbackPolicy
		old, new     map[string]string
		newCompose   string // overrides the compose rendered from new
		wantRollback bool
		wantReason   string
	}{
		{
			name: "pinned digest moved", policy: config.RollbackPatchOnly,
			old: map[string]string{"app": "app:1.2.3" + digestA}, new: map[string]string{"app": "app:1.2.3" + digestB},
			wantRollback: true,
		},
		{
			name: "patch bump", policy: config.RollbackPatchOnly,
			old: map[string]string{"app": "app:1.2.3"}, new: map[string]string{"app": "app:1.2.4"},
			wantRollback: true,
		},
		{
			name: "no image changed", policy: config.RollbackPatchOnly,
			old: map[string]string{"app": "app:1.2.3"}, new: map[string]string{"app": "app:1.2.3"},
			newCompose:   "services:\n  app:\n    image: app:1.2.3\n    environment:\n      MODE: new\n",
			wantRollback: true,
		},
		{
			name: "minor bump", policy: config.RollbackPatchOnly,
			old: map[string]string{"app": "app:1.2"}, new: map[string]string{"app": "app:1.3"},
			wantReason: "rollback skipped: app 1.2 -> 1.3 is a minor update",
		},
		{
			name: "major bump", policy: config.RollbackPatchOnly,
			old: map[string]string{"postgres": "postgres:15"}, new: map[string]string{"postgres": "postgres:16"},
			wantReason: "rollback skipped: postgres 15 -> 16 is a major update",
		},
		{
			name: "non-semver tag", policy: config.RollbackPatchOnly,
			old: map[string]string{"app": "app:latest"}, new: map[string]string{"app": "app:stable"},
			wantReason: "rollback skipped: app app:latest -> app:stable is not a classifiable update",
		},
		{
			name: "added service", policy: config.RollbackPatchOnly,
			old: map[string]string{"app": "app:1.2.3"}, new: map[string]string{"app": "app:1.2.3", "cache": "redis:7"},
			wantReason: "rollback skipped: cache is a new service, so its update level is unknown",
		},
		{
			name: "removed service", policy: config.RollbackPatchOnly,
			old: map[string]string{"app": "app:1.2.3", "cache": "redis:7"}, new: map[string]string{"app": "app:1.2.3"},
			wantReason: "rollback skipped: cache was removed, so its update level is unknown",
		},
		{
			name: "build service", policy: config.RollbackPatchOnly,
			old: map[string]string{"app": "app:1.2.3"}, new: map[string]string{"app": "app:1.2.3"},
			newCompose: "services:\n  app:\n    image: app:1.2.3\n  web:\n    build: .\n",
			wantReason: "rollback skipped: web is built from a Dockerfile, so its update level is unknown",
		},
		{
			name: "multi-service: the largest step decides", policy: config.RollbackPatchOnly,
			old:        map[string]string{"app": "app:1.2.3", "db": "postgres:15", "web": "nginx:1.25.1"},
			new:        map[string]string{"app": "app:1.2.4", "db": "postgres:16", "web": "nginx:1.26.0"},
			wantReason: "rollback skipped: db 15 -> 16 is a major update",
		},
		{
			name: "multi-service: patch and digest steps together roll back", policy: config.RollbackPatchOnly,
			old:          map[string]string{"app": "app:1.2.3", "db": "postgres:16.1" + digestA, "web": "nginx:1.25"},
			new:          map[string]string{"app": "app:1.2.4", "db": "postgres:16.1" + digestB, "web": "nginx:1.25"},
			wantRollback: true,
		},
		{
			name: "always rolls back a major bump", policy: config.RollbackAlways,
			old: map[string]string{"postgres": "postgres:15"}, new: map[string]string{"postgres": "postgres:16"},
			wantRollback: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := newPolicyEnv(t, tt.policy, tt.old, tt.new, tt.newCompose)

			env.d.DeployAllStacks(context.Background(), env.cfg)
			want := [][]string{deployUpArgs}
			if tt.wantRollback {
				want = append(want, rollbackUpArgs)
			}
			if got := upArgs(env.runner.calls); !slices.EqualFunc(got, want, slices.Equal) {
				t.Errorf("up calls = %v, want %v", got, want)
			}
			last := env.emitted[len(env.emitted)-1]
			wantStatus := events.StatusFailed
			if tt.wantRollback {
				wantStatus = events.StatusRolledBack
			}
			if last.Status != wantStatus {
				t.Errorf("event status = %s, want %s (%+v)", last.Status, wantStatus, last)
			}
			if !strings.Contains(last.Error, "new version failed") {
				t.Errorf("event error lost the cause: %q", last.Error)
			}
			if tt.wantReason != "" && !strings.Contains(last.Error, tt.wantReason) {
				t.Errorf("event error = %q, want it to say %q", last.Error, tt.wantReason)
			}
			if tt.wantRollback && strings.Contains(last.Error, "rollback skipped") {
				t.Errorf("a rolled-back deploy must not say it skipped the rollback: %q", last.Error)
			}

			// The failed version is held either way: no further deploy attempt.
			env.emitted = nil
			before := len(env.runner.calls)
			env.d.DeployAllStacks(context.Background(), env.cfg)
			if ups := upArgs(env.runner.calls[before:]); len(ups) != 0 {
				t.Errorf("second run: up calls %v, want none (held)", ups)
			}
			if len(env.emitted) != 1 || env.emitted[0].Status != events.StatusHeld {
				t.Errorf("second run: events %+v, want one held", env.emitted)
			}
		})
	}
}

// policyEnv is one stack "mystack" whose last settled version ran old and whose
// compose file now declares new; its deploy `up` fails.
type policyEnv struct {
	d       *Deployer
	cfg     *config.Config
	runner  *recordingRunner
	emitted []events.DeployEvent
}

func newPolicyEnv(t *testing.T, policy config.RollbackPolicy, old, next map[string]string, newCompose string) *policyEnv {
	t.Helper()
	baseDir := t.TempDir()
	stackDir := filepath.Join(baseDir, "mystack")
	if err := os.MkdirAll(stackDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if newCompose == "" {
		newCompose = composeWithImages(next)
	}
	composePath := filepath.Join(stackDir, "docker-compose.yml")
	writeFile(t, composePath, newCompose)
	writeFile(t, filepath.Join(stackDir, "Dockerfile"), "FROM scratch\n")

	stateDir := t.TempDir()
	seed := newEmptyState()
	seed.LastDeployedCommit = "old-sha"
	seed.Stacks["mystack"] = stackFileHashes{composePath: "hash-of-old"}
	seed.Images["mystack"] = serviceImageByName(old)
	if err := saveDeployState(stateDir, seed); err != nil {
		t.Fatal(err)
	}

	env := &policyEnv{runner: &recordingRunner{failFn: func(_ string, args []string) error {
		if slices.Contains(args, "--remove-orphans") {
			return errors.New("new version failed to start")
		}
		return nil
	}}}
	env.d = New(Config{
		Runner:       env.runner,
		Outputter:    env.runner,
		CommitReader: &fakeCommitReader{files: map[string][]byte{"old-sha:" + composePath: []byte(composeWithImages(old))}},
		RepoDir:      baseDir,
		StateDir:     stateDir,
		EventSink:    func(e events.DeployEvent) { env.emitted = append(env.emitted, e) },
	})
	env.cfg = &config.Config{StacksBaseDir: baseDir, Stacks: []config.Stack{{Name: "mystack", Rollback: &policy}}}
	return env
}

// A global patch_only reaches a stack that sets no rollback of its own.
func TestDeployAllStacks_GlobalRollbackPatchOnlyApplies(t *testing.T) {
	env := newPolicyEnv(t, config.RollbackAlways, map[string]string{"db": "postgres:15"}, map[string]string{"db": "postgres:16"}, "")
	patchOnly := config.RollbackPatchOnly
	env.cfg.Rollback = &patchOnly
	env.cfg.Stacks[0].Rollback = nil

	env.d.DeployAllStacks(context.Background(), env.cfg)
	if got := upArgs(env.runner.calls); !slices.EqualFunc(got, [][]string{deployUpArgs}, slices.Equal) {
		t.Errorf("up calls = %v, want only the failed deploy up", got)
	}
	if last := env.emitted[len(env.emitted)-1]; last.Status != events.StatusFailed || !strings.Contains(last.Error, "db 15 -> 16 is a major update") {
		t.Errorf("last event = %+v, want failed naming the major update", last)
	}
}

func TestUpdateBeyondPatch_UnparsedComposeIsUnknown(t *testing.T) {
	if got := updateBeyondPatch(nil, serviceImageByName{"app": "app:1"}); !strings.Contains(got, "could not be parsed") {
		t.Errorf("updateBeyondPatch(nil) = %q, want the unparsed reason", got)
	}
}
