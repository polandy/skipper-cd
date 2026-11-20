package compose

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/polandy/skipper-cd/internal/regularfile"
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

func TestParse_RejectsDevice(t *testing.T) {
	link := filepath.Join(t.TempDir(), FileName)
	if err := os.Symlink("/dev/zero", link); err != nil {
		t.Fatal(err)
	}

	if _, err := Parse(link); !errors.Is(err, regularfile.ErrNotRegular) {
		t.Fatalf("err = %v, want ErrNotRegular", err)
	}
}

func TestParse_RejectsFileOverLimit(t *testing.T) {
	path := write(t, "")
	if err := os.Truncate(path, MaxFileBytes+1); err != nil {
		t.Fatal(err)
	}

	if _, err := Parse(path); !errors.Is(err, regularfile.ErrTooLarge) {
		t.Fatalf("err = %v, want ErrTooLarge", err)
	}
}

func TestReadinessBudget_DerivesFromHealthcheckTiming(t *testing.T) {
	ten := 10
	tests := []struct {
		name string
		hc   *Healthcheck
		want time.Duration
	}{
		{"nil healthcheck", nil, 0},
		{"docker defaults", &Healthcheck{}, 30*time.Second + 4*30*time.Second},
		{"nextcloud timing", &Healthcheck{StartPeriod: "60s", Interval: "2m", Timeout: "30s", Retries: &ten}, 60*time.Second + 11*2*time.Minute + 30*time.Second},
		{"unparsable falls back to the default", &Healthcheck{Interval: "soon", Timeout: "5s"}, 4*30*time.Second + 5*time.Second},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.hc.ReadinessBudget(); got != tt.want {
				t.Errorf("ReadinessBudget() = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestFile_ReadinessBudget_TakesTheLongestService(t *testing.T) {
	path := write(t, `services:
  db:
    image: postgres:17
    healthcheck:
      interval: 5s
      timeout: 5s
      retries: 2
  app:
    image: nextcloud:35
    healthcheck:
      start_period: 1m
      interval: 10s
  sidecar:
    image: busybox
`)
	f, err := Parse(path)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got, want := f.ReadinessBudget(), time.Minute+4*10*time.Second+30*time.Second; got != want {
		t.Errorf("ReadinessBudget() = %s, want %s", got, want)
	}
}
