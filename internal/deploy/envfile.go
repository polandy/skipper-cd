package deploy

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/polandy/skipper-cd/internal/regularfile"
)

// maxEnvFileBytes caps one env file read; an env file is a handful of lines.
const maxEnvFileBytes = 1 << 20

// envKeyPattern is the portable shell variable name syntax.
var envKeyPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// reservedEnvKeys and reservedEnvKeyPrefixes change how skipper's own child
// processes run — the dynamic loader, the shell hooks run under, or which
// docker daemon, client config and CLI plugins compose uses — rather than
// configure a stack. A pushed env file must not set them.
var (
	reservedEnvKeys        = []string{"PATH", "HOME", "BASH_ENV", "ENV", "DOCKER_HOST", "DOCKER_CONTEXT", "DOCKER_CONFIG", "DOCKER_CERT_PATH"}
	reservedEnvKeyPrefixes = []string{"LD_"}
)

// parseEnvFile reads a KEY=VALUE env file and returns a slice of "KEY=VALUE" strings
// suitable for appending to exec.Cmd.Env. Empty lines and lines starting
// with # (comments) are ignored; values are kept verbatim. A line that is not
// KEY=VALUE, or that sets a reserved key, fails the whole file.
func parseEnvFile(path string) ([]string, error) {
	data, err := regularfile.ReadFile(path, maxEnvFileBytes, regularfile.FollowLinks)
	if err != nil {
		return nil, fmt.Errorf("open env file: %w", err)
	}

	var envVars []string
	for i, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if err := checkEnvLine(line); err != nil {
			return nil, fmt.Errorf("%s line %d: %w", path, i+1, err)
		}
		envVars = append(envVars, line)
	}
	return envVars, nil
}

func checkEnvLine(line string) error {
	key, _, found := strings.Cut(line, "=")
	if !found || !envKeyPattern.MatchString(key) {
		return fmt.Errorf("%q is not a KEY=VALUE entry; the key must match %s (no \"export\", no spaces)", line, envKeyPattern)
	}
	if slices.Contains(reservedEnvKeys, key) || slices.ContainsFunc(reservedEnvKeyPrefixes, func(p string) bool { return strings.HasPrefix(key, p) }) {
		return fmt.Errorf("key %s is reserved: it changes how skipper runs docker compose and hooks; remove it from the env file", key)
	}
	return nil
}
