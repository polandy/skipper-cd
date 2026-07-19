// Package roster builds the Stacks-view inventory: the full set of stacks
// skipper owns (stack discovery, ADR-0034, or the host list in legacy mode)
// merged with each stack's last deploy outcome from the audit log (ADR-0033).
//
// Unlike the deploy table — an event log that only shows stacks with recent
// events — the roster is inventory: every declared stack appears, including
// ones that have never deployed and ones parked with disabled: true. It is
// pure visualization; see dev-docs/stack-roster-spec.md.
package roster

import (
	"sort"
	"time"

	"github.com/polandy/skipper-cd/internal/audit"
	"github.com/polandy/skipper-cd/internal/config"
	"github.com/polandy/skipper-cd/internal/events"
)

// Entry is one stack's inventory row. The frontend resolves the icon from Name
// via /api/icons/<name>, so no icon travels here.
type Entry struct {
	Name string `json:"name"`
	// Disabled marks a stack parked with disabled: true — present in the repo,
	// deliberately not deployed. Disabled entries carry no live outcome.
	Disabled bool `json:"disabled"`
	// LastStatus is the newest terminal deploy status; empty means the stack
	// has never deployed.
	LastStatus events.Status `json:"last_status,omitempty"`
	LastAt     *time.Time    `json:"last_at,omitempty"`
	LastCommit string        `json:"last_commit,omitempty"`
	// Hooks carries the stack's configured deploy-hook command lines (ADR-0038)
	// when it declares any, so the UI can show the hooks badge + panel without a
	// fetch. nil when the stack has no hooks.
	Hooks *Hooks `json:"hooks,omitempty"`
}

// Hooks is the roster view of a stack's deploy hooks: just the command lines the
// UI lists. The deploy-time timeout_seconds is omitted — the UI never shows it.
type Hooks struct {
	PreDeploy  []string `json:"pre_deploy,omitempty"`
	PostDeploy []string `json:"post_deploy,omitempty"`
}

// Build merges the effective stack set with per-stack last outcomes into a
// stable, sorted roster. stacks is the deploy set (disabled stacks are already
// excluded from it, so disabled is merged in separately); last returns a
// stack's newest audit record, if any. Enabled stacks sort first (alphabetical),
// then disabled (alphabetical), so the list reads live-then-parked.
func Build(stacks []config.Stack, disabled []string, last func(name string) (audit.Record, bool)) []Entry {
	entries := make([]Entry, 0, len(stacks)+len(disabled))

	for _, s := range stacks {
		e := Entry{Name: s.Name}
		if rec, ok := last(s.Name); ok {
			ts := rec.Timestamp
			e.LastStatus = rec.Status
			e.LastAt = &ts
			e.LastCommit = rec.CommitSHA
		}
		if len(s.Hooks.PreDeploy) > 0 || len(s.Hooks.PostDeploy) > 0 {
			e.Hooks = &Hooks{PreDeploy: s.Hooks.PreDeploy, PostDeploy: s.Hooks.PostDeploy}
		}
		entries = append(entries, e)
	}
	for _, name := range disabled {
		// Parked stacks show as disabled with no live outcome, even if a
		// historical record exists — the inventory fact is "not deployed".
		entries = append(entries, Entry{Name: name, Disabled: true})
	}

	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].Disabled != entries[j].Disabled {
			return !entries[i].Disabled // enabled before disabled
		}
		return entries[i].Name < entries[j].Name
	})
	return entries
}
