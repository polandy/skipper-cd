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
  assert.match(html, /<span class="td-svc">we&lt;b&gt;<\/span><span>1\.2<\/span>/);
});

test('versionChipHTML drops the service label when service is empty', () => {
  assert.doesNotMatch(r.versionChipHTML('', 'v', 'a', 't'), /td-svc/);
});

test('imageDeltaHTML returns empty for no changes', () => {
  assert.equal(r.imageDeltaHTML(null), '');
  assert.equal(r.imageDeltaHTML([]), '');
});

test('imageDeltaHTML renders a tag bump as old→new with per-service chip', () => {
  const html = r.imageDeltaHTML([
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

test('imageDeltaHTML renders a first image without an old side', () => {
  const html = r.imageDeltaHTML([{ service: 'db', old: '', new: 'postgres:16' }]);
  assert.match(html, /<span class="td-new">16<\/span>/);
  assert.doesNotMatch(html, /td-old|td-arr/);
  assert.match(html, /aria-label="db set to postgres:16"/);
  assert.match(html, /title="db: \(first image\) → postgres:16"/);
});

test('imageDeltaHTML renders a removed service', () => {
  const html = r.imageDeltaHTML([{ service: 'db', old: 'postgres:16', new: '' }]);
  assert.match(html, /<span class="td-old">16<\/span>/);
  assert.match(html, /<span class="td-gone">removed<\/span>/);
  assert.match(html, /aria-label="db removed \(was postgres:16\)"/);
});

test('imageDeltaHTML renders a same-tag digest move as a rebuilt marker', () => {
  const html = r.imageDeltaHTML([
    { service: 'web', old: 'app:1.5@sha256:aaaa1111', new: 'app:1.5@sha256:bbbb2222' },
  ]);
  assert.match(
    html,
    /<span class="td-ctx">1\.5<\/span><span class="td-rebuilt" aria-hidden="true">↻<\/span>/,
  );
  assert.match(html, /aria-label="web rebuilt, tag 1\.5 unchanged"/);
});

test('imageDeltaHTML lists every changed service, one chip each', () => {
  const html = r.imageDeltaHTML([
    { service: 'web', old: 'app:1', new: 'app:2' },
    { service: 'db', old: 'pg:15', new: 'pg:16' },
  ]);
  assert.equal((html.match(/class="tag-delta"/g) || []).length, 2);
  assert.match(html, /web/);
  assert.match(html, /db/);
});

test('renderCommitHead returns empty with neither row echo nor commits', () => {
  assert.equal(r.renderCommitHead(null, null, ''), '');
  assert.equal(r.renderCommitHead([], {}, 'https://forge.example/repo'), '');
});

test('renderCommitHead renders the row echo alone', () => {
  const html = r.renderCommitHead(null, { stack: 'blog', status: 'success' }, '');
  assert.match(html, /^<div class="diff-head" data-testid="diff-head">/);
  assert.match(html, /<span class="dh-who">blog<\/span>/);
  assert.match(html, /deploy diff/);
  assert.match(html, /dh-pill/);
  assert.doesNotMatch(html, /diff-commit/);
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
    null,
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
  const html = r.renderCommitHead(commits, null, '');
  assert.match(html, /<span class="m-range">.*aaaaaaa.*→.*bbbbbbb.*<\/span>/);
  assert.match(html, /<button class="commits-pill" data-testid="commits-pill">2 commits<\/button>/);
  assert.match(html, /<ul class="diff-commit-list" data-testid="diff-commit-list">/);
  assert.match(html, /<span class="cl-subject">newer<\/span>/);
  assert.match(html, /<span class="cl-subject">older<\/span>/);
  assert.doesNotMatch(html, /data-testid="commit-sha"/); // head SHA lives in the range instead
});

test('renderCommitHead renders inert SHA chips without a repo base', () => {
  const html = r.renderCommitHead(
    [{ sha: 'abcdef1234567890', subject: 's', author: 'a' }],
    null,
    '',
  );
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

test('badgeHTML stacks heal_exhausted on two lines', () => {
  const html = r.badgeHTML('heal_exhausted');
  assert.match(html, /<span class="badge-lbl"><span>self-heal<\/span><span>failed<\/span><\/span>/);
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

test('filesHTML renders an em-dash placeholder for no files', () => {
  assert.equal(r.filesHTML(null), '<span class="cell-duration">—</span>');
  assert.equal(r.filesHTML([]), '<span class="cell-duration">—</span>');
});

test('filesHTML renders a pill with the count and the JSON stashed on the button', () => {
  const one = r.filesHTML(['a.yml']);
  assert.match(one, /^<button class="files-pill" data-testid="files-pill"/);
  assert.match(one, /data-files="\[&quot;a\.yml&quot;\]"/);
  assert.match(one, /1 file<\/button>$/);
  assert.match(r.filesHTML(['a', 'b']), /2 files<\/button>$/);
});

test('healPillHTML stashes the drift JSON and defaults it to an empty list', () => {
  const html = r.healPillHTML([{ name: 'web', status: 'exited' }]);
  assert.match(html, /^<button class="heal-pill" data-testid="heal-pill"/);
  assert.match(html, /data-drift="\[{&quot;name&quot;:&quot;web&quot;/);
  assert.match(html, /self-heal<\/button>$/);
  assert.match(r.healPillHTML(null), /data-drift="\[\]"/);
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

test('healthPillHTML carries the status in class hooks, label and title', () => {
  const html = r.healthPillHTML('web', 'unhealthy');
  assert.match(html, /^<button class="health-pill" type="button" data-testid="health-pill"/);
  assert.match(html, /data-health="unhealthy"/);
  assert.match(html, /data-stack="web"/);
  assert.match(html, /<span class="hlabel">unhealthy<\/span>/);
  assert.match(html, /title="web — unhealthy"/);
});

test('hookCount sums pre- and post-deploy commands, tolerating absent groups', () => {
  assert.equal(r.hookCount({ pre_deploy: ['a', 'b'], post_deploy: ['c'] }), 3);
  assert.equal(r.hookCount({ post_deploy: ['c'] }), 1);
  assert.equal(r.hookCount({}), 0);
});

test('hooksBadgeHTML shows the pre+post split, not the sum', () => {
  const html = r.hooksBadgeHTML('web', { pre_deploy: ['a', 'b'], post_deploy: ['c'] });
  assert.match(html, /^<button class="hooks-badge" type="button" data-testid="hooks-badge"/);
  assert.match(html, /data-hooks-stack="web"/);
  assert.match(html, /<span class="hk-count">2\+1<\/span>/);
  assert.match(html, /aria-label="pre-deploy hook: 2\npost-deploy hook: 1"/);
});

test('jumpBtnHTML labels each target view and escapes the stack', () => {
  const toStacks = r.jumpBtnHTML('stacks', 'web<x>');
  assert.match(toStacks, /data-jump-view="stacks"/);
  assert.match(toStacks, /data-jump-stack="web&lt;x&gt;"/);
  assert.match(toStacks, /title="View in Stacks"/);
  assert.match(r.jumpBtnHTML('deploys', 'web'), /title="View in Deploys"/);
});

test('pendingTagHTML renders a blocked reason as text, never markup', () => {
  assert.equal(
    r.pendingTagHTML('blocked', 'blocked by <db>'),
    '<span class="paused-tag">blocked by &lt;db&gt;</span>',
  );
  assert.equal(r.pendingTagHTML('blocked', ''), '<span class="paused-tag">blocked</span>');
});

test('pendingTagHTML labels queued rows paused, with the reason when given', () => {
  assert.equal(
    r.pendingTagHTML('queued', 'window'),
    '<span class="paused-tag">paused: window</span>',
  );
  assert.equal(r.pendingTagHTML('queued', undefined), '<span class="paused-tag">paused</span>');
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
