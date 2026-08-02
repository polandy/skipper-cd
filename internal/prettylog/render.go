package prettylog

import (
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// ANSI SGR codes, deliberately the basic 16-color set (not 256-color) so the
// viewer's own terminal theme remaps them, rather than imposing a fixed
// palette that may clash with it.
const (
	ansiReset    = "\x1b[0m"
	ansiDim      = "\x1b[2m"
	ansiBold     = "\x1b[1m"
	ansiAccent   = "\x1b[36m" // cyan: system/lifecycle events (sync, webhook, startup)
	ansiSuccess  = "\x1b[32m" // green: deployed, self-heal restored
	ansiWarn     = "\x1b[33m" // yellow: queued/deferred
	ansiDanger   = "\x1b[31m" // red: failed, unhealthy
	ansiRollback = "\x1b[35m" // magenta: rolled back (recovered, but not the intended outcome)
)

// colorize wraps s in code when enabled; a blank code or disabled color
// returns s unchanged.
func colorize(enabled bool, code, s string) string {
	if !enabled || code == "" || s == "" {
		return s
	}
	return code + s + ansiReset
}

// style is a known "anchor" message's fixed rendering: the icon and color for
// its own line, whether it nests under the enclosing stack's block, and an
// optional custom body (falls back to genericBody when nil).
type style struct {
	glyph  string
	color  string
	indent bool
	gap    bool // blank line before this record — a new stack block starting
	body   func(attrs []attr, color bool) string
	// block renders extra lines *below* the record's own line, already
	// newline-terminated, for a record that carries more than one line's worth
	// of detail. Empty output means nothing is appended.
	block func(attrs []attr, color bool) string
}

// anchors maps the exact message text of skipper-cd's deploy-lifecycle log
// lines (internal/deploy, internal/git, internal/webhook, plus the
// pretty-only lines in msgs.go) to their narrative rendering. The message
// text is intentionally duplicated from those packages rather than shared via
// an import: this is a display-layer concern and must not gain influence over
// core packages' log wording. A message that drifts from this table simply
// falls through to the generic level-based renderer — never dropped.
var anchors = map[string]style{
	"starting deploy run":                             {glyph: "⇢", color: ansiAccent, gap: true, body: bodyStartingRun},
	"webhook accepted, starting deploy in background": {glyph: "⇢", color: ansiAccent, gap: true, body: bodyPlain("webhook received, deploying")},
	"pulling latest commits":                          {glyph: "⇢", color: ansiAccent, body: bodySync},
	"cloning repository":                              {glyph: "⇢", color: ansiAccent, body: bodySync},

	"deploying stack":               {glyph: "▸", body: bodyDeployingStack},
	"running deploy hook":           {glyph: "↳", indent: true, body: bodyDeployHook},
	"deploy complete":               {glyph: "✓", color: ansiSuccess, body: bodyDeployComplete},
	"deploy failed":                 {glyph: "✗", color: ansiDanger, body: bodyFailed("failed", ansiDanger)},
	"deploy failed but rolled back": {glyph: "↺", color: ansiRollback, body: bodyFailed("rolled back", ansiRollback)},
	"deploy failed, rollback ran but stack is still unhealthy": {glyph: "↺", color: ansiDanger, body: bodyFailed("rolled back · still unhealthy", ansiDanger)},
	"skipping stack, no changes detected":                      {glyph: "▪", color: ansiDim, body: bodySkipped},
	"deploy deferred: autosync paused":                         {glyph: "▪", color: ansiWarn, body: bodyDeferred},
	"self-heal: restoring stack to its deployed running state": {glyph: "⟲", color: ansiSuccess, body: bodySelfHeal("self-heal: restoring")},
	"self-heal: stack restored":                                {glyph: "⟲", color: ansiSuccess, body: bodySelfHeal("self-heal: restored")},
	"file changed":                                             {glyph: "↳", indent: true, body: bodyFileChanged, block: blockDiff},

	// Multi-host fan-in (ADR-0048): the startup line and per-peer reachability
	// edges. Message text mirrors cmd/skipper and internal/peers by hand — same
	// display-layer-owns-its-strings convention as the deploy lines above.
	"multi-host fan-in enabled": {glyph: "⇢", color: ansiAccent, body: bodyFanIn},
	"peer unreachable":          {glyph: "▲", color: ansiWarn, body: bodyPeer("unreachable", ansiWarn)},
	"peer reachable again":      {glyph: "✓", color: ansiSuccess, body: bodyPeer("reachable again", ansiSuccess)},
}

// render renders one slog.Record as a single line (including its trailing
// newline).
func render(r slog.Record, attrs []attr, color bool) string {
	switch r.Message {
	case MsgRunComplete:
		glyph, code := runCompleteTone(attrs)
		return renderLine(r.Time, glyph, code, false, true, bodyRunComplete(attrs, color), color)
	case MsgStacksResolved:
		return renderLine(r.Time, "▣", ansiBold, false, true, bodyStacksResolved(attrs, color), color)
	case MsgStackDiscovered:
		return renderLine(r.Time, "◆", ansiAccent, false, false, bodyStackDiscovered(attrs, color), color)
	case MsgStacksDisabled:
		return renderLine(r.Time, "▪", ansiDim, false, false, bodyStacksDisabled(attrs, color), color)
	}
	if st, ok := anchors[r.Message]; ok {
		line := renderLine(r.Time, st.glyph, st.color, st.indent, st.gap, st.body(attrs, color), color)
		if st.block != nil {
			line += st.block(attrs, color)
		}
		return line
	}
	glyph, code := levelGlyph(r.Level)
	indent := hasKey(attrs, "stack")
	return renderLine(r.Time, glyph, code, indent, false, genericBody(r.Message, attrs, color), color)
}

func renderLine(t time.Time, glyph, code string, indent, gap bool, body string, color bool) string {
	var b strings.Builder
	if gap {
		b.WriteByte('\n')
	}
	b.WriteString(colorize(color, ansiDim, t.Format("15:04:05")))
	b.WriteString("  ")
	if indent {
		b.WriteString(colorize(color, ansiDim, "  ↳ "))
	} else {
		b.WriteString(colorize(color, code, glyph))
		b.WriteString(" ")
	}
	b.WriteString(body)
	b.WriteByte('\n')
	return b.String()
}

// levelGlyph is the fallback icon/color for any message not in anchors.
func levelGlyph(level slog.Level) (glyph, color string) {
	switch {
	case level >= slog.LevelError:
		return "✗", ansiDanger
	case level >= slog.LevelWarn:
		return "▲", ansiWarn
	case level >= slog.LevelInfo:
		return "·", ""
	default:
		return "·", ansiDim
	}
}

// genericBody renders message and its attrs (dim "key=value" pairs) for any
// record that has no anchor-specific rendering.
func genericBody(message string, attrs []attr, color bool) string {
	var b strings.Builder
	b.WriteString(message)
	for _, a := range attrs {
		fmt.Fprintf(&b, "%s", colorize(color, ansiDim, fmt.Sprintf("  %s=%s", a.key, a.val.String())))
	}
	return b.String()
}

func bodyPlain(text string) func([]attr, bool) string {
	return func([]attr, bool) string { return text }
}

func bodyStartingRun(attrs []attr, color bool) string {
	return colorize(color, ansiBold, "run starting") + colorize(color, ansiDim, "  · "+str(attrs, "stacks")+" stacks")
}

func bodySync(attrs []attr, color bool) string {
	branch := str(attrs, "branch")
	if branch == "" {
		return "sync"
	}
	return "sync" + colorize(color, ansiDim, "  branch="+branch)
}

// changeSummary renders how many files changed, without dumping the whole
// path list onto one line (watch_dirs contents can be long).
func changeSummary(files []string) string {
	switch len(files) {
	case 0:
		return "changed"
	case 1:
		return "changed · " + files[0]
	default:
		return fmt.Sprintf("changed · %d files", len(files))
	}
}

func bodyDeployingStack(attrs []attr, color bool) string {
	stack := colorize(color, ansiBold, str(attrs, "stack"))
	return stack + "  " + colorize(color, ansiDim, changeSummary(strSlice(attrs, "changed_files")))
}

func bodyDeployComplete(attrs []attr, color bool) string {
	return colorize(color, ansiBold, str(attrs, "stack")) + "  " + colorize(color, ansiSuccess, "deployed")
}

func bodyFailed(label, tone string) func([]attr, bool) string {
	return func(attrs []attr, color bool) string {
		line := colorize(color, ansiBold, str(attrs, "stack")) + "  " + colorize(color, tone, label)
		if e := str(attrs, "err"); e != "" {
			line += colorize(color, ansiDim, "  — "+e)
		}
		return line
	}
}

func bodySkipped(attrs []attr, color bool) string {
	return colorize(color, ansiDim, str(attrs, "stack")+"  unchanged, skipped")
}

func bodyDeferred(attrs []attr, color bool) string {
	return colorize(color, ansiBold, str(attrs, "stack")) + "  " + colorize(color, ansiWarn, "deferred · autosync paused")
}

func bodySelfHeal(label string) func([]attr, bool) string {
	return func(attrs []attr, color bool) string {
		return colorize(color, ansiBold, str(attrs, "stack")) + "  " + colorize(color, ansiSuccess, label)
	}
}

// bodyDeployHook renders the 0-based "index" attr (internal/deploy/hooks.go)
// as a 1-based step number, matching how a human counts hook commands.
func bodyDeployHook(attrs []attr, color bool) string {
	return colorize(color, ansiDim, fmt.Sprintf("%s [%d]", str(attrs, "phase"), intAttr(attrs, "index")+1))
}

func bodyFileChanged(attrs []attr, color bool) string {
	return colorize(color, ansiDim, str(attrs, "file"))
}

// diffIndent aligns a diff block under the file name it belongs to, past the
// timestamp column, so the block reads as that file's detail rather than as
// further log lines.
const diffIndent = "      "

// blockDiff renders the changed file's diff below its name, one line per diff
// line in the usual add/remove/hunk colours. The console is the only surface
// with no way to *fetch* the diff — the web UI opens it from the deploy event
// on demand — so here it is printed inline. The content is already capped
// upstream (10 KB per file, internal/deploy), which bounds this block too.
func blockDiff(attrs []attr, color bool) string {
	diff := str(attrs, "diff")
	if diff == "" {
		return ""
	}
	var b strings.Builder
	for _, line := range strings.Split(strings.TrimRight(diff, "\n"), "\n") {
		b.WriteString(diffIndent)
		b.WriteString(colorize(color, diffLineColor(line), line))
		b.WriteByte('\n')
	}
	return b.String()
}

// diffLineColor picks a unified-diff line's colour. The `+++`/`---` file
// headers are checked before the plain `+`/`-` cases so they read as metadata
// rather than as a huge addition and removal.
func diffLineColor(line string) string {
	switch {
	case strings.HasPrefix(line, "+++"), strings.HasPrefix(line, "---"):
		return ansiDim
	case strings.HasPrefix(line, "@@"):
		return ansiWarn
	case strings.HasPrefix(line, "+"):
		return ansiSuccess
	case strings.HasPrefix(line, "-"):
		return ansiDanger
	default:
		return ansiDim
	}
}

// bodyFanIn narrates the multi-host startup line: how many peers are watched
// and on what cadence.
func bodyFanIn(attrs []attr, color bool) string {
	return colorize(color, ansiBold, "multi-host fan-in") +
		colorize(color, ansiDim, fmt.Sprintf("  · %s peers · poll %ss", str(attrs, "peers"), str(attrs, "poll_interval_seconds")))
}

// bodyPeer renders a per-peer reachability edge — the host name in bold, its new
// state in the given tone, and (when going down) the failure reason.
func bodyPeer(label, tone string) func([]attr, bool) string {
	return func(attrs []attr, color bool) string {
		line := colorize(color, ansiBold, "peer "+str(attrs, "peer")) + "  " + colorize(color, tone, label)
		if e := str(attrs, "err"); e != "" {
			line += colorize(color, ansiDim, "  — "+e)
		}
		return line
	}
}

// runCompleteTone picks the summary line's icon/color from the worst outcome
// present: unhealthy rollback > plain failure > rollback > success > idle.
func runCompleteTone(attrs []attr) (glyph, color string) {
	switch {
	case intAttr(attrs, "failed") > 0 || intAttr(attrs, "rolled_back_unhealthy") > 0:
		return "✗", ansiDanger
	case intAttr(attrs, "rolled_back") > 0:
		return "↺", ansiRollback
	case intAttr(attrs, "deployed") > 0:
		return "✓", ansiSuccess
	default:
		return "·", ansiDim
	}
}

func bodyRunComplete(attrs []attr, color bool) string {
	type part struct {
		n     int64
		label string
		tone  string
	}
	order := []part{
		{intAttr(attrs, "deployed"), "deployed", ansiSuccess},
		{intAttr(attrs, "rolled_back"), "rolled back", ansiRollback},
		{intAttr(attrs, "rolled_back_unhealthy"), "rolled back · unhealthy", ansiDanger},
		{intAttr(attrs, "queued"), "queued", ansiWarn},
		{intAttr(attrs, "blocked"), "blocked", ansiWarn},
		{intAttr(attrs, "skipped"), "skipped", ansiDim},
		{intAttr(attrs, "failed"), "failed", ansiDanger},
	}
	var parts []string
	for _, p := range order {
		if p.n == 0 {
			continue
		}
		parts = append(parts, colorize(color, p.tone, fmt.Sprintf("%d %s", p.n, p.label)))
	}
	if len(parts) == 0 {
		return colorize(color, ansiDim, "run complete · no changes")
	}
	return colorize(color, ansiBold, "run complete") + "  " + strings.Join(parts, colorize(color, ansiDim, " · "))
}

func bodyStacksResolved(attrs []attr, color bool) string {
	return colorize(color, ansiBold, "stacks") + colorize(color, ansiDim, "  · "+str(attrs, "stacks")+" discovered")
}

func bodyStacksDisabled(attrs []attr, color bool) string {
	names := strSlice(attrs, "stacks")
	return colorize(color, ansiDim, fmt.Sprintf("parked · disabled: %s", strings.Join(names, ", ")))
}

func bodyStackDiscovered(attrs []attr, color bool) string {
	name := colorize(color, ansiBold, str(attrs, "stack"))

	hooks := "—"
	pre, post := intAttr(attrs, "pre_deploy_hooks"), intAttr(attrs, "post_deploy_hooks")
	if pre+post > 0 {
		var hp []string
		if pre > 0 {
			hp = append(hp, fmt.Sprintf("pre_deploy·%d", pre))
		}
		if post > 0 {
			hp = append(hp, fmt.Sprintf("post_deploy·%d", post))
		}
		hooks = strings.Join(hp, " ")
	}

	watch := "—"
	if dirs := strSlice(attrs, "watch_dirs"); len(dirs) > 0 {
		watch = strings.Join(dirs, ", ")
	}

	return name + colorize(color, ansiDim, fmt.Sprintf("  hooks %s   watch %s", hooks, watch))
}
