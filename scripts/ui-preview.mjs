// ui-preview — boot a seeded skipper-cd instance for manually eyeballing the web
// UI (`make ui-preview`, or `PORT=3000 node scripts/ui-preview.mjs`).
//
// It builds the binary from the current checkout, stands up a throwaway origin
// repo + a stub `docker` on PATH + a config, launches skipper, and seeds a
// representative spread of states — several deployed stacks, a pushed change
// with a single- and a multi-commit diff, and health that is healthy / degraded
// / stopped. Then it prints the URL and stays up until Ctrl-C, cleaning up its
// temp dir on exit. No docker, no network, no node_modules — just Node + git +
// the Go toolchain.
//
// This is a deliberately self-contained twin of the Playwright launcher
// (e2e/ui/fixtures/harness.ts). That harness — driven by Playwright's TS loader
// — remains the authoritative, asserted way skipper is booted for tests; this
// script trades a little duplication for zero toolchain dependencies so anyone
// can spin up the UI with one command. Keep the config shape here in rough sync
// with the harness's.
import { spawn, execFileSync } from 'node:child_process';
import { createServer } from 'node:net';
import { createHmac } from 'node:crypto';
import { mkdtempSync, mkdirSync, writeFileSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';
import { setTimeout as sleep } from 'node:timers/promises';

const repoRoot = join(dirname(fileURLToPath(import.meta.url)), '..');
const PORT = Number(process.env.PORT || 3000);
const SECRET = 'ui-preview-secret';
const stacks = ['web', 'api', 'worker', 'database'];

// Stub docker: records nothing, succeeds at everything, and answers two reads —
// `compose ps --format json` per stack for the health poller, and bare
// `docker ps -a` (orphan detection, ADR-0036) from a shared listing. Mirrors
// the harness's stub (minus the test hooks).
// A single `case` per subcommand is nested under a `compose` check: matching
// " compose " and (e.g.) " ps " as separate single-segment globs avoids the
// overlapping-space trap where `*" compose "*" ps "*` fails to match the
// adjacent "compose ps".
const stubDocker = `#!/bin/sh
case " $* " in
  *" compose "*)
    case " $* " in
      *" logs "*)
        svc="$(basename "$(pwd)")"
        # Merged (whole-stack) reads keep the compose service prefix; a single
        # service is invoked with --no-log-prefix, so drop it there.
        pfx="\${svc}-1  | "
        case " $* " in *" --no-log-prefix "*) pfx="" ;; esac
        ts() { date -u +%Y-%m-%dT%H:%M:%SZ; }
        echo "\${pfx}$(ts) starting \${svc} service"
        echo "\${pfx}$(ts) \${svc} listening on :8080"
        echo "\${pfx}$(ts) WARN slow upstream response 812ms"
        echo "\${pfx}$(ts) GET /api/health 200 3ms"
        case " $* " in
          *" --follow "*)
            i=0
            while :; do
              i=$((i+1))
              echo "\${pfx}$(ts) GET /api/health 200 \${i}ms"
              sleep 1
            done ;;
        esac
        exit 0 ;;
      *" ps "*)
        f="$STUB_PS_DIR/$(basename "$(pwd)").json"
        [ -f "$f" ] && cat "$f"
        exit 0 ;;
    esac
    exit 0 ;;
  *" volume "*)
    [ -f "$STUB_PS_DIR/volumes.txt" ] && cat "$STUB_PS_DIR/volumes.txt"
    exit 0 ;;
  *" ps "*)
    [ -f "$STUB_PS_DIR/orphans.txt" ] && cat "$STUB_PS_DIR/orphans.txt"
    exit 0 ;;
esac
exit 0
`;

function git(dir, ...args) {
  execFileSync('git', ['-C', dir, '-c', 'user.name=preview', '-c', 'user.email=preview@localhost', ...args], {
    stdio: 'pipe',
  });
}
async function freePort() {
  const srv = createServer();
  await new Promise((r) => srv.listen(0, '127.0.0.1', r));
  const p = srv.address().port;
  await new Promise((r) => srv.close(r));
  return p;
}
async function webhook() {
  const body = JSON.stringify({ ref: 'refs/heads/main' });
  const sig = createHmac('sha256', SECRET).update(body).digest('hex');
  const resp = await fetch(`http://127.0.0.1:${PORT}/webhook`, {
    method: 'POST',
    headers: { 'X-Gitea-Signature': sig },
    body,
  });
  return resp.status;
}
async function healthy() {
  try {
    return (await fetch(`http://127.0.0.1:${PORT}/healthz`)).status === 200;
  } catch {
    return false;
  }
}

const base = mkdtempSync(join(tmpdir(), 'skipper-ui-preview-'));
const origin = join(base, 'origin');
const repoDir = join(base, 'state', 'repo');
const healthDir = join(base, 'health');
const stubDir = join(base, 'bin');
const bin = join(base, 'skipper');
for (const d of [join(base, 'state'), healthDir, stubDir]) mkdirSync(d, { recursive: true });
writeFileSync(join(stubDir, 'docker'), stubDocker, { mode: 0o755 });

function cleanup() {
  try {
    proc?.kill('SIGKILL');
  } catch {}
  rmSync(base, { recursive: true, force: true });
}
process.on('SIGINT', () => {
  cleanup();
  process.exit(0);
});
process.on('SIGTERM', () => {
  cleanup();
  process.exit(0);
});

// Build the binary from this checkout, so the preview always reflects the code
// you are on. Version/commit are stamped like the real builds so the header
// renders a `v… · <commit>` identity.
console.error('[ui-preview] building skipper from the current checkout…');
const commit = execFileSync('git', ['-C', repoRoot, 'rev-parse', '--short', 'HEAD'], { encoding: 'utf8' }).trim();
execFileSync(
  'go',
  ['build', '-buildvcs=false', '-ldflags', `-X main.version=preview -X main.commit=${commit}`, '-o', bin, './cmd/skipper'],
  { cwd: repoRoot, stdio: 'inherit' },
);

// Origin repo: one committed compose (+ a coloured icon) per stack.
git(base, 'init', '-b', 'main', origin);
const icon = '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="#7aa2f7"><rect width="24" height="24" rx="5"/></svg>';
for (const n of stacks) {
  mkdirSync(join(origin, n), { recursive: true });
  writeFileSync(join(origin, n, 'docker-compose.yml'), `services:\n  ${n}:\n    image: nginx:1.25\n`);
  writeFileSync(join(origin, n, 'icon.svg'), icon);
}
// A parked stack: a real compose directory kept out of deploys via
// disabled: true in the repo skipper.yaml — so the roster shows a disabled row.
mkdirSync(join(origin, 'experiments'), { recursive: true });
writeFileSync(join(origin, 'experiments', 'docker-compose.yml'), `services:\n  experiments:\n    image: nginx:1.25\n`);
writeFileSync(join(origin, 'experiments', 'icon.svg'), icon);
// Stack-discovery overrides (ADR-0034), at the stacks_base_dir root: api keeps
// its health-check gate; experiments is parked.
writeFileSync(
  join(origin, 'skipper.yaml'),
  `stacks:\n` +
    `  api:\n` +
    `    health_check:\n      timeout_seconds: 1\n` +
    // Deploy hooks (ADR-0038): drive the hooks badge + panel. Commands are
    // harmless echoes so the preview's startup deploy still succeeds.
    `    hooks:\n      pre_deploy:\n        - "echo dumping api database"\n      post_deploy:\n        - "echo smoke test ok"\n` +
    `  web:\n` +
    // A short sleep so the running-hook phase sub-label is visible for a few
    // seconds during the startup deploy of web.
    `    hooks:\n      pre_deploy:\n        - "echo starting backup"\n        - "sleep 4"\n      post_deploy:\n        - "echo verifying deploy"\n` +
    `  experiments:\n    disabled: true\n`,
);
git(origin, 'add', '.');
git(origin, 'commit', '-m', 'initial');

const metricsPort = await freePort();
const cfg =
  `repo_url: ${JSON.stringify(origin)}\n` +
  `repo_dir: ${JSON.stringify(repoDir)}\n` +
  `stacks_base_dir: ${JSON.stringify(repoDir)}\n` +
  `branch: main\n` +
  `webhook_secret: ${JSON.stringify(SECRET)}\n` +
  `port: ${PORT}\n` +
  `metrics_port: ${metricsPort}\n` +
  `ui_enabled: true\n` +
  `ui_theme_switcher: true\n` +
  `health_poll_interval_seconds: 3\n` +
  `health_watch:\n  debounce_polls: 1\n` +
  `command_timeout_seconds: 120\n` +
  // Dead source_url → auto-match icon fetches fail fast; committed icon.svg
  // overrides still resolve, so the preview stays fully offline.
  `icons:\n  cache_dir: ${JSON.stringify(join(base, 'icons'))}\n  source_url: "http://127.0.0.1:1"\n` +
  // Stack discovery (ADR-0034): the stack set + per-stack config come from the
  // repo (stacks_base_dir root skipper.yaml), matching production. worker and
  // database are discovered but never pushed → they show as "never deployed".
  `stack_discovery: true\n`;
const cfgPath = join(base, 'skipper.yml');
writeFileSync(cfgPath, cfg);

const proc = spawn(bin, ['-config', cfgPath], {
  env: { ...process.env, PATH: `${stubDir}:${process.env.PATH}`, STUB_PS_DIR: healthDir },
  stdio: ['ignore', 'inherit', 'inherit'],
});
proc.on('exit', (code) => {
  if (code) {
    console.error(`[ui-preview] skipper exited with code ${code}`);
    cleanup();
    process.exit(code);
  }
});

for (let i = 0; i < 150 && !(await healthy()); i++) await sleep(200);
if (!(await healthy())) {
  console.error('[ui-preview] skipper did not become healthy');
  cleanup();
  process.exit(1);
}
console.error('[ui-preview] healthy — seeding states…');

// Health: healthy / degraded / stopped, so the pills and panel show variety.
const setHealth = (n, svcs) => writeFileSync(join(healthDir, `${n}.json`), JSON.stringify(svcs));
setHealth('web', [{ Service: 'web', Name: 'web-1', State: 'running', Health: 'healthy' }]);
setHealth('api', [{ Service: 'api', Name: 'api-1', State: 'running', Health: 'healthy' }]);
setHealth('worker', [{ Service: 'worker', Name: 'worker-1', State: 'restarting', Health: 'unhealthy' }]);
setHealth('database', [{ Service: 'database', Name: 'db-1', State: 'exited', ExitCode: 0 }]);

// Orphan detection listing (one line per container, columns per psColumns:
// project, working_dir, config_file, name, service, image, state, status,
// ports): the four active stacks (matched + filtered as managed), a removed
// stack still running under stacks_base_dir (orphaned + prunable, 2 containers —
// one running, one exited), and a hand-started project outside it (unmanaged).
const compose = (dir) => join(dir, 'docker-compose.yml');
const orphanRow = (proj, dir, name, svc, image, state, status, ports) =>
  [proj, dir, compose(dir), name, svc, image, state, status, ports].join('\t');
writeFileSync(
  join(healthDir, 'orphans.txt'),
  [
    ...stacks.map((n) => orphanRow(n, join(repoDir, n), `${n}-1`, n, 'nginx:1.25', 'running', 'Up 4 minutes', '')),
    orphanRow('legacy-cache', join(repoDir, 'legacy-cache'), 'legacy-cache-redis-1', 'redis', 'redis:7', 'running', 'Up 5 days', '0.0.0.0:6379->6379/tcp'),
    orphanRow('legacy-cache', join(repoDir, 'legacy-cache'), 'legacy-cache-worker-1', 'worker', 'redis:7', 'exited', 'Exited (0) 2 days ago', ''),
    orphanRow('portainer', '/opt/portainer', 'portainer', 'portainer', 'portainer/portainer-ce:2.19', 'running', 'Up 3 weeks', '0.0.0.0:9000->9000/tcp'),
  ].join('\n') + '\n',
);

// Named volumes per project (project<TAB>volume), for the orphan data-safety
// note: the removed stack's data that prune keeps.
writeFileSync(
  join(healthDir, 'volumes.txt'),
  ['legacy-cache\tlegacy-cache_redis-data', 'legacy-cache\tlegacy-cache_backups', 'web\tweb_data'].join('\n') + '\n',
);

// A pushed change → a deploy row carrying a real git diff + commit metadata.
const bump = (n, tag, msg) => {
  writeFileSync(join(origin, n, 'docker-compose.yml'), `services:\n  ${n}:\n    image: nginx:${tag}\n`);
  git(origin, 'commit', '-am', msg);
};
bump('web', '1.26', 'feat(web): bump nginx to 1.26');
await webhook();
await sleep(1500);
bump('api', '1.27', 'fix(api): pin nginx 1.27 for CVE');
bump('api', '1.27.1', 'chore(api): patch bump'); // two commits → the multi-commit diff head
await webhook();
await sleep(1500);

const bar = '─'.repeat(48);
console.error(`\n[ui-preview] ${bar}`);
console.error(`[ui-preview]   UI ready:  http://127.0.0.1:${PORT}/`);
console.error(`[ui-preview]   (bound on all interfaces, so a LAN device can reach it too)`);
console.error(`[ui-preview]   Ctrl-C to stop and clean up.`);
console.error(`[ui-preview] ${bar}\n`);
