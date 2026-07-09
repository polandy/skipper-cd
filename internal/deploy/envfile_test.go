package deploy

import (
	"path/filepath"
	"slices"
	"testing"
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
