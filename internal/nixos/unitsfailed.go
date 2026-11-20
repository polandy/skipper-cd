package nixos

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// ErrActivatedUnitsFailed marks a switch that activated the new configuration
// but left units failed (switch-to-configuration exit status 4, ADR-0071).
var ErrActivatedUnitsFailed = errors.New("nixos-rebuild switch activated the new configuration, but units failed")

// switchUnitsFailedStatus is the exit status switch-to-configuration (and the
// nixos-rebuild that propagates it) reports for ErrActivatedUnitsFailed.
const switchUnitsFailedStatus = "4"

// failedUnitsPrefix starts the line switch-to-configuration prints listing them.
const failedUnitsPrefix = "the following units failed: "

// journalTailLines bounds the journal read; the unit list sits near the end,
// followed by a `systemctl status` of each failed unit.
const journalTailLines = "200"

// UnitsFailedError is ErrActivatedUnitsFailed with the units the switch named.
type UnitsFailedError struct {
	Unit  string   // the transient rebuild unit, for the journalctl hint
	Units []string // empty when the journal could not be read
}

func (e *UnitsFailedError) Error() string {
	failed := "some units failed"
	if len(e.Units) > 0 {
		failed = "these units failed: " + strings.Join(e.Units, ", ")
	}
	return fmt.Sprintf("nixos-rebuild switch activated the new configuration, but %s (see: journalctl -u %s)", failed, e.Unit)
}

// Is makes errors.Is(err, ErrActivatedUnitsFailed) hold.
func (e *UnitsFailedError) Is(target error) bool { return target == ErrActivatedUnitsFailed }

// activatedWithFailedUnits reads the ended unit's exit status and, for status
// 4, the failed units from this invocation's journal. ok is false for any other
// or unreadable status.
func (r *Rebuilder) activatedWithFailedUnits(unit string) (*UnitsFailedError, bool) {
	if r.out == nil {
		return nil, false
	}
	props := parseProperties(r.readOutput("systemctl", "show", "-p", "ExecMainStatus", "-p", "InvocationID", unit))
	if props["ExecMainStatus"] != switchUnitsFailedStatus {
		return nil, false
	}
	failed := &UnitsFailedError{Unit: unit}
	if id := props["InvocationID"]; id != "" {
		failed.Units = parseFailedUnits(r.readOutput("journalctl", "_SYSTEMD_INVOCATION_ID="+id, "-n", journalTailLines, "--no-pager", "-o", "cat"))
	}
	return failed, true
}

// readOutput returns a command's stdout, or "" when it fails: an unreadable
// post-mortem leaves the failure unclassified. A background context resolves
// the outcome even as shutdown cancels the run.
func (r *Rebuilder) readOutput(name string, args ...string) string {
	out, err := r.out.Output(context.Background(), "", name, args...)
	if err != nil {
		return ""
	}
	return string(out)
}

// parseProperties parses `systemctl show` Key=Value lines.
func parseProperties(out string) map[string]string {
	props := make(map[string]string)
	for line := range strings.Lines(out) {
		if key, value, ok := strings.Cut(strings.TrimSpace(line), "="); ok {
			props[key] = value
		}
	}
	return props
}

// parseFailedUnits returns the units of the last failed-units line in journal.
func parseFailedUnits(journal string) []string {
	var units []string
	for line := range strings.Lines(journal) {
		_, list, ok := strings.Cut(line, failedUnitsPrefix)
		if !ok {
			continue
		}
		units = nil
		for unit := range strings.SplitSeq(list, ",") {
			if unit = strings.TrimSpace(unit); unit != "" {
				units = append(units, unit)
			}
		}
	}
	return units
}
