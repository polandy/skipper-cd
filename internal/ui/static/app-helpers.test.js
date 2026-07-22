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
