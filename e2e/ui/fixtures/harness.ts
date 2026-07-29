import { spawn, execFileSync, type ChildProcess } from 'node:child_process';
import { createServer } from 'node:net';
import { createServer as createHttpServer, type Server } from 'node:http';
import { createHmac } from 'node:crypto';
import { mkdtempSync, mkdirSync, writeFileSync, rmSync, existsSync, readFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, basename } from 'node:path';
import { setTimeout as sleep } from 'node:timers/promises';

// This is the Node twin of the Go E2E harness (e2e/harness_test.go): it runs the
// real skipper binary against a local git origin and a stub docker on PATH, so
// the Playwright suite drives the exact backend the pipeline tests do. The stub
// docker is now one shared file (e2e/fixtures/docker-stub.sh) rather than a copy
// per harness; the config shape still has to be kept in sync by hand.

// stubDockerScript is the fake `docker` placed first on PATH, shared verbatim
// with the Go harness (e2e/harness_test.go) — one file so a change reaches
// both. Its UI-only branches (orphans, container logs, per-stack health) are
// gated behind STUB_DOCKER_UI, which this harness sets and the Go one does not.
const stubDockerScript = readFileSync(join(__dirname, '..', '..', 'fixtures', 'docker-stub.sh'), 'utf8');

const defaultCompose = 'services:\n  app:\n    image: nginx:1.25\n';
const SECRET = 'e2e-secret';

/** The forge every commit SHA in the UI links to (repo_web_url). */
export const FORGE_URL = 'https://forge.e2e.test/ops/deploy';

/** repoRoot is the repository root, derived from this file's location. */
export function repoRoot(): string {
  // fixtures/harness.ts -> e2e/ui -> e2e -> <root>
  return join(__dirname, '..', '..', '..');
}

/** skipperBinPath is where globalSetup builds (and the harness launches) the binary. */
export function skipperBinPath(): string {
  return process.env.SKIPPER_E2E_BIN || join(tmpdir(), 'skipper-ui-e2e-bin', 'skipper');
}

/** buildVersion is the semver globalSetup injects via -ldflags, mirroring
 *  buildCommit. It is pinned to a fixed value rather than read from the real
 *  release-please manifest so the header version label — and the visual
 *  snapshots that include it — do not churn on every release bump (a longer
 *  string, e.g. 0.9.0 → 0.10.0, shifts the header layout). Deliberately
 *  double-digit so the snapshot exercises a wide, representative label; the
 *  real version is decoupled, so any release renders this same header in e2e.
 *  UD5 asserts the header against this same value, so the ldflags →
 *  /api/version → header through-line is still covered. */
export const buildVersion = '10.10.10';

/** buildCommit is the short commit globalSetup injects via -ldflags (mirroring
 *  the Docker/Nix builds). A fixed value keeps the header assertion (UD5)
 *  deterministic — the real commit that Go stamps into the build info would vary
 *  per checkout. No branch is injected, so the header renders the version path
 *  `v<semver> · <commit>` rather than the feature-branch path. */
export const buildCommit = 'e2ee2ee';

/** Reserve n distinct free TCP ports, holding all listeners open at once so the
 *  OS cannot hand out the same port twice, then releasing them (mirrors freePorts). */
async function freePorts(n: number): Promise<number[]> {
  const servers = [];
  const ports: number[] = [];
  for (let i = 0; i < n; i++) {
    const srv = createServer();
    await new Promise<void>((res, rej) => {
      srv.once('error', rej);
      srv.listen(0, '127.0.0.1', () => res());
    });
    ports.push((srv.address() as { port: number }).port);
    servers.push(srv);
  }
  await Promise.all(servers.map((s) => new Promise<void>((r) => s.close(() => r()))));
  return ports;
}

/** How often start() reserves fresh ports and relaunches after one was stolen
 *  (see start()). Two retries cover the observed ~once-per-thousand collision
 *  with a wide margin while a systematic bind failure still surfaces fast. */
const MAX_LAUNCH_ATTEMPTS = 3;

/** isStolenPort matches the two shapes a stolen reserved port fails with:
 *  skipper's own bind error (captured in the wait failure's output) and a
 *  peer stub's listen rejection. */
function isStolenPort(err: unknown): boolean {
  const msg = err instanceof Error ? err.message : String(err);
  return msg.includes('address already in use') || msg.includes('EADDRINUSE');
}

/** Identity the harness commits as. Overridable per instance
 *  (StartOptions.commitAuthor) so a rendered diff header can name a realistic
 *  author instead of the test default. */
export interface CommitIdentity {
  name: string;
  email: string;
}

const defaultIdentity: CommitIdentity = { name: 'e2e', email: 'e2e@example.com' };

function gitAs(id: CommitIdentity, dir: string, ...args: string[]): void {
  const full = ['-c', `user.name=${id.name}`, '-c', `user.email=${id.email}`, ...args];
  execFileSync('git', dir ? ['-C', dir, ...full] : full, { stdio: 'pipe' });
}

/** repoOverridesToHostStacks converts the former in-repo override shape
 *  (`stacks:` map keyed by name) into the host config's `stacks:` list body
 *  (ADR-0043). Each `  name:` map key becomes `  - name: name`; nested fields
 *  keep their indentation. Returns the list-item body (no leading `stacks:`). */
