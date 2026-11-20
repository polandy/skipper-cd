package ui

import (
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/polandy/skipper-cd/internal/uitheme"
)

func TestScripts_EqualAppFilesOnDisk(t *testing.T) {
	files, err := filepath.Glob("static/app-*.js")
	if err != nil {
		t.Fatal(err)
	}
	var onDisk []string
	for _, f := range files {
		name := strings.TrimSuffix(filepath.Base(f), ".js")
		if strings.HasSuffix(name, ".test") || name == prepaintScript {
			continue
		}
		onDisk = append(onDisk, name)
	}
	got := slices.Clone(appScripts)
	slices.Sort(got)
	slices.Sort(onDisk)
	if !slices.Equal(got, onDisk) {
		t.Errorf("appScripts = %v, static/app-*.js on disk = %v; add the new file to appScripts in its load position", got, onDisk)
	}
}

func TestScriptHandler_ServesEveryScript(t *testing.T) {
	for _, name := range append([]string{prepaintScript}, appScripts...) {
		t.Run(name, func(t *testing.T) {
			want, err := os.ReadFile("static/" + name + ".js")
			if err != nil {
				t.Fatal(err)
			}
			h := ScriptHandler(name)

			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/"+name+".js", nil))
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", rec.Code)
			}
			if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/javascript") {
				t.Errorf("content type = %q, want text/javascript", ct)
			}
			if cc := rec.Header().Get("Cache-Control"); cc != "no-cache" {
				t.Errorf("cache-control = %q, want no-cache (lockstep with the app shell)", cc)
			}
			if rec.Body.String() != string(want) {
				t.Error("body differs from the embedded file")
			}

			req := httptest.NewRequest(http.MethodGet, "/"+name+".js", nil)
			req.Header.Set("Accept-Encoding", "gzip")
			rec = httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if enc := rec.Header().Get("Content-Encoding"); enc != "gzip" {
				t.Fatalf("Content-Encoding = %q, want gzip", enc)
			}
			gr, err := gzip.NewReader(rec.Body)
			if err != nil {
				t.Fatalf("gzip.NewReader: %v", err)
			}
			got, err := io.ReadAll(gr)
			if err != nil {
				t.Fatalf("reading gzip body: %v", err)
			}
			if string(got) != string(want) {
				t.Error("gzip body differs from the embedded file")
			}
		})
	}
}

func TestScriptHandler_PanicsOnUnknownScript(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("ScriptHandler of an unregistered name did not panic")
		}
	}()
	ScriptHandler("app-nope")
}

func TestScripts_ReturnsLoadOrder(t *testing.T) {
	got := Scripts()
	if !slices.Equal(got, appScripts) {
		t.Errorf("Scripts() = %v, want %v", got, appScripts)
	}
	got[0] = "mutated"
	if appScripts[0] == "mutated" {
		t.Error("Scripts() exposes the table's backing array")
	}
}

func TestIndexHandler_LoadsScriptsInTableOrder(t *testing.T) {
	rec := httptest.NewRecorder()
	IndexHandler(uitheme.ThemeCatppuccin, false).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	body := rec.Body.String()
	if strings.Contains(body, "__APP_SCRIPTS__") {
		t.Fatal("script placeholder not replaced")
	}
	pos := strings.Index(body, `<script src="/`+prepaintScript+`.js"></script>`)
	if pos < 0 {
		t.Fatal("prepaint script tag missing")
	}
	for _, name := range appScripts {
		tag := `<script src="/` + name + `.js"></script>`
		i := strings.Index(body[pos:], tag)
		if i < 0 {
			t.Fatalf("%s missing, or loaded before the script ahead of it", tag)
		}
		pos += i + len(tag)
	}
}

func TestServiceWorkerHandler_ShellCachesEveryScript(t *testing.T) {
	rec := httptest.NewRecorder()
	ServiceWorkerHandler(BuildInfo{Version: "1.2.3"}).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/sw.js", nil))
	body := rec.Body.String()
	if strings.Contains(body, "__APP_SHELL_SCRIPTS__") {
		t.Fatal("shell placeholder not replaced")
	}
	for _, name := range append([]string{prepaintScript}, appScripts...) {
		if !strings.Contains(body, "'/"+name+".js'") {
			t.Errorf("service worker SHELL misses /%s.js", name)
		}
	}
}
