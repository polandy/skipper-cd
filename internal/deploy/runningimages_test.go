package deploy

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"

	"github.com/polandy/skipper-cd/internal/compose"
	"github.com/polandy/skipper-cd/internal/config"
	"github.com/polandy/skipper-cd/internal/events"
)

// errFakeOutput stands in for a failing docker read.
var errFakeOutput = errors.New("docker unavailable")

// The fixtures below are verbatim shapes from a real host (Docker Compose
// 5.1.4), trimmed to the fields skipper reads. They are the reason this read
// takes two calls: `images` reports no Service at all, and its Tag is empty for
// every digest-pinned image — only `ps` knows the service and the reference.
const (
	realPSJSON = `[{"Command":"\"/entrypoint.sh\"","Health":"healthy","ID":"098172bde26c","Image":"traefik:v3.7.9@sha256:652929a140a32d7cafafb13c6cdfab5376cfeff800f51397b87b524501ed02a8","Name":"traefik","Service":"app","State":"running"},
	                {"ID":"aa11bb22cc33","Image":"nextcloud:34-ghostscript","Name":"nextcloud-app","Service":"web","State":"running"}]`
	realImagesJSON = `[{"ID":"sha256:0b9520b5460c9c4d6cf0014b73bbcb64e4d7ed92b3ed9ec4536eeab4b8c7944a","ContainerName":"traefik","Repository":"traefik","Tag":"","Platform":"linux/amd64","Size":192245996},
	                   {"ID":"sha256:40c2d6f1d8f0aabbccddeeff00112233445566778899aabbccddeeff00112233","ContainerName":"nextcloud-app","Repository":"nextcloud","Tag":"","Platform":"linux/amd64","Size":1024}]`
)

// outputComposeReads answers the two reads runningImages makes; anything else
// gets empty output.
func outputComposeReads(ps, images string) func(int, []string) ([]byte, error) {
	return func(_ int, args []string) ([]byte, error) {
		switch {
		case slices.Contains(args, "ps"):
			return []byte(ps), nil
		case slices.Contains(args, "images"):
			return []byte(images), nil
		}
		return nil, nil
	}
}

func TestRunningImage(t *testing.T) {
	tests := []struct {
		name string
		ref  string
		id   string
		want string
	}{
		{
			// Already exact: appending an id would bury the tag bump the reference
			// itself reports.
			name: "digest-pinned reference is kept verbatim",
			ref:  "traefik:v3.7.9@sha256:652929a140a3",
			id:   "sha256:0b9520b5460c9c4d",
			want: "traefik:v3.7.9@sha256:652929a140a3",
		},
		{
			// The case the whole read exists for: the reference cannot move.
			name: "floating tag gains the short image id",
			ref:  "nextcloud:34-ghostscript",
			id:   "sha256:40c2d6f1d8f0aabbccdd",
			want: "nextcloud:34-ghostscript@40c2d6f1d8f0",
		},
		{
			// A docker reporting the short id must normalize to the same value as
			// one reporting the full sha256, or an upgrade reads as a rebuild.
			name: "short id form normalizes to the same value",
			ref:  "nextcloud:34-ghostscript",
			id:   "40c2d6f1d8f0",
			want: "nextcloud:34-ghostscript@40c2d6f1d8f0",
		},
		{
			name: "no id reported degrades to the reference",
			ref:  "redis:7.4",
			id:   "",
			want: "redis:7.4",
		},
		{
			name: "no reference at all",
			ref:  "",
			id:   "sha256:abc",
			want: "",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := runningImage(tc.ref, tc.id); got != tc.want {
				t.Errorf("runningImage(%q, %q) = %q, want %q", tc.ref, tc.id, got, tc.want)
			}
		})
	}
}

