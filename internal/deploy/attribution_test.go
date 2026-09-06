package deploy

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/polandy/skipper-cd/internal/config"
	"github.com/polandy/skipper-cd/internal/events"
)

// The case the feature exists for: a backup sidecar gains two environment
// variables, which redeploys the whole stack — the row must be able to say only
// the sidecar moved.
func TestDiffComposeServices_NamesOnlyTheServiceWhoseBlockChanged(t *testing.T) {
	before := `services:
  app:
    image: mealie:3.4.0
  mealie-restic:
    image: restic:1.18
    environment:
      - BACKUP_CRON=0 4 * * *
`
	after := `services:
  app:
    image: mealie:3.4.0
  mealie-restic:
    image: restic:1.18
    environment:
      - BACKUP_CRON=0 4 * * *
      - CHECK_CRON=0 12 9 * *
      - RESTIC_DATA_SUBSET=2G
`

	names, ok := diffComposeServices([]byte(before), []byte(after))

	if !ok {
		t.Fatal("expected the change to be attributable to service blocks")
	}
	if !slices.Equal(names, []string{"mealie-restic"}) {
		t.Errorf("expected only mealie-restic, got %v", names)
	}
}

func TestDiffComposeServices_ReportsAddedAndRemovedServices(t *testing.T) {
	before := "services:\n  app:\n    image: nginx:1.25\n  old:\n    image: redis:7\n"
	after := "services:\n  app:\n    image: nginx:1.25\n  new:\n    image: valkey:8\n"

	names, ok := diffComposeServices([]byte(before), []byte(after))

	if !ok {
		t.Fatal("expected the change to be attributable to service blocks")
	}
	if !slices.Equal(names, []string{"new", "old"}) {
		t.Errorf("expected the added and the removed service, got %v", names)
	}
}

func TestDiffComposeServices_IgnoresAnUnchangedFile(t *testing.T) {
	compose := "services:\n  app:\n    image: nginx:1.25\n"

	names, ok := diffComposeServices([]byte(compose), []byte(compose))

	if !ok {
		t.Fatal("expected an unchanged file to stay attributable")
	}
	if len(names) != 0 {
		t.Errorf("expected no service named for an unchanged file, got %v", names)
	}
}

// A change outside the service blocks reaches services this comparison cannot
// name — an anchor a service only aliases is the classic one — so it must
// report unattributable rather than name nothing at all.
func TestDiffComposeServices_UnattributableWhenSomethingOutsideTheServicesChanged(t *testing.T) {
	before := `x-common: &common
  restart: unless-stopped
services:
  app:
    <<: *common
    image: nginx:1.25
`
	after := `x-common: &common
  restart: always
services:
  app:
    <<: *common
    image: nginx:1.25
`

	names, ok := diffComposeServices([]byte(before), []byte(after))

	if ok {
		t.Fatalf("expected a change to a shared anchor to be unattributable, got %v", names)
	}
	if names != nil {
		t.Errorf("expected no service names for an unattributable change, got %v", names)
	}
}

func TestDiffComposeServices_UnattributableWhenARevisionDoesNotParse(t *testing.T) {
	valid := "services:\n  app:\n    image: nginx:1.25\n"

	if _, ok := diffComposeServices([]byte("services: [unterminated"), []byte(valid)); ok {
		t.Error("expected an unparseable previous revision to be unattributable")
	}
	if _, ok := diffComposeServices([]byte(valid), []byte("services: [unterminated")); ok {
		t.Error("expected an unparseable current revision to be unattributable")
	}
}

// attributionFixture builds a stack whose tracked inputs cover every change
// kind, and returns the deployer, the attribution value and the paths by kind.
type attributionFixture struct {
	deployer    *Deployer
	att         attribution
	composePath string
	dockerfile  string
	envFile     string
	varsFile    string
	watchedFile string
	configKey   string
}

