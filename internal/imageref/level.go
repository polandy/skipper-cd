package imageref

import "strings"

// DigestPrefix marks a value after "@" as a content digest, as opposed to the
// short image id running_images appends to floating tags (ADR-0053).
const DigestPrefix = "sha256:"

// DefaultTag is the tag docker assumes when a reference names none.
const DefaultTag = "latest"

// Split cuts an image reference into its name (registry host and repository,
// as written), its tag ("" when it names none) and its digest ("" unless the
// reference pins a sha256 digest).
func Split(ref string) (name, tag, digest string) {
	if n, after, ok := strings.Cut(ref, "@"); ok {
		if strings.HasPrefix(after, DigestPrefix) {
			digest = after
		}
		ref = n
	}
	// A tag colon can only appear after the last "/" (before it is a port).
	if i := strings.LastIndex(ref, ":"); i >= 0 && !strings.Contains(ref[i:], "/") {
		return ref[:i], ref[i+1:], digest
	}
	return ref, "", digest
}

// Level is how far apart two image references are, ordered from no change to
// a change nothing can be said about.
type Level int

const (
	// LevelNone is the same image.
	LevelNone Level = iota
	// LevelDigest is the same version tag with a different pinned digest.
	LevelDigest
	// LevelPatch is a bump past the second numeric component (1.2.3 → 1.2.4).
	LevelPatch
	// LevelMinor is a bump of the second numeric component (1.2 → 1.3).
	LevelMinor
	// LevelMajor is a bump of the first numeric component (15 → 16).
	LevelMajor
	// LevelUnknown is a change that cannot be classified: a non-version tag, a
	// shape change, another repository, or a service added or removed.
	LevelUnknown
)

// String names the level the way messages and logs print it.
func (l Level) String() string {
	switch l {
	case LevelNone:
		return "none"
	case LevelDigest:
		return "digest"
	case LevelPatch:
		return "patch"
	case LevelMinor:
		return "minor"
	case LevelMajor:
		return "major"
	default:
		return "unclassifiable"
	}
}

// Compare classifies the step from oldRef to newRef. Tags are compared only
// when they have the same shape (see Shape) and the size of the step is the
// first numeric component that differs; a moved digest counts only under a
// version-shaped tag, since a floating tag says nothing about what moved.
// Either reference empty (a service added or removed) is LevelUnknown.
func Compare(oldRef, newRef string) Level {
	if oldRef == newRef {
		return LevelNone
	}
	if oldRef == "" || newRef == "" {
		return LevelUnknown
	}
	oldName, oldTag, oldDigest := Split(oldRef)
	newName, newTag, newDigest := Split(newRef)
	if oldName != newName {
		return LevelUnknown
	}
	oldTag, newTag = orDefaultTag(oldTag), orDefaultTag(newTag)
	if oldTag == newTag {
		return compareDigests(oldTag, oldDigest, newDigest)
	}
	oldShape, okOld := ParseShape(oldTag)
	newShape, okNew := ParseShape(newTag)
	if !okOld || !okNew || !oldShape.Comparable(newShape) {
		return LevelUnknown
	}
	for i := range oldShape.nums {
		if oldShape.nums[i] == newShape.nums[i] {
			continue
		}
		switch i {
		case 0:
			return LevelMajor
		case 1:
			return LevelMinor
		default:
			return LevelPatch
		}
	}
	return LevelUnknown // numerically equal, spelled differently ("1.02" vs "1.2")
}

// compareDigests classifies two references that share tag.
func compareDigests(tag, oldDigest, newDigest string) Level {
	if oldDigest == newDigest {
		return LevelNone // "app" and "app:latest" name the same image
	}
	if oldDigest == "" || newDigest == "" {
		return LevelUnknown
	}
	if _, versioned := ParseShape(tag); !versioned {
		return LevelUnknown
	}
	return LevelDigest
}

func orDefaultTag(tag string) string {
	if tag == "" {
		return DefaultTag
	}
	return tag
}
