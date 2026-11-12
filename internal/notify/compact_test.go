package notify

import (
	"context"
	"io"
	"mime"
	"net/http"
	"testing"

	"github.com/polandy/skipper-cd/internal/events"
)

const (
	immichServer = "ghcr.io/immich-app/immich-server"
	immichML     = "ghcr.io/immich-app/immich-machine-learning"
)

func immichBump() []events.ServiceImageChange {
	return []events.ServiceImageChange{
		{Service: "app", Old: immichServer + ":v3.4.0@sha256:7b7b5750000000", New: immichServer + ":v3.4.1@sha256:7c489ad0000000"},
		{Service: "immich-machine-learning", Old: immichML + ":v3.4.0-openvino@sha256:aaaaaaaa", New: immichML + ":v3.4.1-openvino@sha256:bbbbbbbb"},
	}
}

func TestComposeDeploy_SingleVersionGroupsContainersUnderOneTitle(t *testing.T) {
	m := composeDeploy(events.DeployEvent{Stack: "immich", Status: events.StatusSuccess, ImageChanges: immichBump()})
	if m.Title != "immich: v3.4.0 → v3.4.1" {
		t.Errorf("title = %q", m.Title)
	}
	if m.Body != "app, immich-machine-learning (openvino)" {
		t.Errorf("body = %q", m.Body)
	}
	if m.Click != "https://github.com/immich-app/immich-server/releases/tag/v3.4.1" {
		t.Errorf("click = %q", m.Click)
	}
	if !m.Success {
		t.Error("success expected")
	}
}

func TestComposeDeploy_MixedVersionsListEachChange(t *testing.T) {
	m := composeDeploy(events.DeployEvent{Stack: "nextcloud", Status: events.StatusSuccess, ImageChanges: []events.ServiceImageChange{
		{Service: "app", Old: "nextcloud:29.0.1", New: "nextcloud:29.0.2"},
		{Service: "cron", Old: "nextcloud:29.0.1", New: "nextcloud:29.0.2"},
		{Service: "redis", Old: "docker.io/library/redis:7.2", New: "docker.io/library/redis:7.4"},
	}})
	if m.Title != "nextcloud aktualisiert" {
		t.Errorf("title = %q", m.Title)
	}
	if want := "app, cron: 29.0.1 → 29.0.2\nredis: 7.2 → 7.4"; m.Body != want {
		t.Errorf("body = %q, want %q", m.Body, want)
	}
	if m.Click != "" {
		t.Errorf("click = %q, want none", m.Click)
	}
}

func TestComposeDeploy_DigestOnlyShowsShortDigests(t *testing.T) {
	m := composeDeploy(events.DeployEvent{Stack: "nc", Status: events.StatusSuccess, ImageChanges: []events.ServiceImageChange{
		{Service: "app", Old: "nextcloud:34-apache@sha256:ab12cd34ef567890", New: "nextcloud:34-apache@sha256:9f8e7d6c5b4a3210"},
	}})
	if m.Title != "nc: 34-apache neu gebaut" {
		t.Errorf("title = %q", m.Title)
	}
	if m.Body != "app: ab12cd3 → 9f8e7d6" {
		t.Errorf("body = %q", m.Body)
	}
	if m.Click != "" {
		t.Errorf("click = %q", m.Click)
	}
}

func TestComposeDeploy_FailureUsesFirstErrorLine(t *testing.T) {
	m := composeDeploy(events.DeployEvent{
		Stack: "immich", Status: events.StatusRolledBack,
		Error:        "\ncompose up failed: exit 1\nsecond line",
		ImageChanges: immichBump()[:1],
	})
	if m.Title != "immich: Deploy fehlgeschlagen, zurück auf v3.4.0" {
		t.Errorf("title = %q", m.Title)
	}
	if m.Body != "compose up failed: exit 1" {
		t.Errorf("body = %q", m.Body)
	}
	if m.Success || m.Click != "" {
		t.Errorf("success=%v click=%q", m.Success, m.Click)
	}
}

func TestComposeDeploy_FailureWithoutRestoreDoesNotClaimOne(t *testing.T) {
	m := composeDeploy(events.DeployEvent{Stack: "api", Status: events.StatusFailed, Error: "boom", ImageChanges: immichBump()[:1]})
	if m.Title != "api: Deploy fehlgeschlagen" {
		t.Errorf("title = %q", m.Title)
	}
}

func TestComposeDeploy_RollbackWithDifferingOldTagsOmitsTag(t *testing.T) {
	m := composeDeploy(events.DeployEvent{Stack: "x", Status: events.StatusRolledBack, Error: "e", ImageChanges: []events.ServiceImageChange{
		{Service: "a", Old: "a:1", New: "a:2"}, {Service: "b", Old: "b:5", New: "b:6"},
	}})
	if m.Title != "x: Deploy fehlgeschlagen, zurückgerollt" {
		t.Errorf("title = %q", m.Title)
	}
}

