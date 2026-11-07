package deploy

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/polandy/skipper-cd/internal/regularfile"
)

// --- parseEnvFile tests ---

func TestParseEnvFile_ParsesKeyValuePairs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vars.env")
	writeFile(t, path, "DOMAIN=example.com\nSMTP_HOST=mail.example.com\n")

	vars, err := parseEnvFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !slices.Contains(vars, "DOMAIN=example.com") {
		t.Errorf("expected DOMAIN=example.com in %v", vars)
	}
	if !slices.Contains(vars, "SMTP_HOST=mail.example.com") {
		t.Errorf("expected SMTP_HOST=mail.example.com in %v", vars)
	}
}

func TestParseEnvFile_IgnoresCommentsAndBlankLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vars.env")
	writeFile(t, path, "# this is a comment\n\nDOMAIN=example.com\n")

	vars, err := parseEnvFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(vars) != 1 || vars[0] != "DOMAIN=example.com" {
		t.Errorf("expected [DOMAIN=example.com], got %v", vars)
	}
}

func TestParseEnvFile_MissingFileReturnsError(t *testing.T) {
	_, err := parseEnvFile("/nonexistent/vars.env")
	if err == nil {
		t.Error("expected error for missing file, got nil")
	}
}

func TestParseEnvFile_RejectsInvalidKey(t *testing.T) {
	for _, line := range []string{"export FOO=bar", "1FOO=bar", "FOO BAR=baz", "NOEQUALS", "=value"} {
		t.Run(line, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "app.env")
			writeFile(t, path, "OK=1\n"+line+"\n")

			_, err := parseEnvFile(path)
			if err == nil || !strings.Contains(err.Error(), "line 2") {
				t.Fatalf("err = %v, want an error naming line 2", err)
			}
		})
	}
}

func TestParseEnvFile_RejectsReservedKey(t *testing.T) {
	for _, key := range []string{"LD_PRELOAD", "LD_LIBRARY_PATH", "PATH", "HOME", "BASH_ENV", "ENV", "DOCKER_HOST", "DOCKER_CONFIG", "DOCKER_CONTEXT", "DOCKER_CERT_PATH"} {
		t.Run(key, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "app.env")
			writeFile(t, path, key+"=/tmp/x\n")

			_, err := parseEnvFile(path)
			if err == nil || !strings.Contains(err.Error(), key) {
				t.Fatalf("err = %v, want an error naming %s", err, key)
			}
		})
	}
}

func TestParseEnvFile_KeepsValueVerbatim(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.env")
	writeFile(t, path, "COMPOSE_PROFILES=web\nDOCKER_BUILDKIT=1\nURL=http://x?a=b # not a comment\nEMPTY=\n")

	vars, err := parseEnvFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"COMPOSE_PROFILES=web", "DOCKER_BUILDKIT=1", "URL=http://x?a=b # not a comment", "EMPTY="}
	if !slices.Equal(vars, want) {
		t.Errorf("vars = %v, want %v", vars, want)
	}
}

func TestParseEnvFile_RejectsDevice(t *testing.T) {
	link := filepath.Join(t.TempDir(), "app.env")
	if err := os.Symlink("/dev/zero", link); err != nil {
		t.Fatal(err)
	}

	_, err := parseEnvFile(link)
	if !errors.Is(err, regularfile.ErrNotRegular) {
		t.Fatalf("err = %v, want ErrNotRegular", err)
	}
}

func TestParseEnvFile_RejectsFileOverLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.env")
	writeFile(t, path, "")
	if err := os.Truncate(path, maxEnvFileBytes+1); err != nil {
		t.Fatal(err)
	}

	_, err := parseEnvFile(path)
	if !errors.Is(err, regularfile.ErrTooLarge) {
		t.Fatalf("err = %v, want ErrTooLarge", err)
	}
}
