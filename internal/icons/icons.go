// Package icons resolves and caches recognizable icons for the stacks shown in
// the web UI. An icon is resolved per stack in priority order: a repo override
// file in the stack directory, then a configured slug override, then an
// auto-match on the stack name against an external icon set. Results (hits and
// definitive misses alike) are cached on disk so each stack is fetched at most
// once. Nothing here can block or fail a deploy — it is UI-only.
package icons

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ErrNotFound reports that no icon could be resolved for a stack. Callers map
// it to a 404 so the UI falls back to a monogram.
var ErrNotFound = errors.New("icon not found")

// Fetcher retrieves an icon for a slug from an external icon set. It is an
// interface so tests inject a fake instead of hitting the network.
//
// found == false is a definitive miss (the set has no such icon) and is cached
// negatively. A non-nil err is a transient transport failure and is not cached,
// so the next request retries.
type Fetcher interface {
	Fetch(ctx context.Context, slug string) (data []byte, contentType string, found bool, err error)
}

// Request identifies a stack whose icon to resolve.
type Request struct {
	Name string // stack name; drives auto-match and the monogram fallback
	Slug string // optional config icon: override; empty derives the slug from Name
	Dir  string // stack directory in the clone, holding an optional icon.svg/png
}

// Result is a resolved icon ready to serve.
type Result struct {
	Data        []byte
	ContentType string
}

// Service resolves stack icons against a local cache and a Fetcher.
type Service struct {
	cacheDir string
	fetcher  Fetcher
}

// New returns a Service caching icons under cacheDir and fetching cache misses
// through fetcher.
func New(cacheDir string, fetcher Fetcher) *Service {
	return &Service{cacheDir: cacheDir, fetcher: fetcher}
}

// Resolve returns the icon for a stack following the priority order:
// repo override → configured slug → auto-match on the stack name. It returns
// ErrNotFound when nothing matches (including a definitive fetch miss).
func (s *Service) Resolve(ctx context.Context, req Request) (Result, error) {
	if res, ok := repoOverride(req.Dir); ok {
		return res, nil
	}

	slug := slugify(req.Slug)
	if slug == "" {
		slug = slugify(req.Name)
	}
	if slug == "" {
		return Result{}, ErrNotFound
	}

	if res, state := s.cacheLookup(slug); state != cacheUnknown {
		if state == cacheMiss {
			return Result{}, ErrNotFound
		}
		return res, nil
	}

	data, contentType, found, err := s.fetcher.Fetch(ctx, slug)
	if err != nil {
		return Result{}, fmt.Errorf("fetch icon %q: %w", slug, err)
	}
	if !found {
		s.cacheNegative(slug)
		return Result{}, ErrNotFound
	}

	s.cachePositive(slug, contentType, data)
	return Result{Data: data, ContentType: contentType}, nil
}

// ClearCache removes every cached icon, positive and negative, so renamed
// stacks and newly published icons are picked up on the next Resolve.
func (s *Service) ClearCache() error {
	entries, err := os.ReadDir(s.cacheDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read icon cache: %w", err)
	}
	for _, e := range entries {
		if err := os.Remove(filepath.Join(s.cacheDir, e.Name())); err != nil {
			return fmt.Errorf("clear icon cache: %w", err)
		}
	}
	return nil
}

// repoOverride serves a stack-provided icon file (svg preferred, then png)
// from the stack directory. These are always available offline and never
// touch the cache or the network.
func repoOverride(dir string) (Result, bool) {
	if dir == "" {
		return Result{}, false
	}
	for _, ext := range []string{".svg", ".png"} {
		data, err := os.ReadFile(filepath.Join(dir, "icon"+ext))
		if err == nil {
			return Result{Data: data, ContentType: contentTypeForExt(ext)}, true
		}
	}
	return Result{}, false
}

type cacheState int

const (
	cacheUnknown cacheState = iota // not cached; fetch needed
	cacheHit                       // positive entry present
	cacheMiss                      // negative marker present
)

const negativeExt = ".missing"

func (s *Service) cacheLookup(slug string) (Result, cacheState) {
	for _, ext := range []string{".svg", ".png"} {
		data, err := os.ReadFile(filepath.Join(s.cacheDir, slug+ext))
		if err == nil {
			return Result{Data: data, ContentType: contentTypeForExt(ext)}, cacheHit
		}
	}
	if _, err := os.Stat(filepath.Join(s.cacheDir, slug+negativeExt)); err == nil {
		return Result{}, cacheMiss
	}
	return Result{}, cacheUnknown
}

func (s *Service) cachePositive(slug, contentType string, data []byte) {
	if err := os.MkdirAll(s.cacheDir, 0o755); err != nil {
		return // caching is best-effort; a failure just means a re-fetch next time
	}
	_ = os.WriteFile(filepath.Join(s.cacheDir, slug+extForContentType(contentType)), data, 0o644)
}

func (s *Service) cacheNegative(slug string) {
	if err := os.MkdirAll(s.cacheDir, 0o755); err != nil {
		return
	}
	_ = os.WriteFile(filepath.Join(s.cacheDir, slug+negativeExt), nil, 0o644)
}

// slugify reduces a name to a filesystem-safe token: lowercase, runs of
// non-alphanumeric characters collapsed to a single dash, no leading or
// trailing dash. This also neutralizes path traversal (separators and dots
// never survive), so the result is safe as a cache filename and URL segment.
func slugify(s string) string {
	var b strings.Builder
	prevDash := false
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevDash = false
		default:
			if !prevDash && b.Len() > 0 {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	return strings.TrimRight(b.String(), "-")
}

func contentTypeForExt(ext string) string {
	if ext == ".png" {
		return "image/png"
	}
	return "image/svg+xml"
}

func extForContentType(contentType string) string {
	if contentType == "image/png" {
		return ".png"
	}
	return ".svg"
}
