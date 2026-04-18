package deploy

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// parseEnvFile reads a KEY=VALUE env file and returns a slice of "KEY=VALUE" strings
// suitable for appending to exec.Cmd.Env. Empty lines and lines starting
// with # (comments) are ignored.
func parseEnvFile(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open env file: %w", err)
	}
	defer f.Close()

	var entries []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		entries = append(entries, line)
	}
	return entries, scanner.Err()
}
