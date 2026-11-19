//go:build e2e

package e2e

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// skipperBin is the path to the skipper binary built once for the whole
// package by TestMain.
var skipperBin string

// TestMain builds the real skipper binary once and shares it across tests.
func TestMain(m *testing.M) {
	os.Exit(run(m))
}

// run is TestMain's body so the deferred removal of the binary dir runs on every
// exit path, a panicking test included.
func run(m *testing.M) int {
	dir, err := os.MkdirTemp("", "skipper-e2e-bin-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "e2e: create temp bin dir: %v\n", err)
		return 1
	}
	defer os.RemoveAll(dir)

	skipperBin = filepath.Join(dir, "skipper")
	build := exec.Command("go", "build", "-o", skipperBin, "./cmd/skipper")
	build.Dir = repoRoot()
	if out, err := build.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "e2e: build skipper failed: %v\n%s", err, out)
		return 1
	}
	return m.Run()
}

// repoRoot returns the repository root, derived from this file's location
// (<root>/e2e/main_test.go) so it is independent of the working directory.
func repoRoot() string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		panic("e2e: cannot determine caller for repoRoot")
	}
	return filepath.Dir(filepath.Dir(file))
}
