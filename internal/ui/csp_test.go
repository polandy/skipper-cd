package ui

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/polandy/skipper-cd/internal/uitheme"
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
	body := renderedIndex(t)
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

func TestScriptHandler_PrepaintAppliesThemeOverride(t *testing.T) {
	rec := httptest.NewRecorder()
	ScriptHandler(prepaintScript).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/app-prepaint.js", nil))

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

func renderedIndex(t *testing.T) string {
	t.Helper()
	rec := httptest.NewRecorder()
	IndexHandler(uitheme.ThemeCatppuccin, false).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	return rec.Body.String()
}