func TestRunningImages_JoinsServicesToImageIDs(t *testing.T) {
	runner := &recordingRunner{outputFn: outputComposeReads(realPSJSON, realImagesJSON)}
	d := New(Config{Runner: runner, Outputter: runner})

	got := d.runningImages(context.Background(), newStackRun(config.Stack{Name: "web"}, t.TempDir(), nil))

	want := serviceImageByName{
		// Digest-pinned: the reference stands on its own.
		"app": "traefik:v3.7.9@sha256:652929a140a32d7cafafb13c6cdfab5376cfeff800f51397b87b524501ed02a8",
		// Floating tag: joined to the image id via the container name.
		"web": "nextcloud:34-ghostscript@40c2d6f1d8f0",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("runningImages() = %v, want %v", got, want)
	}
	if len(runner.outputCalls) != 2 {
		t.Fatalf("expected a ps read and an images read, got %d calls", len(runner.outputCalls))
	}
}

// Compose reports no Service on `images`, so a container that `ps` does not
// account for cannot be attributed to one — it must be dropped, not guessed at
// from the container name.
func TestRunningImages_IgnoresImagesWithoutAMatchingService(t *testing.T) {
	runner := &recordingRunner{outputFn: outputComposeReads(
		`[{"Name":"web-app-1","Service":"app","Image":"nginx:1.27"}]`,
		`[{"ContainerName":"web-app-1","ID":"sha256:aaaabbbbcccc"},{"ContainerName":"stranger","ID":"sha256:ddddeeeeffff"}]`,
	)}
	d := New(Config{Runner: runner, Outputter: runner})

	got := d.runningImages(context.Background(), newStackRun(config.Stack{Name: "web"}, t.TempDir(), nil))

	want := serviceImageByName{"app": "nginx:1.27@aaaabbbbcccc"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("runningImages() = %v, want %v", got, want)
	}
}

// A service whose image id compose did not report still has its reference —
// which is all a digest-pinned stack needs.
func TestRunningImages_ServiceWithoutAnImageIDKeepsItsReference(t *testing.T) {
	runner := &recordingRunner{outputFn: outputComposeReads(
		`[{"Name":"web-app-1","Service":"app","Image":"nginx:1.27@sha256:abcd"}]`,
		`[]`,
	)}
	d := New(Config{Runner: runner, Outputter: runner})

	got := d.runningImages(context.Background(), newStackRun(config.Stack{Name: "web"}, t.TempDir(), nil))

	want := serviceImageByName{"app": "nginx:1.27@sha256:abcd"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("runningImages() = %v, want %v", got, want)
	}
}

func TestRunningImages_UnavailableReadYieldsNoKnowledge(t *testing.T) {
	newDeployer := func(fn func(int, []string) ([]byte, error)) *Deployer {
		runner := &recordingRunner{outputFn: fn}
		return New(Config{Runner: runner, Outputter: runner})
	}
	read := func(t *testing.T, d *Deployer) serviceImageByName {
		t.Helper()
		return d.runningImages(context.Background(), newStackRun(config.Stack{Name: "web"}, t.TempDir(), nil))
	}

	t.Run("no outputter wired", func(t *testing.T) {
		d := New(Config{Runner: &recordingRunner{}})
		if got := read(t, d); got != nil {
			t.Errorf("without an Outputter there is nothing to read, got %v", got)
		}
	})

	t.Run("ps read fails", func(t *testing.T) {
		d := newDeployer(func(_ int, _ []string) ([]byte, error) { return nil, errFakeOutput })
		if got := read(t, d); got != nil {
			t.Errorf("a failed read must report no knowledge, not a partial map, got %v", got)
		}
	})

	t.Run("images read fails", func(t *testing.T) {
		d := newDeployer(func(_ int, args []string) ([]byte, error) {
			if slices.Contains(args, "images") {
				return nil, errFakeOutput
			}
			return []byte(realPSJSON), nil
		})
		// Half an answer is worse than none: every floating-tag service would
		// look like it had just moved to a reference with no id.
		if got := read(t, d); got != nil {
			t.Errorf("a failed images read must report no knowledge, got %v", got)
		}
	})

	t.Run("output does not parse", func(t *testing.T) {
		d := newDeployer(func(_ int, _ []string) ([]byte, error) { return []byte("not json"), nil })
		if got := read(t, d); got != nil {
			t.Errorf("unparseable output must report no knowledge, got %v", got)
		}
	})
}

