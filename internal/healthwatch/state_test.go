package healthwatch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/polandy/skipper-cd/internal/health"
)

func TestState_RoundTripsPhasesAndDeploys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "healthwatch.yaml")
	want := newState()
	want.stacks["app"] = &stackState{
		LastDeploy: &deployRecord{Commit: "a1b2c3d", At: time.Date(2026, 7, 16, 14, 3, 10, 0, time.UTC)},
		Services: map[string][]Phase{
			"app": {
				{Status: health.Unhealthy, Since: time.Date(2026, 7, 16, 15, 47, 5, 0, time.UTC), Commit: "a1b2c3d"},
				{Status: health.Healthy, Since: time.Date(2026, 7, 16, 9, 12, 0, 0, time.UTC)},
			},
		},
	}

	if err := want.save(path); err != nil {
		t.Fatal(err)
	}
	got := loadState(path)

	phases := got.stacks["app"].Services["app"]
	if len(phases) != 2 || phases[0].Status != health.Unhealthy || phases[0].Commit != "a1b2c3d" {
		t.Fatalf("phases did not round-trip: %+v", phases)
	}
	if !phases[1].Since.Equal(time.Date(2026, 7, 16, 9, 12, 0, 0, time.UTC)) {
		t.Errorf("since did not round-trip: %v", phases[1].Since)
	}
	ld := got.stacks["app"].LastDeploy
	if ld == nil || ld.Commit != "a1b2c3d" || !ld.At.Equal(want.stacks["app"].LastDeploy.At) {
		t.Fatalf("last deploy did not round-trip: %+v", ld)
	}
}

func TestState_TimesArePersistedAsUTCSeconds(t *testing.T) {
	path := filepath.Join(t.TempDir(), "healthwatch.yaml")
	zurich := time.FixedZone("CEST", 2*60*60)
	s := newState()
	s.stacks["app"] = &stackState{Services: map[string][]Phase{
		"app": {{Status: health.Healthy, Since: time.Date(2026, 7, 16, 17, 47, 5, 123456789, zurich)}},
	}}

	if err := s.save(path); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "2026-07-16T15:47:05Z") {
		t.Fatalf("expected second-granular UTC timestamp on disk, got:\n%s", raw)
	}
}

func TestState_MissingFileIsEmpty(t *testing.T) {
	got := loadState(filepath.Join(t.TempDir(), "nope.yaml"))
	if len(got.stacks) != 0 {
		t.Fatalf("expected empty state, got %+v", got.stacks)
	}
}

func TestState_CorruptFileIsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "healthwatch.yaml")
	if err := os.WriteFile(path, []byte("{nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := loadState(path)
	if len(got.stacks) != 0 {
		t.Fatalf("expected clean slate on corrupt file, got %+v", got.stacks)
	}
}