function repoOverridesToHostStacks(repoConfig: string): string {
  return repoConfig
    .replace(/^stacks:[ \t]*\n/, '')
    .replace(/^ {2}(\S+):[ \t]*$/gm, '  - name: $1');
}

export interface StartOptions {
  stacks?: string[];
  /**
   * Repo icon override per stack: the SVG body is committed as `<stack>/icon.svg`
   * in the origin, so `GET /api/icons/<stack>` resolves it offline (a 200 image).
   * A stack with no entry has no override and — because `icons.source_url` points
   * at a dead address (see start) — 404s, driving the UI's monogram fallback.
   */
  stackIcons?: Record<string, string>;
  /** Per-stack initial `docker-compose.yml` body, keyed by stack name; stacks
   *  without an entry get the default one-service placeholder. Lets a run start
   *  from realistic multi-service composes, so the FIRST deploy is already the
   *  real thing — used by the docs-screenshot renderer, where a
   *  placeholder-to-real migration would show up as junk rows in the feed. */
  initialCompose?: Record<string, string>;
  /** Identity the harness commits as (default `e2e <e2e@example.com>`). The
   *  docs-screenshot renderer sets Renovate, since a Renovate-driven bump is the
   *  loop skipper exists for and the diff panel shows the author. */
  commitAuthor?: CommitIdentity;
  /** Extra env for the stub docker (e.g. STUB_DOCKER_FAIL_NTH_UP, or
   *  STUB_DOCKER_ECHO=<line> to print a line to stdout on `up` so the captured
   *  child-process output reaches the log ring). */
  stubEnv?: Record<string, string>;
  /** Stacks that get a `deploy_health_check:` section (timeout_seconds: 1, no url):
   *  their `up`s run with --wait and a failing rollback `up` drives
   *  `rolled_back_unhealthy`. The stub ignores the flags, so no real waiting. */
  healthCheck?: string[];
  /**
   * How ready the instance must be before start() resolves:
   *  - 'deployed' (default): healthy (/healthz 200) and every stack's startup
   *    deploy has settled into state.yaml.
   *  - 'listening': the HTTP server merely responds. Use when the startup deploy
   *    is expected to fail (e.g. driving a `failed` badge), which would keep
   *    /healthz at 503 and never write state.
   */
  readiness?: 'deployed' | 'listening';
  /** Set `ui_theme_switcher: true` so the in-UI theme picker is present.
   *  Defaults to false, matching the server default (picker hidden, the
   *  configured theme fixed). */
  themeSwitcher?: boolean;
  /** Enable the stack-health poller at this interval (seconds). Default 0
   *  (disabled), so masks predating health stay health-free; Maske H opts in and
   *  scripts per-stack `docker compose ps` output via the stub. */
  healthPoll?: number;
  /** Enable the health watch (ADR-0031): per-service status history + alerts,
   *  riding the health poller. Requires healthPoll > 0. Configured with
   *  debounce_polls 1 so a single scripted `ps` flip is accepted immediately.
   *  Maske H's status-history case opts in. */
  healthWatch?: boolean;
  /** Per-stack on_demand_containers (container names). An exited one classifies
   *  as stopped whatever its exit code and the panel labels it on-demand
   *  (ADR-0027 amendment). Maske H's on-demand case opts in. */
  onDemand?: Record<string, string[]>;
  /** Enable self-heal (ADR-0029): a degraded stack is restored by a corrective
   *  `up`. Requires healthPoll > 0. Maske J opts in. */
  selfHeal?: boolean;
  /** Self-heal pacing overrides; omitted keys use the backend defaults (3/3/60).
   *  A small min_unhealthy_polls / cooldown keeps the mask fast. */
  selfHealMinUnhealthyPolls?: number;
  selfHealMaxAttempts?: number;
  selfHealCooldownSeconds?: number;
  /** Per-stack `docker compose ps` output written *before* skipper starts, so the
   *  first health poll already sees it. Removes the boot-time race where a
   *  freshly-deployed but unscripted stack reads as `stopped` and (with self-heal
   *  on) would be healed spuriously. Same shape as setStackHealth. */
  initialHealth?: Record<
    string,
    Array<{
      Service: string;
      Name?: string;
      /** The image the container runs — what the UI shows as the service's live
       *  version (Maske AK). Omit to seed a snapshot that carries none. */
      Image?: string;
      State: string;
      Health?: string;
      ExitCode?: number;
    }>
  >;
  /** Stack discovery (ADR-0034): boot with `stack_discovery: true` — the origin's
   *  stack dirs are the stack set. `repoConfig` (when set) is the former in-repo
   *  override shape (`stacks:` map); ADR-0043 folds it into the host config's
   *  `stacks:` list. Names in `disabled` are parked and not awaited at startup.
   *  Maske O opts in. */
  discovery?: { repoConfig?: string; disabled?: string[] };
  /** Per-stack depends_on edges (ADR-0032), mirroring the Go harness's
   *  startSkipperOrdered: a stack deploys only after its dependencies, and a
   *  failed dependency blocks it (a `blocked` row). Maske P opts in. */
  dependsOn?: Record<string, string[]>;
  /** Configure `nixos_rebuild` and commit a `configuration.nix` so the startup
   *  sync runs a (stubbed) nixos-rebuild and emits a `_nixos` deploy row. The
   *  rebuild's `systemd-run` / `systemctl` are stubbed to a fast success — no
   *  real switch. Maske W. */
  nixosRebuild?: boolean;
  /** This instance's own host label in the merged multi-host view (ADR-0048).
   *  Written as `host_name:`; defaults to the OS hostname otherwise. Maske V. */
  hostName?: string;
  /** Other skipper instances to fan in (ADR-0048). Each becomes a `peers:` entry
   *  on a reserved local port; a reachable peer gets a stub HTTP server serving
   *  its `/api/v1/snapshot` + `/api/audit`, an unreachable one points at a dead
   *  port so the primary marks it offline. Maske V opts in. */
  peers?: PeerSpec[];
  /** Override `repo_web_url`, the forge every commit SHA in the UI links to.
   *  Defaults to FORGE_URL; pass null to omit the key, which is the instance
   *  that can derive no forge (a clone from a local path) and must therefore
   *  render plain, unlinked SHAs. Maske AL opts out. */
  repoWebURL?: string | null;
}

