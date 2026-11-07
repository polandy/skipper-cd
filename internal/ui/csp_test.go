package ui

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

var (
	scriptTag     = regexp.MustCompile(`<script\b[^>]*>`)
	scriptSrcAttr = regexp.MustCompile(`\bsrc="(/[^"]+)"`)
	inlineHandler = regexp.MustCompile(`(?i)<[^>]*\son[a-z]+\s*=`)
)

// The app shell's CSP allows script only from 'self' (headers.go), so the
// shell must carry no inline script and no on*= handler attribute — either
// would be refused by the browser and silently break the page.
func TestIndexHTML_HasNoInlineScript(t *testing.T) {
	body := readStatic(t, "static/index.html")
	tags := scriptTag.FindAllString(body, -1)
	if len(tags) == 0 {
		t.Fatal("index.html loads no script at all — the regexp no longer matches")
	}
	for _, tag := range tags {
		if !scriptSrcAttr.MatchString(tag) {
			t.Errorf("inline script %q: move it into an embedded static/app-*.js file", tag)
		}
	}
	if m := inlineHandler.FindString(body); m != "" {
		t.Errorf("inline event handler %q: attach it with addEventListener instead", m)
	}
}

// Every script the shell loads is cached by the service worker, or the app
// does not start offline.
func TestIndexHTML_ScriptsAreInServiceWorkerShell(t *testing.T) {
	body := readStatic(t, "static/index.html")
	sw := readStatic(t, "static/sw.js")
	for _, tag := range scriptTag.FindAllString(body, -1) {
		m := scriptSrcAttr.FindStringSubmatch(tag)
		if m == nil {
			continue
		}
		if !strings.Contains(sw, "'"+m[1]+"'") {
			t.Errorf("%s is loaded by index.html but missing from the service worker SHELL", m[1])
		}
	}
}

func TestAppPrepaintJSHandler_ServesJS(t *testing.T) {
	rec := httptest.NewRecorder()
	AppPrepaintJSHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/app-prepaint.js", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/javascript") {
		t.Errorf("content type = %q, want text/javascript", ct)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-cache" {
		t.Errorf("cache-control = %q, want no-cache (lockstep with the app shell)", cc)
	}
	if body := rec.Body.String(); !strings.Contains(body, "themeOverride") {
		t.Error("body does not apply the saved theme override")
	}
}

func readStatic(t *testing.T, name string) string {
	t.Helper()
	data, err := staticFS.ReadFile(name)
	if err != nil {
		t.Fatalf("reading %s: %v", name, err)
	}
	return string(data)
}
