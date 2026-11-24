// Package imageref reads what an image reference says about its version: the
// name/tag/digest split, the shape of a version-shaped tag, and how far two
// references lie apart. Pure; shared by the update check (ADR-0054) and the
// rollback policy (ADR-0074).
package imageref

import (
	"strconv"
	"strings"
)

// Shape is the structure of a version-shaped tag: an optional "v" prefix,
// the dot-separated numeric components, and everything after them verbatim
// ("-alpine"). Two tags are comparable only when prefix, component count and
// suffix all match — a wrong upgrade hint is worse than none (ADR-0054).
type Shape struct {
	prefix string // "v" or ""
	nums   []int
	suffix string // "-alpine", "" — compared verbatim
}

// ParseShape parses a tag into its shape. ok is false for a tag that is not
// version-shaped (no leading digit after the optional "v"): "latest", "stable",
// "pg14" — those never get a newer-tag suggestion, only the digest check.
func ParseShape(tag string) (Shape, bool) {
	s := Shape{}
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
				return Shape{}, false
			}
			s.suffix = "." + rest
			return s, true
		}
		n, err := strconv.Atoi(rest[:i])
		if err != nil {
			return Shape{}, false
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

// Comparable reports whether two shapes may be compared at all: same prefix,
// same number of components, same suffix.
func (s Shape) Comparable(o Shape) bool {
	if s.prefix != o.prefix || s.suffix != o.suffix || len(s.nums) != len(o.nums) {
		return false
	}
	return true
}

// Less orders two comparable shapes component-wise numerically.
func (s Shape) Less(o Shape) bool {
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
	cur, ok := ParseShape(running)
	if !ok {
		return ""
	}
	best := cur
	bestTag := ""
	for _, t := range tags {
		s, ok := ParseShape(t)
		if !ok || !cur.Comparable(s) {
			continue
		}
		if best.Less(s) {
			best = s
			bestTag = t
		}
	}
	return bestTag
}