/** PeerSpec is one stub peer the harness stands up for the multi-host fan-in. */
export interface PeerSpec {
  /** The peer's host label (matches a `peers[].name`). */
  name: string;
  /** The JSON the stub serves at `/api/v1/snapshot` — curated to the keys the
   *  fan-in reads (`stacks`, `health`, `healthwatch`, `app_links`). */
  snapshot?: Record<string, unknown>;
  /** The audit records the stub serves at `/api/audit` (the peer's deploys). */
  audit?: Array<Record<string, unknown>>;
  /** Diffs the stub serves at `/api/events/{id}/diffs`, keyed by event id — what
   *  the primary's peer-diff proxy fetches on a peer-row expand. */
  diffs?: Record<string, unknown>;
  /** Container-log lines the stub streams as SSE at
   *  `/api/container-logs/{stack}`, keyed by `stack` (ADR-0048; service selection
   *  rides the stripped `?services=` query, so the path is stack-only). Each string
   *  is emitted as one `data:` frame and the stream is held open (a real follow)
   *  until the client disconnects. */
  logsByStack?: Record<string, string[]>;
  /** false → no server is started; the peer URL points at a dead port so the
   *  primary sees it unreachable (offline banner). Default true. */
  reachable?: boolean;
}

/** OrphanContainer is one line of the stub's `docker ps -a` listing — a single
 *  compose-managed container. Several sharing a `project` group into one orphan.
 *  Mirrors the columns the detector reads (orphans/detect.go psColumns). */
export interface OrphanContainer {
  project: string;
  workingDir: string;
  /** Defaults to `<workingDir>/docker-compose.yml`. */
  configFile?: string;
  name: string;
  service?: string;
  image?: string;
  /** docker State: running | exited | restarting | dead | created (default running). */
  state?: string;
  status?: string;
  ports?: string;
}

/** OrphanVolume maps a compose-created named volume to its owning project. */
export interface OrphanVolume {
  project: string;
  volume: string;
}

/** orphansListing renders container specs into the tab-separated form the stub's
 *  `docker ps -a` emits (columns per orphans/detect.go psColumns). */
function orphansListing(rows: OrphanContainer[]): string {
  return (
    rows
      .map((r) =>
        [
          r.project,
          r.workingDir,
          r.configFile ?? join(r.workingDir, 'docker-compose.yml'),
          r.name,
          r.service ?? '',
          r.image ?? '',
          r.state ?? 'running',
          r.status ?? '',
          r.ports ?? '',
        ].join('\t'),
      )
      .join('\n') + '\n'
  );
}

/** volumesListing renders volume specs into `project<TAB>volume` lines. */
function volumesListing(vols: OrphanVolume[]): string {
  return vols.map((v) => `${v.project}\t${v.volume}`).join('\n') + '\n';
}

/** Workspace is the on-disk layout one instance runs against: its temp tree, the
 *  origin repo, the stub-binary dir, and the dirs the stub reads its scripted
 *  answers from. Built once, then only read. */
interface Workspace {
  readonly base: string;
  readonly origin: string;
  readonly repoDir: string;
  readonly stateDir: string;
  readonly dockerLog: string;
  readonly holdFile: string;
  readonly hookHoldFile: string;
  readonly stubDir: string;
  readonly healthDir: string;
  readonly orphansDir: string;
  readonly stacks: string[];
  readonly author: CommitIdentity;
}

/** scaffoldWorkspace lays out the temp tree, commits an origin repo with one
 *  compose per stack, and installs the stub binaries and the dirs they read.
 *  Filesystem only — nothing is listening yet. */
