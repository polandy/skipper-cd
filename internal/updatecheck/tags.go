// Package updatecheck periodically compares what a stack's containers run
// against what their registries offer, and reports the answer — in the UI and
// as an optional notification. It is read-only: acting on an update stays a
// git commit (ADR-0054, complementing ADR-0030's delegation to Renovate).
package updatecheck

import (
	"strconv"
	"strings"
)

// tagShape is the structure of a version-shaped tag: an optional "v" prefix,
// the dot-separated numeric components, and everything after them verbatim
// ("-alpine"). Two tags are comparable only when prefix, component count and
// suffix all match — a wrong upgrade hint is worse than none (ADR-0054).
type tagShape struct {
	prefix string // "v" or ""
	nums   []int
	suffix string // "-alpine", "" — compared verbatim
}

// parseTagShape parses a tag into its shape. ok is false for a tag that is not
// version-shaped (no leading digit after the optional "v"): "latest", "stable",
// "pg14" — those never get a newer-tag suggestion, only the digest check.
func parseTagShape(tag string) (tagShape, bool) {
	s := tagShape{}
	rest := tag
	if strings.HasPrefix(rest, "v") {
		s.prefix = "v"
		rest = rest[1:]
	}
	for {
		i := 0
		for i < len(rest) && rest[i] >= '0' && rest[i] <= '9' {
			i++
		}
		if i == 0 {
			// No digits where a component is expected: not version-shaped for
			// the first component, else the remainder is the suffix including
			// the "." that led here.
			if len(s.nums) == 0 {
				return tagShape{}, false
			}
			s.suffix = "." + rest
			return s, true
		}
		n, err := strconv.Atoi(rest[:i])
		if err != nil {
			return tagShape{}, false
		}
		s.nums = append(s.nums, n)
		rest = rest[i:]
		if !strings.HasPrefix(rest, ".") {
			s.suffix = rest
			return s, true
		}
		rest = rest[1:]
	}
}

// comparable reports whether two shapes may be compared at all: same prefix,
// same number of components, same suffix.
func (s tagShape) comparable(o tagShape) bool {
	if s.prefix != o.prefix || s.suffix != o.suffix || len(s.nums) != len(o.nums) {
		return false
	}
	return true
}

// less orders two comparable shapes component-wise numerically.
func (s tagShape) less(o tagShape) bool {
	for i := range s.nums {
		if s.nums[i] != o.nums[i] {
			return s.nums[i] < o.nums[i]
		}
	}
	return false
}

// NewerTag returns the highest tag among tags that is strictly newer than
// running and of the same shape (same "v"-or-no prefix, same number of
// dot-separated numeric components, same suffix — "6.2-alpine" is only ever
// compared against "*-alpine"). Returns "" when running is not version-shaped
// or nothing qualifies. Cross-shape suggestions are never made.
func NewerTag(running string, tags []string) string {
	cur, ok := parseTagShape(running)
	if !ok {
		return ""
	}
	best := cur
	bestTag := ""
	for _, t := range tags {
		s, ok := parseTagShape(t)
		if !ok || !cur.comparable(s) {
			continue
		}
		if best.less(s) {
			best = s
			bestTag = t
		}
	}
	return bestTag
}
