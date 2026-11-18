package deploy

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/polandy/skipper-cd/internal/regularfile"
)

func TestChangedFiles_NoneWhenHashesMatch(t *testing.T) {
	hashes := stackFileHashes{"docker-compose.yml": "abc123"}
	if got := changedFiles(hashes, hashes); len(got) != 0 {
		t.Errorf("expected no changed files, got %v", got)
	}
}

func TestChangedFiles_DetectsChangedFile(t *testing.T) {
	current := stackFileHashes{"docker-compose.yml": "newHash"}
	last := stackFileHashes{"docker-compose.yml": "oldHash"}
	changed := changedFiles(current, last)
	if len(changed) != 1 || changed[0] != "docker-compose.yml" {
		t.Errorf("expected [docker-compose.yml], got %v", changed)
	}
}

func TestChangedFiles_DetectsNewFile(t *testing.T) {
	current := stackFileHashes{"docker-compose.yml": "abc", "app.env": "def"}
	last := stackFileHashes{"docker-compose.yml": "abc"}
	changed := changedFiles(current, last)
	if len(changed) != 1 || changed[0] != "app.env" {
		t.Errorf("expected [app.env], got %v", changed)
	}
}

func TestComputePerFileHashes_ReturnsHashForEachFile(t *testing.T) {
	workDir := makeStackDir(t)
	envFile := filepath.Join(t.TempDir(), "app.env")
	writeFile(t, envFile, "KEY=value\n")

	hashes, err := computePerFileHashes(context.Background(), workDir, []string{envFile}, nil, "", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	composePath := filepath.Join(workDir, "docker-compose.yml")
	if hashes[composePath] == "" {
		t.Errorf("expected hash for docker-compose.yml")
	}
	if hashes[envFile] == "" {
		t.Errorf("expected hash for env file")
	}
}

func TestComputePerFileHashes_IncludesVarsFile(t *testing.T) {
	workDir := makeStackDir(t)
	varsFile := filepath.Join(t.TempDir(), "vars.env")
	writeFile(t, varsFile, "DOMAIN=example.com\n")

	hashes, err := computePerFileHashes(context.Background(), workDir, nil, nil, varsFile, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if hashes[varsFile] == "" {
		t.Errorf("expected hash for vars_file")
	}
}

func TestComputePerFileHashes_IncludesExtraFiles(t *testing.T) {
	workDir := makeStackDir(t)
	dockerfilePath := filepath.Join(workDir, "Dockerfile")
	writeFile(t, dockerfilePath, "FROM nginx:1.25\n")

	hashes, err := computePerFileHashes(context.Background(), workDir, nil, nil, "", []string{dockerfilePath})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if hashes[dockerfilePath] == "" {
		t.Errorf("expected hash for Dockerfile")
	}
}

// mustParseCompose parses a docker-compose.yml or fails the test.

func TestComputePerFileHashes_RejectsDeviceInWatchDir(t *testing.T) {
	workDir := makeStackDir(t)
	watchDir := t.TempDir()
	if err := os.Symlink("/dev/zero", filepath.Join(watchDir, "zero")); err != nil {
		t.Fatal(err)
	}

	_, err := computePerFileHashes(context.Background(), workDir, nil, []string{watchDir}, "", nil)
	if !errors.Is(err, regularfile.ErrNotRegular) {
		t.Fatalf("err = %v, want ErrNotRegular", err)
	}
}

// A FIFO in a watched dir would block the hash, and with it the deploy mutex,
// forever: a hang here is the bug.
func TestComputePerFileHashes_RejectsFIFOInWatchDir(t *testing.T) {
	workDir := makeStackDir(t)
	watchDir := t.TempDir()
	if err := syscall.Mkfifo(filepath.Join(watchDir, "fifo"), 0o600); err != nil {
		t.Skipf("mkfifo: %v", err)
	}

	_, err := computePerFileHashes(context.Background(), workDir, nil, []string{watchDir}, "", nil)
	if !errors.Is(err, regularfile.ErrNotRegular) {
		t.Fatalf("err = %v, want ErrNotRegular", err)
	}
}

func TestComputePerFileHashes_RejectsDeviceAsDockerfile(t *testing.T) {
	workDir := makeStackDir(t)

	_, err := computePerFileHashes(context.Background(), workDir, nil, nil, "", []string{"/dev/zero"})
	if !errors.Is(err, regularfile.ErrNotRegular) {
		t.Fatalf("err = %v, want ErrNotRegular", err)
	}
}

func TestComputePerFileHashes_FollowsSymlinkToRegularFile(t *testing.T) {
	workDir := makeStackDir(t)
	secret := filepath.Join(t.TempDir(), "secret.env")
	writeFile(t, secret, "KEY=value\n")
	link := filepath.Join(t.TempDir(), "app.env")
	if err := os.Symlink(secret, link); err != nil {
		t.Fatal(err)
	}

	hashes, err := computePerFileHashes(context.Background(), workDir, []string{link}, nil, "", nil)
	if err != nil || hashes[link] == "" {
		t.Fatalf("computePerFileHashes = (%v, %v), want a hash for the linked env file", hashes, err)
	}
}

func TestComputePerFileHashes_RejectsFileOverLimit(t *testing.T) {
	workDir := makeStackDir(t)
	big := filepath.Join(t.TempDir(), "big")
	writeFile(t, big, "")
	if err := os.Truncate(big, maxHashedFileBytes+1); err != nil {
		t.Fatal(err)
	}

	_, err := computePerFileHashes(context.Background(), workDir, nil, nil, "", []string{big})
	if !errors.Is(err, regularfile.ErrTooLarge) {
		t.Fatalf("err = %v, want ErrTooLarge", err)
	}
}

func TestComputePerFileHashes_StopsOnCancelledContext(t *testing.T) {
	workDir := makeStackDir(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := computePerFileHashes(ctx, workDir, nil, nil, "", nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}
