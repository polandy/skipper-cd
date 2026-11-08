package deploy

import "strings"

// maskedValue replaces everything after the first "=" of a masked diff line.
const maskedValue = "***"

// maskEnvDiff hides the values in a unified diff of a KEY=VALUE file, keeping
// the keys so the reader still sees which variables changed. A hunk header
// keeps only its ranges: git appends a preceding source line to it as context.
func maskEnvDiff(diff string) string {
	lines := strings.Split(diff, "\n")
	for i, line := range lines {
		switch {
		case strings.HasPrefix(line, "+++ "), strings.HasPrefix(line, "--- "),
			strings.HasPrefix(line, "diff --git "), strings.HasPrefix(line, "index "):
		case strings.HasPrefix(line, "@@"):
			if end := strings.Index(line[2:], "@@"); end >= 0 {
				lines[i] = line[:end+4]
			}
		default:
			if eq := strings.IndexByte(line, '='); eq >= 0 {
				lines[i] = line[:eq+1] + maskedValue
			}
		}
	}
	return strings.Join(lines, "\n")
}
