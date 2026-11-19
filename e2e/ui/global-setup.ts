import { execFileSync } from 'node:child_process';
import { join } from 'node:path';
import { TempDirs } from './fixtures/tempdirs';
import { buildCommit, buildVersion, repoRoot } from './fixtures/harness';

// globalSetup builds the real skipper binary once for the whole run. Every test
// launches this same binary as a subprocess (fixtures/harness.ts). The path is
// published to the workers through SKIPPER_E2E_BIN; a caller-supplied value
// (e.g. a prebuilt binary) wins. The build lands in a per-run temp dir that the
// returned teardown removes.
//
// The build injects the version and commit via -ldflags from harness constants,
// exactly like the Docker and Nix builds, so the header build-identity label
// (UD5) renders a real `v<semver> · <commit>` rather than the "dev" fallback.
// Both are fixed values so the label stays deterministic across releases.
export default function globalSetup() {
  if (process.env.SKIPPER_E2E_BIN) return; // caller supplied a binary
  const binDirs = new TempDirs();
  const bin = join(binDirs.create('skipper-ui-e2e-bin-'), 'skipper');
  process.env.SKIPPER_E2E_BIN = bin;
  const ldflags = `-X main.version=${buildVersion} -X main.commit=${buildCommit}`;
  // -buildvcs=false: version/commit come from ldflags, so the build needs no git
  // VCS stamping — and stamping fails inside the pinned CI container, where the
  // checked-out repo has "dubious ownership" (built as root, files owned by
  // another uid) and `go build`'s git call exits 128.
  try {
    execFileSync('go', ['build', '-buildvcs=false', '-ldflags', ldflags, '-o', bin, './cmd/skipper'], {
      cwd: repoRoot(),
      stdio: 'inherit',
    });
  } catch (err) {
    binDirs.removeAll(); // a failed setup runs no teardown
    throw err;
  }
  return () => binDirs.removeAll();
}
