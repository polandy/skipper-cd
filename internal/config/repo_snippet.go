package config

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// yamlSnippet renders a numbered excerpt of src around the 1-based line, with
// the failing line marked by ">". Empty when the line is unknown (<= 0) or out
// of range, so callers can append it unconditionally.
func yamlSnippet(src []byte, line int) string {
	lines := strings.Split(strings.TrimRight(string(src), "\n"), "\n")
	if line <= 0 || line > len(lines) {
		return ""
	}
	first := max(1, line-2)
	last := min(len(lines), line+2)
	width := len(strconv.Itoa(last))
	var b strings.Builder
	for n := first; n <= last; n++ {
		marker := " "
		if n == line {
			marker = ">"
		}
		fmt.Fprintf(&b, "  %s %*d | %s\n", marker, width, n, lines[n-1])
	}
	return strings.TrimRight(b.String(), "\n")
}

// yamlLinePattern extracts the line number yaml.v3 embeds in its error texts
// ("yaml: line 12: …", "unmarshal errors:\n  line 3: field …").
var yamlLinePattern = regexp.MustCompile(`line (\d+)`)

// yamlErrorLine returns the first line number mentioned in a yaml.v3 error,
// 0 when none is found.
func yamlErrorLine(err error) int {
	m := yamlLinePattern.FindStringSubmatch(err.Error())
	if m == nil {
		return 0
	}
	n, _ := strconv.Atoi(m[1])
	return n
}

// indexOverrideLines maps each stacks.<name> entry — and each of its fields —
// to its line in skipper.yaml, so entry-level errors can show the offending
// excerpt. The key "" holds the entry's own line.
func indexOverrideLines(src []byte) map[string]map[string]int {
	var doc yaml.Node
	if yaml.Unmarshal(src, &doc) != nil || len(doc.Content) == 0 {
		return nil
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return nil
	}
	out := map[string]map[string]int{}
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value != "stacks" {
			continue
		}
		stacksNode := root.Content[i+1]
		if stacksNode.Kind != yaml.MappingNode {
			break
		}
		for j := 0; j+1 < len(stacksNode.Content); j += 2 {
			key, val := stacksNode.Content[j], stacksNode.Content[j+1]
			fields := map[string]int{"": key.Line}
			if val.Kind == yaml.MappingNode {
				for k := 0; k+1 < len(val.Content); k += 2 {
					fields[val.Content[k].Value] = val.Content[k].Line
				}
			}
			out[key.Value] = fields
		}
	}
	return out
}
