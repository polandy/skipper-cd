package orphans

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

// fakeOutputter returns canned output/err and records the argv it was called
// with, so tests assert the exact docker query.
type fakeOutputter struct {
	out  []byte
	err  error
	name string
	args []string
	call int
}

func (f *fakeOutputter) Output(_ context.Context, _ string, name string, args ...string) ([]byte, error) {
	f.call++
	f.name = name
	f.args = args
	return f.out, f.err
}

func TestParseProjects_GroupsContainersByProjectWithDetail(t *testing.T) {
	out := []byte(
		"web\t/repo/stacks/web\tweb-1\tweb\tnginx:1.25\trunning\tUp 3 days\n" +
			"web\t/repo/stacks/web\tweb-db-1\tdb\tpostgres:16\texited\tExited (0) 1h ago\n" +
			"media\t/repo/stacks/media\tmedia-1\tmedia\tjellyfin:10\trunning\tUp 2 hours\n")
	got := parseProjects(out)

	want := []Project{
		{Name: "web", WorkingDir: "/repo/stacks/web", Containers: []Container{
			{Name: "web-1", Service: "web", Image: "nginx:1.25", State: "running", Status: "Up 3 days"},
			{Name: "web-db-1", Service: "db", Image: "postgres:16", State: "exited", Status: "Exited (0) 1h ago"},
		}},
		{Name: "media", WorkingDir: "/repo/stacks/media", Containers: []Container{
			{Name: "media-1", Service: "media", Image: "jellyfin:10", State: "running", Status: "Up 2 hours"},
		}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestParseProjects_SkipsBlankAndUnlabeledLines(t *testing.T) {
	out := []byte("\n\t/orphan/dir\tc1\t\t\t\t\nweb\t/repo/stacks/web\tweb-1\tweb\tnginx\trunning\tUp\n\n")
	got := parseProjects(out)

	want := []Project{{Name: "web", WorkingDir: "/repo/stacks/web", Containers: []Container{
		{Name: "web-1", Service: "web", Image: "nginx", State: "running", Status: "Up"},
	}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestParseProjects_ToleratesShortLinesFromOlderDocker(t *testing.T) {
	// A docker without some template field yields fewer columns; the missing
	// fields are empty, not a panic.
	out := []byte("web\t/repo/stacks/web\tweb-1\n")
	got := parseProjects(out)

	want := []Project{{Name: "web", WorkingDir: "/repo/stacks/web", Containers: []Container{{Name: "web-1"}}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestDetect_QueriesDockerAndClassifies(t *testing.T) {
	out := &fakeOutputter{out: []byte("old\t/repo/stacks/old\nweb\t/repo/stacks/web\n")}
	var published Snapshot
	d := New(Config{
		Outputter: out,
		Managed: func() Managed {
			return Managed{BaseDir: "/repo/stacks", ActiveDirs: map[string]bool{"/repo/stacks/web": true}}
		},
		Publish: func(s Snapshot) { published = s },
	})

	got := d.Detect(context.Background())

	if out.name != "docker" || !reflect.DeepEqual(out.args, psArgs) {
		t.Fatalf("unexpected argv: %s %v", out.name, out.args)
	}
	if len(got.Orphans) != 1 || got.Orphans[0].Project != "old" || !got.Orphans[0].Prunable {
		t.Fatalf("expected old classified as prunable orphan, got %+v", got.Orphans)
	}
	if !reflect.DeepEqual(published, got) {
		t.Fatalf("published snapshot %+v != returned %+v", published, got)
	}
	if !reflect.DeepEqual(d.Current(), got) {
		t.Fatalf("Current() %+v != returned %+v", d.Current(), got)
	}
}

func TestDetect_DockerErrorKeepsLastSnapshot(t *testing.T) {
	out := &fakeOutputter{out: []byte("old\t/repo/stacks/old\n")}
	managed := Managed{BaseDir: "/repo/stacks"}
	d := New(Config{Outputter: out, Managed: func() Managed { return managed }})

	first := d.Detect(context.Background())
	if len(first.Orphans) != 1 {
		t.Fatalf("expected one orphan on first detect, got %+v", first.Orphans)
	}

	out.err = errors.New("docker daemon down")
	second := d.Detect(context.Background())

	if !reflect.DeepEqual(second, first) {
		t.Fatalf("a docker error must return the last snapshot, got %+v", second)
	}
	if !reflect.DeepEqual(d.Current(), first) {
		t.Fatalf("a docker error must not clear Current(), got %+v", d.Current())
	}
}

func TestDetect_PublishesEvenWhenNoOrphans(t *testing.T) {
	out := &fakeOutputter{out: []byte("web\t/repo/stacks/web\n")}
	published := false
	d := New(Config{
		Outputter: out,
		Managed: func() Managed {
			return Managed{BaseDir: "/repo/stacks", ActiveDirs: map[string]bool{"/repo/stacks/web": true}}
		},
		Publish: func(Snapshot) { published = true },
	})

	got := d.Detect(context.Background())
	if len(got.Orphans) != 0 {
		t.Fatalf("expected no orphans, got %+v", got.Orphans)
	}
	if !published {
		t.Fatal("expected a snapshot to be published even with no orphans (to clear a stale section)")
	}
}
