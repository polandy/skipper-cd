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
import { createServer as createHttpServer } from 'node:http';
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

// Stub docker: records nothing, succeeds at everything, and answers four
// reads — `compose ps --format json` per stack for the health poller, bare
// `docker ps -a` (orphan detection, ADR-0036), bare `docker ps` without `-a`
// (app-link detection, ADR-0041) and `docker inspect` (its label lookup) —
// from shared listings. Mirrors the harness's stub (minus the test hooks).
// A single `case` per subcommand is nested under a `compose` check: matching
// " compose " and (e.g.) " ps " as separate single-segment globs avoids the
// overlapping-space trap where `*" compose "*" ps "*` fails to match the
// adjacent "compose ps". `docker ps -a` (orphans) and `docker ps` without
// `-a` (app-link) share the outer "ps" match, so `-a` distinguishes them.
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
  *" inspect "*)
    shift 3 2>/dev/null # drop "inspect" "--format" "{{json .Config.Labels}}"
    for id in "$@"; do
      f="$STUB_PS_DIR/labels-$id.json"
      # A trailing newline per entry: real docker inspect emits one templated
      # line per container, but the seeded label files (no trailing newline)
      # would otherwise glue two containers' JSON onto a single invalid line.
      if [ -f "$f" ]; then cat "$f"; echo; else echo null; fi
    done
    exit 0 ;;
  *" ps "*)
    case " $* " in
      *" -a "*)
        [ -f "$STUB_PS_DIR/orphans.txt" ] && cat "$STUB_PS_DIR/orphans.txt"
        ;;
      *)
        [ -f "$STUB_PS_DIR/applink-ps.txt" ] && cat "$STUB_PS_DIR/applink-ps.txt"
        ;;
    esac
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
  try {
    peerServer?.close();
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
git(origin, 'add', '.');
git(origin, 'commit', '-m', 'initial');

// Multi-host fan-in (ADR-0048): stand up a stub peer serving the read API
// (/api/v1/snapshot + /api/audit) so the merged view, Host column, per-host
// colours and Hosts filter render with real cross-host data. A second peer is
// pointed at a dead port to exercise the unreachable/offline path. Keeping the
// stub here (rather than a full second skipper) keeps the preview lightweight.
const peerAudit = (stack, minsAgo, status, files) => ({
  stack,
  timestamp: new Date(Date.now() - minsAgo * 60000).toISOString(),
  status,
  duration_ms: 1500 + minsAgo * 20,
  changed_files: files,
  commit_sha: 'abc' + minsAgo,
});
const peerRoster = (name, status, minsAgo) => ({
  name,
  disabled: false,
  last_status: status,
  last_at: status ? new Date(Date.now() - minsAgo * 60000).toISOString() : undefined,
  last_commit: status ? 'def' + minsAgo + '0abc' : undefined,
});
const peerBData = {
  snapshot: {
    stacks: {
      roster: [
        peerRoster('gitea', 'success', 2),
        peerRoster('postgres', 'failed', 15),
        peerRoster('cache', 'success', 18),
        peerRoster('worker', '', 0), // discovered but never deployed
      ],
      disabled: [],
    },
    // Fanned-in health/healthwatch/app_links (ADR-0048) so peer rows reach the
    // same containers/app-link parity the primary shows for its own stacks.
    health: {
      stacks: {
        gitea: { status: 'healthy', services: [{ name: 'gitea', state: 'running', status: 'healthy' }] },
        postgres: { status: 'unhealthy', services: [{ name: 'db', state: 'restarting', status: 'unhealthy' }] },
        cache: { status: 'healthy', services: [{ name: 'redis', state: 'running', status: 'healthy' }] },
      },
    },
    healthwatch: {
      stacks: {
        postgres: {
          db: [
            { status: 'unhealthy', since: new Date(Date.now() - 4 * 60000).toISOString() },
            { status: 'healthy', since: new Date(Date.now() - 30 * 60000).toISOString() },
          ],
        },
      },
    },
    app_links: { stacks: { gitea: ['gitea.host-b.lan'] } },
  },
  audit: [
    peerAudit('web', 2, 'success', 1),
    peerAudit('api', 9, 'rolled_back', 2),
    peerAudit('cache', 18, 'success', 1),
    peerAudit('worker', 40, 'failed', 3),
  ],
};
const peerServer = createHttpServer((req, res) => {
  const url = req.url.split('?')[0];
  res.setHeader('Content-Type', 'application/json');
  if (url === '/api/v1/snapshot') return void res.end(JSON.stringify(peerBData.snapshot));
  if (url === '/api/audit') return void res.end(JSON.stringify(peerBData.audit));
  res.statusCode = 404;
  res.end('{}');
});
const peerPort = await freePort();
await new Promise((r) => peerServer.listen(peerPort, '127.0.0.1', r));
const deadPeerPort = await freePort(); // nothing listens here → host-c is unreachable

const metricsPort = await freePort();
const cfg =
  `repo_url: ${JSON.stringify(origin)}\n` +
  `repo_dir: ${JSON.stringify(repoDir)}\n` +
  // stacks_base_dir omitted: stacks live at the repo root, and the field is
  // relative to repo_dir (ADR-0043/#180) — omitting it means the repo root.
  `branch: main\n` +
  `webhook_secret: ${JSON.stringify(SECRET)}\n` +
  `port: ${PORT}\n` +
  `metrics_port: ${metricsPort}\n` +
  // Multi-host fan-in (ADR-0048): this instance is host-a; it fans in host-b
  // (the stub above, reachable) and host-c (a dead port, unreachable).
  `host_name: host-a\n` +
  `peers:\n` +
  `  - name: host-b\n    url: ${JSON.stringify(`http://127.0.0.1:${peerPort}`)}\n` +
  `  - name: host-c\n    url: ${JSON.stringify(`http://127.0.0.1:${deadPeerPort}`)}\n` +
  `ui_enabled: true\n` +
  `ui_theme_switcher: true\n` +
  `health_poll_interval_seconds: 3\n` +
  `health_watch:\n  debounce_polls: 1\n` +
  `command_timeout_seconds: 120\n` +
  // Dead source_url → auto-match icon fetches fail fast; committed icon.svg
  // overrides still resolve, so the preview stays fully offline.
  `icons:\n  cache_dir: ${JSON.stringify(join(base, 'icons'))}\n  source_url: "http://127.0.0.1:1"\n` +
  // Stack discovery (ADR-0034): the stack set comes from the repo dirs. Per-stack
  // overrides live in this one config's stacks: list (ADR-0043) — api keeps its
  // health-check gate + hooks, web has hooks, experiments is parked. worker and
  // database are discovered but never pushed → they show as "never deployed".
  `stack_discovery: true\n` +
  `stacks:\n` +
  `  - name: api\n` +
  `    health_check:\n      timeout_seconds: 1\n` +
  `    hooks:\n      pre_deploy:\n        - "echo dumping api database"\n      post_deploy:\n        - "echo smoke test ok"\n` +
  `  - name: web\n` +
  `    hooks:\n      pre_deploy:\n        - "echo starting backup"\n        - "sleep 4"\n      post_deploy:\n        - "echo verifying deploy"\n` +
  `  - name: experiments\n    disabled: true\n`;
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
// `api` is deployed yet unhealthy (the ADR-0027 case: a green deploy whose
// container crash-looped afterwards) so the health beacon + attention band have
// a landable row to jump to; `worker` is unhealthy but never deployed, so it
// also exercises the no-row jump degradation.
const setHealth = (n, svcs) => writeFileSync(join(healthDir, `${n}.json`), JSON.stringify(svcs));
setHealth('web', [{ Service: 'web', Name: 'web-1', State: 'running', Health: 'healthy' }]);
setHealth('api', [{ Service: 'api', Name: 'api-1', State: 'restarting', Health: 'unhealthy' }]);
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

// App-link detection (ADR-0041): web gets a single discovered host (icon = a
// plain link), api gets two via one variadic Host() call (icon = a popover) —
// worker/database carry no Traefik labels, so they show no icon at all.
writeFileSync(
  join(healthDir, 'applink-ps.txt'),
  [`web-c1\t${join(repoDir, 'web')}`, `api-c1\t${join(repoDir, 'api')}`].join('\n') + '\n',
);
writeFileSync(
  join(healthDir, 'labels-web-c1.json'),
  JSON.stringify({ 'traefik.enable': 'true', 'traefik.http.routers.web.rule': 'Host(`web.example.com`)' }),
);
writeFileSync(
  join(healthDir, 'labels-api-c1.json'),
  JSON.stringify({
    'traefik.enable': 'true',
    'traefik.http.routers.api.rule': 'Host(`api.example.com`,`api-internal.example.com`)',
  }),
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