function scaffoldWorkspace(opts: StartOptions): Workspace {
  const stacks = opts.stacks ?? ['web'];
  const stackIcons = opts.stackIcons ?? {};
  const initialCompose = opts.initialCompose ?? {};
  const author = opts.commitAuthor ?? defaultIdentity;
  const base = mkdtempSync(join(tmpdir(), 'skipper-ui-e2e-'));
  const origin = join(base, 'origin');
  const repoDir = join(base, 'state', 'repo');
  const stateDir = join(base, 'state');
  const dockerLog = join(base, 'docker.log');
  // The hold file gates the stub's `up`. Present = proceed, absent = block.
  // Pre-created so the startup deploy is not held; hold()/release() toggle it.
  const holdFile = join(base, 'hold');
  // The hook-hold file gates a pre/post-deploy hook that waits on it (the
  // running-hook tests). Absent = block, present = proceed — and it is NOT
  // pre-created, so such a hook blocks from boot until releaseHook() creates
  // it. This makes the running-hook phase observable with no wall-clock race.
  const hookHoldFile = join(base, 'hook-hold');
  mkdirSync(stateDir, { recursive: true });
  writeFileSync(holdFile, '');

  // Origin repo with one committed compose per stack.
  gitAs(author, '', 'init', '-b', 'main', origin);
  for (const name of stacks) {
    mkdirSync(join(origin, name), { recursive: true });
    writeFileSync(join(origin, name, 'docker-compose.yml'), initialCompose[name] ?? defaultCompose);
    if (stackIcons[name] !== undefined) {
      writeFileSync(join(origin, name, 'icon.svg'), stackIcons[name]);
    }
  }
  // With no stacks there is nothing to stage, but the origin still needs a HEAD
  // on `main` for skipper to clone and reset onto; a placeholder gives the
  // stack-free instance (UA10 empty state) a valid, deploy-free repo.
  if (stacks.length === 0) {
    writeFileSync(join(origin, '.keep'), '');
  }
  // A tracked .nix file makes the startup sync detect a nix change and run the
  // (stubbed) nixos-rebuild, emitting a `_nixos` row.
  if (opts.nixosRebuild) {
    writeFileSync(join(origin, 'configuration.nix'), '{ }\n');
  }
  // ADR-0043: per-stack overrides go into the host config's stacks: list, not
  // an in-repo skipper.yaml (a leftover one is now a hard error). setRepoConfig
  // still writes one on purpose, to exercise that guard.
  gitAs(author, origin, 'add', '.');
  gitAs(author, origin, 'commit', '-m', 'initial');

  // Stub docker on its own dir, prepended to PATH.
  const stubDir = join(base, 'stub-bin');
  mkdirSync(stubDir, { recursive: true });
  writeFileSync(join(stubDir, 'docker'), stubDockerScript, { mode: 0o755 });
  // Stub the nixos-rebuild transition (ADR-0014): `systemd-run` starts the
  // (fake) unit and returns; `systemctl is-active` reports it already gone and
  // `is-failed` reports not-failed, so Rebuild sees an instant success without
  // any real switch. Harmless when nixos_rebuild is not configured.
  writeFileSync(join(stubDir, 'systemd-run'), '#!/bin/sh\nexit 0\n', { mode: 0o755 });
  writeFileSync(
    join(stubDir, 'systemctl'),
    '#!/bin/sh\ncase " $* " in\n  *" is-active "*) exit 1 ;;\n  *" is-failed "*) exit 1 ;;\n  *) exit 0 ;;\nesac\n',
    { mode: 0o755 },
  );

  // Where setStackHealth writes per-stack `compose ps` output for the stub.
  const healthDir = join(base, 'health');
  mkdirSync(healthDir, { recursive: true });
  // Pre-boot health seed, so the very first poll already sees it.
  for (const [name, services] of Object.entries(opts.initialHealth ?? {})) {
    writeFileSync(join(healthDir, `${name}.json`), JSON.stringify(services));
  }

  // Where setOrphans/setVolumes write the stub's `docker ps -a` / `volume ls`
  // listing (ADR-0036), consulted by orphan detection on the health-poll cadence.
  const orphansDir = join(base, 'orphans');
  mkdirSync(orphansDir, { recursive: true });

  return {
    base,
    origin,
    repoDir,
    stateDir,
    dockerLog,
    holdFile,
    hookHoldFile,
    stubDir,
    healthDir,
    orphansDir,
    stacks,
    author,
  };
}

/** startPeerStubs serves the multi-host fan-in's peers (ADR-0048): the servers
 *  to close at teardown, plus the `peers:` config block naming them. A spec with
 *  no server keeps its reserved port closed, which is how an offline peer is
 *  driven. */
async function startPeerStubs(
  peerSpecs: PeerSpec[],
  peerPorts: number[],
): Promise<{ peerServers: Server[]; peersCfg: string }> {
  // Stub peer servers for the multi-host fan-in (ADR-0048): a reachable peer
  // serves its curated snapshot + audit; an unreachable one has no server, so
  // its reserved port refuses connections and the primary marks it offline.
  const peerServers: Server[] = [];
  let peersCfg = '';
  if (peerSpecs.length) {
    peersCfg = 'peers:\n';
    for (let i = 0; i < peerSpecs.length; i++) {
      const spec = peerSpecs[i];
      const peerPort = peerPorts[i];
      peersCfg += `  - name: ${JSON.stringify(spec.name)}\n    url: ${JSON.stringify(`http://127.0.0.1:${peerPort}`)}\n`;
      if (spec.reachable === false) continue; // dead port → unreachable
      const server = createHttpServer((req, res) => {
        const path = (req.url ?? '').split('?')[0];
        res.setHeader('Content-Type', 'application/json');
        if (path === '/api/v1/snapshot') return void res.end(JSON.stringify(spec.snapshot ?? {}));
        if (path === '/api/audit') return void res.end(JSON.stringify(spec.audit ?? []));
        const diffMatch = path.match(/^\/api\/events\/([^/]+)\/diffs$/);
        if (diffMatch) {
          const d = (spec.diffs ?? {})[diffMatch[1]];
          if (d) return void res.end(JSON.stringify(d));
          res.statusCode = 404;
          return void res.end('{}');
        }
        const logMatch = path.match(/^\/api\/container-logs\/(.+)$/);
        if (logMatch) {
          const lines = (spec.logsByStack ?? {})[decodeURIComponent(logMatch[1])];
          if (!lines) {
            res.statusCode = 404;
            return void res.end('{}');
          }
          // SSE follow: emit each line as a frame, then hold the stream open
          // (the primary proxies it; it drops when the client disconnects or
          // skipper is SIGKILLed at teardown, closing this socket).
          res.setHeader('Content-Type', 'text/event-stream');
          res.setHeader('Cache-Control', 'no-cache');
          res.writeHead(200);
          for (const ln of lines) res.write(`data: ${ln}\n\n`);
          return;
        }
        res.statusCode = 404;
        res.end('{}');
      });
      // Reject on a listen error (a stolen reserved port raises EADDRINUSE)
      // instead of letting it escape as an uncaught exception; close the
      // stubs already up so a retried launch never leaks servers.
      try {
        await new Promise<void>((res, rej) => {
          server.once('error', rej);
          server.listen(peerPort, '127.0.0.1', () => res());
        });
      } catch (err) {
        for (const started of peerServers) started.close();
        throw err;
      }
      peerServers.push(server);
    }
  }

  return { peerServers, peersCfg };
}

