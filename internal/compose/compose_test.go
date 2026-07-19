package compose

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "docker-compose.yml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write compose: %v", err)
	}
	return path
}

func TestParse_ReadsServiceFields(t *testing.T) {
	path := write(t, `services:
  web:
    image: nginx:1.25
    ports:
      - "8080:80"
    healthcheck:
      test: ["CMD", "true"]
  builder:
    build: .
    container_name: fixed
`)

	f, err := Parse(path)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(f.Services) != 2 {
		t.Fatalf("got %d services, want 2", len(f.Services))
	}

	web := f.Services["web"]
	if web.Image != "nginx:1.25" {
		t.Errorf("web.Image = %q", web.Image)
	}
	if !web.PublishesPorts() {
		t.Error("web should publish ports")
	}
	if !web.HasHealthcheck() {
		t.Error("web should have a healthcheck")
	}
	if web.HasContainerName() {
		t.Error("web should not pin a container_name")
	}

	builder := f.Services["builder"]
	if builder.Build == nil {
		t.Error("builder should have a build field")
	}
	if !builder.HasContainerName() {
		t.Error("builder should pin a container_name")
	}
	if builder.PublishesPorts() || builder.HasHealthcheck() {
		t.Error("builder publishes no ports and has no healthcheck")
	}
}

func TestParse_IgnoresUnknownFields(t *testing.T) {
	path := write(t, `version: "3"
networks:
  default: {}
services:
  app:
    image: busybox
    labels:
      - "traefik.enable=true"
`)

	f, err := Parse(path)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if _, ok := f.Services["app"]; !ok || len(f.Services) != 1 {
		t.Errorf("services = %+v, want just app", f.Services)
	}
}

func TestParse_ErrorOnMissingFile(t *testing.T) {
	if _, err := Parse(filepath.Join(t.TempDir(), "missing.yml")); err == nil {
		t.Fatal("expected error for missing compose file")
	}
}

func TestParse_ErrorOnInvalidYAML(t *testing.T) {
	if _, err := Parse(write(t, "services: [broken")); err == nil {
		t.Fatal("expected error for invalid yaml")
	}
}
