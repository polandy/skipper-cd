// Unit layer for the pure UI helpers (app-helpers.js). Runs with the Node
// built-in test runner — `node --test` (or `make ui-unit`), no build step, no
// dependencies. Co-located with the source; it is neither embedded (the go:embed
// directive names app-helpers.js specifically) nor served (the handler serves
// only that file), so it never ships in the binary.
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const h = require('./app-helpers.js');

// hexHue returns a #rrggbb colour's HSL hue in degrees (or -1 for a grey).
function hexHue(hex) {
  const r = parseInt(hex.slice(1, 3), 16) / 255;
  const g = parseInt(hex.slice(3, 5), 16) / 255;
  const b = parseInt(hex.slice(5, 7), 16) / 255;
  const max = Math.max(r, g, b);
  const min = Math.min(r, g, b);
  const d = max - min;
  if (d === 0) return -1;
  let hue;
  if (max === r) hue = ((g - b) / d) % 6;
  else if (max === g) hue = (b - r) / d + 2;
  else hue = (r - g) / d + 4;
  hue *= 60;
  return hue < 0 ? hue + 360 : hue;
}

// hueDist is the shortest distance between two hues on the 360° wheel.
function hueDist(a, b) {
  const d = Math.abs(a - b) % 360;
  return Math.min(d, 360 - d);
}

test('formatDuration', () => {
  assert.equal(h.formatDuration(0), '—');
  assert.equal(h.formatDuration(-5), '—');
  assert.equal(h.formatDuration(undefined), '—');
  assert.equal(h.formatDuration(1), '0s'); // sub-second rounds down
  assert.equal(h.formatDuration(5000), '5s');
  assert.equal(h.formatDuration(59000), '59s');
  assert.equal(h.formatDuration(60000), '1m 0s');
  assert.equal(h.formatDuration(90500), '1m 30s');
  assert.equal(h.formatDuration(3661000), '61m 1s'); // no hour rollover, by design
});

test('formatTime relative boundaries', () => {
  const now = Date.now();
  assert.equal(h.formatTime(now), 'just now');
  assert.equal(h.formatTime(now - 3000), 'just now'); // < 5s
  assert.equal(h.formatTime(now - 10000), '10s ago');
  assert.equal(h.formatTime(now - 5 * 60000), '5m ago');
  assert.equal(h.formatTime(now - 3 * 3600000), '3h ago');
  assert.equal(h.formatTime(now - 2 * 86400000), '2d ago');
});

test('fullTime is a non-empty locale string', () => {
  assert.equal(typeof h.fullTime(Date.now()), 'string');
  assert.ok(h.fullTime(Date.now()).length > 0);
});

test('classifyDiffLine', () => {
  assert.equal(h.classifyDiffLine('+++ b/file'), 'diff-meta');
  assert.equal(h.classifyDiffLine('--- a/file'), 'diff-meta');
  assert.equal(h.classifyDiffLine('diff --git a b'), 'diff-meta');
  assert.equal(h.classifyDiffLine('index abc..def'), 'diff-meta');
  assert.equal(h.classifyDiffLine('@@ -1,2 +1,2 @@'), 'diff-hunk');
  assert.equal(h.classifyDiffLine('+added'), 'diff-add');
  assert.equal(h.classifyDiffLine('-removed'), 'diff-del');
  assert.equal(h.classifyDiffLine(' context'), '');
  // Precedence: the +++/--- meta check must win over the +/- add/del check.
  assert.equal(h.classifyDiffLine('+++ x'), 'diff-meta');
});

test('shortSHA', () => {
  assert.equal(h.shortSHA('0123456789abcdef'), '0123456');
  assert.equal(h.shortSHA('abc'), 'abc');
  assert.equal(h.shortSHA(''), '');
  assert.equal(h.shortSHA(null), '');
  assert.equal(h.shortSHA(undefined), '');
});

test('commitURL builds the forge commit page from the repo browse URL', () => {
  const base = 'https://forge.example.com/owner/repo';
  const sha = '0123456789abcdef';
  assert.equal(h.commitURL(base, sha), base + '/commit/' + sha);
  // The full SHA is linked even where the UI only prints the short form.
  assert.equal(h.commitURL(base, h.shortSHA(sha)), base + '/commit/0123456');
  assert.equal(h.commitURL(base + '/', sha), base + '/commit/' + sha);
  assert.equal(
    h.commitURL('http://forge.example.com:3000/o/r', sha),
    'http://forge.example.com:3000/o/r/commit/' + sha,
  );
});

test('commitURL yields no link without a usable base or sha', () => {
  const sha = '0123456789abcdef';
  assert.equal(h.commitURL('', sha), '');
  assert.equal(h.commitURL(null, sha), '');
  assert.equal(h.commitURL(undefined, sha), '');
  assert.equal(h.commitURL('https://forge.example.com/owner/repo', ''), '');
  assert.equal(h.commitURL('https://forge.example.com/owner/repo', null), '');
  // A non-http(s) base must never reach an href.
  assert.equal(h.commitURL('javascript:alert(1)', sha), '');
  assert.equal(h.commitURL('file:///srv/git/repo', sha), '');
  assert.equal(h.commitURL('/srv/git/repo', sha), '');
});

test('parseImageRef splits tag and digest, dropping registry/repo', () => {
  assert.deepEqual(h.parseImageRef('ghcr.io/acme/api:1.5.0'), { tag: '1.5.0', digest: '' });
  assert.deepEqual(h.parseImageRef('app@sha256:ab34cd90ef56'), { tag: '', digest: 'ab34cd90ef56' });
  assert.deepEqual(h.parseImageRef('app:1.25@sha256:ab34cd90ef56'), {
    tag: '1.25',
    digest: 'ab34cd90ef56',
  });
  // a ':' in a registry host:port is not a tag
  assert.deepEqual(h.parseImageRef('registry:5000/app'), { tag: '', digest: '' });
  assert.deepEqual(h.parseImageRef('registry:5000/app:2.0'), { tag: '2.0', digest: '' });
  assert.deepEqual(h.parseImageRef(''), { tag: '', digest: '' });
});

test('shortImageTag — shortest identifying token of one reference', () => {
  assert.equal(h.shortImageTag('ghcr.io/acme/api:1.5.0'), '1.5.0');
  assert.equal(h.shortImageTag('app@sha256:ab34cd90ef56'), 'ab34cd90');
  assert.equal(h.shortImageTag('busybox'), 'busybox');
  assert.equal(h.shortImageTag(''), '');
  assert.equal(h.shortImageTag(null), '');
});

test('imageDelta — shows the tokens that actually differ', () => {
  // plain tag bump
  assert.deepEqual(h.imageDelta('nginx:1.25', 'nginx:1.26'), {
    from: '1.25',
    to: '1.26',
    tag: '',
  });
  // tag bump on digest-pinned refs — the TAG is the meaningful change, not the digest
  assert.deepEqual(h.imageDelta('nginx:1.25@sha256:aaaa1111', 'nginx:1.26@sha256:bbbb2222'), {
    from: '1.25',
    to: '1.26',
    tag: '',
  });
  // same tag, only the digest moved (a rebuild) — show short digests, keep the
  // shared tag as context so it doesn't read as "1.25 → 1.25"
  assert.deepEqual(h.imageDelta('nginx:1.25@sha256:aaaa1111ff', 'nginx:1.25@sha256:bbbb2222ff'), {
    from: 'aaaa1111',
    to: 'bbbb2222',
    tag: '1.25',
  });
  // a moved floating tag, as the running-image identity reports it: same tag,
  // a bare (un-prefixed) short image id — the everyday `:latest` redeploy
  assert.deepEqual(h.imageDelta('nextcloud:latest@a1b2c3d4e5f6', 'nextcloud:latest@9f8e7d6c5b4a'), {
    from: 'a1b2c3d4',
    to: '9f8e7d6c',
    tag: 'latest',
  });
  // digest-only refs with no tag at all
  assert.deepEqual(h.imageDelta('nginx@sha256:aaaa1111ff', 'nginx@sha256:bbbb2222ff'), {
    from: 'aaaa1111',
    to: 'bbbb2222',
    tag: '',
  });
});

