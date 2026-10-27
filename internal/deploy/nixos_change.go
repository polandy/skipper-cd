package deploy

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"slices"
	"time"

	"github.com/polandy/skipper-cd/internal/metrics"
	"github.com/polandy/skipper-cd/internal/nixos"
)

// errNoOutputter is returned when derivation change detection runs on a
// Deployer wired without an Outputter: the evaluation needs its stdout.
var errNoOutputter = errors.New("nixos_rebuild.change_detection: derivation needs an output-capturing runner (deploy.Config.Outputter)")

// systemDrvCache is the last successfully evaluated system derivation and the
// commit it was evaluated at. With locked inputs the same commit always gives
// the same derivation, so one evaluation per commit suffices (ADR-0065).
type systemDrvCache struct {
	commit string
	drv    string
}

// systemDerivation returns the system derivation for HEAD, evaluating only
// when HEAD moved since the last successful evaluation. A failure leaves the
// cache untouched.
func (d *Deployer) systemDerivation(ctx context.Context, flake string) (string, error) {
	if d.outputter == nil {
		return "", errNoOutputter
	}
	head := d.headForDrvCache(ctx)
	if head != "" && head == d.systemDrv.commit {
		return d.systemDrv.drv, nil
	}
	start := time.Now()
	drv, err := nixos.SystemDerivation(ctx, d.outputter, d.repoDir, flake, os.Hostname)
	if err != nil {
		return "", withFailureCause(err)
	}
	metrics.NixOSEvaluationSeconds.Set(time.Since(start).Seconds())
	d.systemDrv = systemDrvCache{commit: head, drv: drv}
	return drv, nil
}

// headForDrvCache returns HEAD's SHA, or "" when it cannot be read — the
// evaluation then runs without the cache rather than not at all.
func (d *Deployer) headForDrvCache(ctx context.Context) string {
	if d.commitReader == nil {
		return ""
	}
	sha, err := d.commitReader.HeadCommitSHA(ctx)
	if err != nil {
		slog.Warn("could not read HEAD commit SHA; evaluating the system derivation without the per-commit cache", "err", err)
		return ""
	}
	return sha
}

// inFlightMarker is what the in-flight marker records for a rebuild with the
// given changed nix files. A derivation-only change has none, and an empty
// marker would read as "no rebuild in flight", so it records the reserved
// key instead (ADR-0025, ADR-0065).
func inFlightMarker(changed []string) []string {
	if len(changed) == 0 {
		return []string{nixos.SystemDrvKey}
	}
	return changed
}

// markedFiles returns the files an in-flight marker names, without the
// reserved key inFlightMarker may have put there.
func markedFiles(marker []string) []string {
	return slices.DeleteFunc(slices.Clone(marker), func(f string) bool { return f == nixos.SystemDrvKey })
}
