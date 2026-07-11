import { execFileSync } from 'node:child_process';
import { mkdirSync } from 'node:fs';
import { dirname } from 'node:path';
import { repoRoot, skipperBinPath } from './fixtures/harness';

// globalSetup builds the real skipper binary once for the whole run. Every test
// launches this same binary as a subprocess (fixtures/harness.ts). The path is
// derived deterministically (skipperBinPath) so workers find it without any
// shared env plumbing; SKIPPER_E2E_BIN overrides it (e.g. a prebuilt binary).
export default function globalSetup() {
  if (process.env.SKIPPER_E2E_BIN) return; // caller supplied a binary
  const bin = skipperBinPath();
  mkdirSync(dirname(bin), { recursive: true });
  execFileSync('go', ['build', '-o', bin, './cmd/skipper'], {
    cwd: repoRoot(),
    stdio: 'inherit',
  });
}