func newAttributionFixture(t *testing.T, previousCompose string) attributionFixture {
	t.Helper()
	repoDir := t.TempDir()
	baseDir := filepath.Join(repoDir, "stacks")
	stackDir := filepath.Join(baseDir, "web")
	watchDir := filepath.Join(stackDir, "conf")
	if err := os.MkdirAll(watchDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	composePath := filepath.Join(stackDir, "docker-compose.yml")
	writeFile(t, composePath, "services:\n  app:\n    image: nginx:1.26\n  builder:\n    build:\n      context: .\n      dockerfile: Dockerfile\n")
	dockerfile := filepath.Join(stackDir, "Dockerfile")
	writeFile(t, dockerfile, "FROM alpine\n")
	envFile := filepath.Join(stackDir, ".env")
	writeFile(t, envFile, "TOKEN=1\n")
	varsFile := filepath.Join(repoDir, "vars.env")
	writeFile(t, varsFile, "DOMAIN=example.com\n")
	watchedFile := filepath.Join(watchDir, "nginx.conf")
	writeFile(t, watchedFile, "server {}\n")

	cf, err := parseComposeFile(composePath)
	if err != nil {
		t.Fatalf("parse compose: %v", err)
	}

	cr := &fakeCommitReader{files: map[string][]byte{}}
	if previousCompose != "" {
		cr.files["old-sha:"+composePath] = []byte(previousCompose)
	}
	d := New(Config{Runner: &recordingRunner{}, CommitReader: cr, RepoDir: repoDir, StateDir: t.TempDir()})

	return attributionFixture{
		deployer: d,
		att: attribution{
			composePath: composePath,
			compose:     cf,
			stack: config.Stack{
				Name:      "web",
				EnvFiles:  []string{envFile},
				WatchDirs: []string{watchDir},
			},
			varsFile:      varsFile,
			stacksBaseDir: baseDir,
		},
		composePath: composePath,
		dockerfile:  dockerfile,
		envFile:     envFile,
		varsFile:    varsFile,
		watchedFile: watchedFile,
		configKey:   filepath.Join(baseDir, config.RepoConfigFileName),
	}
}

func attributedKinds(changes []events.FileChange) map[string]events.FileChange {
	byKind := make(map[string]events.FileChange, len(changes))
	for _, c := range changes {
		byKind[c.Kind] = c
	}
	return byKind
}

func TestAttributeChanges_ClassifiesEveryTrackedInput(t *testing.T) {
	f := newAttributionFixture(t, "services:\n  app:\n    image: nginx:1.25\n  builder:\n    build:\n      context: .\n      dockerfile: Dockerfile\n")

	changes := f.deployer.attributeChanges(context.Background(), f.att,
		[]string{f.composePath, f.dockerfile, f.envFile, f.varsFile, f.watchedFile, f.configKey}, "old-sha")

	if len(changes) != 6 {
		t.Fatalf("expected one entry per changed file, got %d: %+v", len(changes), changes)
	}
	byKind := attributedKinds(changes)
	for _, kind := range []string{
		events.ChangeKindCompose, events.ChangeKindBuild, events.ChangeKindEnv,
		events.ChangeKindVars, events.ChangeKindWatch, events.ChangeKindConfig,
	} {
		if _, ok := byKind[kind]; !ok {
			t.Errorf("no changed file classified as %q: %+v", kind, changes)
		}
	}
	// Only the app service's image moved between the two revisions.
	if got := byKind[events.ChangeKindCompose].Services; !slices.Equal(got, []string{"app"}) {
		t.Errorf("expected the compose change attributed to app, got %v", got)
	}
	// The Dockerfile is the build: service's own input.
	if got := byKind[events.ChangeKindBuild].Services; !slices.Equal(got, []string{"builder"}) {
		t.Errorf("expected the Dockerfile attributed to builder, got %v", got)
	}
	// Project-wide inputs reach every service and are reported stack-wide.
	for _, kind := range []string{events.ChangeKindEnv, events.ChangeKindVars, events.ChangeKindWatch, events.ChangeKindConfig} {
		if got := byKind[kind]; got.Services != nil || !got.Wide {
			t.Errorf("expected %q reported stack-wide, got %+v", kind, got)
		}
	}
	// An attributed file is not stack-wide: the two states must not overlap.
	if byKind[events.ChangeKindCompose].Wide || byKind[events.ChangeKindBuild].Wide {
		t.Error("expected an attributed change not to be marked stack-wide")
	}
	// Paths are repo-relative, like ChangedFiles — the UI has no clone dir.
	if got := byKind[events.ChangeKindCompose].File; got != "stacks/web/docker-compose.yml" {
		t.Errorf("expected a repo-relative path, got %q", got)
	}
}

func TestAttributeChanges_ReportsComposeStackWideWithoutAPreviousCommit(t *testing.T) {
	f := newAttributionFixture(t, "")

	changes := f.deployer.attributeChanges(context.Background(), f.att, []string{f.composePath}, "")

	if len(changes) != 1 {
		t.Fatalf("expected one entry, got %+v", changes)
	}
	if changes[0].Kind != events.ChangeKindCompose {
		t.Errorf("expected the compose kind, got %q", changes[0].Kind)
	}
	if changes[0].Services != nil || !changes[0].Wide {
		t.Errorf("expected a bootstrap compose change reported stack-wide, got %+v", changes[0])
	}
}

func TestAttributeChanges_ReportsComposeStackWideWhenThePreviousRevisionIsUnreadable(t *testing.T) {
	f := newAttributionFixture(t, "") // no canned content: FileAtCommit errors

	changes := f.deployer.attributeChanges(context.Background(), f.att, []string{f.composePath}, "old-sha")

	if len(changes) != 1 || changes[0].Services != nil || !changes[0].Wide {
		t.Fatalf("expected the compose change reported stack-wide, got %+v", changes)
	}
}

func TestAttributeChanges_ReturnsNothingForAnEmptyChangeSet(t *testing.T) {
	f := newAttributionFixture(t, "")

	if changes := f.deployer.attributeChanges(context.Background(), f.att, nil, "old-sha"); changes != nil {
		t.Errorf("expected no attribution for an empty change set, got %+v", changes)
	}
}

// A Dockerfile shared by two build: services rebuilds both, so both are named.
func TestDockerfileServices_NamesEveryServiceBuildingFromTheFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "docker-compose.yml")
	writeFile(t, path, "services:\n  api:\n    build:\n      context: .\n      dockerfile: Dockerfile\n  worker:\n    build: .\n  web:\n    image: nginx:1.26\n")
	cf, err := parseComposeFile(path)
	if err != nil {
		t.Fatalf("parse compose: %v", err)
	}

	byPath := cf.dockerfileServices(dir)

	got := byPath[filepath.Join(dir, "Dockerfile")]
	if !slices.Equal(got, []string{"api", "worker"}) {
		t.Errorf("expected both build services, got %v", got)
	}
	if len(byPath) != 1 {
		t.Errorf("expected only the one Dockerfile, got %+v", byPath)
	}
}

