package orphans

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

// fakeOutputter answers the two docker reads Detect makes — `ps` from out and
// `volume ls` from volOut — and records the ps argv so tests assert the query.
type fakeOutputter struct {
	out    []byte
	volOut []byte
	err    error
	name   string
	args   []string
	call   int
}

func (f *fakeOutputter) Output(_ context.Context, _ string, name string, args ...string) ([]byte, error) {
	f.call++
	f.name = name
	if len(args) > 0 && args[0] == "volume" {
		return f.volOut, f.err
	}
	f.args = args
	return f.out, f.err
}

// psLine builds one tab-separated docker ps output line in psColumns order.
func psLine(project, dir, config, name, service, image, state, status, ports string) string {
	return strings.Join([]string{project, dir, config, name, service, image, state, status, ports}, "\t") + "\n"
}

func TestParseProjects_GroupsContainersByProjectWithDetail(t *testing.T) {
	out := []byte(
		psLine("web", "/repo/stacks/web", "/repo/stacks/web/docker-compose.yml", "web-1", "web", "nginx:1.25", "running", "Up 3 days", "0.0.0.0:80->80/tcp") +
			psLine("web", "/repo/stacks/web", "/repo/stacks/web/docker-compose.yml", "web-db-1", "db", "postgres:16", "exited", "Exited (0) 1h ago", "") +
			psLine("media", "/repo/stacks/media", "", "media-1", "media", "jellyfin:10", "running", "Up 2 hours", ""))
	got := parseProjects(out)

	want := []Project{
		{Name: "web", WorkingDir: "/repo/stacks/web", ConfigFile: "/repo/stacks/web/docker-compose.yml", Containers: []Container{
			{Name: "web-1", Service: "web", Image: "nginx:1.25", State: "running", Status: "Up 3 days", Ports: "0.0.0.0:80->80/tcp"},
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
	out := []byte("\n" + psLine("", "/orphan/dir", "", "c1", "", "", "", "", "") +
		psLine("web", "/repo/stacks/web", "", "web-1", "web", "nginx", "running", "Up", "") + "\n")
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
	out := []byte("web\t/repo/stacks/web\t\tweb-1\n")
	got := parseProjects(out)

	want := []Project{{Name: "web", WorkingDir: "/repo/stacks/web", Containers: []Container{{Name: "web-1"}}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestParseVolumes_GroupsByProjectSkippingUnlabeled(t *testing.T) {
	out := []byte("web\tweb_data\nweb\tweb_cache\n\tstray_volume\nold\told_data\n")
	got := parseVolumes(out)

	want := map[string][]string{
		"web": {"web_data", "web_cache"},
		"old": {"old_data"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestDetect_AttachesConfigFileAndVolumes(t *testing.T) {
	out := &fakeOutputter{
		out:    []byte(psLine("old", "/repo/stacks/old", "/repo/stacks/old/docker-compose.yml", "old-1", "app", "nginx", "running", "Up 1 day", "")),
		volOut: []byte("old\told_data\nold\told_config\n"),
	}
	d := New(Config{
		Outputter: out,
		Managed:   func() Managed { return Managed{BaseDir: "/repo/stacks"} },
	})

	got := d.Detect(context.Background())
	if len(got.Orphans) != 1 {
		t.Fatalf("expected one orphan, got %+v", got.Orphans)
	}
	o := got.Orphans[0]
	if o.ConfigFile != "/repo/stacks/old/docker-compose.yml" {
		t.Errorf("expected config file on orphan, got %q", o.ConfigFile)
	}
	if !reflect.DeepEqual(o.Volumes, []string{"old_data", "old_config"}) {
		t.Errorf("expected volumes attached, got %v", o.Volumes)
	}
}

func TestDetect_VolumeQueryFailureStillClassifies(t *testing.T) {
	// A volume-listing failure must not blank detection; the orphan still shows,
	// just without volumes. The ps read succeeds, only volume ls errors.
	out := &failingVolumeOutputter{ps: []byte(psLine("old", "/repo/stacks/old", "", "old-1", "app", "nginx", "running", "Up", ""))}
	d := New(Config{Outputter: out, Managed: func() Managed { return Managed{BaseDir: "/repo/stacks"} }})

	got := d.Detect(context.Background())
	if len(got.Orphans) != 1 || got.Orphans[0].Volumes != nil {
		t.Fatalf("expected one orphan with no volumes, got %+v", got.Orphans)
	}
}

// failingVolumeOutputter answers ps but errors on volume ls.
type failingVolumeOutputter struct{ ps []byte }

func (f *failingVolumeOutputter) Output(_ context.Context, _ string, _ string, args ...string) ([]byte, error) {
	if len(args) > 0 && args[0] == "volume" {
		return nil, errors.New("volume ls failed")
	}
	return f.ps, nil
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
