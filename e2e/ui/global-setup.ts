import { execFileSync } from 'node:child_process';
import { mkdirSync } from 'node:fs';
import { dirname } from 'node:path';
import { buildCommit, manifestVersion, repoRoot, skipperBinPath } from './fixtures/harness';

// globalSetup builds the real skipper binary once for the whole run. Every test
// launches this same binary as a subprocess (fixtures/harness.ts). The path is
// derived deterministically (skipperBinPath) so workers find it without any
// shared env plumbing; SKIPPER_E2E_BIN overrides it (e.g. a prebuilt binary).
//
// The build injects the version and commit via -ldflags from
// .release-please-manifest.json / harness constants, exactly like the Docker and
// Nix builds, so the header build-identity label (UD5) renders the real deployed
// version rather than the "dev" fallback. A fixed commit keeps it deterministic.
export default function globalSetup() {
  if (process.env.SKIPPER_E2E_BIN) return; // caller supplied a binary
  const bin = skipperBinPath();
  mkdirSync(dirname(bin), { recursive: true });
  const ldflags = `-X main.version=${manifestVersion()} -X main.commit=${buildCommit}`;
  // -buildvcs=false: version/commit come from ldflags, so the build needs no git
  // VCS stamping — and stamping fails inside the pinned CI container, where the
  // checked-out repo has "dubious ownership" (built as root, files owned by
  // another uid) and `go build`'s git call exits 128.
  execFileSync('go', ['build', '-buildvcs=false', '-ldflags', ldflags, '-o', bin, './cmd/skipper'], {
    cwd: repoRoot(),
    stdio: 'inherit',
  });
}