/** buildConfig renders the host skipper.yml for one instance. Every option that
 *  only shapes configuration lands here, so start() reads as scaffold → stubs →
 *  config → spawn instead of interleaving all four. */
function buildConfig(
  opts: StartOptions,
  ws: Workspace,
  ports: { port: number; metricsPort: number },
  peersCfg: string,
): string {
  const { base, origin, repoDir, stacks } = ws;
  const { port, metricsPort } = ports;
  const cfg =
    `repo_url: ${JSON.stringify(origin)}\n` +
    `repo_dir: ${JSON.stringify(repoDir)}\n` +
    // The harness clones from a local path, which no forge URL can be derived
    // from — set one explicitly so commit SHAs render as links (Maske AL).
    (opts.repoWebURL === null ? '' : `repo_web_url: ${JSON.stringify(opts.repoWebURL ?? FORGE_URL)}\n`) +
    // stacks_base_dir omitted: it is relative to repo_dir and defaults to the
    // repo root, which is exactly repoDir here (stacks live at the clone root).
    `branch: main\n` +
    `webhook_secret: ${JSON.stringify(SECRET)}\n` +
    `port: ${port}\n` +
    `metrics_port: ${metricsPort}\n` +
    `ui_enabled: true\n` +
    (opts.themeSwitcher ? `ui_theme_switcher: true\n` : '') +
    (opts.hostName ? `host_name: ${JSON.stringify(opts.hostName)}\n` : '') +
    peersCfg +
    // Health polling off by default so masks predating it stay health-free;
    // Maske H opts in via healthPoll (ADR-0027).
    `runtime_health_poll_interval_seconds: ${opts.healthPoll ?? 0}\n` +
    (opts.healthWatch ? `health_watch:\n  debounce_polls: 1\n` : '') +
    (opts.selfHeal ? `self_heal: true\n` : '') +
    (opts.selfHealMinUnhealthyPolls !== undefined
      ? `self_heal_min_unhealthy_polls: ${opts.selfHealMinUnhealthyPolls}\n`
      : '') +
    (opts.selfHealMaxAttempts !== undefined
      ? `self_heal_max_attempts: ${opts.selfHealMaxAttempts}\n`
      : '') +
    (opts.selfHealCooldownSeconds !== undefined
      ? `self_heal_cooldown_seconds: ${opts.selfHealCooldownSeconds}\n`
      : '') +
    `command_timeout_seconds: 30\n` +
    (opts.nixosRebuild ? `nixos_rebuild:\n  flake: ".#test"\n` : '') +
    // source_url points at a closed local port so auto-match icon fetches fail
    // fast and deterministically (connection refused → 404 → monogram), keeping
    // the whole UI suite offline. Repo icon.svg overrides still resolve.
    `icons:\n  cache_dir: ${JSON.stringify(join(base, 'icons'))}\n  source_url: "http://127.0.0.1:1"\n` +
    (opts.discovery
      ? `stack_discovery: true\n` +
        (opts.discovery.repoConfig !== undefined
          ? `stacks:\n` + repoOverridesToHostStacks(opts.discovery.repoConfig)
          : '')
      : // ADR-0043: discovery is the default, so explicit-list masks opt out.
        `stack_discovery: false\n` +
        `stacks:\n` +
        stacks
          .map(
            (n) =>
              `  - name: ${JSON.stringify(n)}\n` +
              ((opts.dependsOn?.[n] ?? []).length
                ? `    depends_on: [${(opts.dependsOn?.[n] ?? []).map((d) => JSON.stringify(d)).join(', ')}]\n`
                : '') +
              ((opts.healthCheck ?? []).includes(n)
                ? `    deploy_health_check:\n      timeout_seconds: 1\n`
                : '') +
              ((opts.onDemand?.[n] ?? []).length
                ? `    on_demand_containers:\n` +
                  (opts.onDemand?.[n] ?? []).map((c) => `      - ${JSON.stringify(c)}\n`).join('')
                : ''),
          )
          .join(''));
  return cfg;
}

/** Skipper is a running skipper binary under test with its origin, stub docker,
 *  and derived paths — the Node twin of the Go `skipper` struct. */
