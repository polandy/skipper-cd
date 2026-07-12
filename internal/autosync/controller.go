// Package autosync decides whether a stack's detected changes deploy
// automatically. It resolves an effective state per stack from two layers —
// config-as-code defaults (immutable) and in-memory UI overrides that are never
// persisted — and, via Queue, tracks the deploys deferred while sync is paused.
// See docs/autosync.md for the full specification.
package autosync

import (
	"maps"
	"sync"
)

// Controller resolves the effective autosync state per stack. A per-stack
// setting overrides the global one; within a scope, a UI override overrides the
// config-as-code value. Construct with NewController; the zero value is unusable.
type Controller struct {
	mu sync.RWMutex

	globalConfig *bool            // config-as-code global default; nil = on
	stackConfig  map[string]*bool // per-stack config value; nil/absent = inherit

	globalOverride *bool            // UI override; nil = none
	stackOverride  map[string]*bool // per-stack UI override; absent = none
}

// NewController builds a Controller from config-as-code values. stackConfig maps
// a stack name to its configured autosync (nil or absent = inherit global).
func NewController(globalConfig *bool, stackConfig map[string]*bool) *Controller {
	sc := make(map[string]*bool, len(stackConfig))
	maps.Copy(sc, stackConfig)
	return &Controller{
		globalConfig:  globalConfig,
		stackConfig:   sc,
		stackOverride: make(map[string]*bool),
	}
}

// Effective reports whether autosync is currently on for the stack.
func (c *Controller) Effective(name string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.effectiveLocked(name)
}

func (c *Controller) effectiveLocked(name string) bool {
	if ov, ok := c.stackOverride[name]; ok {
		return *ov
	}
	if cv := c.stackConfig[name]; cv != nil {
		return *cv
	}
	return c.globalEffectiveLocked()
}

func (c *Controller) globalEffectiveLocked() bool {
	if c.globalOverride != nil {
		return *c.globalOverride
	}
	if c.globalConfig != nil {
		return *c.globalConfig
	}
	return true
}

// baselineLocked is what the stack would resolve to without its UI override — its
// config value if set, otherwise the effective global. A UI override is only ever
// an exception to this baseline (see ADR-0019); config-pinned stacks ignore global.
func (c *Controller) baselineLocked(name string) bool {
	if cv := c.stackConfig[name]; cv != nil {
		return *cv
	}
	return c.globalEffectiveLocked()
}

// GlobalEffective reports the effective global autosync state.
func (c *Controller) GlobalEffective() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.globalEffectiveLocked()
}

// Reason explains why a stack is paused: "stack" when its own setting causes the
// pause, "global" when the global setting does, or "" when autosync is on.
func (c *Controller) Reason(name string) string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.effectiveLocked(name) {
		return ""
	}
	if c.stackScopeSetLocked(name) {
		return "stack"
	}
	return "global"
}

func (c *Controller) stackScopeSetLocked(name string) bool {
	if _, ok := c.stackOverride[name]; ok {
		return true
	}
	return c.stackConfig[name] != nil
}

// SetGlobal sets (v non-nil) or clears (v nil) the global UI override. Changing
// the global state can make a per-stack override coincide with its new baseline;
// any such override collapses back to inherit so the global switch acts as a true
// master (ADR-0019).
func (c *Controller) SetGlobal(v *bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.globalOverride = v
	for name, ov := range c.stackOverride {
		if ov != nil && *ov == c.baselineLocked(name) {
			delete(c.stackOverride, name)
		}
	}
}

// SetStack sets or clears the per-stack UI override. A UI override is held only
// while it differs from the stack's baseline: clearing it (v nil) or setting it to
// the value the stack would inherit anyway both collapse to inherit rather than
// pinning a redundant override (ADR-0019).
func (c *Controller) SetStack(name string, v *bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if v == nil || *v == c.baselineLocked(name) {
		delete(c.stackOverride, name)
		return
	}
	c.stackOverride[name] = v
}

// StackState is the per-stack autosync state in an API snapshot.
type StackState struct {
	Name       string `json:"name"`
	Effective  bool   `json:"effective"`
	Config     *bool  `json:"config"`     // config-as-code value; null = inherit
	Overridden bool   `json:"overridden"` // a UI override is in force
}

// Snapshot is the autosync state served at GET /api/autosync.
type Snapshot struct {
	Global           bool         `json:"global"`
	GlobalConfig     *bool        `json:"global_config"`
	GlobalOverridden bool         `json:"global_overridden"`
	Stacks           []StackState `json:"stacks"`
}

// Snapshot returns the current state for the given stacks, in the given order.
func (c *Controller) Snapshot(order []string) Snapshot {
	c.mu.RLock()
	defer c.mu.RUnlock()
	s := Snapshot{
		Global:           c.globalEffectiveLocked(),
		GlobalConfig:     c.globalConfig,
		GlobalOverridden: c.globalOverride != nil,
		Stacks:           make([]StackState, 0, len(order)),
	}
	for _, name := range order {
		_, overridden := c.stackOverride[name]
		s.Stacks = append(s.Stacks, StackState{
			Name:       name,
			Effective:  c.effectiveLocked(name),
			Config:     c.stackConfig[name],
			Overridden: overridden,
		})
	}
	return s
}
