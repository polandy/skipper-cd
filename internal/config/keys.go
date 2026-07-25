package config

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// renamedKeys maps config keys retired by an accepted ADR to the key that
// replaced them. Decoding rejects unknown keys outright, so an un-migrated
// config already refuses to start; this map exists so the refusal names the
// replacement instead of reporting a nameless unknown field.
//
// Keys are matched by name anywhere in the document — the three retired names
// are unique across nesting levels, so no per-level scoping is needed.
var renamedKeys = map[string]string{
	"working_dir":                  "project_directory",                    // ADR-0045
	"health_check":                 "deploy_health_check",                  // ADR-0047
	"health_poll_interval_seconds": "runtime_health_poll_interval_seconds", // ADR-0047
}

// checkRenamedKeys reports the first retired config key found in the document,
// naming the key that replaced it. A config written before the rename would
// otherwise fail with a bare "unknown key", leaving the operator to work out
// that the setting still exists under a new name.
func checkRenamedKeys(node *yaml.Node) error {
	if node == nil {
		return nil
	}
	switch node.Kind {
	case yaml.DocumentNode, yaml.SequenceNode:
		for _, child := range node.Content {
			if err := checkRenamedKeys(child); err != nil {
				return err
			}
		}
	case yaml.MappingNode:
		// Mapping content alternates key, value, key, value, …
		for i := 0; i+1 < len(node.Content); i += 2 {
			key, value := node.Content[i], node.Content[i+1]
			if replacement, renamed := renamedKeys[key.Value]; renamed {
				return fmt.Errorf("config key %q (line %d) was renamed to %q — rename the key; its meaning is unchanged", key.Value, key.Line, replacement)
			}
			if err := checkRenamedKeys(value); err != nil {
				return err
			}
		}
	}
	return nil
}
