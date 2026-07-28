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