test('imageRepoName — bare repository name, registry and tag dropped', () => {
  assert.equal(h.imageRepoName('ghcr.io/immich-app/immich-server:v1.119.0'), 'immich-server');
  assert.equal(h.imageRepoName('nextcloud:30.0.2'), 'nextcloud');
  assert.equal(h.imageRepoName('localhost:5000/app:1.0'), 'app');
  assert.equal(h.imageRepoName('paperless-ngx@sha256:aaaa1111'), 'paperless-ngx');
  assert.equal(h.imageRepoName(''), '');
});

test('rosterVersion — leads with the service the stack is named after', () => {
  // Image repository mentions the stack, service names do not.
  assert.deepEqual(
    h.rosterVersion('nextcloud', [
      { name: 'app', image: 'nextcloud:30.0.2' },
      { name: 'db', image: 'postgres:16' },
      { name: 'redis', image: 'redis:7.2' },
    ]),
    { service: 'app', image: 'nextcloud:30.0.2', more: 2 },
  );
  // Two services mention it — the shorter name wins, not `compose ps` order
  // (which is alphabetical and would crown `database`).
  assert.deepEqual(
    h.rosterVersion('immich', [
      { name: 'database', image: 'ghcr.io/tensorchord/pgvecto-rs:pg16-v0.3.0' },
      {
        name: 'immich-machine-learning',
        image: 'ghcr.io/immich-app/immich-machine-learning:v1.119.0',
      },
      { name: 'immich-server', image: 'ghcr.io/immich-app/immich-server:v1.119.0' },
      { name: 'redis', image: 'redis:7.2' },
    ]),
    { service: 'immich-server', image: 'ghcr.io/immich-app/immich-server:v1.119.0', more: 3 },
  );
  // An exact service-name match wins over any mention.
  assert.deepEqual(
    h.rosterVersion('gitea', [
      { name: 'gitea-runner', image: 'gitea/act_runner:0.2.11' },
      { name: 'gitea', image: 'gitea/gitea:1.22.3' },
    ]),
    { service: 'gitea', image: 'gitea/gitea:1.22.3', more: 1 },
  );
  // A digest-pinned lead falls back to the short digest.
  assert.deepEqual(h.rosterVersion('app', [{ name: 'app', image: 'app@sha256:ab34cd90ef' }]), {
    service: 'app',
    image: 'app@sha256:ab34cd90ef',
    more: 0,
  });
});

test('rosterVersion — a single service is the lead whatever it is called', () => {
  assert.deepEqual(
    h.rosterVersion('monitoring', [{ name: 'grafana', image: 'grafana/grafana:11.3.0' }]),
    {
      service: 'grafana',
      image: 'grafana/grafana:11.3.0',
      more: 0,
    },
  );
});

test('rosterVersion — no lead when the stack name identifies none of the services', () => {
  // A role-named stack: picking one of three peers would be arbitrary, so the
  // row reports the count and the panel carries the versions.
  assert.deepEqual(
    h.rosterVersion('monitoring', [
      { name: 'prometheus', image: 'prom/prometheus:v3.0.0' },
      { name: 'grafana', image: 'grafana/grafana:11.3.0' },
      { name: 'loki', image: 'grafana/loki:3.2.1' },
    ]),
    { service: '', image: '', more: 3 },
  );
});

test('rosterVersion — null when no service reports an image', () => {
  assert.equal(h.rosterVersion('gitea', []), null);
  assert.equal(h.rosterVersion('gitea', null), null);
  // A snapshot from a skipper too old to carry images.
  assert.equal(h.rosterVersion('gitea', [{ name: 'server', state: 'running' }]), null);
});

test('statusText flattens the stacked statuses', () => {
  assert.equal(h.statusText('rolled_back'), 'rolled back');
  assert.equal(h.statusText('rolled_back_unhealthy'), 'rolled back · unhealthy');
  assert.equal(h.statusText('heal_exhausted'), 'self-heal · failed');
  assert.equal(h.statusText('success'), 'success');
  assert.equal(h.statusText(''), '');
  assert.equal(h.statusText(undefined), '');
});

test('auditStatusLabel', () => {
  assert.equal(h.auditStatusLabel('rolled_back'), 'rolled back');
  assert.equal(h.auditStatusLabel('rolled_back_unhealthy'), 'rolled back · unhealthy');
  assert.equal(h.auditStatusLabel('heal_exhausted'), 'self-heal failed'); // no middot, unlike statusText
  assert.equal(h.auditStatusLabel('success'), 'success');
});

test('statusIcon maps each terminal status to a distinct leading glyph', () => {
  // Every terminal status a badge can carry gets an inline <svg> in the shared
  // .badge-ico slot (T3.14). deploying keeps its spinner, so it — and any
  // unknown/label-only status — returns '' (no icon markup).
  const iconStatuses = [
    'success',
    'failed',
    'rolled_back',
    'rolled_back_unhealthy',
    'healed',
    'heal_exhausted',
    'queued',
    'blocked',
  ];
  for (const s of iconStatuses) {
    const svg = h.statusIcon(s);
    assert.match(svg, /^<svg class="badge-ico"/, `${s} should render a badge-ico svg`);
    assert.match(svg, /aria-hidden="true"/, `${s} icon is decorative`);
    assert.match(svg, /currentColor/, `${s} icon inherits the badge colour`);
  }

  // deploying (spinner) and unrecognised statuses are label-only.
  assert.equal(h.statusIcon('deploying'), '');
  assert.equal(h.statusIcon('skipped'), '');
  assert.equal(h.statusIcon(''), '');
  assert.equal(h.statusIcon(undefined), '');

  // Icons are distinct where the states differ; the two worst states share the
  // same warning glyph by design.
  assert.notEqual(h.statusIcon('success'), h.statusIcon('failed'));
  assert.notEqual(h.statusIcon('queued'), h.statusIcon('blocked'));
  assert.notEqual(h.statusIcon('healed'), h.statusIcon('rolled_back'));
  assert.equal(h.statusIcon('rolled_back_unhealthy'), h.statusIcon('heal_exhausted'));
});

test('phaseDuration coarse units', () => {
  assert.equal(h.phaseDuration(0), '0s');
  assert.equal(h.phaseDuration(500), '0s'); // < 1s
  assert.equal(h.phaseDuration(5000), '5s');
  assert.equal(h.phaseDuration(5 * 60000), '5m');
  assert.equal(h.phaseDuration(6 * 3600000 + 12 * 60000), '6h12m');
  assert.equal(h.phaseDuration(3 * 3600000), '3h'); // exact hour, no trailing minutes
  assert.equal(h.phaseDuration(3 * 86400000 + 4 * 3600000), '3d4h');
  assert.equal(h.phaseDuration(2 * 86400000), '2d'); // exact day, no trailing hours
});

