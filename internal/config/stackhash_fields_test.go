package config

import (
	"reflect"
	"testing"
)

// notHashed lists the Stack fields that deliberately stay out of ConfigHash:
// editing them must never redeploy.
var notHashed = map[string]bool{
	"Name":        true, // identity, not config
	"Icon":        true,
	"Autosync":    true,
	"SelfHeal":    true,
	"Rollback":    true,
	"UpdateCheck": true,
	"DependsOn":   true,
	"Hooks":       true,
	"Rollout":     true,
	"Disabled":    true,
	"ConfigHash":  true, // the hash itself
}

func TestStackFields_AreHashedOrExplicitlyExcluded(t *testing.T) {
	hashed := map[string]bool{}
	for f := range reflect.TypeFor[stackDeployInputs]().Fields() {
		hashed[f.Name] = true
	}
	for f := range reflect.TypeFor[Stack]().Fields() {
		switch {
		case hashed[f.Name] && notHashed[f.Name]:
			t.Errorf("Stack.%s is both in stackDeployInputs and notHashed", f.Name)
		case !hashed[f.Name] && !notHashed[f.Name]:
			t.Errorf("Stack.%s is neither in stackDeployInputs nor notHashed: decide whether editing it should redeploy", f.Name)
		}
	}
}
