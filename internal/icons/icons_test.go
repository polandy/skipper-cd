package icons

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// fakeFetcher is an in-memory Fetcher. A slug present in icons is a hit; an
// absent slug is a definitive miss (found == false). A non-nil err simulates a
// transient transport failure and takes precedence.
type fakeFetcher struct {
	icons map[string][]byte
	err   error
	calls []string
}

func (f *fakeFetcher) Fetch(_ context.Context, slug string) (data []byte, contentType string, found bool, err error) {
	f.calls = append(f.calls, slug)
	if f.err != nil {
		return nil, "", false, f.err
	}
	if d, ok := f.icons[slug]; ok {
		return d, "image/svg+xml", true, nil
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
