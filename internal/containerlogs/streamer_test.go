package containerlogs

import (
	"context"
	"os/exec"
	"sync"
	"testing"
	"time"
)

// These tests run a real child process — the same deliberate exception the
// internal/command tests make: faking exec at this boundary would test nothing.

func TestExecStreamer_ScansLines(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available")
	}
	var mu sync.Mutex
	var got []string
	err := ExecStreamer{}.Stream(context.Background(), "", nil, "sh",
		[]string{"-c", "printf 'a\\nb\\nc\\n'"},
		func(l string) { mu.Lock(); got = append(got, l); mu.Unlock() })
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if len(got) != 3 || got[0] != "a" || got[1] != "b" || got[2] != "c" {
		t.Errorf("lines = %v, want [a b c]", got)
	}
}

func TestExecStreamer_MergesStderr(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available")
	}
	var mu sync.Mutex
	seen := map[string]bool{}
	err := ExecStreamer{}.Stream(context.Background(), "", nil, "sh",
		[]string{"-c", "printf 'out\\n'; printf 'err\\n' 1>&2"},
		func(l string) { mu.Lock(); seen[l] = true; mu.Unlock() })
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if !seen["out"] || !seen["err"] {
		t.Errorf("want both stdout and stderr lines, got %v", seen)
	}
}

func TestExecStreamer_ContextCancelStops(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available")
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- ExecStreamer{}.Stream(ctx, "", nil, "sh",
			[]string{"-c", "sleep 30"}, func(string) {})
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("stream did not stop promptly after context cancel")
	}
}
