package deploy

import (
	"os"
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

func TestLoadVarsFile_ConvertsKeysToUppercase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vars.yml")
	writeFile(t, path, "domain: example.com\nsmtp_host: mail.example.com\n")

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

func TestLoadVarsFile_MissingFileReturnsError(t *testing.T) {
	_, err := loadVarsFile("/nonexistent/vars.yml")
	if err == nil {
		t.Error("expected error for missing file, got nil")
	}
}

func TestLoadVarsFile_InvalidYAMLReturnsError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vars.yml")
	os.WriteFile(path, []byte("key: [invalid: yaml"), 0o644)

	_, err := loadVarsFile(path)
	if err == nil {
		t.Error("expected error for invalid YAML, got nil")
	}
}
