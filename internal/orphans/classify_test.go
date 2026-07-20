package orphans

import (
	"reflect"
	"testing"
)

// cs builds a container slice with the given names, for terse test inputs.
func cs(names ...string) []Container {
	out := make([]Container, len(names))
	for i, n := range names {
		out[i] = Container{Name: n}
	}
	return out
}

func TestClassify_ManagedProjectsAreNotSurfaced(t *testing.T) {
	m := Managed{
		BaseDir:    "/repo/stacks",
		ActiveDirs: map[string]bool{"/repo/stacks/web": true},
	}
	got := Classify([]Project{
		{Name: "web", WorkingDir: "/repo/stacks/web", Containers: cs("web-1", "web-2")},
	}, m)

	if len(got.Orphans) != 0 {
		t.Fatalf("expected no orphans for a managed project, got %+v", got.Orphans)
	}
}

func TestClassify_DisabledProjectIsManagedNotOrphan(t *testing.T) {
	m := Managed{
		BaseDir:      "/repo/stacks",
		DisabledDirs: map[string]bool{"/repo/stacks/media": true},
	}
	got := Classify([]Project{
		{Name: "media", WorkingDir: "/repo/stacks/media", Containers: cs("media-1")},
	}, m)

	if len(got.Orphans) != 0 {
		t.Fatalf("disabled stack is hands-off, must not be an orphan: %+v", got.Orphans)
	}
}

func TestClassify_RemovedStackUnderBaseDirIsOrphanedAndCarriesContainers(t *testing.T) {
	m := Managed{
		BaseDir:    "/repo/stacks",
		ActiveDirs: map[string]bool{"/repo/stacks/web": true},
	}
	old := []Container{
		{Name: "old-app-1", Service: "app", Image: "nginx:1.25", State: "running", Status: "Up 3 days"},
		{Name: "old-db-1", Service: "db", Image: "postgres:16", State: "exited", Status: "Exited (0) 1h ago"},
	}
	got := Classify([]Project{
		{Name: "web", WorkingDir: "/repo/stacks/web", Containers: cs("web-1")},
		{Name: "old", WorkingDir: "/repo/stacks/old", Containers: old},
	}, m)

	want := []Orphan{{
		Project:    "old",
		Class:      Orphaned,
		WorkingDir: "/repo/stacks/old",
		Containers: old,
		Prunable:   true,
	}}
	if !reflect.DeepEqual(got.Orphans, want) {
		t.Fatalf("got %+v, want %+v", got.Orphans, want)
	}
}

func TestClassify_ProjectOutsideBaseDirIsUnmanagedNeverPrunable(t *testing.T) {
	m := Managed{BaseDir: "/repo/stacks"}
	got := Classify([]Project{
		{Name: "some-app", WorkingDir: "/opt/some-app", Containers: cs("some-app-1")},
	}, m)

	want := []Orphan{{
		Project:    "some-app",
		Class:      Unmanaged,
		WorkingDir: "/opt/some-app",
		Containers: cs("some-app-1"),
		Prunable:   false,
	}}
	if !reflect.DeepEqual(got.Orphans, want) {
		t.Fatalf("got %+v, want %+v", got.Orphans, want)
	}
}

func TestClassify_RemovedStackWithProjectDirOutsideBaseIsOrphanedViaState(t *testing.T) {
	// A stack whose project_directory pointed outside stacks_base_dir is still
	// recognized as formerly managed through its recorded state project dir.
	m := Managed{
		BaseDir:   "/repo/stacks",
		StateDirs: map[string]string{"legacy": "/srv/legacy"},
	}
	got := Classify([]Project{
		{Name: "legacy", WorkingDir: "/srv/legacy", Containers: cs("legacy-1")},
	}, m)

	if len(got.Orphans) != 1 || got.Orphans[0].Class != Orphaned || !got.Orphans[0].Prunable {
		t.Fatalf("expected orphaned+prunable via state dir, got %+v", got.Orphans)
	}
	if got.Orphans[0].StateOnly {
		t.Fatalf("a running project must not be marked state-only: %+v", got.Orphans[0])
	}
}

func TestClassify_StaleStateEntryWithNothingRunningIsStateOnlyOrphan(t *testing.T) {
	m := Managed{
		BaseDir:    "/repo/stacks",
		ActiveDirs: map[string]bool{"/repo/stacks/web": true},
		StateDirs:  map[string]string{"web": "/repo/stacks/web", "gone": "/repo/stacks/gone"},
	}
	got := Classify(nil, m)

	want := []Orphan{{
		Project:    "gone",
		Class:      Orphaned,
		WorkingDir: "/repo/stacks/gone",
		Prunable:   true,
		StateOnly:  true,
	}}
	if !reflect.DeepEqual(got.Orphans, want) {
		t.Fatalf("got %+v, want %+v", got.Orphans, want)
	}
}

func TestClassify_StaleStateNotDuplicatedWhenProjectStillRunning(t *testing.T) {
	// The removed stack is both running (surfaced from the project list) and in
	// state; it must appear once, not twice.
	m := Managed{
		BaseDir:   "/repo/stacks",
		StateDirs: map[string]string{"old": "/repo/stacks/old"},
	}
	got := Classify([]Project{
		{Name: "old", WorkingDir: "/repo/stacks/old", Containers: cs("old-1")},
	}, m)

	if len(got.Orphans) != 1 {
		t.Fatalf("expected exactly one orphan, got %+v", got.Orphans)
	}
	if got.Orphans[0].StateOnly {
		t.Fatalf("running orphan must not be state-only: %+v", got.Orphans[0])
	}
}

func TestClassify_ActiveStackInStateIsNotAStaleOrphan(t *testing.T) {
	m := Managed{
		BaseDir:    "/repo/stacks",
		ActiveDirs: map[string]bool{"/repo/stacks/web": true},
		StateDirs:  map[string]string{"web": "/repo/stacks/web"},
	}
	got := Classify(nil, m)
	if len(got.Orphans) != 0 {
		t.Fatalf("active stack recorded in state must not be an orphan: %+v", got.Orphans)
	}
}

func TestClassify_SortsByProjectName(t *testing.T) {
	m := Managed{BaseDir: "/repo/stacks"}
	got := Classify([]Project{
		{Name: "zeta", WorkingDir: "/repo/stacks/zeta", Containers: cs("z-1")},
		{Name: "alpha", WorkingDir: "/repo/stacks/alpha", Containers: cs("a-1")},
	}, m)

	if len(got.Orphans) != 2 || got.Orphans[0].Project != "alpha" || got.Orphans[1].Project != "zeta" {
		t.Fatalf("expected sorted by project name, got %+v", got.Orphans)
	}
}