export class Skipper {
  readonly baseURL: string;
  readonly metricsURL: string;
  /** stacksBaseDir is the repo clone dir (== stacks_base_dir); a stack `foo`
   *  deploys with working_dir `<stacksBaseDir>/foo`. Orphan tests build a
   *  container's working_dir under it to classify as orphaned. */
  readonly stacksBaseDir: string;
  private readonly origin: string;
  private readonly stateDir: string;
  private readonly dockerLog: string;
  private readonly holdFile: string;
  private readonly hookHoldFile: string;
  private readonly stacks: string[];
  private readonly author: CommitIdentity;
  private readonly cfgPath: string;
  private readonly healthDir: string;
  private readonly orphansDir: string;
  private readonly spawnEnv: NodeJS.ProcessEnv;
  private readonly peerServers: Server[];
  private proc: ChildProcess;
  private out = '';

  private constructor(init: {
    baseURL: string;
    metricsURL: string;
    stacksBaseDir: string;
    origin: string;
    stateDir: string;
    dockerLog: string;
    holdFile: string;
    hookHoldFile: string;
    stacks: string[];
    author: CommitIdentity;
    cfgPath: string;
    healthDir: string;
    orphansDir: string;
    spawnEnv: NodeJS.ProcessEnv;
    peerServers: Server[];
    proc: ChildProcess;
  }) {
    this.baseURL = init.baseURL;
    this.metricsURL = init.metricsURL;
    this.stacksBaseDir = init.stacksBaseDir;
    this.origin = init.origin;
    this.stateDir = init.stateDir;
    this.dockerLog = init.dockerLog;
    this.holdFile = init.holdFile;
    this.hookHoldFile = init.hookHoldFile;
    this.stacks = init.stacks;
    this.author = init.author;
    this.cfgPath = init.cfgPath;
    this.healthDir = init.healthDir;
    this.orphansDir = init.orphansDir;
    this.spawnEnv = init.spawnEnv;
    this.peerServers = init.peerServers;
    this.attach(init.proc);
  }

  /** attach binds a freshly spawned process and pipes its output into the buffer. */
  private attach(proc: ChildProcess): void {
    this.proc = proc;
    this.proc.stdout?.on('data', (b) => (this.out += b));
    this.proc.stderr?.on('data', (b) => (this.out += b));
  }

  /** start builds an origin with one dir per stack, launches the binary, waits
   *  until healthy, and waits for the startup deploy of every stack to settle.
   *  Reads as its four phases — scaffold, peer stubs, config, spawn-and-wait —
   *  each of which lives in its own helper above.
   *
   *  The reserved ports are released again before skipper (or a peer stub)
   *  binds them, and they come from the same ephemeral range every outbound
   *  connection draws from — so under parallel workers another process can
   *  steal one in that gap. The theft is detected deterministically (skipper
   *  exits with its bind error, a stub's listen rejects with EADDRINUSE) and
   *  the launch is retried on fresh ports; any other failure propagates on
   *  the first throw. */
  static async start(opts: StartOptions = {}): Promise<Skipper> {
    const ws = scaffoldWorkspace(opts);
    for (let attempt = 1; ; attempt++) {
      try {
        return await Skipper.launch(opts, ws);
      } catch (err) {
        if (attempt >= MAX_LAUNCH_ATTEMPTS || !isStolenPort(err)) throw err;
      }
    }
  }

  /** launch runs the port-dependent phases of start() once: reserve ports,
   *  start peer stubs, write the config, spawn skipper and wait for readiness.
   *  A failure after spawn tears the instance down before rethrowing, so a
   *  retried launch never leaks a process or a stub server. */
  private static async launch(opts: StartOptions, ws: Workspace): Promise<Skipper> {
    const { base, origin, repoDir, stateDir, dockerLog, holdFile, hookHoldFile } = ws;
    const { stubDir, healthDir, orphansDir, stacks, author } = ws;

    // Two ports for this instance, plus one per peer (reachable or dead).
    const peerSpecs = opts.peers ?? [];
    const ports = await freePorts(2 + peerSpecs.length);
    const [port, metricsPort] = ports;
    const peerPorts = ports.slice(2);
    const baseURL = `http://127.0.0.1:${port}`;
    const metricsURL = `http://127.0.0.1:${metricsPort}`;

    const { peerServers, peersCfg } = await startPeerStubs(peerSpecs, peerPorts);

    const cfg = buildConfig(opts, ws, { port, metricsPort }, peersCfg);

    const cfgPath = join(base, 'skipper.yml');
    writeFileSync(cfgPath, cfg);

    const spawnEnv: NodeJS.ProcessEnv = {
      ...process.env,
      PATH: `${stubDir}:${process.env.PATH}`,
      DOCKER_LOG: dockerLog,
      STUB_DOCKER_HOLD_UP: holdFile,
      // Inherited by hook commands (run via `sh -c` with os.Environ()); a hook
      // can wait on this path to hold the running-hook phase deterministically.
      SKIPPER_E2E_HOOK_HOLD: hookHoldFile,
      STUB_DOCKER_UI: '1', // enables the stub's orphan/logs/per-stack-health branches
      STUB_DOCKER_PS_DIR: healthDir,
      STUB_DOCKER_ORPHANS_DIR: orphansDir,
      ...opts.stubEnv,
    };
    const proc = spawn(skipperBinPath(), ['-config', cfgPath], {
      env: spawnEnv,
      stdio: ['ignore', 'pipe', 'pipe'],
    });

    const s = new Skipper({
      baseURL,
      metricsURL,
      stacksBaseDir: repoDir,
      origin,
      stateDir,
      dockerLog,
      holdFile,
      hookHoldFile,
      stacks,
      author,
      cfgPath,
      healthDir,
      orphansDir,
      spawnEnv,
      peerServers,
      proc,
    });
    try {
      if ((opts.readiness ?? 'deployed') === 'listening') {
        await s.waitListening();
      } else {
        await s.waitHealthy();
        const parked = new Set(opts.discovery?.disabled ?? []);
        for (const name of stacks) {
          if (parked.has(name)) continue; // disabled: never deploys, never in state
          await s.waitFor(`startup deploy of ${name}`, () => s.stateHasStack(name));
        }
      }
    } catch (err) {
      await s.stop();
      throw err;
    }
    return s;
  }

