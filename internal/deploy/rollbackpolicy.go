package deploy

import (
	"fmt"
	"sort"

	"github.com/polandy/skipper-cd/internal/config"
	"github.com/polandy/skipper-cd/internal/imageref"
)

// rollbackPolicyOf is the stack's resolved rollback policy; nil (unresolved,
// as in direct unit-test calls) means RollbackAlways.
func rollbackPolicyOf(stack config.Stack) config.RollbackPolicy {
	if stack.Rollback == nil {
		return config.RollbackAlways
	}
	return *stack.Rollback
}

// updateBeyondPatch says why the step from the last settled images (previous)
// to the compose file's goes further than a patch bump or a pinned digest, or
// "" when it does not. The largest step across services decides; anything
// that cannot be classified counts as too far (ADR-0074).
func updateBeyondPatch(cf *composeFile, previous serviceImageByName) string {
	if cf == nil {
		return "the compose file could not be parsed, so its update level is unknown"
	}
	if built := cf.builtServices(); len(built) > 0 {
		return fmt.Sprintf("%s is built from a Dockerfile, so its update level is unknown", built[0])
	}
	worst, reason := imageref.LevelNone, ""
	for _, c := range imageChanges(cf.images(), previous) {
		if level := imageref.Compare(c.Old, c.New); level > worst {
			worst, reason = level, describeUpdate(c.Service, c.Old, c.New, level)
		}
	}
	if worst <= imageref.LevelPatch {
		return ""
	}
	return reason
}

// describeUpdate renders one service's step for the log and the event error:
// "postgres 15 -> 16 is a major update".
func describeUpdate(service, oldRef, newRef string, level imageref.Level) string {
	switch {
	case oldRef == "":
		return fmt.Sprintf("%s is a new service, so its update level is unknown", service)
	case newRef == "":
		return fmt.Sprintf("%s was removed, so its update level is unknown", service)
	case level == imageref.LevelUnknown:
		return fmt.Sprintf("%s %s -> %s is not a classifiable update", service, oldRef, newRef)
	}
	_, oldTag, _ := imageref.Split(oldRef)
	_, newTag, _ := imageref.Split(newRef)
	return fmt.Sprintf("%s %s -> %s is a %s update", service, oldTag, newTag, level)
}

// builtServices returns the names of the services with a build: section, sorted.
func (cf *composeFile) builtServices() []string {
	var names []string
	for name := range cf.buildSpecs() {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
