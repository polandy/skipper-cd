package events

import "time"

// Span is the bookkeeping a collapsed run of repeated outcomes carries
// (ADR-0056): how many occurrences it stands for, when the run started, and
// when it last recurred. Both stores that apply the collapse rule — the event
// history here and the per-stack audit log — keep these three values, and both
// fold a new occurrence in through absorbInto, so neither can drift on what a
// repeat means.
type Span struct {
	Count     int
	FirstSeen time.Time
	Latest    time.Time
}

// AbsorbInto folds the single occurrence s stands for into the standing run
// prev, returning the run's new bookkeeping. It is order-independent — a
// reloaded log can hold occurrences out of order — so the result keeps the
// oldest start and the newest recurrence.
func (s Span) AbsorbInto(prev Span) Span {
	return Span{
		Count:     prev.occurrences() + 1,
		FirstSeen: earlier(prev.start(), s.Latest),
		Latest:    later(prev.Latest, s.Latest),
	}
}

// occurrences is how many identical outcomes the span stands for: 1 unless it
// already collapsed a run.
func (s Span) occurrences() int {
	if s.Count > 1 {
		return s.Count
	}
	return 1
}

// start is when the span's run began — its own recurrence unless it already
// collapsed a run.
func (s Span) start() time.Time {
	if !s.FirstSeen.IsZero() {
		return s.FirstSeen
	}
	return s.Latest
}

// earlier and later keep a collapsed run honest regardless of the order the
// occurrences arrived in.
func earlier(a, b time.Time) time.Time {
	if b.Before(a) {
		return b
	}
	return a
}

func later(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}