test('healthClass maps state/health to the rollup vocabulary', () => {
  assert.equal(h.healthClass({ health: 'unhealthy' }), 'unhealthy');
  assert.equal(h.healthClass({ state: 'restarting' }), 'unhealthy');
  assert.equal(h.healthClass({ state: 'dead' }), 'unhealthy');
  assert.equal(h.healthClass({ health: 'starting' }), 'starting');
  assert.equal(h.healthClass({ state: 'created' }), 'starting');
  assert.equal(h.healthClass({ health: 'healthy' }), 'healthy');
  assert.equal(h.healthClass({ state: 'running' }), 'healthy');
  assert.equal(h.healthClass({ state: 'exited' }), 'stopped');
  assert.equal(h.healthClass({}), 'stopped');
  // unhealthy takes precedence over a running state.
  assert.equal(h.healthClass({ health: 'unhealthy', state: 'running' }), 'unhealthy');
});

test('HEALTH exposes the per-stack rollup status vocabulary', () => {
  assert.equal(h.HEALTH.HEALTHY, 'healthy');
  assert.equal(h.HEALTH.UNHEALTHY, 'unhealthy');
  assert.equal(h.HEALTH.STARTING, 'starting');
  assert.equal(h.HEALTH.STOPPED, 'stopped');
  assert.equal(h.HEALTH.UNKNOWN, 'unknown');
});

test('attentionStacks returns only unhealthy stacks, sorted, as {stack,status}', () => {
  assert.deepEqual(h.attentionStacks({}), []);
  assert.deepEqual(h.attentionStacks(null), []);
  assert.deepEqual(h.attentionStacks(undefined), []);

  // Only 'unhealthy' qualifies — starting/stopped/healthy/unknown are the
  // reporting-only states the beacon and band deliberately stay quiet about.
  const snap = {
    grafana: { status: 'healthy' },
    mealie: { status: 'unhealthy' },
    gitea: { status: 'starting' },
    loki: { status: 'stopped' },
    paperless: { status: 'unknown' },
    immich: { status: 'unhealthy' },
  };
  assert.deepEqual(h.attentionStacks(snap), [
    { stack: 'immich', status: 'unhealthy' },
    { stack: 'mealie', status: 'unhealthy' },
  ]);

  // A missing, null, or empty status is not "unhealthy".
  assert.deepEqual(h.attentionStacks({ x: {}, y: null, z: { status: '' } }), []);
});

test('levelClass clamps unknown levels to INFO', () => {
  for (const lvl of ['DEBUG', 'INFO', 'WARN', 'ERROR']) assert.equal(h.levelClass(lvl), lvl);
  assert.equal(h.levelClass('TRACE'), 'INFO');
  assert.equal(h.levelClass(''), 'INFO');
  assert.equal(h.levelClass(undefined), 'INFO');
});

test('logTime: same-day is time-only, other-day carries a date prefix', () => {
  const now = new Date();
  const sameDay = h.logTime(now.getTime());
  const otherDay = h.logTime(now.getTime() - 3 * 86400000);
  assert.equal(typeof sameDay, 'string');
  // The other-day form is strictly longer — it prepends the date to the time.
  assert.ok(otherDay.length > sameDay.length, `expected date prefix: ${otherDay}`);
});

test('reasonFromSnap', () => {
  assert.equal(h.reasonFromSnap({ effective: true }), '');
  assert.equal(h.reasonFromSnap({ effective: false, overridden: true }), 'stack');
  assert.equal(h.reasonFromSnap({ effective: false, config: false }), 'stack');
  assert.equal(h.reasonFromSnap({ effective: false }), 'global');
});

test('snapshotIsFresh drops a payload older than the one already applied', () => {
  // Nothing applied yet: anything is fresh, including version 0.
  assert.equal(h.snapshotIsFresh(null, { version: 0 }), true);
  assert.equal(h.snapshotIsFresh(null, { version: 7 }), true);

  assert.equal(h.snapshotIsFresh(3, { version: 4 }), true);
  assert.equal(h.snapshotIsFresh(3, { version: 3 }), true); // a republish of the same state
  assert.equal(h.snapshotIsFresh(3, { version: 2 }), false); // the race this guards
  assert.equal(h.snapshotIsFresh(3, { version: 0 }), false);
});

test('snapshotIsFresh accepts an unversioned payload rather than freezing', () => {
  assert.equal(h.snapshotIsFresh(3, {}), true);
  assert.equal(h.snapshotIsFresh(3, { version: null }), true);
  assert.equal(h.snapshotIsFresh(3, null), true);
});

test('orphanMeta: state-only vs container count with pluralization', () => {
  assert.equal(h.orphanMeta({ state_only: true }), 'state only');
  assert.equal(h.orphanMeta({ containers: [{ name: 'a' }] }), '1 container');
  assert.equal(
    h.orphanMeta({ containers: [{ name: 'a' }, { name: 'b' }, { name: 'c' }] }),
    '3 containers',
  );
  assert.equal(h.orphanMeta({}), '0 containers');
});

test('orphanStateClass maps docker state to the dot vocabulary', () => {
  assert.equal(h.orphanStateClass('running'), 'healthy');
  assert.equal(h.orphanStateClass('restarting'), 'unhealthy');
  assert.equal(h.orphanStateClass('dead'), 'unhealthy');
  assert.equal(h.orphanStateClass('exited'), 'stopped');
  assert.equal(h.orphanStateClass('created'), 'stopped');
});

test('containerMatchesQuery scans a container across its fields', () => {
  const c = {
    name: 'legacy-redis-1',
    service: 'redis',
    image: 'redis:7',
    ports: '6379',
    status: 'Up 5 days',
  };
  assert.equal(h.containerMatchesQuery(c, 'redis'), true);
  assert.equal(h.containerMatchesQuery(c, '6379'), true);
  assert.equal(h.containerMatchesQuery(c, 'up 5'), true);
  assert.equal(h.containerMatchesQuery(c, 'nginx'), false);
  assert.equal(h.containerMatchesQuery(c, ''), false); // inactive search never matches
});

test('orphanMatchesQuery scans project fields and containers', () => {
  const o = {
    project: 'legacy-cache',
    working_dir: '/repo/stacks/legacy-cache',
    config_file: '/repo/stacks/legacy-cache/docker-compose.yml',
    volumes: ['legacy_data'],
    containers: [{ name: 'legacy-redis-1', image: 'redis:7' }],
  };
  assert.equal(h.orphanMatchesQuery(o, 'legacy'), true); // project name
  assert.equal(h.orphanMatchesQuery(o, 'legacy_data'), true); // volume
  assert.equal(h.orphanMatchesQuery(o, 'redis'), true); // container
  assert.equal(h.orphanMatchesQuery(o, 'nginx'), false);
  assert.equal(h.orphanMatchesQuery(o, ''), false);
});

test('logLineVisible: empty query shows all, else case-insensitive contains', () => {
  assert.equal(h.logLineVisible('GET /api/health 200', ''), true); // no filter
  assert.equal(h.logLineVisible('GET /api/health 200', 'api'), true);
  assert.equal(h.logLineVisible('GET /api/health 200', 'API'), true); // case-insensitive
  assert.equal(h.logLineVisible('GET /api/health 200', 'wget'), false);
  assert.equal(h.logLineVisible('', 'x'), false);
});

