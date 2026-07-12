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
      [ "$c" = "$STUB_DOCKER_FAIL_NTH_UP" ] && exit 1
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

/** manifestVersion is the release-please-tracked version that globalSetup injects
 *  into the binary via -ldflags (mirroring the Docker/Nix builds) and that the
 *  header version label is asserted against. Reading the same file both sides
 *  keeps the check release-proof. */
export function manifestVersion(): string {
  const raw = readFileSync(join(repoRoot(), '.release-please-manifest.json'), 'utf8');
  return (JSON.parse(raw) as Record<string, string>)['.'];
}

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
  /**
   * How ready the instance must be before start() resolves:
   *  - 'deployed' (default): healthy (/healthz 200) and every stack's startup
   *    deploy has settled into state.yaml.
   *  - 'listening': the HTTP server merely responds. Use when the startup deploy
   *    is expected to fail (e.g. driving a `failed` badge), which would keep
   *    /healthz at 503 and never write state.
   */
  readiness?: 'deployed' | 'listening';
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
  private readonly proc: ChildProcess;
  private out = '';

  private constructor(init: {
    baseURL: string;
    metricsURL: string;
    origin: string;
    stateDir: string;
    dockerLog: string;
    holdFile: string;
    stacks: string[];
    proc: ChildProcess;
  }) {
    this.baseURL = init.baseURL;
    this.metricsURL = init.metricsURL;
    this.origin = init.origin;
    this.stateDir = init.stateDir;
    this.dockerLog = init.dockerLog;
    this.holdFile = init.holdFile;
    this.stacks = init.stacks;
    this.proc = init.proc;
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
      `command_timeout_seconds: 30\n` +
      // source_url points at a closed local port so auto-match icon fetches fail
      // fast and deterministically (connection refused → 404 → monogram), keeping
      // the whole UI suite offline. Repo icon.svg overrides still resolve.
      `icons:\n  cache_dir: ${JSON.stringify(join(base, 'icons'))}\n  source_url: "http://127.0.0.1:1"\n` +
      `stacks:\n` +
      stacks.map((n) => `  - name: ${JSON.stringify(n)}\n`).join('');
    const cfgPath = join(base, 'skipper.yml');
    writeFileSync(cfgPath, cfg);

    const proc = spawn(skipperBinPath(), ['-config', cfgPath], {
      env: {
        ...process.env,
        PATH: `${stubDir}:${process.env.PATH}`,
        DOCKER_LOG: dockerLog,
        STUB_DOCKER_HOLD_UP: holdFile,
        ...opts.stubEnv,
      },
      stdio: ['ignore', 'pipe', 'pipe'],
    });

    const s = new Skipper({ baseURL, metricsURL, origin, stateDir, dockerLog, holdFile, stacks, proc });
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

  /** stop terminates the process gracefully (SIGINT, then SIGKILL after 10s). */
  async stop(): Promise<void> {
    if (this.proc.exitCode !== null || this.proc.signalCode !== null) return;
    const exited = new Promise<void>((res) => this.proc.once('exit', () => res()));
    this.proc.kill('SIGINT');
    const timer = sleep(10_000).then(() => this.proc.kill('SIGKILL'));
    await Promise.race([exited, timer]);
  }
}
