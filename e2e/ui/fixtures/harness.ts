import { spawn, execFileSync, type ChildProcess } from 'node:child_process';
import { createServer } from 'node:net';
import { createHmac } from 'node:crypto';
import { mkdtempSync, mkdirSync, writeFileSync, rmSync, existsSync, readFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, dirname, basename } from 'node:path';
import { setTimeout as sleep } from 'node:timers/promises';

// This is the Node twin of the Go E2E harness (e2e/harness_test.go): it runs the
// real skipper binary against a local git origin and a stub docker on PATH, so
// the Playwright suite drives the exact backend the pipeline tests do. Keep the
// two in sync — notably the stub docker script and the config shape.

// stubDockerScript is a fake `docker` placed first on PATH. It records each
// invocation as "<cwd>\t<args>" and is scripted via env vars so one stub drives
// every UI status. Identical to the Go harness's stubDockerScript.
const stubDockerScript = `#!/bin/sh
dir=$(pwd)
printf '%s\\t%s\\n' "$dir" "$*" >> "$DOCKER_LOG"

if [ -n "$STUB_DOCKER_ECHO" ]; then
  case " $* " in
    *" up "*) echo "$STUB_DOCKER_ECHO" ;;
  esac
fi

case " $* " in
  *" up "*)
    if [ -n "$STUB_DOCKER_HOLD_UP" ]; then
      while [ ! -f "$STUB_DOCKER_HOLD_UP" ]; do sleep 0.05; done
    fi
    ;;
esac

if [ -n "$STUB_DOCKER_FAIL_ON" ]; then
  case " $* " in
    *" $STUB_DOCKER_FAIL_ON "*) exit 1 ;;
  esac
fi

if [ -n "$STUB_DOCKER_FAIL_NTH_UP" ]; then
  case " $* " in
    *" up "*)
      c=$(cat "$DOCKER_LOG.upcount" 2>/dev/null || echo 0)
      c=$((c + 1))
      echo "$c" > "$DOCKER_LOG.upcount"
      case ",$STUB_DOCKER_FAIL_NTH_UP," in
        *",$c,"*) exit 1 ;;
      esac
      ;;
  esac
fi
exit 0
`;

const defaultCompose = 'services:\n  app:\n    image: nginx:1.25\n';
const SECRET = 'e2e-secret';

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

function git(dir: string, ...args: string[]): void {
  const full = ['-c', 'user.name=e2e', '-c', 'user.email=e2e@example.com', ...args];
  execFileSync('git', dir ? ['-C', dir, ...full] : full, { stdio: 'pipe' });
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
  /** Extra env for the stub docker (e.g. STUB_DOCKER_FAIL_NTH_UP, or
   *  STUB_DOCKER_ECHO=<line> to print a line to stdout on `up` so the captured
   *  child-process output reaches the log ring). */
  stubEnv?: Record<string, string>;
  /** Stacks that get a `health_check:` section (timeout_seconds: 1, no url):
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
  /** Enable the web UI auth gate (ADR-0028). `token` turns on the PWA token
   *  path; `trustedHeader` + `trustedProxies` turn on the proxy path. Omit for
   *  an open UI (the default). */
  auth?: { trustedHeader?: string; trustedProxies?: string[]; token?: string };
}

/** Skipper is a running skipper binary under test with its origin, stub docker,
 *  and derived paths — the Node twin of the Go `skipper` struct. */
export class Skipper {
  readonly baseURL: string;
  readonly metricsURL: string;
  private readonly origin: string;
  private readonly stateDir: string;
  private readonly dockerLog: string;
  private readonly holdFile: string;
  private readonly stacks: string[];
  private readonly cfgPath: string;
  private readonly spawnEnv: NodeJS.ProcessEnv;
  private proc: ChildProcess;
  private out = '';

  private constructor(init: {
    baseURL: string;
    metricsURL: string;
    origin: string;
    stateDir: string;
    dockerLog: string;
    holdFile: string;
    stacks: string[];
    cfgPath: string;
    spawnEnv: NodeJS.ProcessEnv;
    proc: ChildProcess;
  }) {
    this.baseURL = init.baseURL;
    this.metricsURL = init.metricsURL;
    this.origin = init.origin;
    this.stateDir = init.stateDir;
    this.dockerLog = init.dockerLog;
    this.holdFile = init.holdFile;
    this.stacks = init.stacks;
    this.cfgPath = init.cfgPath;
    this.spawnEnv = init.spawnEnv;
    this.attach(init.proc);
  }

  /** attach binds a freshly spawned process and pipes its output into the buffer. */
  private attach(proc: ChildProcess): void {
    this.proc = proc;
    this.proc.stdout?.on('data', (b) => (this.out += b));
    this.proc.stderr?.on('data', (b) => (this.out += b));
  }

