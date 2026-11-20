// Unit layer for the pure HTML-string render layer (app-render.js). Runs with
// the Node built-in test runner — `node --test` (or `make ui-unit`), no build
// step, no dependencies. Like app-helpers.test.js it is neither embedded nor
// served, so it never ships in the binary.
const test = require('node:test');
const assert = require('node:assert/strict');
const r = require('./app-render.js');

test('escapeHtml escapes the three markup characters and nothing else', () => {
  assert.equal(r.escapeHtml('a & b <c> "d" \'e\''), 'a &amp; b &lt;c&gt; "d" \'e\'');
  assert.equal(r.escapeHtml('plain'), 'plain');
});

test('escapeHtml renders null and undefined as empty', () => {
  assert.equal(r.escapeHtml(null), '');
  assert.equal(r.escapeHtml(undefined), '');
  assert.equal(r.escapeHtml(''), '');
});

test('escapeAttr additionally encodes double quotes', () => {
  assert.equal(r.escapeAttr('say "hi" & <go>'), 'say &quot;hi&quot; &amp; &lt;go&gt;');
});

test('commitLinkHTML with a forge base links the short SHA to the full commit', () => {
  const html = r.commitLinkHTML('abcdef1234567890', {
    cls: 'm-sha',
    base: 'https://forge.example/repo',
    title: 'abcdef1234567890',
    testid: 'commit-sha',
  });
  assert.match(html, /^<a /);
  assert.match(html, /class="m-sha sha-link"/);
  assert.match(html, /href="https:\/\/forge\.example\/repo\/commit\/abcdef1234567890"/);
  assert.match(html, /data-testid="commit-sha"/);
  assert.match(html, /title="abcdef1234567890"/);
  assert.match(html, /target="_blank" rel="noopener noreferrer"/);
  assert.match(html, />abcdef1</); // label is the 7-char short form
});

test('commitLinkHTML without a base degrades to an inert span without sha-link', () => {
  const html = r.commitLinkHTML('abcdef1234567890', { cls: 'm-sha' });
  assert.match(html, /^<span class="m-sha">abcdef1<\/span>$/);
  assert.doesNotMatch(html, /sha-link|href/);
});

test('commitLinkHTML refuses a non-http base (commitURL guard)', () => {
  const html = r.commitLinkHTML('abcdef1234567890', { base: 'javascript:alert(1)' });
  assert.doesNotMatch(html, /href|<a /);
});

test('versionChipHTML frames the body with an escaped service label, aria and title', () => {
  const html = r.versionChipHTML('we<b>', '<span>1.2</span>', 'we<b> "up"', 'we<b>: x');
  assert.match(html, /^<span class="tag-delta" role="img"/);
  assert.match(html, /aria-label="we&lt;b&gt; &quot;up&quot;"/);
  assert.match(html, /title="we&lt;b&gt;: x"/);
  assert.match(
    html,
    /<span class="td-svc">we&lt;b&gt;<\/span><span class="td-val"><span>1\.2<\/span><\/span>/,
  );
});

test('versionChipHTML groups the change tokens so a wrap never splits a version', () => {
  const html = r.versionChipHTML('web', '<span>1.5</span><span>→</span><span>1.6</span>', 'a', 't');
  // Label outside the group, every change token inside it: the only wrap
  // opportunity is between the service name and the change.
  assert.match(
    html,
    /<span class="td-svc">web<\/span><span class="td-val"><span>1\.5<\/span><span>→<\/span><span>1\.6<\/span><\/span>/,
  );
});

test('versionChipHTML drops the service label when service is empty', () => {
  assert.doesNotMatch(r.versionChipHTML('', 'v', 'a', 't'), /td-svc/);
});

test('changeCellHTML returns empty for no changes', () => {
  assert.equal(r.changeCellHTML(null, null), '');
  assert.equal(r.changeCellHTML([], null), '');
});

test('changeCellHTML renders a tag bump as old→new with per-service chip', () => {
  const html = r.changeCellHTML([
    { service: 'web', old: 'ghcr.io/app:1.25', new: 'ghcr.io/app:1.26' },
  ]);
  assert.match(html, /^<span class="svc-delta" data-testid="svc-delta">/);
  assert.match(html, /<span class="td-svc">web<\/span>/);
  assert.match(html, /<span class="td-old">1\.25<\/span>/);
  assert.match(html, /<span class="td-arr" aria-hidden="true">→<\/span>/);
  assert.match(html, /<span class="td-new">1\.26<\/span>/);
  assert.match(html, /aria-label="web updated from 1\.25 to 1\.26"/);
  assert.match(html, /title="web: ghcr\.io\/app:1\.25 → ghcr\.io\/app:1\.26"/);
});

test('changeCellHTML renders a first image without an old side', () => {
  const html = r.changeCellHTML([{ service: 'db', old: '', new: 'postgres:16' }]);
  assert.match(html, /<span class="td-new">16<\/span>/);
  assert.doesNotMatch(html, /td-old|td-arr/);
  assert.match(html, /aria-label="db set to postgres:16"/);
  assert.match(html, /title="db: \(first image\) → postgres:16"/);
});

test('changeCellHTML renders a removed service', () => {
  const html = r.changeCellHTML([{ service: 'db', old: 'postgres:16', new: '' }]);
  assert.match(html, /<span class="td-old">16<\/span>/);
  assert.match(html, /<span class="td-gone">removed<\/span>/);
  assert.match(html, /aria-label="db removed \(was postgres:16\)"/);
});

test('changeCellHTML renders a same-tag digest move as a rebuilt marker', () => {
  const html = r.changeCellHTML([
    { service: 'web', old: 'app:1.5@sha256:aaaa1111', new: 'app:1.5@sha256:bbbb2222' },
  ]);
  assert.match(
    html,
    /<span class="td-ctx">1\.5<\/span><span class="td-rebuilt" aria-hidden="true">↻<\/span>/,
  );
  assert.match(html, /aria-label="web rebuilt, tag 1\.5 unchanged"/);
});

