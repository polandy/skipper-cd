// ui-preview — boot a seeded skipper-cd instance for manually eyeballing the web
// UI (`make ui-preview`, or `PORT=3000 node scripts/ui-preview.mjs`).
//
// It builds the binary from the current checkout, stands up a throwaway origin
// repo + a stub `docker` on PATH + a config, launches skipper, and seeds every
// state the UI can render — deploys that succeed, fail, roll back, queue and
// block, health that is healthy / degraded / stopped, a self-heal, a parked
// stack, orphans, app links and a second host. Then it prints the URL and stays
// up until Ctrl-C, cleaning up its temp dir on exit. No docker, no network, no
// node_modules — just Node + git + the Go toolchain.
//
// This is a deliberately self-contained twin of the Playwright launcher
// (e2e/ui/fixtures/harness.ts). That harness — driven by Playwright's TS loader
// — remains the authoritative, asserted way skipper is booted for tests; this
// script trades a little duplication for zero toolchain dependencies so anyone
// can spin up the UI with one command. The two configs are deliberately not
// shared: the harness's is option-driven (one shape per mask), this one is a
// fixed scenario. What keeps this one honest is the `-validate` call below,
// not a rule about staying in sync.
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
let PORT = Number(process.env.PORT || 3000);
// --smoke: seed as usual, assert the result, clean up and exit instead of
// serving. CI runs this as skipper's startup smoke test — nothing else asserts
// that the binary boots and serves with a full-featured config (discovery,
// peers, hooks, icons, health watch, self-heal together), and it doubles as the
// guard that this fixture still produces the spread it advertises.
const SMOKE = process.argv.includes('--smoke');
const SECRET = 'ui-preview-secret';
// The four happy-path stacks the image-delta demos push through, plus the
// three that exist to make every failure badge reachable locally (see the
// pushed changes at the bottom).
const stacks = ['immich', 'nextcloud', 'paperless', 'gitea'];
const failureStacks = ['vaultwarden', 'wiki', 'backup', 'monitoring', 'syncthing'];

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
      *" up "*)
        # Failure switch: a file named after the stack in $STUB_FAIL_DIR makes
        # its \`up\` fail, so the preview can show a real failed/rolled-back
        # deploy instead of only the happy path. A file whose content is "once"
        # is consumed on first use, so the deploy fails but the rollback's own
        # \`up\` succeeds — which is what turns the row into rolled_back.
        f="$STUB_FAIL_DIR/$(basename "$(pwd)")"
        if [ -f "$f" ]; then
          [ "$(cat "$f")" = "once" ] && rm -f "$f"
          echo "Error response from daemon: driver failed programming external connectivity" >&2
          exit 1
        fi
        exit 0 ;;
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
      *" images "*)
        # \`compose images --format json\` — the second half of the
        # running-image read (ADR-0053): ContainerName + image ID.
        f="$STUB_PS_DIR/$(basename "$(pwd)")-images.json"
        [ -f "$f" ] && cat "$f"
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
  *" image inspect "*)
    # \`docker image inspect --format {{json .RepoDigests}} <name>\` — the
    # local half of the update check's digest comparison (ADR-0054). Matched
    # as one adjacent-word glob — two separate " image " / " inspect " globs
    # would hit the overlapping-space trap described above.
    for last; do :; done
    f="$STUB_PS_DIR/rd-$(printf %s "$last" | tr '/:' '__').json"
    if [ -f "$f" ]; then cat "$f"; else echo '[]'; fi
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
// settled waits until a stack has a terminal record in its durable audit log
// (ADR-0033) — the deploy actually finished. The seeding used to sleep a fixed
// 1.5s per push, which immich's `sleep 4` pre_deploy hook already outran, so
// the next push landed mid-run; polling the outcome is both faster and correct.
async function settled(stack, want) {
  for (let i = 0; i < 300; i++) {
    try {
      const recs = await (await fetch(`http://127.0.0.1:${PORT}/api/audit?stack=${stack}`)).json();
      if (recs?.length >= (want ?? 1)) return;
    } catch {}
    await sleep(200);
  }
  console.error(`[ui-preview] warning: ${stack} did not settle in time; seeding continues`);
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
const failDir = join(base, 'fail');
const stubDir = join(base, 'bin');
const bin = join(base, 'skipper');
for (const d of [join(base, 'state'), healthDir, failDir, stubDir]) mkdirSync(d, { recursive: true });
writeFileSync(join(stubDir, 'docker'), stubDocker, { mode: 0o755 });

function cleanup() {
  try {
    proc?.kill('SIGKILL');
  } catch {}
  try {
    peerServer?.close();
  } catch {}
  try {
    registryServer?.close();
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

// Local registry stub (ADR-0054): answers the update check's two read-only
// questions for the demo repos — gitea has a newer same-shape tag upstream
// (1.22.3 → 1.22.6), nextcloud's running tag was rebuilt (its digest moved).
// The demo composes point these two images at it via the 127.0.0.1 host
// prefix (a loopback registry is plain HTTP, like docker treats localhost
// registries); the visible chips only ever show the tag, so the prefix does
// not change what the UI renders. Everything else stays offline and is opted
// out in the config below.
const NEXTCLOUD_REG_DIGEST = 'sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee';
const regTags = {
  'gitea/gitea': ['1.21.0', '1.22.3', '1.22.6', 'latest'],
  nextcloud: ['30.0.1', '30.0.2'],
};
const registryServer = createHttpServer((req, res) => {
  const tagsMatch = req.url.match(/^\/v2\/(.+)\/tags\/list/);
  const manifestMatch = req.url.match(/^\/v2\/(.+)\/manifests\//);
  if (tagsMatch && regTags[tagsMatch[1]]) {
    res.setHeader('Content-Type', 'application/json');
    return void res.end(JSON.stringify({ tags: regTags[tagsMatch[1]] }));
  }
  if (manifestMatch && regTags[manifestMatch[1]]) {
    res.setHeader('Docker-Content-Digest', NEXTCLOUD_REG_DIGEST);
    res.statusCode = 200;
    return void res.end();
  }
  res.statusCode = 404;
  res.end('{}');
});
const regPort = await freePort();
await new Promise((r) => registryServer.listen(regPort, '127.0.0.1', r));
const regHost = `127.0.0.1:${regPort}`;

// Origin repo: one committed compose (+ a coloured icon) per stack.
git(base, 'init', '-b', 'main', origin);
const icon = '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="#7aa2f7"><rect width="24" height="24" rx="5"/></svg>';
// Real-world compose shapes (multi-service, realistic images) so the per-service
// image delta shows meaningful service labels: immich is multi-service (a deploy
// names several changed services + the "+N more" cap), paperless is digest-pinned
// (a same-tag rebuild exercises the digest path).
const composeYaml = (services) =>
  'services:\n' + services.map(([svc, image]) => `  ${svc}:\n    image: ${image}\n`).join('');
// paperless webserver is digest-pinned so a later same-tag rebuild (below) moves
// only the digest — the image delta then shows the shared tag + short digests.
const PAPERLESS_DIGEST_OLD = 'sha256:1111111111111111111111111111111111111111111111111111111111111111';
const initCompose = {
  immich: composeYaml([
    ['immich-server', 'ghcr.io/immich-app/immich-server:v1.118.0'],
    ['machine-learning', 'ghcr.io/immich-app/immich-machine-learning:v1.118.0'],
    ['redis', 'redis:7.2'],
    ['database', 'ghcr.io/tensorchord/pgvecto-rs:pg16-v0.3.0'],
  ]),
  nextcloud: composeYaml([
    ['app', `${regHost}/nextcloud:30.0.1`],
    ['db', 'postgres:16'],
    ['redis', 'redis:7.2'],
  ]),
  paperless: composeYaml([
    ['webserver', `ghcr.io/paperless-ngx/paperless-ngx:2.13.0@${PAPERLESS_DIGEST_OLD}`],
    ['broker', 'redis:7.2'],
    ['db', 'postgres:16'],
  ]),
  gitea: composeYaml([
    ['server', `${regHost}/gitea/gitea:1.22.3`],
    ['db', 'postgres:16'],
  ]),
};
for (const n of stacks) {
  mkdirSync(join(origin, n), { recursive: true });
  writeFileSync(join(origin, n, 'docker-compose.yml'), initCompose[n]);
  writeFileSync(join(origin, n, 'icon.svg'), icon);
}
// The failure-demo stacks: ordinary single-service composes. What makes each
// interesting is its override in the config below plus the pushed change at the
// bottom, not its contents.
for (const n of failureStacks) {
  mkdirSync(join(origin, n), { recursive: true });
  writeFileSync(join(origin, n, 'docker-compose.yml'), composeYaml([['app', `${n}:1.0.0`]]));
  writeFileSync(join(origin, n, 'icon.svg'), icon);
}
// A parked stack: a real compose directory kept out of deploys via
// disabled: true in the repo skipper.yaml — so the roster shows a disabled row.
mkdirSync(join(origin, 'experiments'), { recursive: true });
writeFileSync(join(origin, 'experiments', 'docker-compose.yml'), composeYaml([['app', 'alpine:3.20']]));
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
      // A peer tracks its own deploy repo, so its commit SHAs link to its own
      // forge — a different host than the primary's, on purpose.
      repo_web_url: 'https://forge.host-b.example/ops/deploy',
      // The peer's own update check (ADR-0054), fanned in with its roster: its
      // gitea lags further behind than the primary's.
      updates: {
        stacks: { gitea: { gitea: { running: '1.22.1', latest: '1.22.6' } } },
        checked_at: new Date(Date.now() - 8 * 60000).toISOString(),
      },
    },
    // Fanned-in health/healthwatch/app_links (ADR-0048) so peer rows reach the
    // same containers/app-link parity the primary shows for its own stacks.
    health: {
      stacks: {
        // A peer's services carry their running image too, so a peer row shows the
        // same version cell the primary's own rows do (the fan-in forwards health
        // verbatim).
        gitea: { status: 'healthy', services: [{ name: 'gitea', image: 'gitea/gitea:1.22.1', state: 'running', status: 'healthy' }] },
        postgres: { status: 'unhealthy', services: [{ name: 'db', image: 'postgres:16', state: 'restarting', status: 'unhealthy' }] },
        cache: { status: 'healthy', services: [{ name: 'redis', image: 'redis:7.2', state: 'running', status: 'healthy' }] },
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
// Canned container-log frames the peer streams as SSE, keyed by stack/service —
// what the primary's peer-logs proxy forwards so a peer's per-service log button
// shows something (ADR-0048).
const peerLogs = {
  'gitea/gitea': ['2026-07-23T09:59:58Z Server listening on :3000', '2026-07-23T10:00:02Z Repository indexer ready'],
  'postgres/db': ['2026-07-23T09:59:55Z database system is ready to accept connections', '2026-07-23T10:00:07Z checkpoint complete'],
  'cache/redis': ['2026-07-23T09:59:59Z Ready to accept connections tcp'],
};
const peerServer = createHttpServer((req, res) => {
  const url = req.url.split('?')[0];
  const logMatch = url.match(/^\/api\/container-logs\/(.+)$/);
  if (logMatch) {
    const lines = peerLogs[decodeURIComponent(logMatch[1])];
    if (!lines) { res.statusCode = 404; return void res.end('{}'); }
    res.setHeader('Content-Type', 'text/event-stream');
    res.setHeader('Cache-Control', 'no-cache');
    res.writeHead(200);
    for (const ln of lines) res.write(`data: ${ln}\n\n`);
    return; // hold open (a real follow); drops when the client disconnects
  }
  res.setHeader('Content-Type', 'application/json');
  if (url === '/api/v1/snapshot') return void res.end(JSON.stringify(peerBData.snapshot));
  if (url === '/api/audit') return void res.end(JSON.stringify(peerBData.audit));
  res.statusCode = 404;
  res.end('{}');
});
const peerPort = await freePort();
await new Promise((r) => peerServer.listen(peerPort, '127.0.0.1', r));
const deadPeerPort = await freePort(); // nothing listens here → host-c is unreachable

// A smoke run serves nothing, so a fixed port is only a way to collide with a
// preview someone already has open — which is exactly when you would run it.
if (SMOKE && !process.env.PORT) PORT = await freePort();

const metricsPort = await freePort();
const cfg =
  `repo_url: ${JSON.stringify(origin)}\n` +
  `repo_dir: ${JSON.stringify(repoDir)}\n` +
  // The preview clones from a local path, which no forge URL can be derived
  // from — set one so commit SHAs render as the links they are in production.
  `repo_web_url: "https://forge.host-a.example/ops/deploy"\n` +
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
  `runtime_health_poll_interval_seconds: 3\n` +
  `health_watch:\n  debounce_polls: 1\n` +
  // Self-heal (ADR-0029) so a healed row and its drift panel are reachable. It
  // is global because under discovery only the global flag activates the
  // poller; the stacks that exist to *show* a degraded pill opt out below, so
  // syncthing is the one it acts on. The long cooldown keeps that to one heal
  // rather than one every few seconds.
  `self_heal: true\nself_heal_min_unhealthy_polls: 2\nself_heal_cooldown_seconds: 3600\n` +
  `command_timeout_seconds: 120\n` +
  // Reconcile off. The seeded failure states are deliberately transient in
  // product terms — a rolled-back stack stays dirty and retries, a blocked one
  // unblocks once its dependency succeeds — so a reconcile tick would quietly
  // resolve them a few minutes after the preview starts and leave whoever opens
  // it later looking at an all-green board. Deploys here come from the seeding's
  // own webhooks, which is enough to keep the spread stable for the session.
  `reconcile_interval_seconds: 0\n` +
  // Dead source_url → auto-match icon fetches fail fast; committed icon.svg
  // overrides still resolve, so the preview stays fully offline.
  `icons:\n  cache_dir: ${JSON.stringify(join(base, 'icons'))}\n  source_url: "http://127.0.0.1:1"\n` +
  // Stack discovery (ADR-0034): the stack set comes from the repo dirs. Per-stack
  // overrides live in this one config's stacks: list (ADR-0043) — nextcloud keeps
  // a deploy-health gate + hooks, immich has hooks, experiments is parked. The
  // rest are discovered.
  `stack_discovery: true\n` +
  `stacks:\n` +
  // nextcloud, gitea and paperless are the health-variety demos (unhealthy /
  // unhealthy / stopped); self-heal would drive each back to a healed row and
  // take its pill with it, so they opt out and syncthing is the one it heals.
  `  - name: nextcloud\n    self_heal: false\n` +
  `    deploy_health_check:\n      timeout_seconds: 1\n` +
  `    hooks:\n      pre_deploy:\n        - "echo backing up nextcloud database"\n      post_deploy:\n        - "echo occ upgrade complete"\n` +
  `  - name: gitea\n    self_heal: false\n` +
  `  - name: paperless\n    self_heal: false\n    update_check: false\n` +
  `  - name: immich\n    update_check: false\n` +
  `    hooks:\n      pre_deploy:\n        - "echo pausing backups"\n        - "sleep 4"\n      post_deploy:\n        - "echo verifying immich"\n` +
  // vaultwarden's up fails once → the deploy fails and the rollback's own up
  // succeeds, so the row lands on rolled_back with its error panel and diff.
  // wiki opts out of rollback (ADR-0050) and fails every time → a plain failed
  // row whose containers are deliberately left running. backup depends on
  // vaultwarden, so the same run holds it back → blocked. (monitoring is paused
  // at runtime further down, once the rest has been seeded.)
  `  - name: backup\n    depends_on: ["vaultwarden"]\n    update_check: false\n` +
  `  - name: wiki\n    rollback: false\n    update_check: false\n` +
  `  - name: vaultwarden\n    update_check: false\n` +
  `  - name: monitoring\n    update_check: false\n` +
  `  - name: syncthing\n    update_check: false\n` +
  `  - name: experiments\n    disabled: true\n`;
const cfgPath = join(base, 'skipper.yml');
writeFileSync(cfgPath, cfg);

// Check the config with the binary that is about to read it. This fixture is a
// hand-written config living outside anything CI exercises, so it drifts when a
// key is renamed — it silently did for two releases. `-validate` turns that into
// one actionable line here rather than a startup failure three steps later.
try {
  execFileSync(bin, ['-config', cfgPath, '-validate'], { stdio: 'pipe' });
} catch (err) {
  console.error('[ui-preview] the seeded config is not valid for this build:\n');
  console.error(String(err.stdout ?? '').trim() || String(err.stderr ?? '').trim());
  cleanup();
  process.exit(1);
}

// Health: healthy / degraded / stopped, so the pills and panel show variety.
// nextcloud and gitea are deployed yet unhealthy (the ADR-0027 case: a green
// deploy whose container crash-looped afterwards) so the health beacon +
// attention band have landable rows to jump to; paperless is stopped.
// Each container also reports the Image it runs — what the Stacks view shows as
// the service's live version (and what the roster row's lead version is picked
// from): immich has two services mentioning the stack name (the shorter,
// immich-server, leads), nextcloud's and gitea's leads are found via their image
// repository, and paperless is digest-pinned on top of its tag.
const setHealth = (n, svcs) => writeFileSync(join(healthDir, `${n}.json`), JSON.stringify(svcs));
setHealth('immich', [
  { Service: 'immich-server', Name: 'immich-server-1', Image: 'ghcr.io/immich-app/immich-server:v1.119.0', State: 'running', Health: 'healthy' },
  { Service: 'machine-learning', Name: 'immich-machine-learning-1', Image: 'ghcr.io/immich-app/immich-machine-learning:v1.119.0', State: 'running', Health: 'healthy' },
  { Service: 'redis', Name: 'immich-redis-1', Image: 'redis:7.4', State: 'running', Health: 'healthy' },
  { Service: 'database', Name: 'immich-database-1', Image: 'ghcr.io/tensorchord/pgvecto-rs:pg16-v0.4.0', State: 'running', Health: 'healthy' },
]);
setHealth('nextcloud', [
  { Service: 'app', Name: 'nextcloud-app-1', Image: `${regHost}/nextcloud:30.0.2`, State: 'running', Health: 'healthy' },
  { Service: 'db', Name: 'nextcloud-db-1', Image: 'postgres:16', State: 'restarting', Health: 'unhealthy' },
  { Service: 'redis', Name: 'nextcloud-redis-1', Image: 'redis:7.2', State: 'running', Health: 'healthy' },
]);
setHealth('paperless', [
  {
    Service: 'webserver',
    Name: 'paperless-webserver-1',
    Image: 'ghcr.io/paperless-ngx/paperless-ngx:2.13.0@sha256:2222222222222222222222222222222222222222222222222222222222222222',
    State: 'exited',
    ExitCode: 0,
  },
]);
// The failure-demo stacks report healthy containers with their running image,
// so their rows carry a version and a pill like any other — only their deploy
// outcome is unusual. syncthing is the exception: it is seeded degraded, which
// is what gives self-heal something to act on.
for (const n of ['vaultwarden', 'wiki', 'backup', 'monitoring']) {
  setHealth(n, [
    { Service: 'app', Name: `${n}-app-1`, Image: `${n}:1.1.0`, State: 'running', Health: 'healthy' },
  ]);
}
setHealth('syncthing', [
  { Service: 'app', Name: 'syncthing-app-1', Image: 'syncthing:1.1.0', State: 'restarting', Health: 'unhealthy' },
]);
setHealth('gitea', [
  { Service: 'server', Name: 'gitea-server-1', Image: `${regHost}/gitea/gitea:1.22.3`, State: 'restarting', Health: 'unhealthy' },
  { Service: 'db', Name: 'gitea-db-1', Image: 'postgres:16', State: 'running', Health: 'healthy' },
]);

// Update-check demo data (ADR-0054): the \`compose images\` half of the
// running-image read for the two demo stacks (the others deliberately record
// none and are opted out below), and nextcloud's local RepoDigests — different
// from what the registry stub advertises, so its running tag reads rebuilt.
writeFileSync(
  join(healthDir, 'gitea-images.json'),
  JSON.stringify([
    { ContainerName: 'gitea-server-1', ID: 'sha256:40c2d6f1d8f0aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa' },
    { ContainerName: 'gitea-db-1', ID: 'sha256:51d3e7f2e9f1bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb' },
  ]),
);
writeFileSync(
  join(healthDir, 'nextcloud-images.json'),
  JSON.stringify([
    { ContainerName: 'nextcloud-app-1', ID: 'sha256:62e4f8f3faf2cccccccccccccccccccccccccccccccccccccccccccccccccccc' },
  ]),
);
writeFileSync(
  join(healthDir, `rd-${`${regHost}/nextcloud:30.0.2`.replace(/[/:]/g, '_')}.json`),
  JSON.stringify([`${regHost}/nextcloud@sha256:1010101010101010101010101010101010101010101010101010101010101010`]),
);

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
  ['legacy-cache\tlegacy-cache_redis-data', 'legacy-cache\tlegacy-cache_backups', 'nextcloud\tnextcloud_data'].join('\n') + '\n',
);

// App-link detection (ADR-0041): nextcloud gets a single discovered host (icon =
// a plain link), immich gets two via one variadic Host() call (icon = a popover)
// — paperless/gitea carry no Traefik labels, so they show no icon at all.
writeFileSync(
  join(healthDir, 'applink-ps.txt'),
  [`nextcloud-c1\t${join(repoDir, 'nextcloud')}`, `immich-c1\t${join(repoDir, 'immich')}`].join('\n') + '\n',
);
writeFileSync(
  join(healthDir, 'labels-nextcloud-c1.json'),
  JSON.stringify({ 'traefik.enable': 'true', 'traefik.http.routers.nextcloud.rule': 'Host(`cloud.example.com`)' }),
);
writeFileSync(
  join(healthDir, 'labels-immich-c1.json'),
  JSON.stringify({
    'traefik.enable': 'true',
    'traefik.http.routers.immich.rule': 'Host(`photos.example.com`,`immich-internal.example.com`)',
  }),
);

const proc = spawn(bin, ['-config', cfgPath], {
  env: { ...process.env, PATH: `${stubDir}:${process.env.PATH}`, STUB_PS_DIR: healthDir, STUB_FAIL_DIR: failDir },
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
console.error('[ui-preview] healthy — seeding deploys…');


// Pushed changes → deploy rows carrying real git diffs + commit metadata, each
// shaped to show a different image-delta case.
// nextcloud: a single-service tag bump.
writeFileSync(
  join(origin, 'nextcloud', 'docker-compose.yml'),
  composeYaml([
    ['app', `${regHost}/nextcloud:30.0.2`],
    ['db', 'postgres:16'],
    ['redis', 'redis:7.2'],
  ]),
);
git(origin, 'commit', '-am', 'feat(nextcloud): bump app to 30.0.2');
await webhook();
await settled('nextcloud', 2);
// immich: several services change across two commits in one deploy — the delta
// names each changed service (four services → three chips + "+1 more"), and the
// diff head shows the multi-commit range.
writeFileSync(
  join(origin, 'immich', 'docker-compose.yml'),
  composeYaml([
    ['immich-server', 'ghcr.io/immich-app/immich-server:v1.119.0'],
    ['machine-learning', 'ghcr.io/immich-app/immich-machine-learning:v1.118.0'],
    ['redis', 'redis:7.2'],
    ['database', 'ghcr.io/tensorchord/pgvecto-rs:pg16-v0.3.0'],
  ]),
);
git(origin, 'commit', '-am', 'feat(immich): bump server to v1.119.0');
writeFileSync(
  join(origin, 'immich', 'docker-compose.yml'),
  composeYaml([
    ['immich-server', 'ghcr.io/immich-app/immich-server:v1.119.0'],
    ['machine-learning', 'ghcr.io/immich-app/immich-machine-learning:v1.119.0'],
    ['redis', 'redis:7.4'],
    ['database', 'ghcr.io/tensorchord/pgvecto-rs:pg16-v0.4.0'],
  ]),
);
git(origin, 'commit', '-am', 'chore(immich): bump ml, redis, database');
await webhook();
await settled('immich', 2);
// paperless: a same-tag rebuild — only the pinned digest of webserver moves, so
// the delta shows the shared tag as context plus a short-digest old→new.
const PAPERLESS_DIGEST_NEW = 'sha256:2222222222222222222222222222222222222222222222222222222222222222';
writeFileSync(
  join(origin, 'paperless', 'docker-compose.yml'),
  composeYaml([
    ['webserver', `ghcr.io/paperless-ngx/paperless-ngx:2.13.0@${PAPERLESS_DIGEST_NEW}`],
    ['broker', 'redis:7.2'],
    ['db', 'postgres:16'],
  ]),
);
git(origin, 'commit', '-am', 'chore(paperless): rebuild webserver (digest bump, same tag)');
await webhook();
await settled('paperless', 2);

// Failure states, so every badge the deploy log can render is reachable here
// and not only in the e2e suite. One run produces all four, because a stack
// that rolled back stays dirty by design and a later run would redeploy it —
// successfully this time — and overwrite the outcome we want to show.
//
//   vaultwarden  up fails once → rollback restores the previous compose  → rolled_back
//   wiki         up always fails, rollback: false                        → failed
//   backup       depends_on vaultwarden, which just failed               → blocked
//   monitoring   paused through the API the autosync drawer calls        → queued
//
// The pause comes first but the commit base is already set by the pushes above,
// so the rollback has a previous commit to restore. From here on the base stays
// pinned, which is correct — a deferred change keeps its diff base until it
// actually deploys.
await fetch(`http://127.0.0.1:${PORT}/api/autosync`, {
  method: 'POST',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify({ scope: 'stack', stack: 'monitoring', enabled: false }),
});
writeFileSync(join(failDir, 'vaultwarden'), 'once');
writeFileSync(join(failDir, 'wiki'), 'always');
for (const n of ['vaultwarden', 'wiki', 'backup', 'monitoring']) {
  writeFileSync(join(origin, n, 'docker-compose.yml'), composeYaml([['app', `${n}:1.1.0`]]));
}
git(origin, 'commit', '-am', 'chore: bump the vaultwarden, wiki, backup and monitoring images');
await webhook();
await settled('vaultwarden', 2);
await settled('wiki', 2);

if (SMOKE) {
  const fail = (msg) => {
    console.error(`[ui-preview] smoke: ${msg}`);
    cleanup();
    process.exit(1);
  };
  const get = async (path) => (await fetch(`http://127.0.0.1:${PORT}${path}`)).json();

  const roster = (await get('/api/v1/snapshot'))?.stacks?.roster ?? [];
  if (roster.length !== stacks.length + failureStacks.length + 1) {
    fail(`roster has ${roster.length} stacks, expected ${stacks.length + failureStacks.length + 1}`);
  }
  // The outcomes the audit log records, so the deploy log is showing more than
  // a wall of green.
  const outcomes = new Set(roster.map((e) => e.last_status).filter(Boolean));
  for (const want of ['success', 'rolled_back', 'failed', 'healed']) {
    if (!outcomes.has(want)) fail(`no stack ended up ${want} (got ${[...outcomes].join(', ')})`);
  }
  // queued and blocked are deliberately not audit-recorded, so they are checked
  // where they do live: the pending registry behind the autosync drawer.
  const reasons = ((await get('/api/queue'))?.pending ?? []).map((p) => p.reason);
  if (!reasons.some((r) => r.startsWith('blocked by'))) fail(`nothing is blocked (queue: ${reasons.join(', ')})`);
  if (!reasons.includes('stack')) fail(`nothing is paused per-stack (queue: ${reasons.join(', ')})`);

  // The registry update check (ADR-0054): the post-run nudge is async, so wait
  // for the snapshot to carry the two demo markers — an effect the seeded
  // registry stub guarantees.
  let updates = null;
  for (let i = 0; i < 100; i++) {
    updates = (await get('/api/v1/snapshot'))?.stacks?.updates?.stacks;
    if (updates?.gitea && updates?.nextcloud) break;
    await sleep(200);
  }
  if (updates?.gitea?.server?.latest !== '1.22.6') fail(`gitea update marker missing (got ${JSON.stringify(updates?.gitea)})`);
  if (updates?.nextcloud?.app?.rebuilt !== true) fail(`nextcloud rebuilt marker missing (got ${JSON.stringify(updates?.nextcloud)})`);

  console.error(`[ui-preview] smoke OK — ${roster.length} stacks, outcomes: ${[...outcomes].sort().join(', ')}`);
  cleanup();
  process.exit(0);
}

const bar = '─'.repeat(48);
console.error(`\n[ui-preview] ${bar}`);
console.error(`[ui-preview]   UI ready:  http://127.0.0.1:${PORT}/`);
console.error(`[ui-preview]   (bound on all interfaces, so a LAN device can reach it too)`);
console.error(`[ui-preview]   Ctrl-C to stop and clean up.`);
console.error(`[ui-preview] ${bar}\n`);
