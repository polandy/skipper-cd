package updatecheck

import "testing"

func TestNewerTag_SameShapeOnly(t *testing.T) {
	tests := []struct {
		name    string
		running string
		tags    []string
		want    string
	}{
		{
			name:    "plain semver bump",
			running: "1.22.3",
			tags:    []string{"1.21.0", "1.22.3", "1.22.6", "latest"},
			want:    "1.22.6",
		},
		{
			name:    "v-prefixed semver bump keeps the prefix",
			running: "v1.135.3",
			tags:    []string{"v1.135.3", "v1.137.1", "1.140.0", "latest"},
			want:    "v1.137.1",
		},
		{
			name:    "already newest",
			running: "1.22.6",
			tags:    []string{"1.21.0", "1.22.3", "1.22.6"},
			want:    "",
		},
		{
			name:    "component count must match: 1.22 never suggests 1.23.1",
			running: "1.22",
			tags:    []string{"1.22", "1.23.1", "2"},
			want:    "",
		},
		{
			name:    "fewer components than running never suggested",
			running: "1.22.3",
			tags:    []string{"1.23"},
			want:    "",
		},
		{
			name:    "suffix must match: alpine only compares against alpine",
			running: "6.2-alpine",
			tags:    []string{"7.4", "7.2-alpine", "6.2-alpine", "7.2-bookworm"},
			want:    "7.2-alpine",
		},
		{
			name:    "prefix must match: v-tag ignores bare tags",
			running: "v3.1",
			tags:    []string{"3.2", "v3.1"},
			want:    "",
		},
		{
			name:    "bare tag ignores v-tags",
			running: "3.1",
			tags:    []string{"v3.2", "3.1"},
			want:    "",
		},
		{
			name:    "single-component major bump",
			running: "29",
			tags:    []string{"29", "30", "30.0.1", "latest"},
			want:    "30",
		},
		{
			name:    "numeric compare, not lexicographic",
			running: "1.9.0",
			tags:    []string{"1.9.0", "1.10.0"},
			want:    "1.10.0",
		},
		{
			name:    "non-version running tag never gets a tag suggestion",
			running: "latest",
			tags:    []string{"1.0.0", "2.0.0", "latest"},
			want:    "",
		},
		{
			name:    "word-prefixed running tag is not version-shaped",
			running: "pg14",
			tags:    []string{"pg14", "pg16"},
			want:    "",
		},
		{
			name:    "highest same-shape tag wins across several",
			running: "1.0.0",
			tags:    []string{"1.2.0", "1.10.3", "1.9.9"},
			want:    "1.10.3",
		},
		{
			name:    "no tags",
			running: "1.0.0",
			tags:    nil,
			want:    "",
		},
		{
			name:    "date-style tag with same shape still compares numerically",
			running: "2026.07.1",
			tags:    []string{"2026.07.1", "2026.07.2"},
			want:    "2026.07.2",
		},
		{
			name:    "suffix with its own digits must match exactly",
			running: "34-ghostscript",
			tags:    []string{"35-ghostscript", "35-apache", "35"},
			want:    "35-ghostscript",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NewerTag(tt.running, tt.tags); got != tt.want {
				t.Errorf("NewerTag(%q, %v) = %q, want %q", tt.running, tt.tags, got, tt.want)
			}
		})
	}
}
