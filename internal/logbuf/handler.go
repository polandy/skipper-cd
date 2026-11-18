package logbuf

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"strings"
)

// Handler is a slog.Handler that appends every record to a Log and then
// delegates to the wrapped handler, so the captured stream matches the
// wrapped handler's output. It must not log via slog itself (that would
// recurse through this handler).
//
// Group structure is flattened into dotted attr keys (e.g. "http.method");
// attr values are stringified, so value ordering and typing are lossy
// compared to the wrapped handler's native output.
type Handler struct {
	next   slog.Handler
	log    *Log
	attrs  map[string]string // pre-bound via WithAttrs, keys already group-prefixed
	groups []string          // open groups from WithGroup
}

// NewHandler returns a Handler capturing records into log and delegating
// to next.
func NewHandler(next slog.Handler, log *Log) *Handler {
	return &Handler{next: next, log: log}
}

// Enabled delegates to the wrapped handler so capture and output agree on
// level filtering.
func (h *Handler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

// Handle appends the record to the Log, then delegates to the wrapped
// handler. Appending first means a write error downstream cannot lose the
// entry.
func (h *Handler) Handle(ctx context.Context, r slog.Record) error {
	attrs := maps.Clone(h.attrs)
	if attrs == nil && r.NumAttrs() > 0 {
		attrs = make(map[string]string, r.NumAttrs())
	}
	prefix := ""
	if len(h.groups) > 0 {
		prefix = strings.Join(h.groups, ".") + "."
	}
	r.Attrs(func(a slog.Attr) bool {
		flattenAttr(attrs, prefix, a)
		return true
	})
	h.log.Append(r.Time, r.Level.String(), r.Message, attrs)
	return h.next.Handle(ctx, r)
}

// WithAttrs returns a copy with the attrs pre-bound; the wrapped handler
// receives them natively.
func (h *Handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	merged := maps.Clone(h.attrs)
	if merged == nil {
		merged = make(map[string]string, len(attrs))
	}
	prefix := ""
	if len(h.groups) > 0 {
		prefix = strings.Join(h.groups, ".") + "."
	}
	for _, a := range attrs {
		flattenAttr(merged, prefix, a)
	}
	return &Handler{next: h.next.WithAttrs(attrs), log: h.log, attrs: merged, groups: h.groups}
}

// WithGroup returns a copy that prefixes subsequent attr keys with the
// group name; the wrapped handler keeps its native grouping.
func (h *Handler) WithGroup(name string) slog.Handler {
	groups := make([]string, len(h.groups), len(h.groups)+1)
	copy(groups, h.groups)
	groups = append(groups, name)
	return &Handler{next: h.next.WithGroup(name), log: h.log, attrs: h.attrs, groups: groups}
}

// The ring is bounded and every entry is streamed to every connected browser,
// so a captured attr value is clamped: a record that attaches a payload (a
// file diff is up to 10 KB) would evict real history and push kilobytes per
// line down /api/logs. The wrapped handler still receives the record in full —
// the console is the surface such a payload exists for.
//
// A multi-line value is clamped by *lines*, since that is the unit its reader
// thinks in: "12 lines omitted" says how much of a diff is missing, where a
// byte count says nothing anyone can act on. maxAttrValueLen is the ceiling
// for a value with few but very long lines, so one runaway line cannot slip
// past the line budget.
const (
	maxAttrValueLines = 40
	maxAttrValueLen   = 4096
)

// flattenAttr stringifies a into dst under prefix, expanding group values
// into dotted keys. An oversized value is summarised rather than dropped, so
// the entry never silently pretends the attr was short.
func flattenAttr(dst map[string]string, prefix string, a slog.Attr) {
	v := a.Value.Resolve()
	if v.Kind() == slog.KindGroup {
		for _, ga := range v.Group() {
			flattenAttr(dst, prefix+a.Key+".", ga)
		}
		return
	}
	dst[prefix+a.Key] = clampValue(v.String())
}

// clampValue shortens s to the line and byte budgets above, announcing in each
// case what was left out so the entry never pretends the value was short.
func clampValue(s string) string {
	if total := strings.Count(s, "\n"); total >= maxAttrValueLines {
		kept := strings.SplitN(s, "\n", maxAttrValueLines+1)[:maxAttrValueLines]
		return strings.Join(kept, "\n") + fmt.Sprintf("\n… (%d lines omitted)", total-maxAttrValueLines)
	}
	if len(s) > maxAttrValueLen {
		return s[:maxAttrValueLen] + "… (truncated)"
	}
	return s
}
