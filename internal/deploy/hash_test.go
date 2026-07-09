package deploy

import (
	"path/filepath"
	"testing"
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

	hashes, err := computePerFileHashes(workDir, []string{envFile}, nil, "", nil)
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

	hashes, err := computePerFileHashes(workDir, nil, nil, varsFile, nil)
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

	hashes, err := computePerFileHashes(workDir, nil, nil, "", []string{dockerfilePath})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if hashes[dockerfilePath] == "" {
		t.Errorf("expected hash for Dockerfile")
	}
}

// mustParseCompose parses a docker-compose.yml or fails the test.
