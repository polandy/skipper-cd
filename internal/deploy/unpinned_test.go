package deploy

import (
	"context"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	"github.com/polandy/skipper-cd/internal/events"
)

const unpinnedWarning = "image reference not pinned to a digest"

func TestUnpinnedImages_ReportsOnlyRegistryImagesWithoutDigest(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "docker-compose.yml"), `services:
  proxy:
    image: caddy:2.8
  db:
    image: postgres:16@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef
  app:
    build: .
    image: example/app:latest
  worker:
    image: example/app:latest
  plain:
    build: ./plain
  templated:
    image: caddy:${CADDY_TAG}
`)
	cf, err := parseComposeFile(filepath.Join(dir, "docker-compose.yml"))
	if err != nil {
		t.Fatal(err)
	}

	got := cf.unpinnedImages()

	if len(got) != 1 || got["proxy"] != "caddy:2.8" {
		t.Errorf("unpinnedImages = %v, want only proxy=caddy:2.8 (pinned, built, locally-built and interpolated images excluded)", got)
	}
}

func countLines(log, substr string) int {
	n := 0
	for _, line := range strings.Split(log, "\n") {
		if strings.Contains(line, substr) {
			n++
		}
	}
	return n
}

func TestDeployAllStacks_WarnsUnpinnedImageOncePerDistinctMessage(t *testing.T) {
	repoDir, cfg := discoveryRepo(t, []string{"web"}, nil)
	d, _, recorded := discoveryDeployer(t, repoDir)
	composePath := filepath.Join(repoDir, "stacks", "web", "docker-compose.yml")

	log := captureLogAt(t, slog.LevelInfo, func(*testing.T) {
		d.DeployAllStacks(context.Background(), cfg)
		d.DeployAllStacks(context.Background(), cfg)
	})

	if got := countLines(log, unpinnedWarning); got != 1 {
		t.Fatalf("unpinned warnings over two identical runs = %d, want 1:\n%s", got, log)
	}
	for _, want := range []string{"stack=web", "app=nginx:1.25"} {
		if !strings.Contains(log, want) {
			t.Errorf("warning does not name %q:\n%s", want, log)
		}
	}
	// Positive signal that the second run reached the stack phase.
	if got := eventsWithStatus(*recorded, events.StatusSkipped); len(got) != 1 {
		t.Fatalf("skipped events = %v, want one from the second run", got)
	}

	writeFile(t, composePath, composeWithImage("nginx:1.27"))
	log = captureLogAt(t, slog.LevelInfo, func(*testing.T) {
		d.DeployAllStacks(context.Background(), cfg)
	})
	if got := countLines(log, unpinnedWarning); got != 1 || !strings.Contains(log, "app=nginx:1.27") {
		t.Errorf("a changed unpinned reference must warn again, got %d warnings:\n%s", got, log)
	}
}

func TestDeployAllStacks_PinnedImageDoesNotWarn(t *testing.T) {
	repoDir, cfg := discoveryRepo(t, []string{"web"}, nil)
	writeFile(t, filepath.Join(repoDir, "stacks", "web", "docker-compose.yml"),
		composeWithImage("nginx:1.25@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"))
	d, runner, _ := discoveryDeployer(t, repoDir)

	log := captureLogAt(t, slog.LevelInfo, func(*testing.T) {
		d.DeployAllStacks(context.Background(), cfg)
	})

	assertCommandCalled(t, runner.calls, "up")
	if strings.Contains(log, unpinnedWarning) {
		t.Errorf("a digest-pinned image must not warn:\n%s", log)
	}
}

func TestDeployAllStacks_WarnsAgainAfterPinningClearedIt(t *testing.T) {
	repoDir, cfg := discoveryRepo(t, []string{"web"}, nil)
	d, _, _ := discoveryDeployer(t, repoDir)
	composePath := filepath.Join(repoDir, "stacks", "web", "docker-compose.yml")

	log := captureLogAt(t, slog.LevelInfo, func(*testing.T) {
		d.DeployAllStacks(context.Background(), cfg)
		writeFile(t, composePath, composeWithImage("nginx:1.25@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"))
		d.DeployAllStacks(context.Background(), cfg)
		writeFile(t, composePath, composeWithImage("nginx:1.25"))
		d.DeployAllStacks(context.Background(), cfg)
	})

	if got := countLines(log, "image references now pinned"); got != 1 {
		t.Errorf("resolved notices = %d, want 1 once the stack was pinned:\n%s", got, log)
	}
	if got := countLines(log, unpinnedWarning); got != 2 {
		t.Errorf("unpinned warnings = %d, want 2 (before pinning and after unpinning again):\n%s", got, log)
	}
}
