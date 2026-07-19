package applink

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

// fakeOutputter answers the two docker reads Detect makes — `ps` and
// `inspect` — distinguished by the leading arg, and records the inspect argv
// so tests can assert which container IDs were inspected.
type fakeOutputter struct {
	psOut       []byte
	psErr       error
	inspectOut  []byte
	inspectErr  error
	inspectArgs []string
	calls       int
}

func (f *fakeOutputter) Output(_ context.Context, _ string, _ string, args ...string) ([]byte, error) {
	f.calls++
	if len(args) > 0 && args[0] == "inspect" {
		f.inspectArgs = args
		return f.inspectOut, f.inspectErr
	}
	return f.psOut, f.psErr
}

func labels(pairs ...string) string {
	s := "{"
	for i := 0; i+1 < len(pairs); i += 2 {
		if i > 0 {
			s += ","
		}
		s += `"` + pairs[i] + `":"` + pairs[i+1] + `"`
	}
	return s + "}"
}

func TestDetect_SurfacesSingleHostForManagedStack(t *testing.T) {
	out := &fakeOutputter{
		psOut:      []byte("c1\t/repo/stacks/media\n"),
		inspectOut: []byte(labels("traefik.enable", "true", "traefik.http.routers.media.rule", "Host(`media.example.com`)") + "\n"),
	}
	d := New(Config{
		Outputter: out,
		Managed:   func() map[string]string { return map[string]string{"media": "/repo/stacks/media"} },
	})

	got := d.Detect(context.Background())
	want := Snapshot{Stacks: map[string][]string{"media": {"media.example.com"}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	if len(out.inspectArgs) < 1 || out.inspectArgs[len(out.inspectArgs)-1] != "c1" {
		t.Errorf("expected inspect to target container c1, got args %v", out.inspectArgs)
	}
}

func TestDetect_UnmanagedWorkingDirIsNotLeaked(t *testing.T) {
	out := &fakeOutputter{
		psOut:      []byte("c1\t/repo/stacks/rogue\n"),
		inspectOut: []byte(labels("traefik.enable", "true", "traefik.http.routers.r.rule", "Host(`rogue.example.com`)") + "\n"),
	}
	d := New(Config{
		Outputter: out,
		Managed:   func() map[string]string { return map[string]string{"media": "/repo/stacks/media"} },
	})

	got := d.Detect(context.Background())
	if len(got.Stacks) != 0 {
		t.Fatalf("expected no stacks for an unmanaged working_dir, got %+v", got.Stacks)
	}
}

func TestDetect_NoContainersYieldsEmptySnapshot(t *testing.T) {
	out := &fakeOutputter{psOut: []byte("")}
	published := 0
	d := New(Config{
		Outputter: out,
		Managed:   func() map[string]string { return map[string]string{"media": "/repo/stacks/media"} },
		Publish:   func(Snapshot) { published++ },
	})

	got := d.Detect(context.Background())
	if len(got.Stacks) != 0 {
		t.Fatalf("expected empty stacks, got %+v", got.Stacks)
	}
	if published != 1 {
		t.Errorf("expected one publish, got %d", published)
	}
	if out.inspectArgs != nil {
		t.Error("expected no inspect call when ps found no containers")
	}
}

func TestDetect_PSFailureReturnsLastSnapshot(t *testing.T) {
	out := &fakeOutputter{
		psOut:      []byte("c1\t/repo/stacks/media\n"),
		inspectOut: []byte(labels("traefik.enable", "true", "traefik.http.routers.media.rule", "Host(`media.example.com`)") + "\n"),
	}
	d := New(Config{
		Outputter: out,
		Managed:   func() map[string]string { return map[string]string{"media": "/repo/stacks/media"} },
	})
	first := d.Detect(context.Background())

	out.psErr = errors.New("docker daemon unreachable")
	got := d.Detect(context.Background())
	if !reflect.DeepEqual(got, first) {
		t.Fatalf("expected ps failure to keep the last snapshot, got %+v, want %+v", got, first)
	}
}

func TestDetect_InspectFailureReturnsLastSnapshot(t *testing.T) {
	out := &fakeOutputter{
		psOut:      []byte("c1\t/repo/stacks/media\n"),
		inspectOut: []byte(labels("traefik.enable", "true", "traefik.http.routers.media.rule", "Host(`media.example.com`)") + "\n"),
	}
	d := New(Config{
		Outputter: out,
		Managed:   func() map[string]string { return map[string]string{"media": "/repo/stacks/media"} },
	})
	first := d.Detect(context.Background())

	out.inspectErr = errors.New("no such container")
	got := d.Detect(context.Background())
	if !reflect.DeepEqual(got, first) {
		t.Fatalf("expected inspect failure to keep the last snapshot, got %+v, want %+v", got, first)
	}
}

func TestCurrent_DefaultsToEmptySnapshotBeforeFirstDetect(t *testing.T) {
	d := New(Config{Outputter: &fakeOutputter{}, Managed: func() map[string]string { return nil }})
	got := d.Current()
	want := Snapshot{Stacks: map[string][]string{}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}
