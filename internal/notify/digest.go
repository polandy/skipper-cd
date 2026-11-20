package notify

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/polandy/skipper-cd/internal/events"
)

// maxDigestEvents bounds one run's buffered successes; past it a stack is only
// counted, and the digest says how many it could not name.
const maxDigestEvents = 4 * notifyBufferSize

// maxDigestImageLines caps the image-change lines of a digest message.
const maxDigestImageLines = 10

// Digest wording; German like the rest of the compact messages (compact.go).
const (
	wordStacksDeployed = "Stacks deployt"
	wordHealthGated    = "mit Health-Gate geprüft"
	wordMore           = "weitere"
	digestGateMark     = "✓ "
	digestBullet       = "• "
	digestKind         = "run_digest"
)

// digestBuffer collects one run's success events until the run ends.
type digestBuffer struct {
	events   []events.DeployEvent
	overflow int
}

func (b *digestBuffer) add(ev events.DeployEvent) {
	if len(b.events) >= maxDigestEvents {
		b.overflow++
		return
	}
	b.events = append(b.events, ev)
}

// take returns the run's digest and empties the buffer; false when the run
// had no success.
func (b *digestBuffer) take(end time.Time) (runDigest, bool) {
	if len(b.events) == 0 && b.overflow == 0 {
		return runDigest{}, false
	}
	evs := slices.Clone(b.events)
	slices.SortStableFunc(evs, func(a, b events.DeployEvent) int { return strings.Compare(a.Stack, b.Stack) })
	d := runDigest{events: evs, overflow: b.overflow, end: end}
	*b = digestBuffer{}
	return d, true
}

// runDigest is one run's successes, sorted by stack.
type runDigest struct {
	events   []events.DeployEvent
	overflow int       // successes past maxDigestEvents, counted but not kept
	end      time.Time // when the run ended
}

// single reports whether the digest stands for exactly one success, which is
// sent as its ordinary message instead.
func (d runDigest) single() bool { return len(d.events) == 1 && d.overflow == 0 }

func (d runDigest) count() int { return len(d.events) + d.overflow }

func (d runDigest) stacks() []string {
	names := make([]string, 0, len(d.events))
	for _, ev := range d.events {
		names = append(names, ev.Stack)
	}
	return names
}

// span is the run's wall-clock time from the earliest deploy start to the end
// of the run — not the sum of the deploys.
func (d runDigest) span() time.Duration {
	start := d.end
	for _, ev := range d.events {
		if s := ev.Timestamp.Add(-time.Duration(ev.DurationMs) * time.Millisecond); s.Before(start) {
			start = s
		}
	}
	return d.end.Sub(start).Round(time.Second)
}

// composeDigest builds the compact message of a run's digest.
func composeDigest(d runDigest) compactMessage {
	names := joinNames(d.stacks())
	if d.overflow > 0 {
		names += fmt.Sprintf("%s+%d %s", groupNameSeparator, d.overflow, wordMore)
	}
	lines := []string{names}
	if n := d.gated(); n > 0 {
		lines = append(lines, fmt.Sprintf("%s%d %s", digestGateMark, n, wordHealthGated))
	}
	lines = append(lines, d.imageLines()...)
	return compactMessage{
		Title:   fmt.Sprintf("%d %s (%s)", d.count(), wordStacksDeployed, d.span()),
		Body:    strings.Join(lines, "\n"),
		Success: true,
	}
}

func (d runDigest) gated() int {
	n := 0
	for _, ev := range d.events {
		if ev.HealthGated {
			n++
		}
	}
	return n
}

// imageLines is one "• stack/service: from → to" line per moved image, in the
// same reduction as a single message, capped at maxDigestImageLines.
func (d runDigest) imageLines() []string {
	var lines []string
	for _, ev := range d.events {
		for _, c := range classifyChanges(ev.ImageChanges) {
			line := digestBullet + ev.Stack + "/" + c.name
			switch {
			case c.from != "" && c.to != "":
				line += ": " + c.from + arrow + c.to
			case c.to != "":
				line += ": " + c.to
			}
			lines = append(lines, line)
		}
	}
	if extra := len(lines) - maxDigestImageLines; extra > 0 {
		lines = append(lines[:maxDigestImageLines], fmt.Sprintf("%s+%d %s", digestBullet, extra, wordMore))
	}
	return lines
}

func (f signalFormatter) FormatDigest(ctx context.Context, d runDigest) (*http.Request, error) {
	return f.post(ctx, renderCompact(f.prefix, composeDigest(d)))
}

func (f ntfyFormatter) FormatDigest(ctx context.Context, d runDigest) (*http.Request, error) {
	m := composeDigest(d)
	return ntfyPost(ctx, f.url, f.headers, ntfyMessage{title: prefixed(f.prefix, m.Title), body: m.Body, tag: ntfyTagOK, priority: ntfyPrioLow})
}

// genericDigest is the generic format's digest body: every buffered event in
// full, so a downstream consumer loses nothing.
type genericDigest struct {
	Kind     string               `json:"kind"`
	Stacks   []string             `json:"stacks"`
	Events   []events.DeployEvent `json:"events"`
	Overflow int                  `json:"overflow,omitempty"`
}

func (f genericFormatter) FormatDigest(ctx context.Context, d runDigest) (*http.Request, error) {
	payload := genericDigest{Kind: digestKind, Stacks: d.stacks(), Overflow: d.overflow}
	for _, ev := range d.events {
		payload.Events = append(payload.Events, ev.SSEPayload())
	}
	return jsonPost(ctx, f.url, payload, f.headers)
}
