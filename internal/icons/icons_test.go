package icons

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeFetcher is an in-memory Fetcher. A slug present in icons is a hit; an
// absent slug is a definitive miss (found == false). A non-nil err simulates a
// transient transport failure and takes precedence.
type fakeFetcher struct {
	icons       map[string][]byte
	contentType string // content type for hits; defaults to image/svg+xml
	err         error
	calls       []string
}

func (f *fakeFetcher) Fetch(_ context.Context, slug string) (data []byte, contentType string, found bool, err error) {
	f.calls = append(f.calls, slug)
	if f.err != nil {
		return nil, "", false, f.err
	}
	if d, ok := f.icons[slug]; ok {
		ct := f.contentType
		if ct == "" {
			ct = "image/svg+xml"
		}
		return d, ct, true, nil
	}
	return nil, "", false, nil
}

func newTestService(t *testing.T) (*Service, *fakeFetcher) {
	t.Helper()
	ff := &fakeFetcher{icons: map[string][]byte{}}
	return New(t.TempDir(), ff), ff
}

func writeStackIcon(t *testing.T, name, data string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestResolve_RepoOverrideSvgWinsOverFetch(t *testing.T) {
	svc, ff := newTestService(t)
	ff.icons["media"] = []byte("<svg>from-set</svg>")
	dir := writeStackIcon(t, "icon.svg", "<svg>from-repo</svg>")

	got, err := svc.Resolve(context.Background(), Request{Name: "media", Dir: dir})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if string(got.Data) != "<svg>from-repo</svg>" {
		t.Errorf("data = %q, want repo override", got.Data)
	}
	if got.ContentType != "image/svg+xml" {
		t.Errorf("content type = %q", got.ContentType)
	}
	if len(ff.calls) != 0 {
		t.Errorf("fetcher called %v, want no calls when repo override present", ff.calls)
	}
}

func TestResolve_RepoOverridePngWhenNoSvg(t *testing.T) {
	svc, _ := newTestService(t)
	dir := writeStackIcon(t, "icon.png", "PNGDATA")

	got, err := svc.Resolve(context.Background(), Request{Name: "media", Dir: dir})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if string(got.Data) != "PNGDATA" || got.ContentType != "image/png" {
		t.Errorf("got %q / %q, want PNGDATA / image/png", got.Data, got.ContentType)
	}
}

func TestResolve_RepoOverridePrefersSvgOverPng(t *testing.T) {
	svc, _ := newTestService(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "icon.svg"), []byte("SVG"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "icon.png"), []byte("PNG"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := svc.Resolve(context.Background(), Request{Name: "media", Dir: dir})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if string(got.Data) != "SVG" {
		t.Errorf("data = %q, want svg preferred", got.Data)
	}
}

func TestResolve_SlugOverrideFetched(t *testing.T) {
	svc, ff := newTestService(t)
	ff.icons["jellyfin"] = []byte("<svg>jf</svg>")

	got, err := svc.Resolve(context.Background(), Request{Name: "media", Slug: "jellyfin", Dir: t.TempDir()})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if string(got.Data) != "<svg>jf</svg>" {
		t.Errorf("data = %q", got.Data)
	}
	if len(ff.calls) != 1 || ff.calls[0] != "jellyfin" {
		t.Errorf("fetcher calls = %v, want [jellyfin]", ff.calls)
	}
}

func TestResolve_AutoMatchByStackName(t *testing.T) {
	svc, ff := newTestService(t)
	ff.icons["jellyfin"] = []byte("<svg>jf</svg>")

	got, err := svc.Resolve(context.Background(), Request{Name: "Jellyfin", Dir: t.TempDir()})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if string(got.Data) != "<svg>jf</svg>" {
		t.Errorf("data = %q", got.Data)
	}
	if len(ff.calls) != 1 || ff.calls[0] != "jellyfin" {
		t.Errorf("fetcher calls = %v, want [jellyfin] (slugified)", ff.calls)
	}
}

func TestResolve_NotFoundReturnsErrNotFound(t *testing.T) {
	svc, _ := newTestService(t)

	_, err := svc.Resolve(context.Background(), Request{Name: "ghost", Dir: t.TempDir()})
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestResolve_UnslugifiableNameSkipsFetch(t *testing.T) {
	svc, ff := newTestService(t)

	_, err := svc.Resolve(context.Background(), Request{Name: "!!!", Dir: t.TempDir()})
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
	if len(ff.calls) != 0 {
		t.Errorf("fetcher called %v, want no call for empty slug", ff.calls)
	}
}

func TestResolve_CachesPositiveResult(t *testing.T) {
	svc, ff := newTestService(t)
	ff.icons["media"] = []byte("<svg>m</svg>")
	dir := t.TempDir()

	for i := range 2 {
		got, err := svc.Resolve(context.Background(), Request{Name: "media", Dir: dir})
		if err != nil {
			t.Fatalf("Resolve #%d: %v", i, err)
		}
		if string(got.Data) != "<svg>m</svg>" {
			t.Errorf("data = %q", got.Data)
		}
	}
	if len(ff.calls) != 1 {
		t.Errorf("fetcher calls = %d, want 1 (second served from cache)", len(ff.calls))
	}
}

func TestResolve_CachesNonSvgContentTypes(t *testing.T) {
	cases := []struct{ ct, data string }{
		{"image/png", "PNGDATA"},
		{"image/webp", "WEBPDATA"},
	}
	for _, tc := range cases {
		t.Run(tc.ct, func(t *testing.T) {
			svc, ff := newTestService(t)
			ff.contentType = tc.ct
			ff.icons["media"] = []byte(tc.data)
			dir := t.TempDir()

			for i := range 2 {
				got, err := svc.Resolve(context.Background(), Request{Name: "media", Dir: dir})
				if err != nil {
					t.Fatalf("Resolve #%d: %v", i, err)
				}
				if string(got.Data) != tc.data || got.ContentType != tc.ct {
					t.Errorf("#%d got %q / %q, want %q / %q", i, got.Data, got.ContentType, tc.data, tc.ct)
				}
			}
			if len(ff.calls) != 1 {
				t.Errorf("fetcher calls = %d, want 1 (second served from cache with correct type)", len(ff.calls))
			}
		})
	}
}

func TestResolve_CachesNegativeResult(t *testing.T) {
	svc, ff := newTestService(t)
	dir := t.TempDir()

	for i := range 2 {
		_, err := svc.Resolve(context.Background(), Request{Name: "ghost", Dir: dir})
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("Resolve #%d err = %v, want ErrNotFound", i, err)
		}
	}
	if len(ff.calls) != 1 {
		t.Errorf("fetcher calls = %d, want 1 (second served from negative cache)", len(ff.calls))
	}
}

func TestResolve_FetchErrorIsNotCached(t *testing.T) {
	svc, ff := newTestService(t)
	ff.err = errors.New("network down")
	dir := t.TempDir()

	_, err := svc.Resolve(context.Background(), Request{Name: "media", Dir: dir})
	if err == nil || errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want a transport error (not ErrNotFound)", err)
	}

	// Source recovers: a transient failure must not have been cached.
	ff.err = nil
	ff.icons["media"] = []byte("<svg>m</svg>")
	got, err := svc.Resolve(context.Background(), Request{Name: "media", Dir: dir})
	if err != nil {
		t.Fatalf("Resolve after recovery: %v", err)
	}
	if string(got.Data) != "<svg>m</svg>" {
		t.Errorf("data = %q", got.Data)
	}
	if len(ff.calls) != 2 {
		t.Errorf("fetcher calls = %d, want 2 (error not cached, so retried)", len(ff.calls))
	}
}

func TestResolve_SanitizesSlugStaysInCacheDir(t *testing.T) {
	cacheDir := t.TempDir()
	ff := &fakeFetcher{icons: map[string][]byte{"etc-passwd": []byte("<svg/>")}}
	svc := New(cacheDir, ff)

	got, err := svc.Resolve(context.Background(), Request{Name: "x", Slug: "../../etc/passwd", Dir: t.TempDir()})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if string(got.Data) != "<svg/>" {
		t.Errorf("data = %q", got.Data)
	}
	if len(ff.calls) != 1 || ff.calls[0] != "etc-passwd" {
		t.Errorf("fetcher calls = %v, want [etc-passwd] (traversal stripped)", ff.calls)
	}
	// Every cache entry must live directly inside cacheDir.
	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "etc-passwd.svg" {
		t.Errorf("cache entries = %v, want [etc-passwd.svg]", entries)
	}
}

func TestClearCache_RemovesPositiveAndNegative(t *testing.T) {
	svc, ff := newTestService(t)
	ff.icons["media"] = []byte("<svg>m</svg>")
	dir := t.TempDir()

	// One positive (media) and one negative (ghost) cache entry.
	if _, err := svc.Resolve(context.Background(), Request{Name: "media", Dir: dir}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Resolve(context.Background(), Request{Name: "ghost", Dir: dir}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}

	if err := svc.ClearCache(); err != nil {
		t.Fatalf("ClearCache: %v", err)
	}

	// Both are re-fetched after a clear (2 initial + 2 after = 4 total).
	if _, err := svc.Resolve(context.Background(), Request{Name: "media", Dir: dir}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Resolve(context.Background(), Request{Name: "ghost", Dir: dir}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if len(ff.calls) != 4 {
		t.Errorf("fetcher calls = %d, want 4 (cache cleared, both re-fetched)", len(ff.calls))
	}
}

func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"media":            "media",
		"Jellyfin":         "jellyfin",
		"My App!":          "my-app",
		"Home-Assistant":   "home-assistant",
		"a__b":             "a-b",
		"  spaced  ":       "spaced",
		"../../etc/passwd": "etc-passwd",
		"!!!":              "",
		"":                 "",
	}
	for in, want := range cases {
		if got := slugify(in); got != want {
			t.Errorf("slugify(%q) = %q, want %q", in, got, want)
		}
	}
}

// captureLogs routes the default slog output into a buffer for the test's
// duration, so an emitted warning is a positive, assertable signal.
func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

// readOnlyCacheService returns a Service whose cache directory exists but
// rejects writes, so every cache write fails while lookups still work.
func readOnlyCacheService(t *testing.T, ff *fakeFetcher) *Service {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("running as root: the directory mode below would not deny the write")
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(dir, 0o700); err != nil {
			t.Errorf("restoring the directory mode: %v", err)
		}
	})
	return New(dir, ff)
}