  /** hold makes the next `compose … up` block until release() is called, so the
   *  `deploying` state can be observed in the UI. */
  hold(): void {
    if (existsSync(this.holdFile)) rmSync(this.holdFile);
  }

  /** release unblocks a held `up`. */
  release(): void {
    writeFileSync(this.holdFile, '');
  }

  /** releaseHook unblocks a pre/post-deploy hook that waits on the hook-hold
   *  file (a hook of `while [ ! -f "$SKIPPER_E2E_HOOK_HOLD" ]; do …; done`), so
   *  the running-hook phase can be observed with no timeout, then let go. */
  releaseHook(): void {
    writeFileSync(this.hookHoldFile, '');
  }

  /** setNixConfig rewrites the tracked configuration.nix and commits it, so the
   *  next sync runs a nixos-rebuild whose `_nixos` row carries a real git diff
   *  against the last deployed commit (has_diffs=true) — the realistic case. */
  setNixConfig(content: string): void {
    writeFileSync(join(this.origin, 'configuration.nix'), content);
    gitAs(this.author, this.origin, 'commit', '-am', 'update configuration.nix');
  }

  /** setStackImage retags the FIRST service in a stack's compose and commits it,
   *  simulating a pushed version bump. On the default one-service placeholder that
   *  is `nginx:<tag>`, as it always was; on a realistic multi-service compose
   *  (initialCompose / setStackServices) it moves just that one tag, so the pushed
   *  diff is the single line a dependency bot would write. */
  setStackImage(stack: string, tag: string): void {
    const path = join(this.origin, stack, 'docker-compose.yml');
    const current = readFileSync(path, 'utf8');
    // Only the tag after the last ':' of the first image line — a registry
    // `host:port` earlier in the reference must survive.
    const bumped = current.replace(/^(\s+image: \S*?)(?::[^:/\s]+)?$/m, `$1:${tag}`);
    writeFileSync(path, bumped);
    gitAs(this.author, this.origin, 'commit', '-am', `bump ${stack} to ${tag}`);
  }

  /** setStackServices rewrites a stack's compose to an explicit service→image map
   *  and commits it, simulating a pushed change. Unlike setStackImage (one `app`
   *  service on an nginx tag), this drives multi-service and digest-pinned image
   *  changes, so the deploy's `image_changes` — and the row's per-service image
   *  delta — can be exercised across those shapes. */
  setStackServices(stack: string, services: Record<string, string>): void {
    const body = Object.entries(services)
      .map(([name, image]) => `  ${name}:\n    image: ${image}`)
      .join('\n');
    writeFileSync(join(this.origin, stack, 'docker-compose.yml'), `services:\n${body}\n`);
    gitAs(this.author, this.origin, 'commit', '-am', `update ${stack} services`);
  }

  /** setStackHealth scripts the stub's `docker compose ps` output for a stack, so
   *  the health poller (enable with the `healthPoll` start option) reports the
   *  given services on its next poll. */
  setStackHealth(
    stack: string,
    services: Array<{
      Service: string;
      Name?: string;
      Image?: string;
      State: string;
      Health?: string;
      ExitCode?: number;
    }>,
  ): void {
    writeFileSync(join(this.healthDir, `${stack}.json`), JSON.stringify(services));
  }

  /** setOrphans rewrites the stub's `docker ps -a` listing (ADR-0036), so the
   *  next orphan detection (health-poll cadence, UI-gated) sees exactly these
   *  compose containers. Passing [] clears it (nothing running). */
  setOrphans(rows: OrphanContainer[]): void {
    writeFileSync(join(this.orphansDir, 'orphans.txt'), orphansListing(rows));
  }

  /** setVolumes rewrites the stub's `docker volume ls` listing — the named
   *  volumes an orphan holds, surfaced as its data-safety note. */
  setVolumes(vols: OrphanVolume[]): void {
    writeFileSync(join(this.orphansDir, 'volumes.txt'), volumesListing(vols));
  }

  /** setRepoConfig rewrites the repo-root skipper.yaml (stack discovery,
   *  ADR-0034) and commits it, simulating a pushed config change. */
  setRepoConfig(content: string): void {
    writeFileSync(join(this.origin, 'skipper.yaml'), content);
    gitAs(this.author, this.origin, 'add', 'skipper.yaml');
    gitAs(this.author, this.origin, 'commit', '-m', 'update skipper.yaml');
  }

  /** sendWebhook posts a correctly signed push payload for ref, returning the status. */
  async sendWebhook(ref: string): Promise<number> {
    const body = JSON.stringify({ ref });
    const sig = createHmac('sha256', SECRET).update(body).digest('hex');
    const resp = await fetch(`${this.baseURL}/webhook`, {
      method: 'POST',
      headers: { 'X-Gitea-Signature': sig },
      body,
    });
    return resp.status;
  }

