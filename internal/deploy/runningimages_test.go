package deploy

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"

	"github.com/polandy/skipper-cd/internal/config"
	"github.com/polandy/skipper-cd/internal/events"
)

// errFakeOutput stands in for a failing docker read.
var errFakeOutput = errors.New("docker unavailable")

func TestImageLineIdentity(t *testing.T) {
	tests := []struct {
		name string
		line imageLine
		want string
	}{
		{
			name: "repository tag and short id",
			line: imageLine{Repository: "nginx", Tag: "1.27", ID: "a1b2c3d4e5f6"},
			want: "nginx:1.27@a1b2c3d4e5f6",
		},
		{
			// A docker reporting the full sha256 must normalize to the same value
			// as one reporting the short id, or an upgrade reads as a rebuild.
			name: "full sha256 id truncates to the short form",
			line: imageLine{Repository: "nginx", Tag: "1.27", ID: "sha256:a1b2c3d4e5f6aabbccddeeff00112233445566778899aabbccddeeff001122"},
			want: "nginx:1.27@a1b2c3d4e5f6",
		},
		{
			name: "untagged image drops the tag",
			line: imageLine{Repository: "nginx", Tag: "<none>", ID: "a1b2c3d4e5f6"},
			want: "nginx@a1b2c3d4e5f6",
		},
		{
			// Degrades to exactly the compose-reference form, so comparing an
			// id-less read against a recorded reference still works.
			name: "missing id falls back to the reference",
			line: imageLine{Repository: "nginx", Tag: "1.27"},
			want: "nginx:1.27",
		},
		{
			name: "no image at all",
			line: imageLine{Service: "app"},
			want: "",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.line.identity(); got != tc.want {
				t.Errorf("identity() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestRunningImages_ReadsIdentityPerService(t *testing.T) {
	runner := &recordingRunner{outputFn: func(_ int, _ []string) ([]byte, error) {
		return []byte(`[{"Service":"app","Repository":"nginx","Tag":"1.27","ID":"sha256:aaaabbbbcccc0000"},
		                {"Service":"cache","Repository":"redis","Tag":"7.4","ID":"ddddeeeeffff"}]`), nil
	}}
	d := New(Config{Runner: runner, Outputter: runner})

	got := d.runningImages(context.Background(), newStackRun(config.Stack{Name: "web"}, t.TempDir(), nil))

	want := serviceImageByName{"app": "nginx:1.27@aaaabbbbcccc", "cache": "redis:7.4@ddddeeeeffff"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("runningImages() = %v, want %v", got, want)
	}
	if len(runner.outputCalls) != 1 {
		t.Fatalf("expected one docker read, got %v", runner.outputCalls)
	}
	if args := runner.outputCalls[0].args; !slices.Contains(args, "images") || !slices.Contains(args, "--format") {
		t.Errorf("expected a `compose images --format json` read, got %v", args)
	}
}

func TestRunningImages_UnavailableReadYieldsNoKnowledge(t *testing.T) {
	t.Run("no outputter wired", func(t *testing.T) {
		runner := &recordingRunner{}
		d := New(Config{Runner: runner})
		if got := d.runningImages(context.Background(), newStackRun(config.Stack{Name: "web"}, t.TempDir(), nil)); got != nil {
			t.Errorf("without an Outputter there is nothing to read, got %v", got)
		}
	})

	t.Run("read fails", func(t *testing.T) {
		runner := &recordingRunner{outputFn: func(_ int, _ []string) ([]byte, error) {
			return nil, errFakeOutput
		}}
		d := New(Config{Runner: runner, Outputter: runner})
		if got := d.runningImages(context.Background(), newStackRun(config.Stack{Name: "web"}, t.TempDir(), nil)); got != nil {
			t.Errorf("a failed read must report no knowledge, not a partial map, got %v", got)
		}
	})

	t.Run("output does not parse", func(t *testing.T) {
		runner := &recordingRunner{outputFn: func(_ int, _ []string) ([]byte, error) {
			return []byte("not json"), nil
		}}
		d := New(Config{Runner: runner, Outputter: runner})
		if got := d.runningImages(context.Background(), newStackRun(config.Stack{Name: "web"}, t.TempDir(), nil)); got != nil {
			t.Errorf("unparseable output must report no knowledge, got %v", got)
		}
	})
}

func TestRunningImageDelta_NeedsBothSides(t *testing.T) {
	current := serviceImageByName{"app": "nginx:latest@bbbb"}
	previous := serviceImageByName{"app": "nginx:latest@aaaa"}

	t.Run("both sides known", func(t *testing.T) {
		got, ok := runningImageDelta(current, previous)
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
		if _, ok := runningImageDelta(current, nil); ok {
			t.Error("without a recorded baseline the running images must not answer")
		}
	})

	t.Run("nothing read", func(t *testing.T) {
		if _, ok := runningImageDelta(nil, previous); ok {
			t.Error("without a current read the running images must not answer")
		}
	})
}