// The whole point of attribution is what a terminal event carries, so assert it
// through a real deploy rather than only through the helper.
func TestDeployStack_SuccessEventAttributesTheChangeToItsService(t *testing.T) {
	repoDir := t.TempDir()
	stackDir := filepath.Join(repoDir, "mealie")
	if err := os.MkdirAll(stackDir, 0o755); err != nil {
		t.Fatal(err)
	}
	composePath := filepath.Join(stackDir, "docker-compose.yml")
	writeFile(t, composePath, "services:\n  app:\n    image: mealie:3.4.0\n  mealie-restic:\n    image: restic:1.18\n    environment:\n      - CHECK_CRON=0 12 9 * *\n")

	cr := &fakeCommitReader{files: map[string][]byte{
		"old-sha:" + composePath: []byte("services:\n  app:\n    image: mealie:3.4.0\n  mealie-restic:\n    image: restic:1.18\n"),
	}}
	var success *events.DeployEvent
	d := New(Config{Runner: &recordingRunner{}, CommitReader: cr, RepoDir: repoDir, StateDir: t.TempDir(),
		EventSink: func(e events.DeployEvent) {
			if e.Status == events.StatusSuccess {
				success = &e
			}
		}})

	state := newEmptyState()
	state.LastDeployedCommit = "old-sha"
	if err := d.deployStackIfChanged(context.Background(), config.Stack{Name: "mealie"}, repoDir, "", nil, state); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if success == nil {
		t.Fatal("expected a success event")
	}
	if len(success.FileChanges) != 1 {
		t.Fatalf("expected one attributed file, got %+v", success.FileChanges)
	}
	got := success.FileChanges[0]
	if got.Kind != events.ChangeKindCompose || !slices.Equal(got.Services, []string{"mealie-restic"}) {
		t.Errorf("expected the compose change attributed to mealie-restic, got %+v", got)
	}
	if got.File != "mealie/docker-compose.yml" {
		t.Errorf("expected a repo-relative path, got %q", got.File)
	}
}

// A comment-only edit changes the file but no service definition: the row must
// not blame a container, and must not claim the change reached every one either.
func TestAttributeChanges_ComposeCommentEditNamesNoServiceAndIsNotStackWide(t *testing.T) {
	f := newAttributionFixture(t, "services:\n  app:\n    image: nginx:1.26\n  builder:\n    build:\n      context: .\n      dockerfile: Dockerfile\n# routine converge\n")

	changes := f.deployer.attributeChanges(context.Background(), f.att, []string{f.composePath}, "old-sha")

	if len(changes) != 1 {
		t.Fatalf("expected one entry, got %+v", changes)
	}
	if len(changes[0].Services) != 0 || changes[0].Wide {
		t.Errorf("expected a comment-only change attributed to nothing, got %+v", changes[0])
	}
}

// The change set comes from a map walk, so the attribution must impose an order
// of its own: an unordered one would rewrite the persisted history on every
// identical run.
func TestAttributeChanges_IsOrderedByFile(t *testing.T) {
	f := newAttributionFixture(t, "")

	changes := f.deployer.attributeChanges(context.Background(), f.att,
		[]string{f.watchedFile, f.composePath, f.varsFile, f.envFile}, "old-sha")

	for i := 1; i < len(changes); i++ {
		if changes[i-1].File > changes[i].File {
			t.Fatalf("expected the attribution ordered by file, got %+v", changes)
		}
	}
}
