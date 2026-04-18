package deploy

import (
	"path/filepath"
	"slices"
	"testing"
)

func TestLoadVarsFile_EmptyPathReturnsNil(t *testing.T) {
	vars, err := loadVarsFile("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if vars != nil {
		t.Errorf("expected nil for empty path, got %v", vars)
	}
}

func TestLoadVarsFile_ParsesKeyValuePairs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vars.env")
	writeFile(t, path, "DOMAIN=example.com\nSMTP_HOST=mail.example.com\n")

	vars, err := loadVarsFile(path)
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

func TestLoadVarsFile_IgnoresCommentsAndBlankLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vars.env")
	writeFile(t, path, "# this is a comment\n\nDOMAIN=example.com\n")

	vars, err := loadVarsFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(vars) != 1 || vars[0] != "DOMAIN=example.com" {
		t.Errorf("expected [DOMAIN=example.com], got %v", vars)
	}
}

func TestLoadVarsFile_MissingFileReturnsError(t *testing.T) {
	_, err := loadVarsFile("/nonexistent/vars.env")
	if err == nil {
		t.Error("expected error for missing file, got nil")
	}
}
