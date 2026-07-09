package deploy

import (
	"path/filepath"
	"slices"
	"testing"
)

func mustParseCompose(t *testing.T, path string) *composeFile {
	t.Helper()
	cf, err := parseComposeFile(path)
	if err != nil {
		t.Fatalf("parse compose file %s: %v", path, err)
	}
	return cf
}

func TestParseComposeFile_ErrorOnMissingFile(t *testing.T) {
	if _, err := parseComposeFile(filepath.Join(t.TempDir(), "missing.yml")); err == nil {
		t.Fatal("expected error for missing compose file")
	}
}

func TestParseComposeFile_ErrorOnInvalidYAML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "docker-compose.yml")
	writeFile(t, path, "services: [broken")

	if _, err := parseComposeFile(path); err == nil {
		t.Fatal("expected error for invalid yaml")
	}
}

// --- composeFile.dockerfilePaths tests ---

func TestComposeFile_DockerfilePaths_StringForm(t *testing.T) {
	workDir := t.TempDir()
	writeFile(t, filepath.Join(workDir, "docker-compose.yml"), `services:
  app:
    build: "."
`)
	writeFile(t, filepath.Join(workDir, "Dockerfile"), "FROM nginx:1.25\n")

	paths := mustParseCompose(t, filepath.Join(workDir, "docker-compose.yml")).dockerfilePaths(workDir)
	if len(paths) != 1 {
		t.Fatalf("expected 1 path, got %d: %v", len(paths), paths)
	}
	if paths[0] != filepath.Join(workDir, "Dockerfile") {
		t.Errorf("unexpected path: %s", paths[0])
	}
}

func TestComposeFile_DockerfilePaths_MapForm(t *testing.T) {
	workDir := t.TempDir()
	writeFile(t, filepath.Join(workDir, "docker-compose.yml"), `services:
  app:
    build:
      context: "."
      dockerfile: "Dockerfile.custom"
`)
	writeFile(t, filepath.Join(workDir, "Dockerfile.custom"), "FROM nginx:1.25\n")

	paths := mustParseCompose(t, filepath.Join(workDir, "docker-compose.yml")).dockerfilePaths(workDir)
	if len(paths) != 1 {
		t.Fatalf("expected 1 path, got %d: %v", len(paths), paths)
	}
	if paths[0] != filepath.Join(workDir, "Dockerfile.custom") {
		t.Errorf("unexpected path: %s", paths[0])
	}
}

func TestComposeFile_DockerfilePaths_MissingDockerfileSkipped(t *testing.T) {
	workDir := t.TempDir()
	writeFile(t, filepath.Join(workDir, "docker-compose.yml"), `services:
  app:
    build: "."
`)
	// No Dockerfile written — should return empty, not an error.

	paths := mustParseCompose(t, filepath.Join(workDir, "docker-compose.yml")).dockerfilePaths(workDir)
	if len(paths) != 0 {
		t.Errorf("expected no paths, got %v", paths)
	}
}

func TestComposeFile_DockerfilePaths_NoBuildServicesReturnsEmpty(t *testing.T) {
	workDir := t.TempDir()
	writeFile(t, filepath.Join(workDir, "docker-compose.yml"), composeWithImage("nginx:1.25"))

	paths := mustParseCompose(t, filepath.Join(workDir, "docker-compose.yml")).dockerfilePaths(workDir)
	if len(paths) != 0 {
		t.Errorf("expected no paths, got %v", paths)
	}
}

// --- images.go tests ---

func TestComposeFile_Images_ParsesImages(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "docker-compose.yml")
	writeFile(t, path, `services:
  app:
    image: gitea/gitea:1.21
  db:
    image: postgres:16-alpine
  builder:
    build: .
`)

	images := mustParseCompose(t, path).images()

	if len(images) != 2 {
		t.Fatalf("expected 2 images, got %d: %v", len(images), images)
	}
	if images["app"] != "gitea/gitea:1.21" {
		t.Errorf("expected gitea/gitea:1.21, got %s", images["app"])
	}
	if images["db"] != "postgres:16-alpine" {
		t.Errorf("expected postgres:16-alpine, got %s", images["db"])
	}
}

func TestImagesChanged_DetectsChange(t *testing.T) {
	current := map[string]string{"app": "redis:7.4"}
	previous := map[string]string{"app": "redis:7.2"}
	if !hasAnyImageChanged(current, previous) {
		t.Error("expected images to be detected as changed")
	}
}

func TestImagesChanged_DetectsNoChange(t *testing.T) {
	images := map[string]string{"app": "redis:7.2", "db": "postgres:16"}
	if hasAnyImageChanged(images, images) {
		t.Error("expected images to be detected as unchanged")
	}
}

func TestImagesChanged_DetectsNewService(t *testing.T) {
	current := map[string]string{"app": "redis:7.2", "cache": "memcached:1.6"}
	previous := map[string]string{"app": "redis:7.2"}
	if !hasAnyImageChanged(current, previous) {
		t.Error("expected new service to be detected as change")
	}
}

func TestImagesChanged_NilPreviousIsChanged(t *testing.T) {
	current := map[string]string{"app": "redis:7.2"}
	if !hasAnyImageChanged(current, nil) {
		t.Error("expected nil previous to be detected as changed")
	}
}

// --- composeFile.pullableServices tests ---

func TestComposeFile_PullableServices_ExcludesBuildServices(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "docker-compose.yml")
	writeFile(t, path, `services:
  app:
    build: "."
    image: myapp:custom
  db:
    image: postgres:16-alpine
  redis:
    image: redis:7.2
`)

	pullable := mustParseCompose(t, path).pullableServices()

	if len(pullable) != 2 {
		t.Fatalf("expected 2 pullable services, got %d: %v", len(pullable), pullable)
	}
	if !slices.Contains(pullable, "db") {
		t.Errorf("expected db in pullable services: %v", pullable)
	}
	if !slices.Contains(pullable, "redis") {
		t.Errorf("expected redis in pullable services: %v", pullable)
	}
	if slices.Contains(pullable, "app") {
		t.Errorf("build service app should not be pullable: %v", pullable)
	}
}

func TestComposeFile_PullableServices_ExcludesLocalImageConsumers(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "docker-compose.yml")
	writeFile(t, path, `services:
  app:
    build: "."
    image: nextcloud:34-ghostscript
  cron:
    image: nextcloud:34-ghostscript
  db:
    image: postgres:16-alpine
`)

	pullable := mustParseCompose(t, path).pullableServices()

	if len(pullable) != 1 {
		t.Fatalf("expected 1 pullable service, got %d: %v", len(pullable), pullable)
	}
	if pullable[0] != "db" {
		t.Errorf("expected only db to be pullable, got %v", pullable)
	}
}

func TestComposeFile_PullableServices_AllRemoteImages(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "docker-compose.yml")
	writeFile(t, path, `services:
  app:
    image: nginx:1.25
  db:
    image: postgres:16-alpine
`)

	pullable := mustParseCompose(t, path).pullableServices()

	if len(pullable) != 2 {
		t.Fatalf("expected 2 pullable services, got %d: %v", len(pullable), pullable)
	}
}

func TestComposeFile_PullableServices_AllBuildServices(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "docker-compose.yml")
	writeFile(t, path, `services:
  app:
    build: "."
    image: myapp:latest
  worker:
    image: myapp:latest
`)

	pullable := mustParseCompose(t, path).pullableServices()

	if len(pullable) != 0 {
		t.Errorf("expected no pullable services, got %v", pullable)
	}
}