test('changeCellHTML shows one chip and counts the other changed services', () => {
  const html = r.changeCellHTML([
    { service: 'web', old: 'app:1', new: 'app:2' },
    { service: 'db', old: 'pg:15', new: 'pg:16' },
    { service: 'cache', old: 'redis:7', new: 'redis:8' },
  ]);
  assert.equal((html.match(/class="tag-delta"/g) || []).length, 1);
  assert.match(html, /<span class="td-svc">web<\/span>/);
  assert.doesNotMatch(html, /td-svc">db/);
  assert.match(
    html,
    /<span class="ver-count" data-testid="change-more" title="2 more changes — open the row for every change">\+2<\/span>/,
  );
});

test('changeCellHTML prints no count for a single change', () => {
  const html = r.changeCellHTML([{ service: 'web', old: 'app:1', new: 'app:2' }]);
  assert.doesNotMatch(html, /ver-count/);
});

// --- change chips (which container a change without a new image reached) ---

test('changeChipsHTML names the services a compose change reached', () => {
  const html = r.changeChipsHTML(
    [{ file: 'mealie/docker-compose.yml', kind: 'compose', services: ['mealie-restic'] }],
    null,
  );
  assert.match(html, /<span class="td-kind">compose<\/span>/);
  assert.match(html, /<span class="td-svc">mealie-restic<\/span>/);
  assert.match(html, /aria-label="compose changed for mealie-restic"/);
});

test('changeChipsHTML folds one chip per kind, in a fixed order', () => {
  const html = r.changeChipsHTML(
    [
      { file: 'vars.env', kind: 'vars', wide: true },
      { file: 'web/Dockerfile', kind: 'build', services: ['api'] },
      { file: 'web/docker-compose.yml', kind: 'compose', services: ['web'] },
    ],
    null,
  );
  const kinds = [...html.matchAll(/class="td-kind">(\w+)</g)].map((m) => m[1]);
  assert.deepEqual(kinds, ['compose', 'build', 'vars']);
});

test('changeChipsHTML caps the names it prints and counts the rest', () => {
  const html = r.changeChipsHTML(
    [{ file: 'x/docker-compose.yml', kind: 'compose', services: ['a', 'b', 'c', 'd'] }],
    null,
  );
  assert.equal((html.match(/class="td-svc"/g) || []).length, 2);
  assert.match(html, /<span class="td-count">\+2 more<\/span>/);
  assert.match(html, /aria-label="compose changed for a, b and 2 more services"/);
});

test('changeChipsHTML drops a compose service the image delta already names', () => {
  const html = r.changeChipsHTML(
    [{ file: 'x/docker-compose.yml', kind: 'compose', services: ['app'] }],
    [{ service: 'app', old: 'app:1', new: 'app:2' }],
  );
  assert.equal(html, '');
});

test('changeChipsHTML keeps a stack-wide vars change but not a stack-wide compose one', () => {
  assert.match(
    r.changeChipsHTML([{ file: 'vars.env', kind: 'vars', wide: true }], null),
    /<span class="td-wide">stack-wide<\/span>/,
  );
  assert.equal(
    r.changeChipsHTML([{ file: 'x/docker-compose.yml', kind: 'compose', wide: true }], null),
    '',
  );
});

test('changeChipsHTML renders nothing for a change that touched no service', () => {
  assert.equal(r.changeChipsHTML([{ file: 'x/docker-compose.yml', kind: 'compose' }], null), '');
});

test('changeCellHTML leads with an image bump over a change attribution', () => {
  const html = r.changeCellHTML(
    [{ service: 'app', old: 'app:1', new: 'app:2' }],
    [
      { file: 'x/docker-compose.yml', kind: 'compose', services: ['sidecar'] },
      { file: 'x/.env', kind: 'env', wide: true },
    ],
  );
  assert.match(html, /<span class="td-svc">app<\/span>/);
  assert.doesNotMatch(html, /td-kind/);
  assert.match(html, /title="2 more changes — open the row for every change">\+2</);
});

test('changeCellHTML falls back to the first change kind without an image bump', () => {
  const html = r.changeCellHTML(null, [
    { file: 'x/.env', kind: 'env', wide: true },
    { file: 'x/docker-compose.yml', kind: 'compose', services: ['sidecar'] },
  ]);
  assert.match(html, /<span class="td-kind">compose<\/span><span class="td-svc">sidecar<\/span>/);
  assert.doesNotMatch(html, /td-kind">env/);
  assert.match(html, /title="1 more change — open the row for every change">\+1</);
});

test('changeAttributionHTML labels the three attribution states', () => {
  const files = [
    { file: 'a.yml', kind: 'compose', services: ['db'] },
    { file: 'b.env', kind: 'env', wide: true },
    { file: 'c.yml', kind: 'compose' },
  ];
  assert.match(r.changeAttributionHTML(files, 'a.yml'), />db</);
  assert.match(r.changeAttributionHTML(files, 'b.env'), /stack-wide/);
  assert.match(r.changeAttributionHTML(files, 'c.yml'), /no service changed/);
  assert.equal(r.changeAttributionHTML(files, 'unknown.yml'), '');
  assert.equal(r.changeAttributionHTML(null, 'a.yml'), '');
});

test('renderCommitHead returns empty without commits', () => {
  assert.equal(r.renderCommitHead(null, ''), '');
  assert.equal(r.renderCommitHead([], 'https://forge.example/repo'), '');
});

test('renderCommitHead renders a single commit with subject, author, time and linked SHA', () => {
  const html = r.renderCommitHead(
    [
      {
        sha: 'abcdef1234567890',
        subject: 'fix <thing>',
        author: 'a&b',
        date: '2026-07-01T10:00:00Z',
      },
    ],
    'https://forge.example/repo',
  );
  assert.match(html, /<span class="dh-subject">fix &lt;thing&gt;<\/span>/);
  assert.match(html, /a&amp;b/);
  assert.match(html, /data-testid="commit-time"/);
  assert.match(html, /data-testid="commit-sha"/);
  assert.match(html, /href="https:\/\/forge\.example\/repo\/commit\/abcdef1234567890"/);
  assert.doesNotMatch(html, /commits-pill|diff-commit-list/);
});

test('renderCommitHead renders a multi-commit range with pill and list', () => {
  const commits = [
    { sha: 'bbbbbbb2222222', subject: 'newer', author: 'x', date: '2026-07-02T10:00:00Z' },
    { sha: 'aaaaaaa1111111', subject: 'older', author: 'y', date: '2026-07-01T10:00:00Z' },
  ];
  const html = r.renderCommitHead(commits, '');
  assert.match(html, /<span class="m-range">.*aaaaaaa.*→.*bbbbbbb.*<\/span>/);
  assert.match(html, /<button class="commits-pill" data-testid="commits-pill">2 commits<\/button>/);
  assert.match(html, /<ul class="diff-commit-list" data-testid="diff-commit-list">/);
  assert.match(html, /<span class="cl-subject">newer<\/span>/);
  assert.match(html, /<span class="cl-subject">older<\/span>/);
  assert.doesNotMatch(html, /data-testid="commit-sha"/); // head SHA lives in the range instead
});

test('renderCommitHead renders inert SHA chips without a repo base', () => {
  const html = r.renderCommitHead([{ sha: 'abcdef1234567890', subject: 's', author: 'a' }], '');
  assert.match(html, /<span class="m-sha"[^>]*>abcdef1<\/span>/);
  assert.doesNotMatch(html, /href/);
});

test('badgeHTML renders deploying with a spinner instead of an icon', () => {
  const html = r.badgeHTML('deploying');
  assert.match(html, /^<span class="badge badge-deploying" data-testid="status-badge">/);
  assert.match(html, /<span class="spinner"><\/span>deploying/);
  assert.doesNotMatch(html, /badge-ico/);
});

test('badgeHTML renders a terminal status with its icon and label', () => {
  const html = r.badgeHTML('success');
  assert.match(html, /class="badge badge-success"/);
  assert.match(html, /<svg class="badge-ico"/);
  assert.match(html, />success<\/span>$/);
});

test('badgeHTML spells rolled_back as two words', () => {
  assert.match(r.badgeHTML('rolled_back'), /rolled back<\/span>$/);
});

test('badgeHTML stacks rolled_back_unhealthy on two lines', () => {
  const html = r.badgeHTML('rolled_back_unhealthy');
  assert.match(
    html,
    /<span class="badge-lbl"><span>rolled back<\/span><span>unhealthy<\/span><\/span>/,
  );
});

test('badgeHTML labels a removed stack with its own icon and chip class', () => {
  const html = r.badgeHTML('removed');
  assert.match(html, /badge badge-removed/);
  assert.match(html, /<svg class="badge-ico"/);
  assert.match(html, />removed</);
});

test('badgeHTML stacks heal_exhausted on two lines', () => {
  const html = r.badgeHTML('heal_exhausted');
  assert.match(html, /<span class="badge-lbl"><span>self-heal<\/span><span>failed<\/span><\/span>/);
});

// A peer's audit record or roster snapshot is relayed verbatim, so its status
// is untrusted: it must never reach the markup as anything but escaped text.
const XSS = '"><img src=x onerror=alert(1)>';

test('badgeHTML escapes an unknown status and gives it the neutral class', () => {
  const html = r.badgeHTML(XSS);
  assert.doesNotMatch(html, /<img/);
  assert.match(html, /^<span class="badge badge-unknown" data-testid="status-badge">/);
  assert.match(html, /"&gt;&lt;img src=x onerror=alert\(1\)&gt;<\/span>$/);
});

test("rosterStatusHTML escapes a peer stack's unknown last status", () => {
  const html = r.rosterStatusHTML({ name: 'web', last_status: XSS }, false);
  assert.doesNotMatch(html, /<img/);
  assert.match(html, /class="badge badge-unknown"/);
});

test('serviceVersionHTML renders the current tag as a labelled neutral chip', () => {
  const html = r.serviceVersionHTML('web', 'ghcr.io/app:1.26', true);
  assert.match(html, /<span class="td-svc">web<\/span>/);
  assert.match(html, /<span class="td-cur">1\.26<\/span>/);
  assert.match(html, /aria-label="web running 1\.26"/);
  assert.match(html, /title="web: ghcr\.io\/app:1\.26"/);
  assert.doesNotMatch(html, /td-old|td-new|td-arr/);
});

test('serviceVersionHTML drops the visible label with labelled=false but keeps aria/title', () => {
  const html = r.serviceVersionHTML('web', 'app:2', false);
  assert.doesNotMatch(html, /td-svc/);
  assert.match(html, /aria-label="web running 2"/);
});

test('healthHistoryHTML returns empty without at least two phases', () => {
  assert.equal(r.healthHistoryHTML(null, '', 0), '');
  assert.equal(
    r.healthHistoryHTML([{ status: 'healthy', since: '2026-07-01T12:00:00Z' }], '', 0),
    '',
  );
});

test('healthHistoryHTML renders newest-first phases with held durations from nowMs', () => {
  const phases = [
    { status: 'healthy', since: '2026-07-01T12:00:00Z' },
    { status: 'unhealthy', since: '2026-07-01T11:00:00Z' },
  ];
  const html = r.healthHistoryHTML(phases, '', Date.parse('2026-07-01T12:05:00Z'));
  assert.match(html, /^<div class="hp-history" data-testid="health-history">/);
  const rows = html.match(/data-testid="health-phase" data-health="(\w+)"/g);
  assert.deepEqual(rows, [
    'data-testid="health-phase" data-health="healthy"',
    'data-testid="health-phase" data-health="unhealthy"',
  ]);
  // Current phase: held since its start up to nowMs; older phase: until the newer began.
  assert.match(html, /<span>for 5m<\/span>/);
  assert.match(html, /<span>1h<\/span>/);
  assert.doesNotMatch(html, /health-phase-commit/);
});

test('healthHistoryHTML links a deploy-correlated phase commit through repoBase', () => {
  const phases = [
    { status: 'unhealthy', since: '2026-07-01T12:00:00Z' },
    {
      status: 'healthy',
      since: '2026-07-01T11:00:00Z',
      deploy_correlated: true,
      commit: 'abcdef1234567890',
    },
  ];
  const html = r.healthHistoryHTML(
    phases,
    'https://forge.example/repo',
    Date.parse('2026-07-01T12:05:00Z'),
  );
  assert.match(html, /class="hp-commit sha-link"/);
  assert.match(html, /data-testid="health-phase-commit"/);
  assert.match(html, /href="https:\/\/forge\.example\/repo\/commit\/abcdef1234567890"/);
  assert.match(html, /title="deployed just before this phase began"/);
});

test('healthHistoryHTML shows no fold affordances when nothing folds', () => {
  const phases = [
    { status: 'healthy', since: '2026-07-01T12:00:00Z' },
    { status: 'unhealthy', since: '2026-07-01T11:00:00Z' },
  ];
  const html = r.healthHistoryHTML(phases, '', Date.parse('2026-07-01T12:05:00Z'));
  assert.doesNotMatch(html, /health-fold/);
  assert.doesNotMatch(html, /hp-raw/);
});

test('healthHistoryHTML folds routine cycles behind a summary line, a toggle and a raw list', () => {
  const phases = [
    { status: 'healthy', since: '2026-08-09T12:00:00Z' },
    { status: 'starting', since: '2026-08-09T11:59:00Z' },
    { status: 'healthy', since: '2026-08-09T10:00:00Z' },
    {
      status: 'starting',
      since: '2026-08-09T09:59:00Z',
      deploy_correlated: true,
      commit: 'aaa111bcd0000',
    },
    { status: 'healthy', since: '2026-08-09T08:00:00Z' },
    {
      status: 'starting',
      since: '2026-08-09T07:59:30Z',
      deploy_correlated: true,
      commit: 'bbb222bcd0000',
    },
    { status: 'healthy', since: '2026-08-09T06:00:00Z' },
  ];
  const html = r.healthHistoryHTML(
    phases,
    'https://forge.example/repo',
    Date.parse('2026-08-09T12:05:00Z'),
  );
  // The head line carries the folded start; the rest is one summary line.
  assert.equal((html.match(/data-testid="health-phase"/g) || []).length, 1);
  assert.match(html, /up in 1m/);
  assert.match(html, /data-testid="health-fold"/);
  assert.match(html, /hp-count">2<\/span> more starts since/);
  assert.match(html, /up in ≤1m/);
  // The correlated deploys ride the summary as commit chips.
  assert.equal((html.match(/data-testid="health-fold-commit"/g) || []).length, 2);
  // The raw list stays reachable behind the toggle, without duplicate testids.
  assert.match(html, /data-testid="health-fold-toggle"[^>]*>all 7 phases</);
  const raw = html.split('class="hp-raw"')[1];
  assert.ok(raw);
  assert.equal((raw.match(/class="hp-phase"/g) || []).length, 7);
  assert.doesNotMatch(raw, /data-testid="health-phase"/);
});

test('healthHistoryHTML caps the summary commit chips and counts the rest', () => {
  // Six deploy-correlated cycles, one every two hours. The newest start is
  // absorbed into the head line, so five reach the summary: three chips + "+2".
  const HOUR = 3600000;
  const iso = (ms) => new Date(ms).toISOString().replace('.000', '');
  const base = Date.parse('2026-08-09T12:00:00Z');
  const phases = [{ status: 'healthy', since: iso(base) }];
  for (let i = 0; i < 6; i++) {
    phases.push({
      status: 'starting',
      since: iso(base - 2 * i * HOUR - 60000),
      deploy_correlated: true,
      commit: `c${i}0000000000`,
    });
    phases.push({ status: 'healthy', since: iso(base - 2 * (i + 1) * HOUR) });
  }
  const html = r.healthHistoryHTML(phases, '', Date.parse('2026-08-09T12:05:00Z'));
  assert.equal((html.match(/data-testid="health-fold-commit"/g) || []).length, 3);
  assert.match(html, /<span class="hp-commit">\+2<\/span>/);
});

test('healthHistoryHTML points the fold toggle at the raw list it reveals', () => {
  const phases = [
    { status: 'healthy', since: '2026-08-09T12:00:00Z' },
    { status: 'starting', since: '2026-08-09T11:59:00Z' },
    { status: 'healthy', since: '2026-08-09T10:00:00Z' },
  ];
  const withID = r.healthHistoryHTML(phases, '', Date.parse('2026-08-09T12:05:00Z'), { id: 'h7' });
  assert.match(withID, /aria-expanded="false" aria-controls="hp-raw-h7"/);
  assert.match(withID, /<div class="hp-raw" id="hp-raw-h7">/);
  // Without an id there is nothing unique to point at — the attribute is left off
  // rather than pointing at a duplicate.
  const noID = r.healthHistoryHTML(phases, '', Date.parse('2026-08-09T12:05:00Z'));
  assert.doesNotMatch(noID, /aria-controls/);
  assert.match(noID, /<div class="hp-raw">/);
});

test('healthHistoryHTML keeps an incident and its last-good phase as full lines', () => {
  const phases = [
    {
      status: 'unhealthy',
      since: '2026-08-09T12:00:00Z',
      deploy_correlated: true,
      commit: 'abcdef1234567890',
    },
    { status: 'healthy', since: '2026-08-09T09:00:00Z' },
    { status: 'starting', since: '2026-08-09T08:59:00Z' },
    { status: 'healthy', since: '2026-08-09T07:00:00Z' },
    { status: 'starting', since: '2026-08-09T06:59:00Z' },
    { status: 'healthy', since: '2026-08-09T05:00:00Z' },
    { status: 'starting', since: '2026-08-09T04:59:00Z' },
    { status: 'healthy', since: '2026-08-09T03:00:00Z' },
  ];
  const html = r.healthHistoryHTML(phases, '', Date.parse('2026-08-09T12:05:00Z'));
  const rows = html.match(/data-testid="health-phase" data-health="(\w+)"/g);
  assert.deepEqual(rows, [
    'data-testid="health-phase" data-health="unhealthy"',
    'data-testid="health-phase" data-health="healthy"',
  ]);
  assert.match(html, /data-testid="health-phase-commit"/); // the incident keeps its chip
  assert.match(html, /hp-count">2<\/span> more starts since/);
});

test('healthHistoryHTML labels an on-demand service’s folded cycles as idle cycles', () => {
  const phases = [
    { status: 'stopped', since: '2026-08-09T12:00:00Z' },
    { status: 'healthy', since: '2026-08-09T11:00:00Z' },
    { status: 'starting', since: '2026-08-09T10:59:20Z' },
    { status: 'stopped', since: '2026-08-09T08:00:00Z' },
    { status: 'healthy', since: '2026-08-09T07:00:00Z' },
    { status: 'starting', since: '2026-08-09T06:59:00Z' },
    { status: 'stopped', since: '2026-08-09T04:00:00Z' },
  ];
  const html = r.healthHistoryHTML(phases, '', Date.parse('2026-08-09T12:05:00Z'), {
    onDemand: true,
  });
  assert.match(html, /hp-count">2<\/span> idle cycles since/);
});

test('healthStripHTML renders one duration-weighted segment per phase, oldest first', () => {
  const phases = [
    { status: 'unhealthy', since: '2026-07-01T12:00:00Z' },
    { status: 'starting', since: '2026-07-01T11:59:00Z' },
    { status: 'healthy', since: '2026-07-01T10:00:00Z' },
  ];
  const html = r.healthStripHTML(phases, Date.parse('2026-07-01T12:05:00Z'));
  assert.match(html, /^<div class="hp-strip" data-testid="health-strip" aria-hidden="true">/);
  const segs = html.match(/data-health="(\w+)"/g);
  assert.deepEqual(segs, [
    'data-health="healthy"',
    'data-health="starting"',
    'data-health="unhealthy"',
  ]);
  // flex-grow carries the duration in seconds: 1h59m healthy, 1m starting,
  // 5m (so far) unhealthy — and the current segment's title says so.
  assert.match(html, /flex-grow:7140/);
  assert.match(html, /flex-grow:60/);
  assert.match(html, /flex-grow:300/);
  assert.match(html, /title="unhealthy · [^"]*for 5m"/);
  assert.equal(r.healthStripHTML([{ status: 'healthy', since: '2026-07-01T10:00:00Z' }], 0), '');
});

test('clogBtnHTML names the stack/service pair and escapes both into the data attributes', () => {
  const html = r.clogBtnHTML('web<x>', 'app "a"');
  assert.match(html, /^<button class="clog-btn" type="button" data-testid="clog-btn"/);
  assert.match(html, /data-clog-stack="web&lt;x&gt;"/);
  assert.match(html, /data-clog-service="app &quot;a&quot;"/);
  assert.match(html, /aria-label="logs for web&lt;x&gt; \/ app &quot;a&quot;"/);
  assert.doesNotMatch(html, /data-clog-host/);
});

test('clogBtnHTML without a service labels the merged all-services log', () => {
  const html = r.clogBtnHTML('web', '');
  assert.match(html, /data-clog-service=""/);
  assert.match(html, /title="logs for web \(all services\)"/);
});

test('clogBtnHTML tags a peer host so the stream routes through the proxy', () => {
  assert.match(r.clogBtnHTML('web', 'app', 'nuc'), /data-clog-host="nuc"/);
});

test('hookCount sums pre- and post-deploy commands, tolerating absent groups', () => {
  assert.equal(r.hookCount({ pre_deploy: ['a', 'b'], post_deploy: ['c'] }), 3);
  assert.equal(r.hookCount({ post_deploy: ['c'] }), 1);
  assert.equal(r.hookCount({}), 0);
});

test('hostChipHTML renders the monogram chip with the supplied colour slot', () => {
  const html = r.hostChipHTML('nuc', 3);
  assert.match(html, /^<span class="host-mono" role="button" tabindex="0"/);
  assert.match(html, /aria-label="Filter view to host nuc"/);
  assert.match(html, /data-host-color="3"/);
  assert.match(html, /title="nuc">NUC<\/span>$/);
});

test('hostChipHTML drops the colour attribute for an unassigned slot', () => {
  assert.doesNotMatch(r.hostChipHTML('nuc', undefined), /data-host-color/);
});

test('hostChipHTML is empty on a single-host instance (no host name)', () => {
  assert.equal(r.hostChipHTML('', 0), '');
  assert.equal(r.hostChipHTML(undefined, 0), '');
});

test('rosterVersionInnerHTML is empty while the stack has no health entry', () => {
  assert.equal(r.rosterVersionInnerHTML('web', undefined), '');
  assert.equal(r.rosterVersionInnerHTML('web', { services: [] }), '');
});

test('rosterVersionInnerHTML shows the lead service version plus a +N for the rest', () => {
  const health = {
    services: [
      { name: 'web', image: 'nginx:1.27' },
      { name: 'db', image: 'postgres:16' },
    ],
  };
  const html = r.rosterVersionInnerHTML('web', health);
  assert.match(html, /td-svc">web</);
  assert.match(html, /td-cur">1\.27</);
  assert.match(html, /ver-count" title="1 more service — open the row for every version">\+1</);
});

test('rosterVersionInnerHTML reports only a count when no lead service exists', () => {
  const health = {
    services: [
      { name: 'prometheus', image: 'prom/prometheus:v2' },
      { name: 'grafana', image: 'grafana/grafana:11' },
    ],
  };
  assert.equal(
    r.rosterVersionInnerHTML('monitoring', health),
    '<span class="ver-count" title="No single main service — open the row for every version">2 services</span>',
  );
});

test('rosterVersionCellHTML always emits the cell, empty for a disabled stack', () => {
  const health = { services: [{ name: 'web', image: 'nginx:1.27' }] };
  assert.equal(
    r.rosterVersionCellHTML('web', health, true),
    '<span class="col-version" data-testid="roster-version"></span>',
  );
  assert.match(r.rosterVersionCellHTML('web', health, false), /td-cur">1\.27</);
});

test('rosterStatusHTML lets a live deploy win over every stored state', () => {
  const html = r.rosterStatusHTML({ name: 'web', disabled: true, last_status: 'success' }, true);
  assert.match(html, /badge-deploying/);
  assert.match(html, /spinner/);
});

test('rosterStatusHTML falls through deploy → disabled → never deployed → last badge', () => {
  assert.equal(
    r.rosterStatusHTML({ disabled: true }, false),
    '<span class="roster-flag">disabled</span>',
  );
  assert.equal(
    r.rosterStatusHTML({ disabled: false }, false),
    '<span class="roster-flag">never deployed</span>',
  );
  assert.match(r.rosterStatusHTML({ last_status: 'success' }, false), /badge badge-success/);
});

test('rosterStatusHTML appends the held chip after the last badge — never a retry button', () => {
  const held = { since: '2026-09-29T00:05:00Z', status: 'rolled_back', commit: 'd430ece9aa' };
  const html = r.rosterStatusHTML({ name: 'signal', last_status: 'rolled_back', held }, false);
  assert.match(html, /badge badge-rolled_back[\s\S]*data-testid="held-chip"/);
  assert.doesNotMatch(html, /retry-btn/, 'retry lives in the detail verdict');

  const deploying = r.rosterStatusHTML({ name: 'signal', last_status: 'rolled_back', held }, true);
  assert.doesNotMatch(deploying, /held-chip/, 'a live deploy replaces the standing hold');

  assert.doesNotMatch(r.rosterStatusHTML({ name: 'web', last_status: 'success' }, false), /held/);
});

test('heldChipHTML says how and at which commit the change failed, and escapes the stack', () => {
  const html = r.heldChipHTML('a"b', {
    since: '2026-09-29T00:05:00Z',
    status: 'rolled_back_unhealthy',
    commit: 'd430ece9aa',
  });
  assert.match(html, /title="The change at d430ece rolled back · unhealthy /);
  assert.match(html, /not retried until a new commit changes a&quot;b\./);
  assert.equal(r.heldChipHTML('web', undefined), '');
});

test('heldChipHTML reads backoff while a change that failed before it started retries on its own', () => {
  const html = r.heldChipHTML('nextcloud', {
    since: '2026-10-07T02:06:16Z',
    status: 'failed',
    commit: 'abeb8e2b11',
    attempts: 2,
    retry_at: '2026-10-07T02:21:00Z',
  });
  assert.match(html, /<\/svg>backoff<\/span>/);
  assert.match(
    html,
    /title="The change at abeb8e2 failed before it started \(2 attempts so far\), /,
  );
  assert.ok(
    html.includes('Next attempt ' + new Date('2026-10-07T02:21:00Z').toLocaleString() + ';'),
    'the tooltip names when the next attempt is due',
  );
});

test('heldChipHTML counts the attempts of a change held after failing before it started', () => {
  const html = r.heldChipHTML('nextcloud', {
    since: '2026-10-07T02:21:00Z',
    status: 'failed',
    commit: 'abeb8e2b11',
    attempts: 3,
  });
  assert.match(html, /<\/svg>held<\/span>/);
  assert.match(html, /title="The change at abeb8e2 failed 3 times before it started, last /);
  assert.match(html, /not retried until a new commit changes nextcloud\./);
});

test("rosterHealthHTML is the row's health word: a dot and the status, not a control", () => {
  assert.equal(r.rosterHealthHTML(undefined), '');
  assert.equal(r.rosterHealthHTML({}), '');
  const html = r.rosterHealthHTML({ status: 'unhealthy' });
  assert.match(
    html,
    /^<span class="health-pill" data-testid="health-pill" data-health="unhealthy">/,
  );
  assert.match(html, /<span class="hlabel">unhealthy<\/span>/);
  assert.doesNotMatch(html, /<button/);
});

// Fixed clock + a queue entry two minutes old: the autosync row renderers take
// the clock as a parameter, so their wait text is asserted exactly, not raced.
const NOW = Date.parse('2026-07-28T12:00:00Z');
const queuedItem = {
  since: new Date(NOW - 120000).toISOString(),
  changed_files: ['a', 'b'],
};

test('autosyncDetailHTML reports the wait for a queued stack', () => {
  const html = r.autosyncDetailHTML({ name: 'web', effective: true }, queuedItem, NOW);
  assert.match(html, /2 files/);
  assert.match(html, /data-testid="wait-cell">2m</);
  // Singular for exactly one changed file.
  assert.match(
    r.autosyncDetailHTML(
      { effective: true },
      { since: queuedItem.since, changed_files: ['a'] },
      NOW,
    ),
    /1 file</,
  );
});

test('autosyncDetailHTML reports the resting state when nothing is queued', () => {
  assert.equal(r.autosyncDetailHTML({ effective: true }, undefined, NOW), 'synced');
  assert.equal(r.autosyncDetailHTML({ effective: false }, undefined, NOW), 'paused · no changes');
});

test('autosyncPosText numbers a queue row and dots a queued all-stacks row', () => {
  assert.equal(r.autosyncPosText(2, queuedItem), '2');
  assert.equal(r.autosyncPosText(0, undefined), '0');
  assert.equal(r.autosyncPosText(null, queuedItem), '●');
  assert.equal(r.autosyncPosText(null, undefined), '');
});

test('autosyncReasonChipHTML prefers the queue item reason over the snapshot', () => {
  assert.equal(
    r.autosyncReasonChipHTML({ effective: false }, { reason: 'stack' }),
    '<span class="reason reason-stack">stack</span>',
  );
  assert.equal(
    r.autosyncReasonChipHTML({ effective: false, overridden: true }, undefined),
    '<span class="reason reason-stack">stack</span>',
  );
  assert.equal(
    r.autosyncReasonChipHTML({ effective: false }, undefined),
    '<span class="reason reason-global">global</span>',
  );
  // An unknown reason is escaped text on the neutral class, never markup.
  const odd = r.autosyncReasonChipHTML({ effective: false }, { reason: XSS });
  assert.doesNotMatch(odd, /<img/);
  assert.equal(
    odd,
    '<span class="reason reason-unknown">"&gt;&lt;img src=x onerror=alert(1)&gt;</span>',
  );
  // A running stack is never tagged, whatever the queue says.
  assert.equal(r.autosyncReasonChipHTML({ effective: true }, { reason: 'stack' }), '');
});

test('autosyncSwitchTitle names the action, not the state', () => {
  assert.equal(r.autosyncSwitchTitle({ effective: true }), 'Pause autosync');
  assert.equal(r.autosyncSwitchTitle({ effective: false }), 'Resume autosync');
});

test('autosyncRowHTML tags a queue row without a second switch testid', () => {
  const html = r.autosyncRowHTML({ name: 'web', effective: true }, 1, queuedItem, NOW);
  assert.match(html, /data-testid="queue-item"/);
  assert.ok(!html.includes('stack-switch'), `queue row must not expose a switch testid: ${html}`);
  assert.match(html, /class="qpos">1</);
  assert.match(html, /aria-checked="true"/);
});

test('autosyncRowHTML tags an all-stacks row as the switch owner', () => {
  const html = r.autosyncRowHTML({ name: 'web', effective: false }, null, undefined, NOW);
  assert.match(html, /data-testid="stack-item"/);
  assert.match(html, /data-testid="stack-switch"/);
  assert.match(html, /class="qpos blank"><\/div>/);
  assert.match(html, /aria-checked="false"/);
  assert.match(html, /title="Resume autosync"/);
});

test('autosyncRowHTML escapes the stack name in both text and attributes', () => {
  const html = r.autosyncRowHTML({ name: 'a<b&"c"', effective: true }, null, undefined, NOW);
  assert.ok(!html.includes('a<b&"c"'), `raw name leaked: ${html}`);
  assert.match(html, /data-stack="a&lt;b&amp;&quot;c&quot;"/);
  assert.match(html, /class="stack-name">a&lt;b&amp;"c"/);
});

test('nextTrailHTML lists the upcoming stacks separated by dots', () => {
  const html = r.nextTrailHTML(['web', 'db']);
  assert.match(html, /class="arrow">→</);
  assert.match(html, /class="up">web<\/span><span class="sep">·<\/span><span class="up more">db</);
  assert.ok(!html.includes('+'), `nothing was capped, so no overflow chip: ${html}`);
});

test('nextTrailHTML caps the trail at three names and counts the rest', () => {
  const html = r.nextTrailHTML(['a', 'b', 'c', 'd', 'e']);
  assert.match(html, /class="up more">\+2</);
  assert.ok(!html.includes('>d<'), `the fourth name must fold into the count: ${html}`);
});

test('nextTrailHTML escapes the stack names', () => {
  assert.match(r.nextTrailHTML(['a<b']), /class="up">a&lt;b</);
});

test('runSummaryHTML names the active stack and how much of the run is left', () => {
  assert.equal(
    r.runSummaryHTML(['web'], ['db', 'cache']),
    '<b>web</b> deploying · 2 more this run',
  );
  assert.equal(r.runSummaryHTML(['web'], []), '<b>web</b> deploying · last in this run');
});

test('runSummaryHTML counts a queue with nothing deploying yet', () => {
  assert.equal(r.runSummaryHTML([], ['db']), '1 stack upcoming');
  assert.equal(r.runSummaryHTML([], ['db', 'cache']), '2 stacks upcoming');
});

test('runSummaryHTML reports an empty run', () => {
  assert.equal(r.runSummaryHTML([], []), 'Nothing deploying.');
});

test('runSummaryHTML escapes the active stack names', () => {
  assert.equal(r.runSummaryHTML(['a<b'], []), '<b>a&lt;b</b> deploying · last in this run');
});

test('runRowHTML marks the active row and passes the badge through unescaped', () => {
  const html = r.runRowHTML('web', '<svg/>', 'deploying now', true);
  assert.match(html, /class="run-row active"/);
  assert.match(html, /class="run-badge ship"><svg\/></);
  assert.match(html, /class="run-detail">deploying now</);
});

test('runRowHTML escapes the stack name in both text and attributes', () => {
  const html = r.runRowHTML('a<b&"c"', '1', 'next', false);
  assert.ok(!html.includes('a<b&"c"'), `raw name leaked: ${html}`);
  assert.match(html, /data-stack="a&lt;b&amp;&quot;c&quot;"/);
  assert.match(html, /class="run-name">a&lt;b&amp;"c"/);
});

test('runListHTML leads with the active stack and numbers the rest in order', () => {
  const html = r.runListHTML(['web'], ['db', 'cache']);
  const details = [...html.matchAll(/class="run-detail">([^<]*)</g)].map((m) => m[1]);
  assert.deepEqual(details, ['deploying now', 'next', 'then']);
  // The active row carries the ship glyph, the waiting ones their queue position.
  assert.ok(html.includes(`class="run-badge ship">${r.SHIP_ICON}<`), `ship badge missing: ${html}`);
  const positions = [...html.matchAll(/class="run-badge">(\d+)</g)].map((m) => m[1]);
  assert.deepEqual(positions, ['1', '2']);
});

test('runListHTML falls back to the empty-state line', () => {
  assert.match(r.runListHTML([], []), /class="qempty"/);
});

test('beaconPopHTML heads the popover with the shared label and lists each stack', () => {
  const html = r.beaconPopHTML([
    { stack: 'web', status: 'unhealthy' },
    { stack: 'db', status: 'unhealthy' },
  ]);
  assert.match(html, /class="bp-head">2 stacks unhealthy</);
  const stacks = [...html.matchAll(/class="beacon-item"[^>]*data-stack="([^"]*)"/g)].map(
    (m) => m[1],
  );
  assert.deepEqual(stacks, ['web', 'db']); // caller's order preserved
  assert.match(html, /class="bi-dot" data-health="unhealthy"/);
});

test('beaconPopHTML escapes the stack name in both text and attributes', () => {
  const html = r.beaconPopHTML([{ stack: 'a<b&"c"', status: 'unhealthy' }]);
  assert.ok(!html.includes('a<b&"c"'), `raw name leaked: ${html}`);
  assert.match(html, /data-stack="a&lt;b&amp;&quot;c&quot;"/);
  assert.match(html, /class="bi-name">a&lt;b&amp;"c"/);
});

test('attentionBandHTML counts the rows and leaves the icon slot for the caller', () => {
  const html = r.attentionBandHTML([
    { stack: 'web', status: 'unhealthy' },
    { stack: 'db', status: 'unhealthy' },
  ]);
  assert.ok(html.startsWith(`<div class="att-head">${r.WARN_ICON}`), `head mismatch: ${html}`);
  assert.match(html, /class="att-count">2</);
  const stacks = [...html.matchAll(/class="attention-row"[^>]*data-stack="([^"]*)"/g)].map(
    (m) => m[1],
  );
  assert.deepEqual(stacks, ['web', 'db']);
  // populateIcon fills these asynchronously, so the renderer must emit them empty.
  assert.match(html, /<span class="stack-icon" data-testid="stack-icon"><\/span>/);
});

test('attentionBandHTML escapes the stack name and the health status', () => {
  const html = r.attentionBandHTML([{ stack: 'a<b&"c"', status: 'un"healthy' }]);
  assert.ok(!html.includes('a<b&"c"'), `raw name leaked: ${html}`);
  assert.match(html, /data-stack="a&lt;b&amp;&quot;c&quot;"/);
  assert.match(html, /class="health-pill att-pill" data-health="un&quot;healthy"/);
  assert.match(html, /class="hlabel">un"healthy</);
});

test('watchedLeadHTML links the commit the settled lead names', () => {
  const entry = { last_status: 'success', last_commit: 'deadbeefcafe1234' };
  const html = r.watchedLeadHTML(entry, 2, 'https://git.example/repo');
  assert.match(
    html,
    /Unchanged since <a[^>]*href="https:\/\/git\.example\/repo\/commit\/deadbeefcafe1234"/,
  );
  assert.match(html, /title="deadbeefcafe1234"/);
  assert.match(html, />deadbee</); // the short SHA stays the link text
});

test('watchedLeadHTML leaves an unsettled lead as plain text', () => {
  // Only the settled phrasing names a commit; anything else must not gain a link.
  const html = r.watchedLeadHTML(
    { last_status: 'failed', last_commit: 'deadbeefcafe1234' },
    1,
    'b',
  );
  assert.ok(!html.includes('<a '), `unexpected link: ${html}`);
});

test('watchedLeadHTML stays plain text when the entry names no commit', () => {
  const html = r.watchedLeadHTML({ last_status: 'success', last_commit: '' }, 1, 'b');
  assert.ok(!html.includes('<a '), `unexpected link: ${html}`);
});

test('watchedPanelHTML lists the watched files and appends the settings entry', () => {
  const html = r.watchedPanelHTML(
    { watched: ['compose.yaml', 'app.env'], watched_config: true },
    'b',
  );
  const files = [...html.matchAll(/class="wp-file" data-testid="watched-file">([^<]*)</g)].map(
    (m) => m[1],
  );
  assert.deepEqual(files, ['compose.yaml', 'app.env']);
  assert.match(html, /class="wp-config" data-testid="watched-config"/);
});

test('watchedPanelHTML counts the settings entry in the lead', () => {
  // The synthetic config entry is a hashed input like any file, so it must be
  // part of the count the lead phrases — one file plus it reads as plural.
  const entry = { watched: ['compose.yaml'], watched_config: true };
  assert.match(r.watchedPanelHTML(entry, 'b'), /A deploy runs when any of these change:/);
  // Counter-probe: had the panel passed the file count alone, the lead would
  // have come out singular — so the assertion above really does discriminate.
  assert.match(r.watchedLeadHTML(entry, 1, 'b'), /A deploy runs when this file changes:/);
});

test('watchedPanelHTML drops the list when nothing is watched', () => {
  const html = r.watchedPanelHTML({}, 'b');
  assert.ok(!html.includes('wp-files'), `unexpected list: ${html}`);
  assert.match(html, /class="wp-head"/);
});

test('watchedPanelHTML escapes the watched file names', () => {
  const html = r.watchedPanelHTML({ watched: ['a<b&"c".env'] }, 'b');
  assert.ok(!html.includes('a<b&"c"'), `raw path leaked: ${html}`);
  assert.match(html, /watched-file">a&lt;b&amp;"c"\.env</);
});

test('driftListHTML lists the drifted services with their status', () => {
  const html = r.driftListHTML([
    { name: 'web', status: 'exited' },
    { name: 'db', status: 'missing' },
  ]);
  const names = [...html.matchAll(/class="hd-name">([^<]*)</g)].map((m) => m[1]);
  assert.deepEqual(names, ['web', 'db']);
  // The status doubles as a CSS-class suffix and as the visible text.
  assert.match(html, /class="hd-status hd-exited">exited</);
});

test('driftListHTML escapes the drifted service name and status', () => {
  const html = r.driftListHTML([{ name: 'a<b&"c"', status: 'x"y' }]);
  assert.ok(!html.includes('a<b&"c"'), `raw name leaked: ${html}`);
  assert.match(html, /class="hd-name">a&lt;b&amp;"c"</);
  // The status lands in an attribute and in text, so it takes the two different
  // escapes: the quote must die inside the class, but stays as typed in the text.
  assert.match(html, /class="hd-status hd-x&quot;y">x"y</);
});

test('filesPanelHTML renders one escaped path per line', () => {
  const html = r.filesPanelHTML(['a/b.yaml', 'x<y.env']);
  assert.equal(
    html,
    '<span class="file-path">a/b.yaml</span><br><span class="file-path">x&lt;y.env</span>',
  );
});

test('filesPanelHTML renders nothing for an empty change set', () => {
  assert.equal(r.filesPanelHTML([]), '');
});

test('diffContentHTML tags each line with its diff class', () => {
  const html = r.diffContentHTML('@@ -1 +1 @@\n-old\n+new\n plain');
  const classes = [...html.matchAll(/class="diff-line([^"]*)"/g)].map((m) => m[1].trim());
  assert.deepEqual(classes, ['diff-hunk', 'diff-del', 'diff-add', '']);
  // Block lines need no separator: a '\n' under white-space: pre doubles the height.
  assert.ok(!html.includes('\n'), `newline between lines: ${html}`);
});

test('diffContentHTML drops the leading git header block but keeps hunks and content', () => {
  const diff =
    'diff --git a/x.yml b/x.yml\nindex 1..2 100644\n--- a/x.yml\n+++ b/x.yml\n' +
    '@@ -1,2 +1,2 @@\n-- old comment\n+new\n@@ -9 +9 @@\n-z';
  const html = r.diffContentHTML(diff);
  assert.ok(!/diff --git|index 1|a\/x\.yml|b\/x\.yml/.test(html), `header leaked: ${html}`);
  assert.equal([...html.matchAll(/class="diff-line/g)].length, 5);
  assert.match(html, /diff-hunk">@@ -1,2/);
  assert.match(html, /diff-del">-- old comment/);
});

test('diffContentHTML escapes diff payload that looks like markup', () => {
  const html = r.diffContentHTML('+<script>alert(1)</script>');
  assert.ok(!html.includes('<script>'), `raw markup leaked: ${html}`);
  assert.match(html, /diff-add">\+&lt;script&gt;/);
});

test('diffPanelHTML expands a lone file but collapses several', () => {
  const one = r.diffPanelHTML({ 'a/compose.yaml': '+x' }, null, null, '');
  assert.match(one, /class="diff-file-header expanded"/);
  const many = r.diffPanelHTML({ 'a/compose.yaml': '+x', 'b/app.env': '-y' }, null, null, '');
  assert.ok(!many.includes('expanded'), `unexpectedly expanded: ${many}`);
  // Sections keep the input order, headed by the basename only.
  const names = [...many.matchAll(/diff-file-header[^>]*>.*?<span>([^<]*)</g)].map((m) => m[1]);
  assert.deepEqual(names, ['compose.yaml', 'app.env']);
});

test('diffPanelHTML prefixes the commit head and passes the forge base through', () => {
  const commits = [{ sha: '1234567abcdef', subject: 'bump', author: 'ci' }];
  const html = r.diffPanelHTML({ 'a.yaml': '+x' }, commits, null, 'https://forge/r');
  assert.match(html, /^<div class="diff-head"/);
  assert.match(html, /href="https:\/\/forge\/r\/commit\/1234567abcdef"/);
  assert.ok(html.indexOf('diff-head') < html.indexOf('diff-file-section'));
});

test('diffPanelHTML renders only the sections when there is no head', () => {
  const html = r.diffPanelHTML({ 'a.yaml': '+x' }, null, null, '');
  assert.match(html, /^<div class="diff-file-section"/);
});

test('hookPhaseHTML numbers a phase only when several hooks share it', () => {
  const one = r.hookPhaseHTML({ phase: 'pre', index: 1, total: 1, stack: 'web' });
  assert.match(one, /pre hook</);
  const many = r.hookPhaseHTML({ phase: 'post', index: 2, total: 3, stack: 'web' });
  assert.match(many, /post hook 2\/3</);
});

test('hookPhaseHTML carries the stack on its log button', () => {
  const html = r.hookPhaseHTML({ phase: 'pre', total: 1, stack: 'a"b' });
  assert.match(html, /data-hook-log="a&quot;b"/);
  assert.match(html, /class="clog-btn hook-log-btn"/);
});

test('rolloutPhaseHTML names the service and numbers it only among several', () => {
  const one = r.rolloutPhaseHTML({ stack: 'app', service: 'web', index: 1, total: 1 });
  assert.match(one, /rolling web$/);
  const many = r.rolloutPhaseHTML({ stack: 'app', service: 'web', index: 1, total: 2 });
  assert.match(many, /rolling web \(1\/2\)$/);
});

test('rolloutPhaseHTML escapes the service name', () => {
  const html = r.rolloutPhaseHTML({ stack: 'app', service: '<b>x', index: 1, total: 1 });
  assert.ok(!html.includes('<b>'), `raw markup leaked: ${html}`);
});

test('logLineHTML gives child-process output a cmd prefix, not a level badge', () => {
  const html = r.logLineHTML({
    time: '2026-07-28T10:00:00Z',
    level: 'info',
    msg: 'pulling',
    attrs: { cmd: 'docker', stream: 'stdout' },
  });
  assert.match(html, /class="log-cmd" data-testid="cmd-prefix">\[docker\]</);
  assert.ok(!html.includes('level-badge'), `unexpected level badge: ${html}`);
  assert.match(html, /class="log-msg">pulling</);
});

test('logLineHTML prefixes the stack for hook output too', () => {
  const html = r.logLineHTML({
    time: '2026-07-28T10:00:00Z',
    level: 'info',
    msg: 'hi',
    attrs: { cmd: 'sh', stream: 'stdout', stack: 'web' },
  });
  assert.match(html, /class="log-stack" data-testid="stack-prefix"[^>]*>\[web\]</);
});

test('the stack prefix carries the name and the affordances that make it a filter control', () => {
  const html = r.logLineHTML({
    time: '2026-07-28T10:00:00Z',
    level: 'info',
    msg: 'deploying stack',
    attrs: { stack: 'arr-stack' },
  });
  assert.match(html, /data-stack="arr-stack"/);
  assert.match(html, /role="button"/);
  assert.match(html, /tabindex="0"/);
});

test('a stack name with quotes cannot break out of the data-stack attribute', () => {
  const html = r.logLineHTML({
    time: '2026-07-28T10:00:00Z',
    level: 'info',
    msg: 'deploying stack',
    attrs: { stack: 'we"b<script>' },
  });
  assert.ok(!html.includes('<script>'), 'expected the raw tag escaped, got: ' + html);
  assert.ok(!html.includes('data-stack="we"b'), 'expected the quote escaped, got: ' + html);
});

test('logLineHTML renders a level line with its badge, pill and attrs blob', () => {
  const html = r.logLineHTML({
    time: '2026-07-28T10:00:00Z',
    level: 'error',
    // A message with no narrative — this is the fallback rendering. (It used
    // to say "deploy failed", which the narrative table now claims.)
    msg: 'could not save deploy state',
    attrs: { stack: 'web', event_id: 'e1', took: '3s' },
  });
  assert.match(html, /data-testid="level-badge">error</);
  assert.match(html, /data-testid="diff-pill" data-event-id="e1"/);
  // stack and event_id have their own surfaces, so they must not repeat here.
  assert.match(html, /class="log-attrs">took=3s</);
});

test('logLineHTML drops the attrs blob when nothing is left to show', () => {
  const html = r.logLineHTML({
    time: '2026-07-28T10:00:00Z',
    level: 'info',
    msg: 'ok',
    attrs: { stack: 'web', event_id: 'e1' },
  });
  assert.ok(!html.includes('log-attrs'), `unexpected attrs blob: ${html}`);
});

test('logLineHTML escapes the message and the attr values', () => {
  const html = r.logLineHTML({
    time: '2026-07-28T10:00:00Z',
    level: 'info',
    msg: '<script>x</script>',
    attrs: { path: 'a<b' },
  });
  assert.ok(!html.includes('<script>'), `raw markup leaked: ${html}`);
  assert.match(html, /class="log-attrs">path=a&lt;b</);
});

test('auditRowsHTML renders one row per record with its status and link', () => {
  const html = r.auditRowsHTML(
    [
      {
        timestamp: '2026-07-28T10:00:00Z',
        status: 'success',
        duration_ms: 3000,
        commit_sha: 'abc1234def',
        changed_files: 2,
      },
      { timestamp: '2026-07-28T09:00:00Z', status: 'rolled_back', duration_ms: 1000 },
    ],
    'https://forge/r',
    true,
  );
  const statuses = [...html.matchAll(/data-status="([^"]*)"/g)].map((m) => m[1]);
  assert.deepEqual(statuses, ['success', 'rolled_back']);
  assert.match(html, /href="https:\/\/forge\/r\/commit\/abc1234def"/);
  // The status is the same badge every other surface uses.
  assert.match(html, /class="ar-status"><span class="badge badge-rolled_back"/);
  assert.match(html, /class="ar-files">2 files</);
  // A record with no commit and no file count renders both cells as em dashes.
  assert.match(html, /<span class="ar-sha">—<\/span><span class="ar-files">—</);
});

test('auditRowsHTML swaps lead and tooltip with the time mode', () => {
  const rec = [{ timestamp: '2026-07-28T10:00:00Z', status: 'success', duration_ms: 1000 }];
  const abs = r.auditRowsHTML(rec, '', true);
  const rel = r.auditRowsHTML(rec, '', false);
  const lead = (h) => /class="ar-time"[^>]*>([^<]*)</.exec(h)[1];
  const tip = (h) => /class="ar-time"[^>]*title="([^"]*)"/.exec(h)[1];
  // Whatever leads in one mode is the tooltip in the other, and vice versa.
  assert.equal(lead(abs), tip(rel));
  assert.equal(lead(rel), tip(abs));
  assert.notEqual(lead(abs), lead(rel));
});

test('auditRowsHTML escapes an error into both its text and tooltip', () => {
  const html = r.auditRowsHTML(
    [{ timestamp: '2026-07-28T10:00:00Z', status: 'failed', duration_ms: 1, error: 'boom <b>"x"' }],
    '',
    false,
  );
  assert.ok(!html.includes('<b>'), `raw markup leaked: ${html}`);
  assert.match(html, /title="boom &lt;b&gt;&quot;x&quot;"/);
  assert.match(html, /class="ar-err"[^>]*>boom &lt;b&gt;"x"</);
});

test('auditRowsHTML renders nothing for an empty history', () => {
  assert.equal(r.auditRowsHTML([], '', false), '');
});

test('clogSvcsHTML marks the selected services active', () => {
  const html = r.clogSvcsHTML([{ name: 'web' }, { name: 'db' }], ['db']);
  assert.match(html, /class="clog-chip" type="button" data-svc="web"/);
  assert.match(html, /class="clog-chip active" type="button" data-svc="db"/);
  // "all" is the active chip only while nothing is filtered.
  assert.match(html, /class="clog-chip" type="button" data-svc="">all</);
  assert.match(
    r.clogSvcsHTML([{ name: 'web' }], []),
    /class="clog-chip active" type="button" data-svc="">all</,
  );
});

test('clogSvcsHTML escapes a service name into attribute and label', () => {
  const html = r.clogSvcsHTML([{ name: 'a"<b' }], []);
  assert.match(html, /data-svc="a&quot;&lt;b">a"&lt;b</);
});

// ── Registry update markers (ADR-0054, variant A) ──

test('serviceVersionHTML with a newer-tag update appends the amber marker', () => {
  const html = r.serviceVersionHTML('server', 'gitea/gitea:1.22.3', true, {
    running: '1.22.3',
    latest: '1.22.6',
  });
  assert.match(html, /class="td-svc"[^>]*>server</);
  assert.match(html, /class="td-cur">1\.22\.3</);
  assert.match(html, /class="td-upd"/);
  assert.match(html, /⇡/);
  assert.match(html, />1\.22\.6</);
  assert.match(html, /title="[^"]*upstream 1\.22\.6 available/);
  assert.match(html, /aria-label="[^"]*1\.22\.6 available/);
});

test('serviceVersionHTML with a rebuilt update marks the tag as rebuilt, not a new version', () => {
  const html = r.serviceVersionHTML('traefik', 'traefik:v3.1', true, {
    running: 'v3.1',
    rebuilt: true,
  });
  assert.match(html, /class="td-upd"/);
  assert.match(html, />rebuilt</);
  assert.match(html, /title="[^"]*rebuilt upstream/);
  assert.doesNotMatch(html, /td-new/);
});

test('serviceVersionHTML without an update renders exactly the plain chip', () => {
  assert.equal(
    r.serviceVersionHTML('web', 'app:2', true, undefined),
    r.serviceVersionHTML('web', 'app:2', true),
  );
});

test('rosterVersionInnerHTML marks the lead chip when its service has an update', () => {
  const health = {
    services: [
      { name: 'immich-server', image: 'ghcr.io/immich-app/immich-server:v1.135.3' },
      { name: 'redis', image: 'redis:6.2' },
    ],
  };
  const html = r.rosterVersionInnerHTML('immich', health, {
    'immich-server': { running: 'v1.135.3', latest: 'v1.137.1' },
  });
  assert.match(html, /td-upd/);
  assert.match(html, />v1\.137\.1</);
  assert.match(html, /\+1/); // the +N pointer stays
});

test('rosterVersionInnerHTML shows the updated service even when it is not the lead', () => {
  // The header update badge counts stacks, so a marked stack must be able to
  // say which service moved — the cell steps off the lead to do it.
  const health = {
    services: [
      { name: 'immich-server', image: 'ghcr.io/immich-app/immich-server:v1.135.3' },
      { name: 'redis', image: 'redis:6.2' },
    ],
  };
  const html = r.rosterVersionInnerHTML('immich', health, {
    redis: { running: '6.2', rebuilt: true },
  });
  assert.match(html, /td-upd/);
  assert.match(html, />redis</);
  assert.match(html, /rebuilt/);
  assert.doesNotMatch(html, />immich-server</);
  assert.match(html, /\+1/); // the other service is still pointed at
});

test('rosterUpdateChipHTML renders the updates-only toggle with its stack count', () => {
  const html = r.rosterUpdateChipHTML(4, false);
  assert.match(html, /data-testid="roster-update-chip"/);
  assert.match(html, /data-status="queued"/); // amber, via the shared chip rule
  assert.match(html, /aria-pressed="false"/);
  assert.doesNotMatch(html, /\bactive\b/);
  assert.match(html, /⇡ updates/);
  assert.match(html, /<span class="sc-count">4<\/span>/);
});

test('rosterUpdateChipHTML marks the chip pressed while it narrows, and is empty at zero', () => {
  assert.match(r.rosterUpdateChipHTML(1, true), /class="clog-chip status-chip active"/);
  assert.match(r.rosterUpdateChipHTML(1, true), /aria-pressed="true"/);
  assert.equal(r.rosterUpdateChipHTML(0, false), '');
  assert.equal(r.rosterUpdateChipHTML(0, true), '');
});

test('updateCheckMetaHTML renders count and check age, and is empty without updates', () => {
  const now = Date.parse('2026-07-31T12:12:00Z');
  const html = r.updateCheckMetaHTML(2, '2026-07-31T12:00:00Z', now);
  assert.match(html, /hp-head-check/);
  assert.match(html, /⇡ 2 updates/);
  assert.match(html, /checked 12m ago/);
  assert.match(r.updateCheckMetaHTML(1, '2026-07-31T12:00:00Z', now), /⇡ 1 update\b/);
  assert.equal(r.updateCheckMetaHTML(0, '2026-07-31T12:00:00Z', now), '');
});

// ── Narrated log lines (the console's rendering, mirrored) ──

test('a narrated line renders the glyph, the stack and the story instead of the attrs blob', () => {
  const html = r.logLineHTML({
    time: '2026-08-02T15:22:19Z',
    level: 'INFO',
    msg: 'deploy complete',
    attrs: { stack: 'nextcloud', event_id: '412' },
  });
  assert.match(html, /data-testid="log-glyph"[^>]*>✓</);
  assert.match(html, /tone-ok/);
  assert.match(html, /data-testid="stack-prefix"[^>]*>\[nextcloud\]</);
  assert.match(html, />deployed</);
  // The level badge and the key=value blob are what the narrative replaces.
  assert.ok(
    !html.includes('data-testid="level-badge"'),
    'expected no level badge on a narrated line',
  );
  assert.ok(!html.includes('event_id=412'), 'expected the id folded into the pill, not printed');
  // The diff pill survives — it is how the full diff is reached.
  assert.match(html, /data-testid="diff-pill"/);
});

test('the run summary renders one toned segment per non-zero outcome', () => {
  const html = r.logLineHTML({
    time: '2026-08-02T15:22:41Z',
    level: 'INFO',
    msg: 'run complete',
    attrs: {
      deployed: '1',
      rolled_back: '1',
      rolled_back_unhealthy: '0',
      queued: '0',
      blocked: '0',
      skipped: '29',
      failed: '0',
    },
  });
  assert.match(html, />1 deployed</);
  assert.match(html, />1 rolled back</);
  assert.match(html, />29 skipped</);
  assert.ok(!html.includes('blocked=0'), 'expected the zero counts gone, got: ' + html);
});

test('an unnarrated message keeps its level badge and attrs, so nothing is swallowed', () => {
  const html = r.logLineHTML({
    time: '2026-08-02T15:22:41Z',
    level: 'WARN',
    msg: 'a message nobody mapped',
    attrs: { some: 'detail' },
  });
  assert.match(html, /data-testid="level-badge">WARN</);
  assert.match(html, />a message nobody mapped</);
  assert.match(html, />some=detail</);
});

test('a synthesised label is not offered as a stack filter', () => {
  // "peer argoneon" is the narrative's own wording, not one of the log's
  // stacks — clicking it could only filter to nothing.
  const html = r.logLineHTML({
    time: '2026-08-02T15:22:41Z',
    level: 'WARN',
    msg: 'peer unreachable',
    attrs: { peer: 'argoneon', err: 'connection refused' },
  });
  assert.match(html, />peer argoneon</);
  assert.ok(
    !html.includes('data-testid="stack-prefix"'),
    'expected no filter control on a synthesised label',
  );
  assert.match(html, /— connection refused/);
});

test('the inline diff block reuses the diff panel classes and escapes its content', () => {
  const html = r.logDiffBlockHTML('@@ -1 +1 @@\n-old\n+new <script>\n');
  assert.match(html, /class="diff-line diff-hunk"/);
  assert.match(html, /class="diff-line diff-del"/);
  assert.match(html, /class="diff-line diff-add"/);
  assert.ok(!html.includes('<script>'), 'expected diff content escaped, got: ' + html);
  // A trailing newline must not produce an empty last line.
  assert.equal((html.match(/class="diff-line/g) || []).length, 3);
});

test('a step line renders its ↳ marker — the only thing tying it to the line above', () => {
  const html = r.logLineHTML({
    time: '2026-08-02T15:22:05Z',
    level: 'INFO',
    msg: 'file changed',
    attrs: { file: 'flake.nix' },
  });
  assert.match(html, /data-testid="log-glyph"[^>]*>↳</);
  assert.match(html, />flake\.nix</);
});

// ── Rollback visibility: outcome strip, last incident ──

test('outcomeStripHTML renders one dot per record, oldest → newest', () => {
  const now = Date.parse('2026-08-05T12:00:00Z');
  const recent = [
    { status: 'success', at: '2026-08-05T11:00:00Z', commit: 'a1b2c3d4' },
    { status: 'rolled_back', at: '2026-08-05T10:00:00Z', commit: 'e5f6a7b8' },
    { status: 'success', at: '2026-08-05T09:00:00Z' },
  ];
  const html = r.outcomeStripHTML(recent, now);
  assert.match(html, /data-testid="outcome-strip"/);
  assert.match(html, /aria-hidden="true"/);
  // Reversed: the oldest record's dot comes first, the newest last.
  const statuses = [...html.matchAll(/data-status="([a-z_]+)"/g)].map((m) => m[1]);
  assert.deepEqual(statuses, ['success', 'rolled_back', 'success']);
  // Tooltips carry label · age · short SHA.
  assert.match(html, /title="rolled back · 2h ago · e5f6a7b"/);
});

test('outcomeStripHTML is empty with fewer than two records — a lone dot only repeats the badge', () => {
  const now = Date.parse('2026-08-05T12:00:00Z');
  assert.equal(r.outcomeStripHTML([], now), '');
  assert.equal(r.outcomeStripHTML(null, now), '');
  assert.equal(r.outcomeStripHTML([{ status: 'success', at: '2026-08-05T11:00:00Z' }], now), '');
});

test('lastIncidentHTML names the papered-over outcome with its age', () => {
  const now = Date.parse('2026-08-05T12:00:00Z');
  const html = r.lastIncidentHTML({ status: 'rolled_back', at: '2026-08-05T10:00:00Z' }, now);
  assert.match(html, /data-testid="last-incident"/);
  assert.match(html, /↺ rolled back · 2h ago/);
  // A failure wears the failure glyph, not the rollback arrow.
  assert.match(
    r.lastIncidentHTML({ status: 'failed', at: '2026-08-05T10:00:00Z' }, now),
    /✗ failed/,
  );
  assert.match(
    r.lastIncidentHTML({ status: 'heal_exhausted', at: '2026-08-05T10:00:00Z' }, now),
    /✗ self-heal failed/,
  );
  // Absent field (server says the badge already covers it) renders nothing.
  assert.equal(r.lastIncidentHTML(null, now), '');
});

test('deployStatusChipsHTML renders one chip per present status, worst first, with counts', () => {
  const html = r.deployStatusChipsHTML({ success: 5, failed: 1, rolled_back: 2 }, ['failed']);
  const order = [...html.matchAll(/data-status="([a-z_]+)"/g)].map((m) => m[1]);
  assert.deepEqual(order, ['failed', 'rolled_back', 'success']);
  assert.match(
    html,
    /class="clog-chip status-chip active" data-status="failed" aria-pressed="true"/,
  );
  assert.match(html, /rolled back<span class="sc-count">2<\/span>/);
  // Inactive chips carry aria-pressed=false.
  assert.match(html, /data-status="success" aria-pressed="false"/);
});

test('deployStatusChipsHTML keeps an active status at count 0 so the narrowing stays clearable', () => {
  const html = r.deployStatusChipsHTML({ rolled_back: 0, success: 3 }, ['rolled_back']);
  assert.match(html, /data-status="rolled_back"[^>]*aria-pressed="true"/);
  assert.match(html, /<span class="sc-count">0<\/span>/);
});

test('deployStatusChipsHTML renders nothing for no rows', () => {
  assert.equal(r.deployStatusChipsHTML({}, []), '');
});

// ── deploy-history fold (auditRowsHTML) ────────────────────────────────────
// A long routine history: one incident on top, then a month of successes.
function auditHistory() {
  const recs = [
    {
      timestamp: '2026-08-09T00:16:00Z',
      status: 'heal_exhausted',
      duration_ms: 0,
      error: 'self-heal exhausted',
    },
  ];
  for (let i = 6; i >= 1; i--) {
    recs.push({
      timestamp: `2026-08-0${i}T10:00:00Z`,
      status: 'success',
      duration_ms: 1000,
      commit_sha: 'c0ffee' + i,
      changed_files: 1,
    });
  }
  return recs;
}

test('auditRowsHTML folds a run of routine outcomes into one summary line', () => {
  const html = r.auditRowsHTML(auditHistory(), 'https://forge/r', false, { id: 'a1' });
  // Incident, the success below it as context, then one fold line for the rest.
  const folded = html.slice(0, html.indexOf('audit-fold-toggle'));
  assert.equal([...folded.matchAll(/data-testid="audit-row"/g)].length, 2);
  assert.match(folded, /data-testid="audit-fold"[^>]*data-status="success"/);
  assert.match(folded, /<span class="hp-count">5<\/span> more successful deploys since /);
  assert.match(folded, /· 5 files/);
  // The deploys inside the run stay reachable as commit chips, capped.
  const chips = [...folded.matchAll(/data-testid="audit-fold-commit"/g)].length;
  assert.equal(chips, 3);
  assert.match(folded, /class="hp-commit">\+2</);
});

test('auditRowsHTML keeps the verbatim list behind the toggle, without row testids', () => {
  const html = r.auditRowsHTML(auditHistory(), '', false, { id: 'a1' });
  assert.match(
    html,
    /class="hp-fold-toggle"[^>]*data-testid="audit-fold-toggle"[^>]*aria-expanded="false"/,
  );
  assert.match(html, /aria-controls="ap-raw-a1"/);
  assert.match(html, /data-label="all 7 deploys"/);
  assert.match(html, /data-fold-label="fold routine outcomes"/);
  const raw = html.slice(html.indexOf('id="ap-raw-a1"'));
  // Every record is in the raw list — and none of it counts as an audit-row,
  // so a testid count never sees one record twice.
  assert.equal([...raw.matchAll(/class="audit-row"/g)].length, 7);
  assert.equal([...raw.matchAll(/data-testid="audit-row"/g)].length, 0);
});

test('auditRowsHTML leaves an unfoldable history untouched and offers no toggle', () => {
  const recs = [
    { timestamp: '2026-08-09T10:00:00Z', status: 'success', duration_ms: 1000 },
    { timestamp: '2026-08-08T10:00:00Z', status: 'failed', duration_ms: 1000 },
    { timestamp: '2026-08-07T10:00:00Z', status: 'success', duration_ms: 1000 },
  ];
  const html = r.auditRowsHTML(recs, '', false, { id: 'a2' });
  assert.equal([...html.matchAll(/data-testid="audit-row"/g)].length, 3);
  assert.ok(!html.includes('audit-fold-toggle'), `unexpected toggle: ${html}`);
  assert.ok(!html.includes('audit-fold'), `unexpected fold line: ${html}`);
});

test('auditRowsHTML folds self-heals and successes separately', () => {
  const recs = [{ timestamp: '2026-08-09T12:00:00Z', status: 'healed', duration_ms: 0 }];
  for (let i = 0; i < 3; i++)
    recs.push({ timestamp: `2026-08-0${i + 5}T10:00:00Z`, status: 'healed', duration_ms: 0 });
  for (let i = 0; i < 3; i++)
    recs.push({ timestamp: `2026-08-0${i + 1}T10:00:00Z`, status: 'success', duration_ms: 1000 });
  const html = r.auditRowsHTML(recs, '', false, { id: 'a3' });
  const folds = [...html.matchAll(/data-testid="audit-fold" data-status="([^"]+)"/g)].map(
    (m) => m[1],
  );
  assert.deepEqual(folds, ['healed', 'success']);
  assert.match(html, /more self-heals since /);
  // A heal changes no files, so the fold line drops the file count entirely.
  const healLine = /data-testid="audit-fold" data-status="healed".*?<\/div>/s.exec(html)[0];
  assert.ok(!healLine.includes('files'), healLine);
});

test('auditRowsHTML follows the time mode on the fold line', () => {
  const abs = r.auditRowsHTML(auditHistory(), '', true, { id: 'a4' });
  const rel = r.auditRowsHTML(auditHistory(), '', false, { id: 'a4' });
  const since = (h) => /more successful deploys since ([^<·]+)/.exec(h)[1].trim();
  assert.notEqual(since(abs), since(rel));
  assert.match(since(rel), /ago$/);
});

test('auditRowsHTML names a folded incident repeat by what failed', () => {
  const boom = { error: 'compose up: exit status 1' };
  const recs = [
    { timestamp: '2026-08-09T10:00:00Z', status: 'rolled_back', duration_ms: 0, ...boom },
    { timestamp: '2026-08-08T10:00:00Z', status: 'rolled_back', duration_ms: 0, ...boom },
    { timestamp: '2026-08-07T10:00:00Z', status: 'rolled_back', duration_ms: 0, ...boom },
    { timestamp: '2026-08-06T10:00:00Z', status: 'rolled_back', duration_ms: 0, ...boom },
  ];
  const html = r.auditRowsHTML(recs, '', false, { id: 'i1' });
  // The newest rollback keeps its full row — error and all — above the fold.
  const folded = html.slice(0, html.indexOf('audit-fold-toggle'));
  assert.equal([...folded.matchAll(/data-testid="audit-row"/g)].length, 1);
  assert.match(folded, /class="ar-err"/);
  assert.match(folded, /data-testid="audit-fold" data-status="rolled_back"/);
  assert.match(folded, /<span class="hp-count">3<\/span> more identical rollbacks since /);
});

test('auditRowsHTML names each incident kind separately', () => {
  const repeat = (status, error) =>
    [0, 1, 2, 3].map((d) => ({
      timestamp: `2026-08-0${d + 1}T10:00:00Z`,
      status,
      duration_ms: 0,
      error,
    }));
  const noun = (status) => {
    const html = r.auditRowsHTML(repeat(status, 'boom').reverse(), '', false, { id: 'i2' });
    return /more ([a-z -]+) since/.exec(html)[1];
  };
  assert.equal(noun('failed'), 'identical failures');
  assert.equal(noun('rolled_back'), 'identical rollbacks');
  assert.equal(noun('rolled_back_unhealthy'), 'identical unhealthy rollbacks');
  assert.equal(noun('heal_exhausted'), 'identical self-heal failures');
});

// --- repeatNoteHTML (ADR-0056) ---

test('repeatNoteHTML marks a collapsed run with its count and span', () => {
  const html = r.repeatNoteHTML({
    status: 'failed',
    repeat_count: 199,
    first_seen: '2026-08-18T02:43:00Z',
    timestamp: '2026-08-18T19:13:00Z',
  });
  assert.match(html, /data-testid="repeat-note"/);
  assert.match(html, /×199/);
  assert.match(html, /199 identical failures/);
});

test('repeatNoteHTML renders nothing for a one-off outcome', () => {
  assert.equal(r.repeatNoteHTML({ status: 'failed', timestamp: '2026-08-18T02:43:00Z' }), '');
  assert.equal(r.repeatNoteHTML(null), '');
});

test('auditRowsHTML marks a collapsed repeat on its row instead of listing it', () => {
  const html = r.auditRowsHTML(
    [
      {
        status: 'failed',
        timestamp: '2026-08-18T19:13:00Z',
        first_seen: '2026-08-18T02:43:00Z',
        repeat_count: 199,
        error: 'no stack directory',
      },
      { status: 'success', timestamp: '2026-08-17T10:00:00Z', commit_sha: 'abc1234' },
    ],
    '',
    false,
    {},
  );
  assert.match(html, /repeat-note/);
  assert.match(html, /×199/);
  // Still one row per record — the count is the row's, not extra rows.
  assert.equal(html.match(/class="audit-row"/g).length, 2);
});

test('auditRowsHTML shows a repeat count on an incident that carries no error text', () => {
  const html = r.auditRowsHTML(
    [{ status: 'heal_exhausted', timestamp: '2026-08-18T19:13:00Z', repeat_count: 4 }],
    '',
    false,
    {},
  );
  assert.match(html, /×4/);
});

test('tableRowHTML renders a deploy row: stack, result, change, when', () => {
  const ts = '2020-01-02T03:04:05Z';
  const html = r.tableRowHTML(r.ROW_KIND.deploy, {
    host: '<HOST>',
    stack: 'a<b',
    cause: '',
    ts,
    absolute: false,
    version: '<VER>',
    state: '<STATE>',
    durationMs: 65000,
    commit: 'abc1234def',
    repoBase: '',
  });
  assert.equal(
    html,
    '<span class="cell-who"><span class="cell-stack"><HOST><span class="stack-icon" data-testid="stack-icon"></span><span class="stack-name row-toggle" role="button" tabindex="0" aria-expanded="false">a&lt;b</span></span></span>' +
      '<span class="status-cell"><STATE></span>' +
      '<span class="col-version"><VER></span>' +
      '<span class="cell-when">' +
      `<span class="cell-time" data-testid="time-cell" data-ts="${ts}" title="${globalThis.fullTime(ts)}">${globalThis.formatTime(ts)}</span>` +
      '<span class="cell-duration" data-testid="duration-cell">1m 5s</span>' +
      r.commitLinkHTML('abc1234def', {
        cls: 'cell-sha',
        base: '',
        title: 'abc1234def',
        testid: 'row-sha',
      }) +
      '</span>',
  );
});

test('tableRowHTML puts a deploy row cause on one titled line under the name', () => {
  const html = r.tableRowHTML(r.ROW_KIND.deploy, {
    host: '',
    stack: 'web',
    cause: 'up failed: <port> in use',
    ts: '2020-01-02T03:04:05Z',
    absolute: false,
    version: '',
    state: '',
    durationMs: 0,
    commit: '',
    repoBase: '',
  });
  assert.match(
    html,
    /<span class="stack-name row-toggle" role="button" tabindex="0" aria-expanded="false">web<\/span><\/span><span class="cell-cause" data-testid="cause-line" title="up failed: &lt;port&gt; in use">up failed: &lt;port&gt; in use<\/span><\/span>/,
  );
  assert.match(html, /<span class="cell-duration" data-testid="duration-cell"><\/span>/);
  assert.doesNotMatch(html, /cell-sha/);
});

test('tableRowHTML deploy rows carry no controls and no health pill', () => {
  const html = r.tableRowHTML(r.ROW_KIND.deploy, {
    host: '',
    stack: 'web',
    cause: 'blocked by db',
    ts: '2020-01-02T03:04:05Z',
    absolute: false,
    version: r.changeCellHTML([{ service: 'web', old: 'app:1', new: 'app:2' }]),
    state: r.badgeHTML('failed') + r.repeatNoteHTML({ repeat_count: 3 }),
    durationMs: 1000,
    commit: 'abc1234',
    repoBase: '',
  });
  assert.doesNotMatch(html, /<button|health-pill|jump-btn|more-btn|paused-tag|files-pill/);
});

test('tableRowHTML renders a roster row: identity, health, version, last deploy with its commit', () => {
  const ts = '2020-01-02T03:04:05Z';
  const html = r.tableRowHTML(r.ROW_KIND.roster, {
    host: '<HOST>',
    stack: 'web',
    health: '<HEALTH>',
    version: '<VERCELL>',
    state: '<STATE>',
    lastAt: ts,
    absolute: true,
    commit: 'abc1234def',
    repoBase: '',
  });
  assert.equal(
    html,
    '<span class="roster-stack"><span class="roster-ident"><HOST><span class="stack-icon" data-testid="stack-icon"></span><span class="roster-name row-toggle" title="web" role="button" tabindex="0" aria-expanded="false">web</span></span></span>' +
      '<span class="roster-health" data-testid="roster-health"><HEALTH></span>' +
      '<VERCELL>' +
      '<span class="roster-last"><span class="roster-status"><STATE></span>' +
      `<span class="roster-when" title="${globalThis.formatTime(ts)}">${globalThis.fullTime(ts)}</span>` +
      r.commitLinkHTML('abc1234def', { cls: 'roster-sha', base: '', title: 'abc1234def' }) +
      '</span>',
  );
});

test('tableRowHTML leaves a roster row with no past deploy untitled and its cells empty', () => {
  const html = r.tableRowHTML(r.ROW_KIND.roster, {
    host: '',
    stack: 'web',
    health: '',
    version: '',
    state: '',
    lastAt: '',
    absolute: false,
    commit: '',
    repoBase: '',
  });
  assert.match(html, /<span class="roster-when"><\/span>/);
  assert.match(html, /<span class="roster-health" data-testid="roster-health"><\/span>/);
});

test('tableRowHTML makes the stack name the one keyboard toggle of either row kind', () => {
  for (const kind of [r.ROW_KIND.deploy, r.ROW_KIND.roster]) {
    const html = r.tableRowHTML(kind, { host: '', stack: 'web', ts: '', lastAt: '', commit: '' });
    const toggles = [...html.matchAll(/class="[^"]*\brow-toggle\b[^"]*"[^>]*>([^<]*)</g)];
    assert.equal(toggles.length, 1, kind);
    assert.equal(toggles[0][1], 'web', kind);
    assert.match(toggles[0][0], /role="button" tabindex="0" aria-expanded="false"/, kind);
  }
});

// ── Stacks detail panel (docs/dev/ui-redesign-plan.md §3.2) ──

test('detailHeadHTML names the stack and lays its actions out app → logs → hooks → jump → close', () => {
  const html = r.detailHeadHTML({
    stack: 'web',
    host: '<HOST>',
    links: ['web.example.org'],
    logs: true,
    hooks: { pre_deploy: ['a', 'b'], post_deploy: ['c'] },
    jump: true,
  });
  assert.match(html, /^<div class="dp-head">/);
  assert.match(html, /<span class="dp-name" title="web">web<\/span><HOST>/);
  const actions = [...html.matchAll(/data-action="([a-z]+)"/g)].map((m) => m[1]);
  assert.deepEqual(actions, ['app', 'logs', 'hooks', 'jump', 'close']);
  assert.match(html, /href="https:\/\/web\.example\.org"[^>]*>[\s\S]*Open app/);
  assert.match(html, /data-clog-stack="web" data-clog-service=""/);
  assert.match(
    html,
    /data-action="hooks"[^>]*aria-pressed="false"[\s\S]*<span class="dp-n">2\+1<\/span>/,
  );
  assert.match(html, /data-jump-view="deploys" data-jump-stack="web"[\s\S]*Show in Deploys/);
});

test("detailHeadHTML shows only the actions that apply, and routes a peer's logs through its host", () => {
  const bare = r.detailHeadHTML({ stack: 'web', host: '', links: [], logs: false, hooks: null });
  const actions = [...bare.matchAll(/data-action="([a-z]+)"/g)].map((m) => m[1]);
  assert.deepEqual(actions, ['close']);
  const peer = r.detailHeadHTML({ stack: 'web', host: '', logHost: 'nuc', logs: true });
  assert.match(peer, /data-clog-host="nuc"/);
  const hooksOpen = r.detailHeadHTML({
    stack: 'web',
    hooks: { pre_deploy: ['a'] },
    hooksOpen: true,
  });
  assert.match(hooksOpen, /data-action="hooks"[^>]*aria-pressed="true"/);
});

test('detailHeadHTML turns several app links into a popover button', () => {
  const html = r.detailHeadHTML({ stack: 'web', links: ['a.example.org', 'b.example.org'] });
  assert.match(html, /<button class="dp-act" type="button" data-action="app"[\s\S]*2 apps/);
  assert.match(html, /<div class="link-pop" data-testid="app-link-pop">/);
});

test('detailHeadHTML escapes a repo-controlled stack name', () => {
  const html = r.detailHeadHTML({ stack: XSS, logs: true });
  assert.doesNotMatch(html, /<img/);
});

test('verdictHTML renders one toned line per verdict, with its action', () => {
  const html = r.verdictHTML(
    [
      {
        kind: 'held',
        tone: 'rollback',
        head: 'Commit abc1234',
        text: 'failed and was rolled back.',
        sub: 'A new push releases it.',
        action: { kind: 'retry' },
      },
      {
        kind: 'unhealthy',
        tone: 'danger',
        head: 'db is unhealthy.',
        text: 'Its container reports exited.',
        sub: '',
        action: { kind: 'logs', service: 'db' },
      },
    ],
    'web',
    { canRetry: true, logHost: 'nuc' },
  );
  assert.match(html, /^<div class="dp-verdict" data-testid="detail-verdict">/);
  assert.match(
    html,
    /data-tone="rollback" data-kind="held"[\s\S]*<b>Commit abc1234<\/b> failed and was rolled back\./,
  );
  assert.match(html, /<div class="dp-v-sub">A new push releases it\.<\/div>/);
  assert.match(html, /data-testid="retry-btn" data-retry-stack="web">Retry once<\/button>/);
  assert.match(
    html,
    /<div class="dp-v-msg" role="status" data-testid="retry-msg"><\/div><\/div><button[^>]*retry-btn/,
    'the retry line carries an empty live region for a refusal',
  );
  assert.match(html, /data-tone="danger" data-kind="unhealthy"/);
  assert.match(
    html,
    /data-action="logs"[^>]*data-clog-stack="web" data-clog-service="db" data-clog-host="nuc"/,
  );
});

test('verdictHTML drops the retry action on a read-only (peer) panel', () => {
  const html = r.verdictHTML(
    [{ kind: 'held', tone: 'danger', head: 'h', text: 't', sub: '', action: { kind: 'retry' } }],
    'web',
    { canRetry: false },
  );
  assert.doesNotMatch(html, /retry-btn/);
  assert.doesNotMatch(html, /retry-msg/);
});

test('verdictHTML escapes head, text and sub', () => {
  const html = r.verdictHTML(
    [{ kind: 'x', tone: 'muted', head: XSS, text: XSS, sub: XSS, action: null }],
    'web',
    {},
  );
  assert.doesNotMatch(html, /<img/);
});

test("factsHTML lays out labelled facts, a fact's html passed through", () => {
  const html = r.factsHTML([
    { label: 'Running', html: '<i>v1</i>' },
    { label: 'Hooks', html: 'none' },
  ]);
  assert.equal(
    html,
    '<div class="dp-facts" data-testid="detail-facts">' +
      '<div class="dp-fact"><div class="dp-k">Running</div><div class="dp-fv"><i>v1</i></div></div>' +
      '<div class="dp-fact"><div class="dp-k">Hooks</div><div class="dp-fv">none</div></div>' +
      '</div>',
  );
});

test('tabsHTML marks the active tab and reds a count that holds a problem', () => {
  const html = r.tabsHTML(
    [
      { id: 'containers', label: 'Containers', count: 2, bad: true },
      { id: 'history', label: 'History', count: null, bad: false },
    ],
    'history',
    'sd',
  );
  assert.match(html, /^<div class="dp-tabs" role="tablist">/);
  assert.match(
    html,
    /data-tab="containers" aria-selected="false" tabindex="-1">Containers<span class="dp-n bad">2<\/span>/,
  );
  assert.match(html, /data-tab="history" aria-selected="true" tabindex="0">History<\/button>/);
});

test('tabsHTML shows a red mark on a countless tab that holds a problem', () => {
  const html = r.tabsHTML([{ id: 'history', label: 'History', count: null, bad: true }], 'x', 'sd');
  assert.match(html, /History<span class="dp-n bad">!<\/span>/);
});

test('tabsHTML ties each tab to the pane it controls', () => {
  const html = r.tabsHTML([{ id: 'history', label: 'History', count: null }], 'history', 'sd');
  assert.match(html, /id="sd-tab-history" aria-controls="sd-pane"/);
});

test('tabsHTML keeps the first tab reachable when no strip tab is selected', () => {
  const html = r.tabsHTML(
    [
      { id: 'history', label: 'History', count: null },
      { id: 'triggers', label: 'Triggers', count: 0 },
    ],
    'hooks',
    'sd',
  );
  assert.match(html, /data-tab="history" aria-selected="false" tabindex="0"/);
  assert.match(html, /data-tab="triggers" aria-selected="false" tabindex="-1"/);
});

test('tabPaneHTML is labelled by the selected tab', () => {
  const tabs = [{ id: 'history', label: 'History', count: null }];
  assert.equal(
    r.tabPaneHTML(tabs, 'history', 'sd', 'Hooks'),
    '<div class="dp-pane" role="tabpanel" id="sd-pane" tabindex="0" aria-labelledby="sd-tab-history"></div>',
  );
});

test('tabPaneHTML names a pane no strip tab selects by its label', () => {
  const tabs = [{ id: 'history', label: 'History', count: null }];
  assert.equal(
    r.tabPaneHTML(tabs, 'hooks', 'sd', 'Hooks'),
    '<div class="dp-pane" role="tabpanel" id="sd-pane" tabindex="0" aria-label="Hooks"></div>',
  );
});

test('tabPaneHTML is a plain pane without a tab strip', () => {
  assert.equal(r.tabPaneHTML([], null, 'dd'), '<div class="dp-pane"></div>');
});

test('stackFactsHTML gives a stack its running version, last deploy, health and hooks', () => {
  const now = Date.parse('2026-08-05T12:00:00Z');
  const entry = {
    name: 'web',
    last_status: 'success',
    last_at: '2026-08-05T11:00:00Z',
    last_commit: 'abc1234def',
    recent: [
      { status: 'success', at: '2026-08-05T11:00:00Z' },
      { status: 'rolled_back', at: '2026-08-05T10:00:00Z' },
    ],
    last_incident: { status: 'rolled_back', at: '2026-08-05T10:00:00Z' },
    hooks: { pre_deploy: ['a'], post_deploy: [] },
  };
  const health = {
    services: [
      { name: 'web', state: 'running', health: 'healthy', image: 'nginx:1.27' },
      { name: 'db', state: 'running', health: 'unhealthy', image: 'postgres:16' },
    ],
  };
  const html = r.stackFactsHTML(entry, health, null, { repoBase: '', nowMs: now });
  const labels = [...html.matchAll(/<div class="dp-k">([^<]*)</g)].map((m) => m[1]);
  assert.deepEqual(labels, ['Running', 'Last deploy', 'Health', 'Hooks']);
  assert.match(html, /1h ago · /);
  assert.match(html, /data-testid="outcome-strip"/);
  assert.match(html, /data-testid="last-incident"/);
  assert.match(html, /1 of 2 healthy/);
  assert.match(html, /1 pre · 0 post/);
});

test('stackFactsHTML says what is missing instead of leaving a fact blank', () => {
  const html = r.stackFactsHTML({ name: 'web' }, null, null, { repoBase: '', nowMs: 0 });
  assert.match(html, /Running<\/div><div class="dp-fv">—</);
  assert.match(html, /Last deploy<\/div><div class="dp-fv">never</);
  assert.match(html, /Health<\/div><div class="dp-fv">—</);
  assert.match(html, /Hooks<\/div><div class="dp-fv">none</);
});

// ── Deploys detail panel: the Stacks anatomy for one deploy event ──

const DEPLOY_NOW = Date.parse('2026-11-17T12:00:00Z');
const FAILED_EVT = {
  id: 7,
  stack: 'web',
  status: 'failed',
  timestamp: '2026-11-17T11:55:00Z',
  duration_ms: 4000,
  error: 'docker compose up: exit status 1: port is already allocated',
  has_diffs: true,
  changed_files: ['web/docker-compose.yml', 'web/.env'],
  image_changes: [{ service: 'app', old: 'nginx:1.25', new: 'nginx:1.26' }],
  file_changes: [{ file: 'web/.env', kind: 'env', services: ['app', 'worker'] }],
};

test('detailHeadHTML carries a badge and meta, and jumps to the view it is given', () => {
  const html = r.detailHeadHTML({
    stack: 'web',
    badge: '<BADGE>',
    meta: '5m ago · 4s',
    logs: true,
    jump: true,
    jumpView: 'stacks',
  });
  assert.match(html, /web<\/span><BADGE><span class="dp-meta">5m ago · 4s<\/span>/);
  assert.match(html, /data-jump-view="stacks" data-jump-stack="web"[\s\S]*Show in Stacks/);
  const actions = [...html.matchAll(/data-action="([a-z]+)"/g)].map((m) => m[1]);
  assert.deepEqual(actions, ['logs', 'jump', 'close']);
});

test('deployPanelHTML: head, then the verdict before facts, tabs and the pane', () => {
  const html = r.deployPanelHTML(FAILED_EVT, { nowMs: DEPLOY_NOW, jump: true, tab: 'changes' });
  const order = [
    'class="dp-head"',
    'data-testid="detail-verdict"',
    'data-testid="detail-facts"',
    'class="dp-tabs"',
    'class="dp-pane"',
  ].map((s) => html.indexOf(s));
  order.forEach((i) => assert.ok(i >= 0, html));
  assert.deepEqual(
    [...order].sort((a, b) => a - b),
    order,
  );
  assert.match(html, /badge-failed/);
  assert.match(html, /<span class="dp-meta">5m ago · 4s<\/span>/);
  assert.match(html, /Failed at docker compose up\./);
  assert.match(html, /data-jump-view="stacks"/);
  assert.match(html, /data-tab="changes" aria-selected="true"/);
  assert.match(html, /data-tab="output" aria-selected="false"/);
});

test('deployPanelHTML: a success has no verdict', () => {
  const html = r.deployPanelHTML(
    { ...FAILED_EVT, status: 'success', error: '' },
    { nowMs: DEPLOY_NOW },
  );
  assert.doesNotMatch(html, /detail-verdict/);
  assert.doesNotMatch(html, /data-tab="output"/);
});

test('deployPanelHTML: a retry after a rollback links the rollback from its verdict', () => {
  const html = r.deployPanelHTML(
    { ...FAILED_EVT, status: 'success', error: '', follows_rollback: true, rollback_event_id: 42 },
    { nowMs: DEPLOY_NOW },
  );
  assert.match(html, /data-testid="detail-verdict"[\s\S]*Retry after a rollback\./);
  assert.match(
    html,
    /<button class="dp-act" type="button" data-action="rollback" data-testid="rollback-link" data-rollback-id="42">[\s\S]*Open rollback/,
  );
});

test('deployFactsHTML: Commit waits for the diff, Change lists every chip', () => {
  const facts = r.deployFactsHTML(FAILED_EVT, { commits: null });
  const cells = [...facts.matchAll(/class="dp-k">([^<]*)</g)].map((m) => m[1]);
  assert.deepEqual(cells, ['Commit', 'Change', 'Duration', 'Files']);
  assert.match(facts, /class="dp-k">Commit<\/div><div class="dp-fv">—</);
  assert.match(facts, /app<\/span>[\s\S]*1\.25[\s\S]*1\.26/);
  assert.equal(facts.match(/class="tag-delta/g).length, 2);
  assert.doesNotMatch(facts, /dp-more"/);
  assert.match(facts, /class="dp-k">Duration<\/div><div class="dp-fv">4s</);
  assert.match(facts, /class="dp-k">Files<\/div><div class="dp-fv">2 files</);
});

test('deployFactsHTML: the landed commit shows its SHA and subject', () => {
  const facts = r.deployFactsHTML(FAILED_EVT, {
    commits: [{ sha: 'abcdef1234567', subject: 'bump <nginx>' }],
    repoBase: 'https://forge/r',
  });
  assert.match(facts, /href="https:\/\/forge\/r\/commit\/abcdef1234567"[^>]*>abcdef1</);
  assert.match(facts, /<span class="dp-subject">bump &lt;nginx&gt;<\/span>/);
});

test('deployFactsHTML: nothing changed reads none, a peer count reads as files', () => {
  const none = r.deployFactsHTML({ status: 'success', duration_ms: 0 }, {});
  assert.match(none, /Change<\/div><div class="dp-fv">none</);
  assert.match(none, /Files<\/div><div class="dp-fv">none</);
  const peer = r.deployFactsHTML(
    { status: 'success', changed_files: 1, commit_sha: 'abc1234def' },
    {},
  );
  assert.match(peer, /Files<\/div><div class="dp-fv">1 file</);
  assert.match(peer, /Commit<\/div><div class="dp-fv"><span class="dp-sha"[^>]*>abc1234</);
});

test('deployPaneHTML: changes with a diff, one file open and several closed, with a stat', () => {
  const one = r.deployPaneHTML('changes', FAILED_EVT, {
    diffs: { 'web/.env': '--- a\n+++ b\n@@ -1 +1 @@\n-A=1\n+A=2\n+B=3' },
  });
  assert.match(one, /data-testid="diff-panel"/);
  assert.match(one, /diff-file-header expanded/);
  assert.match(
    one,
    /<span class="df-stat"><span class="df-add">\+2<\/span><span class="df-del">−1<\/span><\/span>/,
  );
  assert.match(one, /data-testid="file-services"/);
  const two = r.deployPaneHTML('changes', FAILED_EVT, { diffs: { 'a.yml': '+x', 'b.yml': '-y' } });
  assert.doesNotMatch(two, /expanded/);
});

test('deployPaneHTML: changes without a diff list the files; loading says so', () => {
  const files = r.deployPaneHTML('changes', FAILED_EVT, { diffs: null });
  assert.match(files, /data-testid="files-panel"/);
  assert.match(files, /web\/docker-compose\.yml/);
  assert.match(r.deployPaneHTML('changes', FAILED_EVT, { loading: true }), /diff-loading/);
});

test('deployPaneHTML: drift lists the drifted containers', () => {
  const html = r.deployPaneHTML('drift', { heal_drift: [{ name: 'web', status: 'exited' }] }, {});
  assert.match(html, /class="hd-name">web</);
  assert.match(html, /class="hd-status hd-exited">exited</);
});

test('deployOutputHTML clamps beyond six lines with a Show all toggle', () => {
  const short = r.deployOutputHTML('one\ntwo', false);
  assert.match(
    short,
    /<pre class="dp-output" id="deploy-detail-output" data-testid="deploy-output">one\ntwo<\/pre>/,
  );
  assert.doesNotMatch(short, /output-more/);
  const long = 'l1\nl2\nl3\nl4\nl5\nl6\nl7\nl8';
  const clamped = r.deployOutputHTML(long, false);
  assert.match(clamped, /class="dp-output clamped"/);
  assert.match(clamped, /data-action="output-more"[^>]*>Show all 8 lines</);
  assert.match(clamped, /<pre class="dp-output clamped" id="deploy-detail-output"/);
  assert.match(
    clamped,
    /data-action="output-more" aria-expanded="false" aria-controls="deploy-detail-output"/,
  );
  const open = r.deployOutputHTML(long, true);
  assert.doesNotMatch(open, /clamped/);
  assert.match(open, /aria-expanded="true"[^>]*>Show less</);
  assert.doesNotMatch(r.deployOutputHTML('<img src=x>', false), /<img/);
});

test('deployPaneHTML: commits list each commit with its linked SHA', () => {
  const html = r.deployPaneHTML('commits', FAILED_EVT, {
    commits: [
      { sha: 'aaaaaaa111', subject: 'two', author: 'ann' },
      { sha: 'bbbbbbb222', subject: 'one', author: 'bob' },
    ],
    repoBase: 'https://forge/r',
  });
  assert.match(html, /<ul class="diff-commit-list expanded" data-testid="diff-commit-list">/);
  assert.equal([...html.matchAll(/<li>/g)].length, 2);
  assert.match(html, /commit\/aaaaaaa111/);
});

test('deployPanelHTML escapes a repo-controlled stack name and error', () => {
  const html = r.deployPanelHTML({ ...FAILED_EVT, stack: XSS, error: XSS }, { nowMs: DEPLOY_NOW });
  assert.doesNotMatch(html, /<img/);
});
