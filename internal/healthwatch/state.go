package healthwatch

import (
	"log/slog"
	"os"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/polandy/skipper-cd/internal/fsatomic"
	"github.com/polandy/skipper-cd/internal/health"
)

// maxPhases bounds each service's persisted status history, mirroring the
// bounded persisted event history (ADR-0031).
const maxPhases = 10

// Phase is one accepted status of a service and when it began. Commit is the
// newest commit that had touched the stack at that time — context, not a causal
// claim (ADR-0031).
type Phase struct {
	Status health.Status `yaml:"status"`
	Since  time.Time     `yaml:"since"`
	Commit string        `yaml:"commit,omitempty"`
}

// deployRecord is a stack's last successful deploy, learned from the deploy
// event feed and used to derive deploy correlation of a phase.
type deployRecord struct {
	Commit string    `yaml:"commit,omitempty"`
	At     time.Time `yaml:"at"`
}

// alertRecord remembers when a service's alerts were last delivered, per
// direction, plus whether a cooldown-suppressed transition still awaits
// catch-up. Maintained (and persisted) only while the alert cooldown is
// enabled, so suppression survives a skipper restart (ADR-0031 amendment).
type alertRecord struct {
	UnhealthyAt time.Time `yaml:"unhealthy_at,omitempty"`
	RecoveredAt time.Time `yaml:"recovered_at,omitempty"`
	Suppressed  bool      `yaml:"suppressed,omitempty"`
}

// stackState is the watcher's persisted knowledge of one stack: its last
// deploy, the phase history of each of its services (newest first), and the
// per-service alert delivery records the cooldown works from.
type stackState struct {
	LastDeploy *deployRecord           `yaml:"last_deploy,omitempty"`
	Services   map[string][]Phase      `yaml:"services"`
	Alerts     map[string]*alertRecord `yaml:"alerts,omitempty"`
}

// ensureAlert returns the service's delivery record, creating it on first use.
func (ss *stackState) ensureAlert(svc string) *alertRecord {
	if ss.Alerts == nil {
		ss.Alerts = map[string]*alertRecord{}
	}
	rec := ss.Alerts[svc]
	if rec == nil {
		rec = &alertRecord{}
		ss.Alerts[svc] = rec
	}
	return rec
}

// state is the watcher's full persisted state, stored in its own file
// (healthwatch.yaml) — deliberately separate from deploy's state.yaml.
type state struct {
	stacks map[string]*stackState
}

// stateFile is the on-disk YAML shape of state.
type stateFile struct {
	Stacks map[string]*stackState `yaml:"stacks"`
}

func newState() *state {
	return &state{stacks: map[string]*stackState{}}
}

// ensure returns the stack's state, creating it on first sight.
func (s *state) ensure(name string) *stackState {
	ss := s.stacks[name]
	if ss == nil {
		ss = &stackState{Services: map[string][]Phase{}}
		s.stacks[name] = ss
	}
	return ss
}

// loadState reads the persisted state. A missing or corrupt file is a clean
// slate — the first poll then baselines silently instead of alerting, the same
// forgiving stance deploy state takes (Invariant 2).
func loadState(path string) *state {
	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			slog.Warn("healthwatch state unreadable, starting clean", "path", path, "err", err)
		}
		return newState()
	}
	var f stateFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		slog.Warn("healthwatch state corrupt, starting clean", "path", path, "err", err)
		return newState()
	}
	s := newState()
	for name, ss := range f.Stacks {
		if ss == nil {
			continue
		}
		if ss.Services == nil {
			ss.Services = map[string][]Phase{}
		}
		s.stacks[name] = ss
	}
	return s
}

// save persists the state atomically (temp file + rename), normalizing all
// timestamps to second-granular UTC so the file is stable across hosts.
func (s *state) save(path string) error {
	out := stateFile{Stacks: make(map[string]*stackState, len(s.stacks))}
	for name, ss := range s.stacks {
		c := &stackState{Services: make(map[string][]Phase, len(ss.Services))}
		if ss.LastDeploy != nil {
			c.LastDeploy = &deployRecord{Commit: ss.LastDeploy.Commit, At: normalizeTime(ss.LastDeploy.At)}
		}
		for svc, phases := range ss.Services {
			cp := make([]Phase, len(phases))
			for i, p := range phases {
				cp[i] = Phase{Status: p.Status, Since: normalizeTime(p.Since), Commit: p.Commit}
			}
			c.Services[svc] = cp
		}
		if len(ss.Alerts) > 0 {
			c.Alerts = make(map[string]*alertRecord, len(ss.Alerts))
			for svc, r := range ss.Alerts {
				c.Alerts[svc] = &alertRecord{
					UnhealthyAt: normalizeTime(r.UnhealthyAt),
					RecoveredAt: normalizeTime(r.RecoveredAt),
					Suppressed:  r.Suppressed,
				}
			}
		}
		out.Stacks[name] = c
	}

	data, err := yaml.Marshal(out)
	if err != nil {
		return err
	}

	return fsatomic.WriteFile(path, data, fsatomic.PrivateFileMode)
}

// normalizeTime truncates to seconds and converts to UTC — the persistence
// granularity of every healthwatch timestamp (ADR-0031).
func normalizeTime(t time.Time) time.Time {
	return t.Truncate(time.Second).UTC()
}
