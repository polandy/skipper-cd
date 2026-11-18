package deploy

import "strings"

// maskedValue replaces everything after the first "=" of a masked diff line.
const maskedValue = "***"

// maskEnvDiff hides the values in a unified diff of a KEY=VALUE file, keeping
// the keys so the reader still sees which variables changed. A hunk header
// keeps only its ranges: git appends a preceding source line to it as context.
func maskEnvDiff(diff string) string {
	lines := strings.Split(diff, "\n")
	inHunk := false
	for i, line := range lines {
		switch {
		case strings.HasPrefix(line, "diff --git "):
			inHunk = false
		case strings.HasPrefix(line, "@@"):
			inHunk = true
			if end := strings.Index(line[2:], "@@"); end >= 0 {
				lines[i] = line[:end+4]
			}
		case !inHunk && isFileHeader(line):
		default:
			if eq := strings.IndexByte(line, '='); eq >= 0 {
				lines[i] = line[:eq+1] + maskedValue
			}
		}
	}
	return strings.Join(lines, "\n")
}

// isFileHeader reports a line of the header git writes before the first hunk.
func isFileHeader(line string) bool {
	for _, prefix := range []string{"--- ", "+++ ", "index ", "new file mode ", "deleted file mode "} {
		if strings.HasPrefix(line, prefix) {
			return true
		}
	}
	return false
}