test('hostColorIndex: deterministic function of the name, in palette range', () => {
  // Same name → same slot every time, independent of any surrounding host set.
  assert.equal(h.hostColorIndex('host-b'), h.hostColorIndex('host-b'));
  // In-range for the palette.
  for (const n of ['host-a', 'host-b', 'host-c', 'nuc', 'argoneon', '']) {
    const i = h.hostColorIndex(n);
    assert.ok(Number.isInteger(i) && i >= 0 && i < h.HOST_COLOR_COUNT, n + ' -> ' + i);
  }
});

test('hostColorIndex: independent of order/count (name-based, not positional)', () => {
  // Whatever slot host-c hashes to, it is the same whether it is the only host
  // or one of many — the colour follows the name, not its position.
  const solo = h.hostColorIndex('host-c');
  assert.equal(h.hostColorIndex('host-c'), solo);
});

test('hostMonogram: initials of a separated name, else first three letters', () => {
  assert.equal(h.hostMonogram('nuc'), 'NUC');
  assert.equal(h.hostMonogram('argoneon'), 'ARG');
  assert.equal(h.hostMonogram('host-a'), 'HA');
  assert.equal(h.hostMonogram('host-b'), 'HB');
  assert.equal(h.hostMonogram('web_server_1'), 'WS1'); // up to three segments
  assert.equal(h.hostMonogram('a'), 'A');
  assert.equal(h.hostMonogram(''), '');
  assert.equal(h.hostMonogram(undefined), '');
});

test('assignHostColors: never reuses a colour while the palette has free slots', () => {
  // Search for a name set that would collide on the raw hash, to prove the
  // set-aware assigner still hands each host a distinct slot.
  const names = [];
  for (let i = 0; i < h.HOST_COLOR_COUNT; i++) names.push('host-' + i);
  const colors = h.assignHostColors(names);
  const slots = names.map((n) => colors[n]);
  assert.equal(
    new Set(slots).size,
    names.length,
    'all ' + names.length + ' hosts distinct: ' + slots.join(','),
  );
  for (const s of slots) assert.ok(s >= 0 && s < h.HOST_COLOR_COUNT);
});

test('assignHostColors: a colliding pair is separated onto different slots', () => {
  // Two names that hash to the same raw slot must still get different colours.
  let a, b;
  outer: for (const x of ['a', 'b', 'c', 'd', 'e', 'f', 'g', 'h', 'i', 'j', 'k']) {
    for (const y of ['l', 'm', 'n', 'o', 'p', 'q', 'r', 's', 't', 'u', 'v']) {
      if (h.hostColorIndex(x) === h.hostColorIndex(y)) {
        a = x;
        b = y;
        break outer;
      }
    }
  }
  assert.ok(a && b, 'found a colliding pair to test');
  const colors = h.assignHostColors([a, b]);
  assert.notEqual(colors[a], colors[b]);
});

test('assignHostColors: independent of input order and of which host is primary', () => {
  const c1 = h.assignHostColors(['host-a', 'host-b', 'host-c']);
  const c2 = h.assignHostColors(['host-c', 'host-a', 'host-b']);
  assert.deepEqual(c1, c2);
});