func TestComposeDeploy_NoImageChanges(t *testing.T) {
	m := composeDeploy(events.DeployEvent{Stack: "api", Status: events.StatusSuccess})
	if m.Title != "api neu deployt" || m.Body != "" {
		t.Errorf("got %q / %q", m.Title, m.Body)
	}
}

func TestComposeDeploy_AddedRemovedAndSwitchedImages(t *testing.T) {
	m := composeDeploy(events.DeployEvent{Stack: "s", Status: events.StatusSuccess, ImageChanges: []events.ServiceImageChange{
		{Service: "new", New: "redis:7"},
		{Service: "gone", Old: "mongo:6"},
		{Service: "db", Old: "mysql:8", New: "mariadb:11"},
	}})
	if m.Title != "s aktualisiert" {
		t.Errorf("title = %q", m.Title)
	}
	if want := "new: 7\ngone: entfernt\ndb: mysql:8 → mariadb:11"; m.Body != want {
		t.Errorf("body = %q, want %q", m.Body, want)
	}
}

func TestSplitTagSuffix(t *testing.T) {
	tests := []struct{ old, new, oldCore, newCore, suffix string }{
		{"v3.4.0-openvino", "v3.4.1-openvino", "v3.4.0", "v3.4.1", "openvino"},
		{"v3.4.0", "v3.4.1", "v3.4.0", "v3.4.1", ""},
		{"1.25-alpine", "1.27-alpine", "1.25", "1.27", "alpine"},
		{"29-apache", "30-fpm", "29-apache", "30-fpm", ""},
		{"v1-rc", "v1-rc", "v1-rc", "v1-rc", ""},
	}
	for _, tc := range tests {
		oc, nc, s := splitTagSuffix(tc.old, tc.new)
		if oc != tc.oldCore || nc != tc.newCore || s != tc.suffix {
			t.Errorf("splitTagSuffix(%q,%q) = %q,%q,%q", tc.old, tc.new, oc, nc, s)
		}
	}
}

func TestNtfyFormatter_HeadersAndPlainBody(t *testing.T) {
	f := ntfyFormatter{url: "https://ntfy.example/skipper", headers: map[string]string{"Authorization": "Bearer t"}, prefix: "host-a"}
	req, err := f.Format(context.Background(), events.DeployEvent{Stack: "immich", Status: events.StatusSuccess, ImageChanges: immichBump()})
	if err != nil {
		t.Fatal(err)
	}
	assertNtfy(t, req, "[host-a] immich: v3.4.0 → v3.4.1", "low", "white_check_mark", "app, immich-machine-learning (openvino)")
	if req.Header.Get("Click") == "" || req.Header.Get("Authorization") != "Bearer t" {
		t.Errorf("headers = %v", req.Header)
	}
}

func TestNtfyFormatter_FailureIsHighPriorityWithCross(t *testing.T) {
	f := ntfyFormatter{url: "https://ntfy.example/skipper"}
	req, err := f.Format(context.Background(), events.DeployEvent{Stack: "api", Status: events.StatusFailed, Error: "boom"})
	if err != nil {
		t.Fatal(err)
	}
	assertNtfy(t, req, "api: Deploy fehlgeschlagen", "high", "x", "boom")
	if req.Header.Get("Click") != "" {
		t.Error("no click expected")
	}
}

func assertNtfy(t *testing.T, req *http.Request, title, prio, tag, body string) {
	t.Helper()
	if req.Method != http.MethodPost {
		t.Errorf("method = %s", req.Method)
	}
	if got := decodeHeader(t, req.Header.Get("Title")); got != title {
		t.Errorf("Title = %q, want %q", got, title)
	}
	if req.Header.Get("Priority") != prio || req.Header.Get("Tags") != tag {
		t.Errorf("Priority=%q Tags=%q", req.Header.Get("Priority"), req.Header.Get("Tags"))
	}
	b, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != body {
		t.Errorf("body = %q, want %q", b, body)
	}
}

func TestSignalMessage_IsTitleThenBody(t *testing.T) {
	got := renderMessage("h", events.DeployEvent{Stack: "immich", Status: events.StatusSuccess, ImageChanges: immichBump()})
	if want := "[h] immich: v3.4.0 → v3.4.1\napp, immich-machine-learning (openvino)"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func decodeHeader(t *testing.T, v string) string {
	t.Helper()
	s, err := new(mime.WordDecoder).DecodeHeader(v)
	if err != nil {
		t.Fatal(err)
	}
	return s
}