func TestRunningImageDelta_NeedsBothSides(t *testing.T) {
	current := serviceImageByName{"app": "nginx:latest@bbbb"}
	previous := serviceImageByName{"app": "nginx:latest@aaaa"}

	t.Run("both sides known", func(t *testing.T) {
		got, ok := runningImageDelta(current, previous, nil)
		if !ok {
			t.Fatal("with a baseline and a current read the running images answer")
		}
		want := []events.ServiceImageChange{{Service: "app", Old: "nginx:latest@aaaa", New: "nginx:latest@bbbb"}}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("delta = %+v, want %+v", got, want)
		}
	})

	t.Run("no baseline", func(t *testing.T) {
		// Every service would read as newly added, which says nothing about what
		// this deploy changed — the caller keeps the compose-reference delta.
		if _, ok := runningImageDelta(current, nil, nil); ok {
			t.Error("without a recorded baseline the running images must not answer")
		}
	})

	t.Run("nothing read", func(t *testing.T) {
		if _, ok := runningImageDelta(nil, previous, nil); ok {
			t.Error("without a current read the running images must not answer")
		}
	})
}

// A service can legitimately have no container — a compose profile that is not
// active, a service scaled to zero — and then `ps` does not list it even though
// the baseline does. That is not a removal, and reporting "<old> (removed)" for
// it would be a false claim. A removal is only real when the service is also
// gone from the compose file.
func TestRunningImageDelta_ServiceWithoutContainerIsNotRemoved(t *testing.T) {
	current := serviceImageByName{"app": "nginx:latest@bbbb"}
	previous := serviceImageByName{
		"app":    "nginx:latest@aaaa",
		"worker": "acme/worker:1.0@cccc",
	}
	appChange := events.ServiceImageChange{Service: "app", Old: "nginx:latest@aaaa", New: "nginx:latest@bbbb"}

	t.Run("still declared in the compose file: not removed", func(t *testing.T) {
		cf := &composeFile{}
		cf.Services = map[string]compose.Service{"app": {}, "worker": {}}
		got, ok := runningImageDelta(current, previous, cf)
		if !ok {
			t.Fatal("expected the running images to answer")
		}
		want := []events.ServiceImageChange{appChange}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("delta = %+v, want only the app change (no false removal): %+v", got, want)
		}
	})

	t.Run("gone from the compose file: reported removed", func(t *testing.T) {
		cf := &composeFile{}
		cf.Services = map[string]compose.Service{"app": {}}
		got, ok := runningImageDelta(current, previous, cf)
		if !ok {
			t.Fatal("expected the running images to answer")
		}
		want := []events.ServiceImageChange{
			appChange,
			{Service: "worker", Old: "acme/worker:1.0@cccc", New: ""},
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("delta = %+v, want the removal reported: %+v", got, want)
		}
	})

	t.Run("no compose parse: the raw delta stands", func(t *testing.T) {
		// Without the compose file there is no way to tell the two cases apart;
		// suppressing removals blindly would hide the real ones.
		got, ok := runningImageDelta(current, previous, nil)
		if !ok {
			t.Fatal("expected the running images to answer")
		}
		if len(got) != 2 {
			t.Errorf("delta = %+v, want both changes kept", got)
		}
	})
}

func TestCurrentRunningImages_SeededFromStateAndCopied(t *testing.T) {
	dir := t.TempDir()
	state := newEmptyState()
	state.recordRunningImages("web", serviceImageByName{"app": "nginx:1.27"})
	if err := saveDeployState(dir, state); err != nil {
		t.Fatal(err)
	}

	d := New(Config{StateDir: dir})
	got := d.CurrentRunningImages()
	if got["web"]["app"] != "nginx:1.27" {
		t.Fatalf("CurrentRunningImages() = %+v, want the seeded state", got)
	}

	// The returned map is a per-call copy: mutating it must not leak back.
	got["web"]["app"] = "mutated"
	if again := d.CurrentRunningImages(); again["web"]["app"] != "nginx:1.27" {
		t.Errorf("mutation leaked into the deployer's view: %+v", again)
	}
}

func TestCurrentRunningImages_EmptyWithoutState(t *testing.T) {
	d := New(Config{StateDir: t.TempDir()})
	if got := d.CurrentRunningImages(); len(got) != 0 {
		t.Errorf("CurrentRunningImages() = %+v, want empty", got)
	}
}