test('host palette: adjacent colour slots stay visually distinct in every theme', () => {
  // assignHostColors hands out numerically adjacent slots first (collision
  // probing steps +1), so two hosts most often land on consecutive slots. On a
  // monotonic hue ramp those are the two closest colours and read as the same;
  // the slot order in app.css is interleaved to keep every adjacent pair far
  // apart. Guard that invariant against a future re-monotonising edit.
  const css = fs.readFileSync(path.join(__dirname, 'app.css'), 'utf8');
  const re =
    /--host-0:(#[0-9a-f]{6}); --host-1:(#[0-9a-f]{6}); --host-2:(#[0-9a-f]{6}); --host-3:(#[0-9a-f]{6}); --host-4:(#[0-9a-f]{6}); --host-5:(#[0-9a-f]{6});/g;
  const blocks = [...css.matchAll(re)];
  // 5 themes × dark + light = 10 palettes (the fallback :root shares the
  // catppuccin-dark line).
  assert.ok(blocks.length >= 10, `expected >= 10 host palettes in app.css, found ${blocks.length}`);
  const MIN_DEG = 38;
  for (const m of blocks) {
    const hexes = m.slice(1, 7);
    const hues = hexes.map(hexHue);
    for (let i = 0; i < 5; i++) {
      const dist = hueDist(hues[i], hues[i + 1]);
      assert.ok(
        dist >= MIN_DEG,
        `adjacent slots ${i}/${i + 1} only ${dist.toFixed(0)}° apart (${hexes[i]} vs ${hexes[i + 1]})`,
      );
    }
    assert.equal(new Set(hexes).size, 6, `six distinct host colours, got ${hexes.join(',')}`);
  }
});

test('assignHostColors: beyond the palette size colours may repeat', () => {
  const names = [];
  for (let i = 0; i < h.HOST_COLOR_COUNT + 3; i++) names.push('overflow-host-' + i);
  const colors = h.assignHostColors(names);
  // Every host is assigned an in-range slot; distinctness is no longer possible.
  const slots = names.map((n) => colors[n]);
  assert.equal(slots.length, names.length);
  for (const s of slots) assert.ok(s >= 0 && s < h.HOST_COLOR_COUNT);
  assert.ok(new Set(slots).size <= h.HOST_COLOR_COUNT);
});

test('hostFilterActive: only a strict non-empty subset lights the control', () => {
  assert.equal(h.hostFilterActive(3, 3), false); // all selected
  assert.equal(h.hostFilterActive(2, 3), true); // subset
  assert.equal(h.hostFilterActive(0, 3), false); // none (guard, treated inactive)
  assert.equal(h.hostFilterActive(1, 1), false); // single host, all selected
});

test('reconcileHostFilter: restores a saved subset against the current host set', () => {
  assert.equal(h.reconcileHostFilter(null, ['host-a', 'host-b']), null); // nothing saved
  assert.equal(h.reconcileHostFilter([], ['host-a', 'host-b']), null); // nothing saved
  assert.deepEqual(h.reconcileHostFilter(['host-b'], ['host-a', 'host-b', 'host-c']), ['host-b']); // subset restored
  assert.equal(h.reconcileHostFilter(['host-x'], ['host-a', 'host-b']), null); // saved host gone → fall back to all
  assert.deepEqual(h.reconcileHostFilter(['host-b', 'host-x'], ['host-a', 'host-b']), ['host-b']); // stale name pruned
  assert.equal(h.reconcileHostFilter(['host-a', 'host-b'], ['host-a', 'host-b']), null); // full set → normalize to all
});

test('clogStreamStatus: a closed stream never promises a retry it will not make', () => {
  // 0 CONNECTING / 1 OPEN — EventSource retries these itself.
  assert.equal(h.clogStreamStatus(0).text, 'reconnecting…');
  assert.equal(h.clogStreamStatus(1).text, 'reconnecting…');
  // 2 CLOSED — a non-2xx (404 stack gone, 429 stream cap) ends the stream.
  assert.match(h.clogStreamStatus(2).text, /closed/);
  assert.match(h.clogStreamStatus(2).text, /retry/);
  assert.equal(h.clogStreamStatus(2).cls, 'err');
  // `closed` drives the live/pause pill too, so the header cannot keep saying
  // "live" while the footer says the stream is gone.
  assert.equal(h.clogStreamStatus(2).closed, true);
  assert.equal(h.clogStreamStatus(0).closed, false);
  assert.equal(h.clogStreamStatus(1).closed, false);
});

test('watchedSummary: only a settled stack claims nothing changed', () => {
  // A clean last deploy: the commit is the answer to "why is nothing happening".
  assert.match(h.watchedSummary('success', 'a1b2c3d4e5', 3), /^Unchanged since a1b2c3d\./);
  assert.match(h.watchedSummary('healed', 'a1b2c3d4e5', 3), /^Unchanged since a1b2c3d\./);
  // A failed/queued/blocked stack has a change pending — the opposite claim.
  for (const s of ['failed', 'rolled_back', 'queued', 'blocked', 'heal_exhausted']) {
    assert.doesNotMatch(h.watchedSummary(s, 'a1b2c3d4e5', 3), /Unchanged/);
  }
  // A first deploy has no prior commit to diff against, so the audit record
  // carries none — the fact still holds, the reference point is just the
  // deploy itself.
  assert.match(h.watchedSummary('success', '', 3), /^Unchanged since the last deploy\./);
  // Never deployed: no tracked inputs at all.
  assert.match(h.watchedSummary('', '', 0), /has not deployed/);
  // Singular vs plural.
  assert.match(h.watchedSummary('', '', 1), /this file changes/);
  assert.match(h.watchedSummary('', '', 2), /any of these change/);
  // A parked stack is not watched at all — whatever it recorded before.
  assert.match(h.watchedSummary('success', 'a1b2c3d4e5', 3, true), /^Parked/);
});

test('deployAnnouncement: only terminal outcomes get a spoken phrase (T2.8)', () => {
  assert.equal(h.deployAnnouncement('success', 'gitea'), 'gitea deployed successfully');
  assert.equal(h.deployAnnouncement('failed', 'gitea'), 'gitea deploy failed');
  assert.equal(h.deployAnnouncement('rolled_back', 'gitea'), 'gitea deploy failed, rolled back');
  assert.equal(
    h.deployAnnouncement('rolled_back_unhealthy', 'gitea'),
    'gitea rolled back, still unhealthy',
  );
  assert.equal(h.deployAnnouncement('healed', 'gitea'), 'gitea self-healed');
  assert.equal(h.deployAnnouncement('heal_exhausted', 'gitea'), 'gitea self-heal failed');
  // Non-terminal statuses are never announced.
  for (const s of ['deploying', 'queued', 'blocked', 'skipped', '', undefined]) {
    assert.equal(h.deployAnnouncement(s, 'gitea'), null);
  }
  assert.equal(h.deployAnnouncement('success', ''), null); // no stack → nothing to say
});

test('waitedSince renders one coarse unit per magnitude', () => {
  const now = Date.parse('2026-07-28T12:00:00Z');
  const ago = (ms) => new Date(now - ms).toISOString();
  assert.equal(h.waitedSince(ago(0), now), '0s');
  assert.equal(h.waitedSince(ago(45000), now), '45s');
  assert.equal(h.waitedSince(ago(59999), now), '59s');
  assert.equal(h.waitedSince(ago(60000), now), '1m');
  assert.equal(h.waitedSince(ago(90 * 60000), now), '1h'); // no compound "1h30m"
  assert.equal(h.waitedSince(ago(3 * 86400000), now), '3d');
});

test('deployStatusLabel spells out what the header trail shows as chips', () => {
  assert.equal(h.deployStatusLabel([], []), 'idle');
  assert.equal(h.deployStatusLabel([], ['db']), 'idle');
  assert.equal(h.deployStatusLabel(['web'], []), 'deploying web');
  assert.equal(h.deployStatusLabel(['web'], ['db', 'cache']), 'deploying web · next db, cache');
});

test('attentionLabel pluralises the unhealthy-stack count', () => {
  assert.equal(h.attentionLabel(0), '0 stacks unhealthy');
  assert.equal(h.attentionLabel(1), '1 stack unhealthy');
  assert.equal(h.attentionLabel(2), '2 stacks unhealthy');
});

test('rowClass tints a row per deploy status', () => {
  for (const s of [
    'deploying',
    'failed',
    'success',
    'rolled_back',
    'rolled_back_unhealthy',
    'healed',
    'heal_exhausted',
    'queued',
    'blocked',
  ]) {
    assert.equal(h.rowClass(s, true), 'event-row ' + s + '-row');
  }
});

test('rowClass leaves an unknown status untinted', () => {
  // No stylesheet defines a class for it, so inventing one would be a silent
  // no-op that reads as styled.
  assert.equal(h.rowClass('pending_review', true), 'event-row');
  assert.equal(h.rowClass('', true), 'event-row');
  assert.equal(h.rowClass(undefined, true), 'event-row');
});

test('rowClass marks live arrivals as new but replayed history not', () => {
  assert.equal(h.rowClass('success', false), 'event-row success-row new-row');
  assert.equal(h.rowClass('success', true), 'event-row success-row');
});

test('auditCountText pluralises the history count and hides a zero', () => {
  assert.equal(h.auditCountText(0), '');
  assert.equal(h.auditCountText(1), '1 deploy');
  assert.equal(h.auditCountText(2), '2 deploys');
});

test('logLineLevel tags child-process output as cmd', () => {
  const cmd = { level: 'info', attrs: { cmd: 'docker', stream: 'stdout' } };
  assert.equal(h.logLineLevel(cmd), 'cmd');
  // Both attrs are required — a level line that merely names a command is not
  // process output and keeps its own level.
  assert.equal(h.logLineLevel({ level: 'info', attrs: { cmd: 'docker' } }), 'info');
  assert.equal(h.logLineLevel({ level: 'error', attrs: { stream: 'stderr' } }), 'error');
  assert.equal(h.logLineLevel({ level: 'warn' }), 'warn');
});

test('phaseSince shows only the time for today, and adds the date otherwise', () => {
  // Fixed clock: the today-or-not branch is a date-boundary comparison, so a
  // self-read clock would make this flake once a day.
  const now = new Date('2026-07-29T18:00:00Z').getTime();
  const sameDay = new Date('2026-07-29T09:30:00Z').getTime();
  const otherDay = new Date('2026-07-20T09:30:00Z').getTime();
  // Asserted against the platform's own formatting rather than a literal, so
  // the test does not depend on the runner's locale or timezone.
  const time = new Date(sameDay).toLocaleTimeString([], {
    hour: '2-digit',
    minute: '2-digit',
  });
  assert.equal(h.phaseSince(sameDay, now), time);
  const older = h.phaseSince(otherDay, now);
  assert.ok(older.endsWith(time), `expected a date before ${time}, got ${older}`);
  assert.ok(older.length > time.length, `expected a date prefix, got ${older}`);
});

test('phaseSince treats a later time on the same local day as today', () => {
  // "Today" is the local calendar day, not "within 24 hours" — an hour earlier
  // but across midnight must still take the dated form.
  const now = new Date('2026-07-29T00:30:00').getTime();
  const justBeforeMidnight = new Date('2026-07-28T23:30:00').getTime();
  const earlierToday = new Date('2026-07-29T00:05:00').getTime();
  assert.notEqual(
    h.phaseSince(justBeforeMidnight, now).length,
    h.phaseSince(earlierToday, now).length,
  );
});

test('watchedSummary opens its settled lead with UNCHANGED_SINCE', () => {
  // The constant is load-bearing: watchedLeadHTML locates the commit token by
  // this exact prefix to turn it into a link, so a reworded lead that no longer
  // starts with it would silently stop linking.
  for (const status of ['success', 'healed']) {
    const settled = h.watchedSummary(status, 'abc1234def', 2, false);
    assert.ok(
      settled.startsWith(h.UNCHANGED_SINCE),
      `lead must open with ${JSON.stringify(h.UNCHANGED_SINCE)}, got ${settled}`,
    );
    assert.match(settled, new RegExp(h.UNCHANGED_SINCE + 'abc1234'));
  }
  // A first deploy has no prior commit; the prefix must still open the lead, or
  // the linker would look for a token that is not there.
  assert.ok(h.watchedSummary('success', '', 2, false).startsWith(h.UNCHANGED_SINCE));
  // An unsettled outcome makes no such claim, so the prefix must be absent.
  assert.ok(!h.watchedSummary('failed', 'abc1234def', 2, false).includes(h.UNCHANGED_SINCE));
});

// A fake timer: records what was scheduled and at which delay, and lets the test
// fire it — so the backoff is asserted directly instead of waited out.
function fakeTimer() {
  const armed = [];
  const cleared = [];
  const setTimer = (fn, delay) => {
    armed.push({ fn, delay });
    return armed.length; // a truthy handle, like the browser's
  };
  const clearTimer = (handle) => cleared.push(handle);
  return {
    setTimer,
    clearTimer,
    delays: () => armed.map((a) => a.delay),
    fireLast: () => armed[armed.length - 1].fn(),
    count: () => armed.length,
    cleared: () => cleared.slice(),
  };
}

test('makeReconnector doubles the delay up to the cap', () => {
  const t = fakeTimer();
  const r = h.makeReconnector(() => {}, t.setTimer, t.clearTimer);
  for (let i = 0; i < 8; i++) {
    r.schedule();
    t.fireLast(); // clear the pending retry so the next schedule() arms
  }
  assert.deepEqual(t.delays(), [1000, 2000, 4000, 8000, 16000, 30000, 30000, 30000]);
  assert.equal(t.delays()[0], h.RECONNECT_BASE_DELAY_MS);
  assert.equal(t.delays()[t.delays().length - 1], h.RECONNECT_MAX_DELAY_MS);
});

test('makeReconnector ignores a second schedule while one is pending', () => {
  const t = fakeTimer();
  const r = h.makeReconnector(() => {}, t.setTimer, t.clearTimer);
  r.schedule();
  r.schedule();
  r.schedule();
  // Without the guard a burst of stream errors would arm a retry each time.
  assert.equal(t.count(), 1);
  t.fireLast();
  r.schedule();
  assert.equal(t.count(), 2);
});

test('makeReconnector runs the connect callback when the timer fires', () => {
  const t = fakeTimer();
  let connects = 0;
  const r = h.makeReconnector(() => connects++, t.setTimer, t.clearTimer);
  r.schedule();
  assert.equal(connects, 0, 'must not connect before the delay elapses');
  t.fireLast();
  assert.equal(connects, 1);
});

test('makeReconnector reset returns the backoff to the base delay', () => {
  const t = fakeTimer();
  const r = h.makeReconnector(() => {}, t.setTimer, t.clearTimer);
  for (let i = 0; i < 3; i++) {
    r.schedule();
    t.fireLast();
  }
  r.reset(); // what a good connection does
  r.schedule();
  assert.equal(t.delays()[t.delays().length - 1], h.RECONNECT_BASE_DELAY_MS);
});

test('makeReconnector instances keep separate backoff state', () => {
  // The events stream and the log stream each own one; one stream backing off
  // must not slow the other's first retry.
  const t = fakeTimer();
  const a = h.makeReconnector(() => {}, t.setTimer, t.clearTimer);
  const b = h.makeReconnector(() => {}, t.setTimer, t.clearTimer);
  for (let i = 0; i < 3; i++) {
    a.schedule();
    t.fireLast();
  }
  b.schedule();
  assert.equal(t.delays()[t.delays().length - 1], h.RECONNECT_BASE_DELAY_MS);
});

// resume() is what a page returning to the foreground calls. A suspended tab
// (a backgrounded PWA, a locked phone) has its stream torn down and its pending
// retry timer dropped or throttled by the OS, so waiting for that timer can
// mean waiting forever — the reconnect has to be driven by the wake-up itself.
test('makeReconnector resume reconnects at once when the stream is not open', () => {
  const t = fakeTimer();
  let connects = 0;
  const r = h.makeReconnector(() => connects++, t.setTimer, t.clearTimer);
  r.resume(false); // came back to a stream that did not survive
  assert.equal(connects, 1, 'must connect on the wake-up, not on a timer');
  assert.equal(t.count(), 0, 'no retry armed — the connection attempt is immediate');
});

test('makeReconnector resume leaves an already-open stream alone', () => {
  const t = fakeTimer();
  let connects = 0;
  const r = h.makeReconnector(() => connects++, t.setTimer, t.clearTimer);
  r.resume(true); // a brief tab switch: the stream stayed open
  assert.equal(connects, 0, 'reconnecting a live stream would drop its events');
});

test('makeReconnector resume clears the pending retry so the wake-up connects once', () => {
  const t = fakeTimer();
  let connects = 0;
  const r = h.makeReconnector(() => connects++, t.setTimer, t.clearTimer);
  r.schedule(); // the stream died while hidden and armed a retry
  const pending = t.count();
  r.resume(false);
  assert.equal(connects, 1);
  // The armed timer must be cancelled, not merely forgotten: left running it
  // would fire after the resume and open a second, duplicate stream.
  assert.deepEqual(t.cleared(), [pending]);
});

test('makeReconnector resume returns the backoff to the base delay', () => {
  const t = fakeTimer();
  const r = h.makeReconnector(() => {}, t.setTimer, t.clearTimer);
  for (let i = 0; i < 4; i++) {
    r.schedule(); // back off to 8s while the tab was hidden and the server unreachable
    t.fireLast();
  }
  r.resume(false);
  r.schedule(); // the resumed attempt failed too — retry promptly, not at the old delay
  assert.equal(t.delays()[t.delays().length - 1], h.RECONNECT_BASE_DELAY_MS);
});

// A stream the browser is still retrying by itself never reaches schedule(),
// so the outage is counted from the failures themselves — otherwise the case
// that prompted this (a page that cannot reach the server at all) would never
// register as offline.
test('makeReconnector reports offline only once attempts keep failing', () => {
  const t = fakeTimer();
  const r = h.makeReconnector(() => {}, t.setTimer, t.clearTimer);
  assert.equal(r.isOffline(), false, 'a page that has not failed yet is not offline');
  for (let i = 1; i < h.OFFLINE_AFTER_FAILURES; i++) {
    r.failed();
    assert.equal(r.isOffline(), false, 'one blip must not claim the server is unreachable');
  }
  r.failed();
  assert.equal(r.isOffline(), true);
});

test('makeReconnector clears the offline state on a good connection', () => {
  const t = fakeTimer();
  const r = h.makeReconnector(() => {}, t.setTimer, t.clearTimer);
  for (let i = 0; i < h.OFFLINE_AFTER_FAILURES + 2; i++) r.failed();
  r.reset(); // what onopen calls
  assert.equal(r.isOffline(), false);
});

test('makeReconnector resume keeps the offline state until a connection succeeds', () => {
  const t = fakeTimer();
  const r = h.makeReconnector(() => {}, t.setTimer, t.clearTimer);
  for (let i = 0; i < h.OFFLINE_AFTER_FAILURES; i++) r.failed();
  r.resume(false); // woke up, trying again — but nothing has answered yet
  assert.equal(r.isOffline(), true, 'only a connection that works may retract "offline"');
});

test('rosterAttentionRank floats only an enabled, unhealthy stack', () => {
  const health = {
    bad: { status: h.HEALTH.UNHEALTHY },
    parked: { status: h.HEALTH.UNHEALTHY },
    ok: { status: h.HEALTH.HEALTHY },
    warming: { status: h.HEALTH.STARTING },
    idle: { status: h.HEALTH.STOPPED },
  };
  assert.equal(h.rosterAttentionRank({ name: 'bad' }, health), 0);
  // A disabled stack never floats — its unhealth is parked on purpose.
  assert.equal(h.rosterAttentionRank({ name: 'parked', disabled: true }, health), 1);
  // The reporting-only states stay put (same rule as attentionStacks).
  assert.equal(h.rosterAttentionRank({ name: 'ok' }, health), 1);
  assert.equal(h.rosterAttentionRank({ name: 'warming' }, health), 1);
  assert.equal(h.rosterAttentionRank({ name: 'idle' }, health), 1);
  // No live-health entry at all (poller off, or a never-deployed stack).
  assert.equal(h.rosterAttentionRank({ name: 'ghost' }, health), 1);
  assert.equal(h.rosterAttentionRank({ name: 'ghost' }, undefined), 1);
});

test('rosterOrdered floats unhealthy stacks, preserving backend order within each group', () => {
  const snap = [{ name: 'a' }, { name: 'b' }, { name: 'c' }, { name: 'd' }, { name: 'e' }];
  const health = {
    b: { status: h.HEALTH.UNHEALTHY },
    d: { status: h.HEALTH.UNHEALTHY },
    a: { status: h.HEALTH.HEALTHY },
  };
  const names = (list) =>
    list.map((e) => {
      return e.name;
    });
  // Stability is the contract: b before d (unhealthy, backend order kept), and
  // a/c/e in backend order after them — never re-alphabetised.
  assert.deepEqual(names(h.rosterOrdered(snap, health)), ['b', 'd', 'a', 'c', 'e']);
  // A sorted copy: the snapshot itself must stay in backend order.
  assert.deepEqual(names(snap), ['a', 'b', 'c', 'd', 'e']);
  // Nothing unhealthy → backend order verbatim.
  assert.deepEqual(names(h.rosterOrdered(snap, {})), ['a', 'b', 'c', 'd', 'e']);
  assert.deepEqual(h.rosterOrdered(undefined, health), []);
});

// A peers snapshot as applyPeers stores it: one fully-populated peer, and one
// older peer whose fanned-in state predates every per-stack section.
function peersFixture() {
  return {
    self: 'primary',
    peers: [
      {
        name: 'host-b',
        url: 'https://host-b:8001',
        reachable: true,
        stale: false,
        state: {
          health: { stacks: { web: { status: 'healthy' } } },
          healthwatch: { stacks: { web: { app: [] } } },
          app_links: { stacks: { web: ['web.example.com'] } },
          stacks: { repo_web_url: 'https://forge-b.example.com/o/r' },
        },
      },
      { name: 'old-peer', url: 'https://old:8001', reachable: false, stale: true, state: {} },
    ],
  };
}

test('resolvePeerView finds the fanned-in peer, never self', () => {
  const peers = peersFixture();
  assert.equal(h.resolvePeerView(peers, 'primary', 'host-b').name, 'host-b');
  // The primary itself never resolves to a peer view — its rows read the live
  // local snapshots, not the fan-in.
  assert.equal(h.resolvePeerView(peers, 'primary', 'primary'), null);
  assert.equal(h.resolvePeerView(null, 'primary', 'host-b'), null); // single-host
  assert.equal(h.resolvePeerView(peers, 'primary', ''), null);
  assert.equal(h.resolvePeerView(peers, 'primary', 'ghost'), null);
  // A snapshot without a peers list must not throw.
  assert.equal(h.resolvePeerView({ self: 'primary' }, 'primary', 'host-b'), null);
});

test('per-host map resolvers return the self snapshot for self', () => {
  const peers = peersFixture();
  const selfHealth = { app: { status: 'unhealthy' } };
  const selfWatch = { app: { web: [] } };
  const selfLinks = { app: ['app.example.com'] };
  assert.equal(h.resolveHealthMap(peers, 'primary', 'primary', selfHealth), selfHealth);
  assert.equal(h.resolveHealthwatchMap(peers, 'primary', 'primary', selfWatch), selfWatch);
  assert.equal(h.resolveAppLinksMap(peers, 'primary', 'primary', selfLinks), selfLinks);
  // Single-host instance: no peers snapshot at all, any host reads self.
  assert.equal(h.resolveHealthMap(null, '', '', selfHealth), selfHealth);
});

test("per-host map resolvers read a peer's own fanned-in state", () => {
  const peers = peersFixture();
  assert.deepEqual(h.resolveHealthMap(peers, 'primary', 'host-b', {}), {
    web: { status: 'healthy' },
  });
  assert.deepEqual(h.resolveHealthwatchMap(peers, 'primary', 'host-b', {}), {
    web: { app: [] },
  });
  assert.deepEqual(h.resolveAppLinksMap(peers, 'primary', 'host-b', {}), {
    web: ['web.example.com'],
  });
});

test('per-host map resolvers tolerate an older peer missing the section', () => {
  const peers = peersFixture();
  const selfHealth = { app: { status: 'healthy' } };
  // old-peer's state carries none of the per-stack sections — each resolver
  // must fall to an empty map, never to the primary's own snapshot and never
  // throw on the missing chain.
  assert.deepEqual(h.resolveHealthMap(peers, 'primary', 'old-peer', selfHealth), {});
  assert.deepEqual(h.resolveHealthwatchMap(peers, 'primary', 'old-peer', selfHealth), {});
  assert.deepEqual(h.resolveAppLinksMap(peers, 'primary', 'old-peer', selfHealth), {});
  // A peer with no state at all (never yet polled) behaves the same.
  peers.peers.push({ name: 'fresh', url: 'https://fresh:8001' });
  assert.deepEqual(h.resolveHealthMap(peers, 'primary', 'fresh', selfHealth), {});
});

test('resolveRepoWebURL links each host through its own forge', () => {
  const peers = peersFixture();
  const selfURL = 'https://forge.example.com/o/self';
  assert.equal(h.resolveRepoWebURL(peers, 'primary', 'primary', selfURL), selfURL);
  assert.equal(
    h.resolveRepoWebURL(peers, 'primary', 'host-b', selfURL),
    'https://forge-b.example.com/o/r',
  );
  // An older peer predating repo_web_url yields '' — plain-text SHAs, never a
  // link through the wrong (primary's) forge.
  assert.equal(h.resolveRepoWebURL(peers, 'primary', 'old-peer', selfURL), '');
  assert.equal(h.resolveRepoWebURL(null, 'primary', 'primary', ''), '');
});

test('buildHostList puts self first and coerces peer flags to booleans', () => {
  // Single host: exactly the self descriptor, always reachable, never stale.
  assert.deepEqual(h.buildHostList(null, 'primary'), [
    { name: 'primary', url: '', self: true, reachable: true, stale: false },
  ]);
  const peers = peersFixture();
  peers.peers.push({ name: 'fresh', url: 'https://fresh:8001' }); // older shape: no flags
  const list = h.buildHostList(peers, 'primary');
  assert.deepEqual(
    list.map((x) => {
      return x.name;
    }),
    ['primary', 'host-b', 'old-peer', 'fresh'],
  );
  assert.deepEqual(list[1], {
    name: 'host-b',
    url: 'https://host-b:8001',
    self: false,
    reachable: true,
    stale: false,
  });
  assert.deepEqual(list[2], {
    name: 'old-peer',
    url: 'https://old:8001',
    self: false,
    reachable: false,
    stale: true,
  });
  // Missing flags coerce to booleans, not undefined — the renderers branch on them.
  assert.deepEqual(list[3], {
    name: 'fresh',
    url: 'https://fresh:8001',
    self: false,
    reachable: false,
    stale: false,
  });
  // A peers snapshot without a peers list must not throw.
  assert.deepEqual(h.buildHostList({ self: 'primary' }, 'primary').length, 1);
});

// ── Registry update resolver (ADR-0054) ──

test('resolveUpdates returns the local snapshot for self and the fanned-in one for a peer', () => {
  const selfUpdates = { stacks: { gitea: { server: { latest: '1.22.6' } } } };
  const peers = {
    peers: [
      {
        name: 'argoneon',
        state: { stacks: { updates: { stacks: { pihole: { pihole: { rebuilt: true } } } } } },
      },
      { name: 'bare' }, // an older peer without the field
    ],
  };
  assert.equal(h.resolveUpdates(peers, 'nuc', 'nuc', selfUpdates), selfUpdates);
  assert.equal(h.resolveUpdates(peers, 'nuc', '', selfUpdates), selfUpdates);
  assert.deepEqual(h.resolveUpdates(peers, 'nuc', 'argoneon', selfUpdates).stacks.pihole.pihole, {
    rebuilt: true,
  });
  assert.equal(h.resolveUpdates(peers, 'nuc', 'bare', selfUpdates), null);
  assert.equal(h.resolveUpdates(null, 'nuc', 'nuc', null), null);
});

// ── Log quick filters ──

const logEntry = (level, msg, attrs) => ({ time: '2026-08-02T14:31:04Z', level, msg, attrs });

test('logKind separates child-process output, the deploy lifecycle and everything else', () => {
  assert.equal(
    logKindOf('INFO', 'Container app-1  Recreated', { cmd: 'docker', stream: 'stdout' }),
    'output',
  );
  // A cmd attr without a stream is not child output — the pairing is what marks it.
  assert.equal(logKindOf('INFO', 'something', { cmd: 'docker' }), 'plain');
  assert.equal(logKindOf('INFO', 'deploying stack', { stack: 'gitea' }), 'deploy');
  assert.equal(logKindOf('INFO', 'run complete', { skipped: '29' }), 'deploy');
  assert.equal(logKindOf('ERROR', 'deploy failed', { stack: 'gitea' }), 'deploy');
  assert.equal(logKindOf('INFO', 'web UI enabled', {}), 'plain');
  assert.equal(logKindOf('WARN', 'peer unreachable', { peer: 'argoneon' }), 'plain');
});

function logKindOf(level, msg, attrs) {
  return h.logKind(logEntry(level, msg, attrs));
}

test('the severity filter selects exactly one level, so the chip and the pane agree', () => {
  const info = logEntry('INFO', 'web UI enabled', {});
  const warn = logEntry('WARN', 'peer unreachable', {});
  const error = logEntry('ERROR', 'deploy failed', {});
  const output = logEntry('INFO', 'Container app-1  Recreated', {
    cmd: 'docker',
    stream: 'stdout',
  });

  const at = (sev) =>
    [info, warn, error, output].filter((e) => h.logQuickVisible(e, { sev, kinds: [], stacks: [] }));

  assert.equal(at('ALL').length, 4);
  // "warnings" means warnings — not warnings and everything worse.
  assert.deepEqual(at('WARN'), [warn]);
  assert.deepEqual(at('ERROR'), [error]);
});

test('kind and stack filters are membership tests, and an empty set means no restriction', () => {
  const deploy = logEntry('INFO', 'deploying stack', { stack: 'gitea' });
  const output = logEntry('INFO', 'Container app-1  Recreated', {
    cmd: 'docker',
    stream: 'stdout',
    stack: 'gitea',
  });
  const other = logEntry('INFO', 'deploying stack', { stack: 'immich' });
  const noStack = logEntry('INFO', 'web UI enabled', {});

  const show = (f) => [deploy, output, other, noStack].filter((e) => h.logQuickVisible(e, f));

  assert.equal(show({ sev: 'ALL', kinds: [], stacks: [] }).length, 4);
  assert.deepEqual(show({ sev: 'ALL', kinds: ['deploy'], stacks: [] }), [deploy, other]);
  assert.deepEqual(show({ sev: 'ALL', kinds: ['output'], stacks: [] }), [output]);
  assert.deepEqual(show({ sev: 'ALL', kinds: ['deploy', 'output'], stacks: [] }), [
    deploy,
    output,
    other,
  ]);
  assert.deepEqual(show({ sev: 'ALL', kinds: [], stacks: ['gitea'] }), [deploy, output]);
  // The axes compose: a stack plus a kind narrows on both.
  assert.deepEqual(show({ sev: 'ALL', kinds: ['deploy'], stacks: ['gitea'] }), [deploy]);
  // An entry with no stack attr is not a member of any stack filter.
  assert.deepEqual(show({ sev: 'ALL', kinds: [], stacks: ['immich'] }), [other]);
});

test('logQuickVisible without filters shows everything', () => {
  const e = logEntry('INFO', 'web UI enabled', {});
  assert.equal(h.logQuickVisible(e, null), true);
  assert.equal(h.logQuickVisible(e, h.DEFAULT_LOG_FILTERS), true);
});

test('logFiltersActive reports whether the view is narrowed', () => {
  assert.equal(h.logFiltersActive(h.DEFAULT_LOG_FILTERS), false);
  assert.equal(h.logFiltersActive(null), false);
  assert.equal(h.logFiltersActive({ sev: 'WARN', kinds: [], stacks: [] }), true);
  assert.equal(h.logFiltersActive({ sev: 'ALL', kinds: ['deploy'], stacks: [] }), true);
  assert.equal(h.logFiltersActive({ sev: 'ALL', kinds: [], stacks: ['gitea'] }), true);
});

test('parseLogFilters falls back to unfiltered for anything it cannot trust', () => {
  assert.deepEqual(h.parseLogFilters(null), h.DEFAULT_LOG_FILTERS);
  assert.deepEqual(h.parseLogFilters('not json'), h.DEFAULT_LOG_FILTERS);
  assert.deepEqual(h.parseLogFilters('[1,2]'), h.DEFAULT_LOG_FILTERS);
  assert.deepEqual(h.parseLogFilters('"a string"'), h.DEFAULT_LOG_FILTERS);
  // An unknown severity would otherwise hide every line with no way to tell why.
  assert.equal(h.parseLogFilters('{"sev":"LOUD"}').sev, 'ALL');
  // A level with no chip is not a filter a viewer could have set or clear.
  assert.equal(h.parseLogFilters('{"sev":"DEBUG"}').sev, 'ALL');
  assert.equal(h.parseLogFilters('{"sev":"INFO"}').sev, 'ALL');
  // Unknown kinds are dropped; a stray non-string never reaches the predicate.
  assert.deepEqual(h.parseLogFilters('{"kinds":["deploy","nonsense",7]}').kinds, ['deploy']);
  assert.deepEqual(h.parseLogFilters('{"stacks":["gitea","",null]}').stacks, ['gitea']);
});

test('parseLogFilters round-trips a state the UI wrote', () => {
  const state = { sev: 'WARN', kinds: ['output'], stacks: ['gitea', 'immich'] };
  assert.deepEqual(h.parseLogFilters(JSON.stringify(state)), state);
});
