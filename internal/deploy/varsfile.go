package deploy

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// loadVarsFile reads a YAML file of simple key→value pairs and returns them
// as KEY=VALUE strings with keys uppercased, ready to append to a process environment.
// Returns nil, nil when path is empty (vars_file not configured).
func loadVarsFile(path string) ([]string, error) {
	if path == "" {
		return nil, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read vars file: %w", err)
	}

	raw := map[string]string{}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse vars file: %w", err)
	}

	envVars := make([]string, 0, len(raw))
	for key, value := range raw {
		envVars = append(envVars, strings.ToUpper(key)+"="+value)
	}
	return envVars, nil
}