// A broken cache dir means every request refetches; that must be visible in
// the logs, not a silent forever-refetch.
func TestResolve_WarnsWhenPositiveCacheWriteFails(t *testing.T) {
	ff := &fakeFetcher{icons: map[string][]byte{"media": []byte("<svg/>")}}
	svc := readOnlyCacheService(t, ff)

	buf := captureLogs(t)
	got, err := svc.Resolve(context.Background(), Request{Name: "media"})
	if err != nil || string(got.Data) != "<svg/>" {
		t.Fatalf("Resolve must still serve the icon on a cache failure, got %v, %v", got, err)
	}
	if !strings.Contains(buf.String(), "icon cache write failed") {
		t.Errorf("expected a warning about the failed cache write, got logs: %q", buf.String())
	}
}

func TestResolve_WarnsWhenNegativeCacheWriteFails(t *testing.T) {
	svc := readOnlyCacheService(t, &fakeFetcher{})

	buf := captureLogs(t)
	if _, err := svc.Resolve(context.Background(), Request{Name: "ghost"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Resolve = %v, want ErrNotFound", err)
	}
	if !strings.Contains(buf.String(), "icon cache write failed") {
		t.Errorf("expected a warning about the failed cache write, got logs: %q", buf.String())
	}
}

func TestResolve_WarnsWhenCacheDirCannotBeCreated(t *testing.T) {
	file := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	ff := &fakeFetcher{icons: map[string][]byte{"media": []byte("<svg/>")}}
	svc := New(filepath.Join(file, "cache"), ff)

	buf := captureLogs(t)
	if _, err := svc.Resolve(context.Background(), Request{Name: "media"}); err != nil {
		t.Fatalf("Resolve must still serve the icon on a cache failure: %v", err)
	}
	if !strings.Contains(buf.String(), "icon cache dir creation failed") {
		t.Errorf("expected a warning about the failed cache dir, got logs: %q", buf.String())
	}
}
