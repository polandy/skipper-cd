package imageref

import "testing"

func TestSplit(t *testing.T) {
	const digest = "sha256:aaaa"
	tests := []struct {
		ref                   string
		name, tag, wantDigest string
	}{
		{"postgres:16", "postgres", "16", ""},
		{"postgres", "postgres", "", ""},
		{"ghcr.io/org/app:1.2.3@" + digest, "ghcr.io/org/app", "1.2.3", digest},
		{"app@" + digest, "app", "", digest},
		// A registry port is not a tag.
		{"localhost:5000/app", "localhost:5000/app", "", ""},
		{"localhost:5000/app:2", "localhost:5000/app", "2", ""},
		// A short image id (ADR-0053's floating-tag form) is not a digest.
		{"app:latest@0123456789ab", "app", "latest", ""},
	}
	for _, tt := range tests {
		t.Run(tt.ref, func(t *testing.T) {
			name, tag, digest := Split(tt.ref)
			if name != tt.name || tag != tt.tag || digest != tt.wantDigest {
				t.Errorf("Split(%q) = (%q, %q, %q), want (%q, %q, %q)", tt.ref, name, tag, digest, tt.name, tt.tag, tt.wantDigest)
			}
		})
	}
}

func TestCompare(t *testing.T) {
	const a, b = "@sha256:aaaa", "@sha256:bbbb"
	tests := []struct {
		name     string
		old, new string
		want     Level
	}{
		{"same reference", "postgres:16", "postgres:16", LevelNone},
		{"pinned digest moved under the same version tag", "app:1.2.3" + a, "app:1.2.3" + b, LevelDigest},
		{"patch bump", "app:1.2.3", "app:1.2.4", LevelPatch},
		{"fourth component counts as patch", "app:1.2.3.4", "app:1.2.3.5", LevelPatch},
		{"minor bump", "app:1.2.3", "app:1.3.0", LevelMinor},
		{"major bump", "app:1.2.3", "app:2.0.0", LevelMajor},
		{"single-component tag is a major", "postgres:15", "postgres:16", LevelMajor},
		{"two-component tag second place is minor", "redis:6.2-alpine", "redis:6.3-alpine", LevelMinor},
		{"downgrade is measured by its size too", "app:2.0.0", "app:1.9.9", LevelMajor},
		{"digest pinned on both, tag bumped", "app:1.2.3" + a, "app:1.2.4" + b, LevelPatch},
		{"v prefix kept", "app:v1.2.3", "app:v1.2.4", LevelPatch},
		{"floating tag digest moved", "app:latest" + a, "app:latest" + b, LevelUnknown},
		{"same tag, digest only on one side", "app:1.2.3", "app:1.2.3" + b, LevelUnknown},
		{"non-semver tags", "app:latest", "app:stable", LevelUnknown},
		{"git sha tags", "app:3f2a9c1", "app:9b1e0d4", LevelUnknown},
		{"different shapes", "app:1.2", "app:1.2.3", LevelUnknown},
		{"different suffix", "app:1.2-alpine", "app:1.3-bookworm", LevelUnknown},
		{"numerically equal but spelled differently", "app:1.02", "app:1.2", LevelUnknown},
		{"different repository", "mariadb:10", "mysql:10", LevelUnknown},
		{"added service", "", "app:1.2.3", LevelUnknown},
		{"removed service", "app:1.2.3", "", LevelUnknown},
		{"omitted tag equals latest", "app", "app:latest", LevelNone},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Compare(tt.old, tt.new); got != tt.want {
				t.Errorf("Compare(%q, %q) = %v, want %v", tt.old, tt.new, got, tt.want)
			}
		})
	}
}

func TestLevel_String(t *testing.T) {
	want := map[Level]string{
		LevelNone:    "none",
		LevelDigest:  "digest",
		LevelPatch:   "patch",
		LevelMinor:   "minor",
		LevelMajor:   "major",
		LevelUnknown: "unclassifiable",
	}
	for l, s := range want {
		if l.String() != s {
			t.Errorf("Level(%d).String() = %q, want %q", int(l), l.String(), s)
		}
	}
}
