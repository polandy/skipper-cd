// Package prettylog implements the "pretty" log_format (docs/configuration.md):
// a colored, icon-led slog.Handler for interactive console use. It recognizes
// skipper-cd's own deploy-lifecycle messages and renders them as a short
// narrative that mirrors the web UI's Deploys view (dev-docs/adr/0042-pretty-console-log.md);
// every other record still renders cleanly via a level-based fallback, so a
// log line anywhere in the codebase is never dropped or garbled.
package prettylog

import (
	"context"
	"io"
	"log/slog"
	"os"
	"sync"
)

// Handler is a slog.Handler rendering colored, icon-led lines for
// interactive terminals. Color is auto-detected from the destination (a
// terminal, unless NO_COLOR is set — https://no-color.org); piping to a file
// or journald falls back to plain (uncolored) icons.
type Handler struct {
	mu    *sync.Mutex
	w     io.Writer
	level slog.Level
	color bool
	attrs []slog.Attr // bound via WithAttrs, applied before the record's own
	group string      // dotted prefix from WithGroup, applied to attr keys
}

// New returns a Handler writing records of at least level to w.
func New(w io.Writer, level slog.Level) *Handler {
	return &Handler{mu: &sync.Mutex{}, w: w, level: level, color: colorEnabled(w)}
}

// colorEnabled reports whether ANSI color should be emitted: w must be a
// terminal (not a pipe/file/buffer) and NO_COLOR must be unset.
func colorEnabled(w io.Writer) bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

// Enabled reports whether level reaches the threshold this handler was
// built with (log_level, default Info).
func (h *Handler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level
}

// WithAttrs returns a copy with attrs bound ahead of every future record's
// own attrs.
func (h *Handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return h
	}
	merged := make([]slog.Attr, 0, len(h.attrs)+len(attrs))
	merged = append(merged, h.attrs...)
	merged = append(merged, attrs...)
	return &Handler{mu: h.mu, w: h.w, color: h.color, attrs: merged, group: h.group}
}

// WithGroup returns a copy that dot-prefixes subsequent attr keys with name.
func (h *Handler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	group := name
	if h.group != "" {
		group = h.group + "." + name
	}
	return &Handler{mu: h.mu, w: h.w, color: h.color, attrs: h.attrs, group: group}
}

// Handle renders one record and writes it to the destination.
func (h *Handler) Handle(_ context.Context, r slog.Record) error {
	line := render(r, collectAttrs(h.attrs, h.group, r), h.color)
	h.mu.Lock()
	defer h.mu.Unlock()
	_, err := io.WriteString(h.w, line)
	return err
}