  /** sendBadWebhook posts a push payload with a deliberately invalid signature.
   *  The backend rejects it (401) and logs a WARN ("webhook rejected: invalid
   *  signature") — a deterministic way to drive a WARN-level log line. */
  async sendBadWebhook(ref: string): Promise<number> {
    const resp = await fetch(`${this.baseURL}/webhook`, {
      method: 'POST',
      headers: { 'X-Gitea-Signature': 'not-a-valid-signature' },
      body: JSON.stringify({ ref }),
    });
    return resp.status;
  }

  /** postAutosync sets a global (stack === '') or per-stack autosync override. */
  async postAutosync(stack: string, enabled: boolean): Promise<number> {
    const scope = stack === '' ? 'global' : 'stack';
    const resp = await fetch(`${this.baseURL}/api/autosync`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ scope, stack, enabled }),
    });
    return resp.status;
  }

  /** dockerUps counts recorded `compose … up` invocations for a stack (attributed
   *  by the invocation's working directory). */
  dockerUps(stack: string): number {
    let n = 0;
    for (const line of this.dockerLogLines()) {
      const tab = line.indexOf('\t');
      if (tab < 0) continue;
      const cwd = line.slice(0, tab);
      const args = line.slice(tab + 1);
      if (basename(cwd) === stack && ` ${args} `.includes(' up ')) n++;
    }
    return n;
  }

  private dockerLogLines(): string[] {
    try {
      return readFileSync(this.dockerLog, 'utf8').trim().split('\n').filter(Boolean);
    } catch {
      return [];
    }
  }

  private stateHasStack(stack: string): boolean {
    try {
      return readFileSync(join(this.stateDir, 'state.yaml'), 'utf8').includes(`${stack}:`);
    } catch {
      return false;
    }
  }

  private async waitHealthy(): Promise<void> {
    await this.waitFor('healthz 200', async () => {
      try {
        return (await fetch(`${this.baseURL}/healthz`)).status === 200;
      } catch {
        return false;
      }
    });
  }

  /** waitListening waits until the HTTP server responds at all (any status),
   *  used when the startup deploy is expected to fail so /healthz stays 503. */
  private async waitListening(): Promise<void> {
    await this.waitFor('server listening', async () => {
      try {
        await fetch(`${this.baseURL}/healthz`);
        return true;
      } catch {
        return false;
      }
    });
  }

  private async waitFor(what: string, cond: () => boolean | Promise<boolean>): Promise<void> {
    const deadline = Date.now() + 20_000;
    while (Date.now() < deadline) {
      if (await cond()) return;
      // A process that has exited can never satisfy the condition (checked
      // after cond so a last-gasp effect, e.g. state written just before the
      // exit, still counts): fail now with the output that says why — a bind
      // failure surfaces here in milliseconds instead of a silent 20s spin.
      if (this.proc.exitCode !== null || this.proc.signalCode !== null) {
        throw new Error(
          `skipper exited while waiting for ${what}\n--- skipper output ---\n${this.out}`,
        );
      }
      await sleep(50);
    }
    throw new Error(`timed out waiting for ${what}\n--- skipper output ---\n${this.out}`);
  }

  /** Captured stdout+stderr, for attaching to a failed test. */
  output(): string {
    return this.out;
  }

  /** stop terminates the process and waits for it to exit.
   *
   *  It sends SIGKILL, not SIGINT: at teardown (and before every relaunch) the
   *  test's browser page is still open, so its long-lived SSE /api/events
   *  connection is still draining. A graceful SIGINT makes skipper's
   *  http.Server.Shutdown block on that connection for the full production
   *  `shutdownTimeout` (10s, cmd/skipper/main.go) — 10s of dead time on *every*
   *  test. Graceful shutdown is a backend concern covered by the Go pipeline
   *  tests; here we only need the process gone and its ports freed, fast. An
   *  abrupt drop is also the more faithful signal for the reconnect cases
   *  (UD2/UE): the SSE stream is cut immediately instead of lingering. */
  async stop(): Promise<void> {
    for (const server of this.peerServers) {
      // closeAllConnections drops any held-open peer socket (an SSE container-log
      // follow) so close() does not wait on it and hang teardown.
      server.closeAllConnections?.();
      server.close();
    }
    this.peerServers.length = 0;
    if (this.proc.exitCode !== null || this.proc.signalCode !== null) return;
    const exited = new Promise<void>((res) => this.proc.once('exit', () => res()));
    this.proc.kill('SIGKILL');
    await exited;
  }

  /** relaunch respawns skipper on the same config and ports (after stop()), so
   *  the UI's SSE stream drops and then re-establishes against the returning
   *  server — used to drive the connection indicator reconnecting→connected (UD2).
   *  Pass `bin` to return a *different* build on the same origin/ports, e.g. one
   *  whose service worker carries a new version so the browser sees a PWA update
   *  (UE1/UE2). */
  async relaunch(bin: string = skipperBinPath()): Promise<void> {
    this.attach(
      spawn(bin, ['-config', this.cfgPath], {
        env: this.spawnEnv,
        stdio: ['ignore', 'pipe', 'pipe'],
      }),
    );
    await this.waitHealthy();
  }
}