  /** start builds an origin with one dir per stack, launches the binary, waits
   *  until healthy, and waits for the startup deploy of every stack to settle. */
  static async start(opts: StartOptions = {}): Promise<Skipper> {
    const stacks = opts.stacks ?? ['web'];
    const stackIcons = opts.stackIcons ?? {};
    const base = mkdtempSync(join(tmpdir(), 'skipper-ui-e2e-'));
    const origin = join(base, 'origin');
    const repoDir = join(base, 'state', 'repo');
    const stateDir = join(base, 'state');
    const dockerLog = join(base, 'docker.log');
    // The hold file gates the stub's `up`. Present = proceed, absent = block.
    // Pre-created so the startup deploy is not held; hold()/release() toggle it.
    const holdFile = join(base, 'hold');
    mkdirSync(stateDir, { recursive: true });
    writeFileSync(holdFile, '');

    // Origin repo with one committed compose per stack.
    git('', 'init', '-b', 'main', origin);
    for (const name of stacks) {
      mkdirSync(join(origin, name), { recursive: true });
      writeFileSync(join(origin, name, 'docker-compose.yml'), defaultCompose);
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
    git(origin, 'add', '.');
    git(origin, 'commit', '-m', 'initial');

    // Stub docker on its own dir, prepended to PATH.
    const stubDir = join(base, 'stub-bin');
    mkdirSync(stubDir, { recursive: true });
    writeFileSync(join(stubDir, 'docker'), stubDockerScript, { mode: 0o755 });

    const [port, metricsPort] = await freePorts(2);
    const baseURL = `http://127.0.0.1:${port}`;
    const metricsURL = `http://127.0.0.1:${metricsPort}`;

    const cfg =
      `repo_url: ${JSON.stringify(origin)}\n` +
      `repo_dir: ${JSON.stringify(repoDir)}\n` +
      `stacks_base_dir: ${JSON.stringify(repoDir)}\n` +
      `branch: main\n` +
      `webhook_secret: ${JSON.stringify(SECRET)}\n` +
      `port: ${port}\n` +
      `metrics_port: ${metricsPort}\n` +
      `ui_enabled: true\n` +
      (opts.themeSwitcher ? `ui_theme_switcher: true\n` : '') +
      `command_timeout_seconds: 30\n` +
      (opts.auth
        ? `auth:\n` +
          (opts.auth.trustedHeader
            ? `  trusted_header: ${JSON.stringify(opts.auth.trustedHeader)}\n`
            : '') +
          (opts.auth.trustedProxies
            ? `  trusted_proxies:\n` +
              opts.auth.trustedProxies.map((p) => `    - ${JSON.stringify(p)}\n`).join('')
            : '') +
          (opts.auth.token ? `  token: ${JSON.stringify(opts.auth.token)}\n` : '')
        : '') +
      // source_url points at a closed local port so auto-match icon fetches fail
      // fast and deterministically (connection refused → 404 → monogram), keeping
      // the whole UI suite offline. Repo icon.svg overrides still resolve.
      `icons:\n  cache_dir: ${JSON.stringify(join(base, 'icons'))}\n  source_url: "http://127.0.0.1:1"\n` +
      `stacks:\n` +
      stacks
        .map(
          (n) =>
            `  - name: ${JSON.stringify(n)}\n` +
            ((opts.healthCheck ?? []).includes(n)
              ? `    health_check:\n      timeout_seconds: 1\n`
              : ''),
        )
        .join('');
    const cfgPath = join(base, 'skipper.yml');
    writeFileSync(cfgPath, cfg);

    const spawnEnv: NodeJS.ProcessEnv = {
      ...process.env,
      PATH: `${stubDir}:${process.env.PATH}`,
      DOCKER_LOG: dockerLog,
      STUB_DOCKER_HOLD_UP: holdFile,
      ...opts.stubEnv,
    };
    const proc = spawn(skipperBinPath(), ['-config', cfgPath], {
      env: spawnEnv,
      stdio: ['ignore', 'pipe', 'pipe'],
    });

    const s = new Skipper({
      baseURL,
      metricsURL,
      origin,
      stateDir,
      dockerLog,
      holdFile,
      stacks,
      cfgPath,
      spawnEnv,
      proc,
    });
    if ((opts.readiness ?? 'deployed') === 'listening') {
      await s.waitListening();
    } else {
      await s.waitHealthy();
      for (const name of stacks) {
        await s.waitFor(`startup deploy of ${name}`, () => s.stateHasStack(name));
      }
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

  /** setStackImage rewrites a stack's compose to a new nginx tag and commits it,
   *  simulating a pushed change. */
  setStackImage(stack: string, tag: string): void {
    writeFileSync(
      join(this.origin, stack, 'docker-compose.yml'),
      `services:\n  app:\n    image: nginx:${tag}\n`,
    );
    git(this.origin, 'commit', '-am', `bump ${stack} to ${tag}`);
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
