// skipper-cd UI helpers — pure, DOM-free functions shared by the app shell
// (index.html) and exercised in isolation by the unit layer (app-helpers.test.js).
//
// ADR-0035 relaxed the "one embedded file" rule to "self-contained": this file
// is embedded and served same-origin (GET /app-helpers.js), loaded before the
// main script so its functions become globals the app calls by bare name — and
// it also exports them for a `node --test` unit layer, with no build step.
//
// Keep every function pure: no DOM, no module-scope state, output a function of
// its arguments only. That purity is exactly what lets the same source run in
// the browser and under node, and be unit-tested without a headless browser.

// formatDuration renders a millisecond span as "Ns" / "Nm Ns"; a zero or
// negative span shows an em dash.
function formatDuration(ms) {
  if (!ms || ms <= 0) return '—';
  const s = Math.floor(ms / 1000);
  if (s < 60) return s + 's';
  const m = Math.floor(s / 60);
  const rem = s % 60;
  return m + 'm ' + rem + 's';
}

// formatTime renders a timestamp relative to now ("just now", "5m ago", …).
function formatTime(ts) {
  const d = new Date(ts);
  const now = new Date();
  const diffS = Math.floor((now - d) / 1000);
  if (diffS < 5) return 'just now';
  if (diffS < 60) return diffS + 's ago';
  if (diffS < 3600) return Math.floor(diffS / 60) + 'm ago';
  if (diffS < 86400) return Math.floor(diffS / 3600) + 'h ago';
  return Math.floor(diffS / 86400) + 'd ago';
}

// fullTime is the absolute local timestamp shown in tooltips.
function fullTime(ts) {
  return new Date(ts).toLocaleString();
}

// classifyDiffLine maps a unified-diff line to its CSS class (meta/hunk/add/del).
function classifyDiffLine(line) {
  if (
    line.startsWith('+++') ||
    line.startsWith('---') ||
    line.startsWith('diff ') ||
    line.startsWith('index ')
  )
    return 'diff-meta';
  if (line.startsWith('@@')) return 'diff-hunk';
  if (line.startsWith('+')) return 'diff-add';
  if (line.startsWith('-')) return 'diff-del';
  return '';
}

// shortSHA truncates a commit SHA to its 7-char short form.
function shortSHA(sha) {
  return (sha || '').slice(0, 7);
}

// COMMIT_PATH is the forge path segment that addresses a single commit. Gitea
// and GitHub — the forges skipper-cd speaks webhook to — share it.
const COMMIT_PATH = '/commit/';

// commitURL builds the forge page for one commit from the repo's browse URL
// (the `repo_web_url` the `stacks` state carries, already credential-free) and
// a SHA. Returns '' when either is missing, or when the base is not an http(s)
// URL — the caller then renders the SHA as plain text rather than a link. The
// scheme check is the guard against a hostile base ('javascript:…') reaching an
// href, even though the server only ever derives http(s).
function commitURL(base, sha) {
  const b = (base || '').replace(/\/+$/, '');
  if (!b || !sha) return '';
  if (!/^https?:\/\//i.test(b)) return '';
  return b + COMMIT_PATH + sha;
}

// parseImageRef splits an image reference into its {tag, digest} — either may be
// '' — dropping the registry/repository (the service name already identifies the
// image). The tag is the part after the last ':' that follows the last '/' (a
// ':' before it is a registry host:port, not a tag); the digest is the part
// after '@', with the sha256: prefix stripped.
function parseImageRef(ref) {
  const r = ref || '';
  let rest = r;
  let digest = '';
  const at = r.indexOf('@');
  if (at !== -1) {
    digest = r.slice(at + 1).replace(/^sha256:/, '');
    rest = r.slice(0, at);
  }
  let tag = '';
  const colon = rest.lastIndexOf(':');
  if (colon > rest.lastIndexOf('/')) tag = rest.slice(colon + 1);
  return { tag: tag, digest: digest };
}

// shortImageTag reduces a full image reference to the shortest token that still
// identifies it: its tag (`ghcr.io/app:1.5.0` → `1.5.0`), or a short digest when
// the reference is only digest-pinned (`app@sha256:ab34cd90…` → `ab34cd90`), or
// the reference unchanged when it has neither. Used for a first-deploy image
// (which has no old to compare against) and as the fallback in imageDelta.
function shortImageTag(ref) {
  if (!ref) return '';
  const { tag, digest } = parseImageRef(ref);
  if (tag) return tag;
  if (digest) return digest.slice(0, 8);
  return ref;
}

// imageRepoName is the bare repository name of an image reference — registry,
// path and tag/digest dropped (`ghcr.io/immich-app/immich-server:v1.1` →
// `immich-server`). Used to recognise the service a stack is named after.
function imageRepoName(ref) {
  const repo = (ref || '').split('@')[0];
  const slash = repo.lastIndexOf('/');
  const base = slash === -1 ? repo : repo.slice(slash + 1);
  const colon = base.lastIndexOf(':');
  return colon === -1 ? base : base.slice(0, colon);
}

// rosterVersion picks the one service version a Stacks row shows, plus how many
// further services it stands for — the glance; the expanded containers panel
// carries every version.
//
// A stack is normally named after its main image, so the lead service is the one
// whose own name or image repository mentions the stack name, shortest name
// first (`immich-server` beats `immich-machine-learning`); a stack of exactly one
// service needs no guess at all. When nothing matches — a stack named for its
// role rather than an app, say `monitoring` over prometheus/grafana/loki — no
// single version can speak for the stack, so `service` comes back '' and the row
// reports only the count (`more`) rather than picking arbitrarily.
//
// Returns { service, image, more } — the raw image reference, so the caller
// renders the same shortened token (and full-reference tooltip) as everywhere
// else — or null when no service reports an image (nothing running, or a snapshot
// from a skipper too old to carry one), so the caller renders an empty cell.
function rosterVersion(stack, services) {
  const all = (services || []).filter(function (s) {
    return s && s.name;
  });
  if (
    !all.some(function (s) {
      return s.image;
    })
  ) {
    return null;
  }
  const lead = leadService(stack, all);
  return {
    service: lead ? lead.name : '',
    image: lead ? lead.image : '',
    more: all.length - (lead ? 1 : 0),
  };
}

// leadService resolves rosterVersion's "the service this stack is named after",
// or null when the name identifies none of them.
function leadService(stack, services) {
  const key = (stack || '').toLowerCase();
  const named = services.filter(function (s) {
    return s.image;
  });
  if (!key || !named.length) return null;
  const exact = named.filter(function (s) {
    return s.name.toLowerCase() === key;
  });
  if (exact.length) return exact[0];
  const mentions = named.filter(function (s) {
    return (
      s.name.toLowerCase().indexOf(key) !== -1 ||
      imageRepoName(s.image).toLowerCase().indexOf(key) !== -1
    );
  });
  if (mentions.length) {
    // Shortest name wins, then alphabetical — a stable pick, never render order
    // (`compose ps` sorts alphabetically, which would crown `database` for
    // immich).
    return mentions.slice().sort(function (a, b) {
      return a.name.length - b.name.length || a.name.localeCompare(b.name);
    })[0];
  }
  return named.length === 1 ? named[0] : null;
}

// imageDelta reduces an old→new image-reference change to the shortest pair of
// tokens that actually differ, so a deploy row shows what moved:
//   - a tag bump           (`app:1.25`        → `app:1.26`)        → 1.25 → 1.26
//   - a same-tag rebuild   (`app:1.25@sha…aa` → `app:1.25@sha…bb`) → aa… → bb…,
//     with the shared tag (1.25) returned as `tag` context, since the tag alone
//     would read as "1.25 → 1.25" and hide that anything changed
//   - anything else (e.g. a repository change under an equal tag) falls back to
//     the shortest identifying token of each side.
// Returns { from, to, tag } — `tag` is '' except for the digest-only case.
function imageDelta(oldRef, newRef) {
  const a = parseImageRef(oldRef);
  const b = parseImageRef(newRef);
  if (a.tag && b.tag && a.tag !== b.tag) return { from: a.tag, to: b.tag, tag: '' };
  if (a.digest !== b.digest && (a.digest || b.digest)) {
    return {
      from: a.digest.slice(0, 8) || shortImageTag(oldRef),
      to: b.digest.slice(0, 8) || shortImageTag(newRef),
      tag: a.tag === b.tag ? a.tag : '',
    };
  }
  return { from: shortImageTag(oldRef), to: shortImageTag(newRef), tag: '' };
}

// statusText renders a deploy status as the short label shown on the diff
// panel's echo pill (mirrors badge wording without the stacked layout).
function statusText(status) {
  if (status === 'rolled_back') return 'rolled back';
  if (status === 'rolled_back_unhealthy') return 'rolled back · unhealthy';
  if (status === 'heal_exhausted') return 'self-heal · failed';
  return status || '';
}

// STATUS_ICON_PATHS is the inner SVG geometry for each status badge's leading
// glyph, drawn on a 24×24 grid to match the stroke-based header icons. The two
// worst terminal states share the warning triangle by design (T3.14).
const STATUS_ICON_PATHS = {
  success: '<path d="M5 12.5l4.5 4.5L19 7.5"/>',
  failed: '<path d="M7 7l10 10M17 7L7 17"/>',
  rolled_back: '<path d="M8.5 5.5 4 10l4.5 4.5"/><path d="M4 10h9a6.5 6.5 0 0 1 0 13H8"/>',
  rolled_back_unhealthy:
    '<path d="M12 4.5 21 19.5H3z"/><path d="M12 10v4.5"/><path d="M12 17.4h.01"/>',
  healed: '<path d="M12 6.5v11M6.5 12h11"/>',
  heal_exhausted: '<path d="M12 4.5 21 19.5H3z"/><path d="M12 10v4.5"/><path d="M12 17.4h.01"/>',
  queued: '<circle cx="12" cy="12" r="8"/><path d="M12 7.5V12l3.2 2"/>',
  blocked: '<circle cx="12" cy="12" r="8"/><path d="M6.3 6.3l11.4 11.4"/>',
};

// statusIcon returns the leading badge glyph for a deploy status as an inline
// <svg> string (currentColor, so it inherits the badge's text colour), or ''
// for statuses with no icon: deploying keeps its animated spinner, and unknown
// or label-only statuses render text alone. Kept a pure string builder here so
// the badge markup stays unit-testable without a DOM (T3.14).
function statusIcon(status) {
  const paths = STATUS_ICON_PATHS[status];
  if (!paths) return '';
  return (
    '<svg class="badge-ico" viewBox="0 0 24 24" fill="none" stroke="currentColor" ' +
    'stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">' +
    paths +
    '</svg>'
  );
}

// auditStatusLabel spells the two stacked-badge statuses on one line — the
// history rows are compact, so the two-line badge wording is flattened here.
function auditStatusLabel(status) {
  if (status === 'rolled_back') return 'rolled back';
  if (status === 'rolled_back_unhealthy') return 'rolled back · unhealthy';
  if (status === 'heal_exhausted') return 'self-heal failed';
  return status;
}

// auditCountText labels the history panel's record count, or '' when there is
// nothing to count — the head then collapses instead of reading "0 deploys".
function auditCountText(n) {
  if (!n) return '';
  return n + (n === 1 ? ' deploy' : ' deploys');
}

// ROW_STATUSES are the deploy statuses that tint their row. Each one adds a
// `<status>-row` class; a status outside this list stays untinted rather than
// inventing a class no stylesheet defines.
const ROW_STATUSES = [
  'deploying',
  'failed',
  'success',
  'rolled_back',
  'rolled_back_unhealthy',
  'healed',
  'heal_exhausted',
  'queued',
  'blocked',
];

// rowClass builds a deploy row's class list. isHistory marks rows replayed on
// load — live arrivals get `new-row` so they can flash in.
function rowClass(status, isHistory) {
  let cls = 'event-row';
  if (ROW_STATUSES.indexOf(status) !== -1) cls += ' ' + status + '-row';
  if (!isHistory) cls += ' new-row';
  return cls;
}

// logLineLevel is the level a log line renders as: child-process output (a
// command's stdout/stderr) is tagged `cmd` and drawn with a command prefix
// instead of a level badge. Shared by the class the line carries and by the
// renderer that branches on it, so the two cannot disagree.
function logLineLevel(entry) {
  const attrs = entry.attrs || {};
  return attrs.cmd && attrs.stream ? 'cmd' : entry.level;
}

// LOG_SEVERITY_FILTERS are the severity quick filters, exactly one of which is
// active. `WARN` and `ERROR` are thresholds — each shows its level *and worse*
// (`WARN` = WARN + ERROR). They started as exact matches, but exact-match plus
// persistence hid a rollback's WARN-level outcome line behind a sticky
// `errors` chip (2026-08-05); a threshold keeps the promise a severity filter
// actually makes ("at least this bad") and can never hide a worse line behind
// a milder one. `ALL` is the unfiltered state and is the only one that shows
// child-process output (`cmd` lines carry no level of their own).
const LOG_SEVERITY_FILTERS = ['ALL', 'WARN', 'ERROR'];

// LOG_LEVEL_RANK orders the slog levels for the threshold test; an unknown
// level (child output's `cmd`) has no rank and only `ALL` shows it.
const LOG_LEVEL_RANK = { DEBUG: 0, INFO: 1, WARN: 2, ERROR: 3 };

// LOG_OUTCOME_MESSAGES are the narrated lines that say how a deploy ended —
// exempt from client-buffer eviction (mirroring internal/logbuf's pinned set),
// so the outcome of a rollback survives the child-process bursts that follow
// it. Duplicated by hand, per the same display-layer rule as
// LOG_DEPLOY_MESSAGES: a drifted message stops being pinned, never hidden.
const LOG_OUTCOME_MESSAGES = [
  'deploy complete',
  'deploy failed',
  'deploy failed but rolled back',
  'deploy failed, rollback ran but stack is still unhealthy',
  'self-heal: stack restored',
  'self-heal exhausted: stack still degraded after repeated redeploys',
  'run complete',
];

// LOG_OUTCOME_ERROR_MESSAGES are the bad outcomes among them: lines the
// severity axis must treat as errors-tier whatever level the core logged them
// at — the rollback outcome is WARN on the record, and hiding it under an
// `errors` filter is exactly the 2026-08-05 failure mode.
const LOG_OUTCOME_ERROR_MESSAGES = [
  'deploy failed',
  'deploy failed but rolled back',
  'deploy failed, rollback ran but stack is still unhealthy',
  'self-heal exhausted: stack still degraded after repeated redeploys',
];

// isLogOutcome reports whether an entry is a terminal deploy-outcome line —
// the client-side pinning predicate, mirroring internal/logbuf's. A run
// summary counts only when its counters contain a real outcome: the periodic
// reconcile's no-op `run complete` (one per tick) would otherwise fill the
// small pinned set within hours and evict exactly the outcomes the exemption
// exists to keep.
function isLogOutcome(entry) {
  if (LOG_OUTCOME_MESSAGES.indexOf(entry.msg) === -1) return false;
  if (entry.msg !== 'run complete') return true;
  const a = entry.attrs || {};
  return (
    Number(a.deployed) > 0 ||
    Number(a.failed) > 0 ||
    Number(a.rolled_back) > 0 ||
    Number(a.rolled_back_unhealthy) > 0
  );
}

// logFilterSeverity is the level the severity quick filter tests: the record's
// own level, lifted to ERROR for a narrated outcome line that reports a
// failure or rollback — including a run summary whose counts contain one. A
// display-layer classification: the core packages' log levels stay untouched.
function logFilterSeverity(entry) {
  if (LOG_OUTCOME_ERROR_MESSAGES.indexOf(entry.msg) !== -1) return 'ERROR';
  if (entry.msg === 'run complete') {
    const a = entry.attrs || {};
    if (Number(a.failed) > 0 || Number(a.rolled_back) > 0 || Number(a.rolled_back_unhealthy) > 0) {
      return 'ERROR';
    }
  }
  return entry.level;
}

// INCIDENT_WINDOW_MS mirrors the server's incidents_24h window. The client
// re-filters the snapshot's list against it on the relative-time tick, so the
// header incident badge ages out deterministically between republishes — the
// roster snapshot only lands after runs, and a stale count until the next
// deploy would defeat a "last 24h" claim.
const INCIDENT_WINDOW_MS = 24 * 60 * 60 * 1000;

// recentIncidentCount counts the snapshot's incidents still inside the window
// ending at nowMs.
function recentIncidentCount(incidents, nowMs) {
  return (incidents || []).filter(function (i) {
    return i && i.at && nowMs - new Date(i.at).getTime() < INCIDENT_WINDOW_MS;
  }).length;
}

// incidentBadgeLabel is the badge's pluralised title/aria-label.
function incidentBadgeLabel(n) {
  return n + (n === 1 ? ' rollback/failure' : ' rollbacks/failures') + ' in the last 24h';
}

// incidentPresetActive reports whether the Deploys filter currently shows
// exactly what the incident badge put there — its status chips and nothing
// else. The badge toggles on this: a second click clears the filter, while a
// selection the operator has since changed (extra/missing chip, a name query)
// is re-applied rather than thrown away.
function incidentPresetActive(statusFilter, nameQuery, preset) {
  if (nameQuery) return false;
  const cur = statusFilter || [];
  if (cur.length !== preset.length) return false;
  return preset.every(function (s) {
    return cur.indexOf(s) !== -1;
  });
}

// mergeLogView merges the pinned outcome entries back into the ring for
// rendering: pinned entries older than the ring's first (i.e. already evicted;
// IDs are monotonic), then the ring — chronological, no duplicates. Mirrors
// internal/logbuf's replay merge.
function mergeLogView(pinned, ring) {
  if (!pinned.length) return ring;
  const start = ring.length ? ring[0].id : Infinity;
  return pinned
    .filter(function (e) {
      return e.id < start;
    })
    .concat(ring);
}

// LOG_DEPLOY_MESSAGES is the set of messages the `deploys` quick filter keeps:
// skipper's deploy lifecycle, as opposed to startup/background chatter. The
// message text is duplicated here from internal/deploy rather than shared,
// following the same rule as internal/prettylog's anchor table — this is a
// display-layer concern and must not gain influence over the core packages'
// log wording. A message that drifts simply stops matching the filter; it is
// never hidden from the unfiltered view.
const LOG_DEPLOY_MESSAGES = [
  'deploying stack',
  'deploy complete',
  'deploy failed',
  'deploy failed but rolled back',
  'deploy failed, rollback ran but stack is still unhealthy',
  'deploy deferred: autosync paused',
  'deploy deferred: waiting for queued dependency',
  'deploy blocked by failed dependency',
  'rolling back with previous compose file',
  'rollback successful, old containers restored',
  'rollback failed',
  'rollback ran but the restored version is still unhealthy',
  'running deploy hook',
  'file changed',
  'run complete',
  'nixos-rebuild complete',
  'nixos-rebuild failed, aborting all stack deploys',
  'self-heal: restoring stack to its deployed running state',
  'self-heal: stack restored',
  'self-heal triggering corrective redeploy',
];

// ── Log narrative (the console's rendering, mirrored) ──
//
// internal/prettylog turns skipper's deploy-lifecycle records into a short
// narrative — `▸ nextcloud  changed · 2 files` rather than the message plus a
// key=value blob. This table is that same rendering for the Logs view, so the
// two surfaces read alike; the console's own table is the reference.
//
// The message strings are duplicated from internal/deploy by hand, exactly as
// prettylog duplicates them and for the same reason: a display layer must not
// gain influence over the core packages' log wording. A message that drifts
// stops matching and falls back to the raw rendering — it is never dropped.

// narrate builds the parts of one narrated line, or null when the message has
// no narrative. Parts: glyph + tone (the status marker), stack (the accent
// name), text (+ its own tone), segments (a list of toned words, for the run
// summary) and dim (trailing detail).
function logNarrative(entry) {
  const a = entry.attrs || {};
  const stack = a.stack || '';
  switch (entry.msg) {
    case 'webhook accepted, starting deploy in background':
      return { glyph: '⇢', tone: 'accent', text: 'webhook received, deploying' };
    case 'starting deploy run':
      return { glyph: '⇢', tone: 'accent', text: 'run starting', dim: '· ' + a.stacks + ' stacks' };
    case 'pulling latest commits':
    case 'cloning repository':
      return {
        glyph: '⇢',
        tone: 'accent',
        text: 'sync',
        dim: a.branch ? 'branch=' + a.branch : '',
      };
    case 'deploying stack':
      return { glyph: '▸', tone: 'accent', stack: stack, dim: changeSummary(a.changed_files) };
    case 'running deploy hook':
      return { glyph: '↳', tone: '', dim: a.phase + ' [' + (Number(a.index) + 1) + ']' };
    case 'file changed':
      return { glyph: '↳', tone: '', dim: a.file, diff: a.diff || '' };
    case 'deploy complete':
      return { glyph: '✓', tone: 'ok', stack: stack, text: 'deployed', textTone: 'ok' };
    case 'deploy failed':
      return {
        glyph: '✗',
        tone: 'bad',
        stack: stack,
        text: 'failed',
        textTone: 'bad',
        dim: errDetail(a),
      };
    case 'deploy failed but rolled back':
      return {
        glyph: '↺',
        tone: 'roll',
        stack: stack,
        text: 'rolled back',
        textTone: 'roll',
        dim: errDetail(a),
      };
    case 'deploy failed, rollback ran but stack is still unhealthy':
      return {
        glyph: '↺',
        tone: 'bad',
        stack: stack,
        text: 'rolled back · still unhealthy',
        textTone: 'bad',
        dim: errDetail(a),
      };
    case 'skipping stack, no changes detected':
      return { glyph: '▪', tone: '', stack: stack, dim: 'unchanged, skipped' };
    case 'deploy deferred: autosync paused':
      return {
        glyph: '▪',
        tone: 'warn',
        stack: stack,
        text: 'deferred · autosync paused',
        textTone: 'warn',
      };
    case 'self-heal: restoring stack to its deployed running state':
      return { glyph: '⟲', tone: 'ok', stack: stack, text: 'self-heal: restoring', textTone: 'ok' };
    case 'self-heal: stack restored':
      return { glyph: '⟲', tone: 'ok', stack: stack, text: 'self-heal: restored', textTone: 'ok' };
    case 'multi-host fan-in enabled':
      return {
        glyph: '⇢',
        tone: 'accent',
        text: 'multi-host fan-in',
        dim: '· ' + a.peers + ' peers · poll ' + a.poll_interval_seconds + 's',
      };
    case 'peer unreachable':
      return {
        glyph: '▲',
        tone: 'warn',
        stack: 'peer ' + a.peer,
        text: 'unreachable',
        textTone: 'warn',
        dim: errDetail(a),
      };
    case 'peer reachable again':
      return {
        glyph: '✓',
        tone: 'ok',
        stack: 'peer ' + a.peer,
        text: 'reachable again',
        textTone: 'ok',
      };
    case 'stacks resolved':
      return { glyph: '▣', tone: '', text: 'stacks', dim: '· ' + a.stacks + ' discovered' };
    case 'stack discovered':
      return { glyph: '◆', tone: 'accent', stack: stack, dim: discoveredDetail(a) };
    case 'stacks disabled':
      return { glyph: '▪', tone: '', dim: 'parked · disabled: ' + listItems(a.stacks).join(', ') };
    case 'run complete':
      return runCompleteNarrative(a);
    default:
      return null;
  }
}

// listItems parses slog's rendered string slice (`[a b c]`) back into items.
function listItems(rendered) {
  return String(rendered || '')
    .replace(/^\[|\]$/g, '')
    .split(/\s+/)
    .filter(Boolean);
}

// changeSummary reports how many files changed without dumping the whole path
// list onto the line — watch_dirs contents can be long.
function changeSummary(renderedFiles) {
  const files = listItems(renderedFiles);
  if (files.length === 0) return 'changed';
  if (files.length === 1) return 'changed · ' + files[0];
  return 'changed · ' + files.length + ' files';
}

function errDetail(attrs) {
  return attrs.err ? '— ' + attrs.err : '';
}

// discoveredDetail mirrors the console's roster line: hook counts and watch
// dirs, with an em dash where a stack declares neither.
function discoveredDetail(attrs) {
  const pre = Number(attrs.pre_deploy_hooks) || 0;
  const post = Number(attrs.post_deploy_hooks) || 0;
  const parts = [];
  if (pre > 0) parts.push('pre_deploy·' + pre);
  if (post > 0) parts.push('post_deploy·' + post);
  const dirs = listItems(attrs.watch_dirs);
  return (
    'hooks ' +
    (parts.length ? parts.join(' ') : '—') +
    '   watch ' +
    (dirs.length ? dirs.join(', ') : '—')
  );
}

// RUN_OUTCOMES orders the run summary's counts and gives each its tone. A zero
// count is left out entirely: a line that is six sevenths `=0` reports nothing.
const RUN_OUTCOMES = [
  ['deployed', 'deployed', 'ok'],
  ['rolled_back', 'rolled back', 'roll'],
  ['rolled_back_unhealthy', 'rolled back · unhealthy', 'bad'],
  ['queued', 'queued', 'warn'],
  ['blocked', 'blocked', 'warn'],
  ['skipped', 'skipped', ''],
  ['failed', 'failed', 'bad'],
];

// runCompleteNarrative summarises a run, taking its glyph and tone from the
// worst outcome present so a failed run is visible without reading the counts.
function runCompleteNarrative(attrs) {
  const segments = RUN_OUTCOMES.filter(function (o) {
    return Number(attrs[o[0]]) > 0;
  }).map(function (o) {
    return { text: Number(attrs[o[0]]) + ' ' + o[1], tone: o[2] };
  });
  if (segments.length === 0) {
    return { glyph: '▪', tone: '', text: 'run complete', dim: '· no changes' };
  }
  const worst =
    Number(attrs.failed) > 0 || Number(attrs.rolled_back_unhealthy) > 0
      ? 'bad'
      : Number(attrs.rolled_back) > 0
        ? 'roll'
        : Number(attrs.deployed) > 0
          ? 'ok'
          : '';
  const glyph = worst === 'bad' ? '✗' : worst === 'roll' ? '↺' : worst === 'ok' ? '✓' : '▪';
  return { glyph: glyph, tone: worst, text: 'run complete', segments: segments };
}

// logKind classifies one log entry for the kind quick filters: `output` is
// child-process output (docker/git/nixos-rebuild), `deploy` is a
// deploy-lifecycle message, and everything else is `plain` (startup lines,
// background-loop warnings). Every entry has exactly one kind.
function logKind(entry) {
  if (logLineLevel(entry) === 'cmd') return 'output';
  if (LOG_DEPLOY_MESSAGES.indexOf(entry.msg) !== -1) return 'deploy';
  return 'plain';
}

// DEFAULT_LOG_FILTERS is the unfiltered state: every severity, no kind
// restriction, no stack restriction.
const DEFAULT_LOG_FILTERS = { sev: 'ALL', kinds: [], stacks: [] };

// logFacets reduces an entry to the three values the quick filters test. The
// renderer stashes them on the line's dataset so a re-filter reads the DOM
// instead of re-deriving them from the buffer for every rendered line.
// `level` is the classified filter severity (logFilterSeverity), not always
// the record's own level — the dataset path round-trips it as-is.
function logFacets(entry) {
  return {
    level: logFilterSeverity(entry),
    kind: logKind(entry),
    stack: (entry.attrs && entry.attrs.stack) || '',
  };
}

// logMatchesFilters reports whether one line's facets survive the quick
// filters. The three axes are independent and each is a narrowing: severity
// is a threshold — the selected level and worse (or `ALL`) — while a
// non-empty kind or stack set is a membership test. An empty set means "no
// restriction on this axis", never "match nothing" — a filter a viewer
// cleared must not blank the pane.
function logMatchesFilters(facets, filters) {
  const f = filters || DEFAULT_LOG_FILTERS;
  if (f.sev && f.sev !== 'ALL') {
    const rank = LOG_LEVEL_RANK[facets.level];
    if (rank === undefined || rank < LOG_LEVEL_RANK[f.sev]) return false;
  }
  if (f.kinds && f.kinds.length && f.kinds.indexOf(facets.kind) === -1) return false;
  if (f.stacks && f.stacks.length && f.stacks.indexOf(facets.stack || '') === -1) return false;
  return true;
}

// logQuickVisible is logMatchesFilters for a whole entry — the form the stream
// path and the tests use.
function logQuickVisible(entry, filters) {
  return logMatchesFilters(logFacets(entry), filters);
}

// logFiltersActive reports whether the quick filters are narrowing the view —
// the signal that keeps a filtered pane from reading as an empty one.
function logFiltersActive(filters) {
  const f = filters || DEFAULT_LOG_FILTERS;
  return f.sev !== 'ALL' || (f.kinds && f.kinds.length > 0) || (f.stacks && f.stacks.length > 0);
}

// parseLogFilters normalizes a persisted quick-filter state (localStorage,
// so: any string a previous version — or a hand-edited entry — may have left
// behind) into a usable one. Anything unrecognized falls back to the
// unfiltered default rather than silently hiding lines a viewer cannot
// explain.
function parseLogFilters(raw) {
  let saved;
  try {
    saved = JSON.parse(raw);
  } catch {
    return Object.assign({}, DEFAULT_LOG_FILTERS);
  }
  if (!saved || typeof saved !== 'object' || Array.isArray(saved)) {
    return Object.assign({}, DEFAULT_LOG_FILTERS);
  }
  const strings = function (v) {
    return Array.isArray(v)
      ? v.filter(function (x) {
          return typeof x === 'string' && x !== '';
        })
      : [];
  };
  return {
    sev: LOG_SEVERITY_FILTERS.indexOf(saved.sev) !== -1 ? saved.sev : 'ALL',
    kinds: strings(saved.kinds).filter(function (k) {
      return k === 'deploy' || k === 'output';
    }),
    stacks: strings(saved.stacks),
  };
}

// RECONNECT_BASE_DELAY_MS is where a stream's manual retry backoff starts, and
// RECONNECT_MAX_DELAY_MS is its cap. EventSource retries a transient drop by
// itself, but gives up for good on a fatal error (non-2xx, bad content-type),
// which is what these retries are for.
const RECONNECT_BASE_DELAY_MS = 1000;
const RECONNECT_MAX_DELAY_MS = 30000;

// OFFLINE_AFTER_FAILURES is how many attempts must fail in a row before the UI
// says the server is unreachable rather than that it is still connecting. More
// than one, so a single blip during a normal connect never flashes "offline";
// low enough that a page with no route to the server stops promising a
// connection within seconds.
const OFFLINE_AFTER_FAILURES = 3;

// makeReconnector owns one stream's retry backoff: schedule() arms a capped,
// doubling retry unless one is already pending, and reset() is what a good
// connection calls so the next outage starts from the base delay again.
// resume(isOpen) is the wake-up path — see below.
//
// setTimer/clearTimer are required rather than defaulting to the globals: this
// file reaches for no ambient globals, which is what lets it run under node —
// and it puts the timer a test drives in the signature instead of behind a
// fallback. The returned state is per instance, so two streams back off
// independently.
function makeReconnector(connect, setTimer, clearTimer) {
  let timer = null;
  let delay = RECONNECT_BASE_DELAY_MS;
  let failures = 0;
  return {
    // failed records one attempt that did not come up. It is counted separately
    // from schedule() because the two do not coincide: a stream the browser is
    // still retrying by itself (readyState CONNECTING) never schedules a retry
    // here, and that is exactly the shape of a page with no route to the server.
    failed: function () {
      failures++;
    },
    // isOffline reports whether enough attempts have failed in a row to say the
    // server is unreachable instead of still being reached for.
    isOffline: function () {
      return failures >= OFFLINE_AFTER_FAILURES;
    },
    schedule: function () {
      if (timer !== null) return; // a retry is already pending
      timer = setTimer(function () {
        timer = null;
        connect();
      }, delay);
      delay = Math.min(delay * 2, RECONNECT_MAX_DELAY_MS);
    },
    // reset is what a connection that came up calls: the next outage starts from
    // the base delay, and only an answer from the server retracts "offline".
    reset: function () {
      delay = RECONNECT_BASE_DELAY_MS;
      failures = 0;
    },
    // resume reconnects a page that just came back to the foreground. The
    // scheduled retry cannot be relied on across a suspension: an OS that
    // freezes a backgrounded tab (an installed PWA, a locked phone) drops or
    // throttles its timers, so a stream torn down while hidden can sit in
    // `reconnecting` forever with nothing left to wake it. The wake-up itself
    // has to drive the reconnect.
    //
    // isOpen says whether the stream survived — a brief tab switch usually
    // leaves it live, and reopening that would drop events for nothing. When it
    // did not survive, the pending retry is cancelled (leaving it armed would
    // open a second stream when it fires) and the backoff resets, since the
    // delay reached while unreachable says nothing about reachability now.
    resume: function (isOpen) {
      if (isOpen) return;
      if (timer !== null) {
        clearTimer(timer);
        timer = null;
      }
      delay = RECONNECT_BASE_DELAY_MS;
      connect();
    },
  };
}

// phaseDuration renders a millisecond span the way the health timeline needs
// it: compact, coarse ("6h12m", "5m", "3d4h"), never seconds-precise beyond
// the first minute.
function phaseDuration(ms) {
  if (!ms || ms < 1000) return '0s';
  const s = Math.floor(ms / 1000);
  if (s < 60) return s + 's';
  const m = Math.floor(s / 60);
  if (m < 60) return m + 'm';
  const h = Math.floor(m / 60);
  if (h < 24) return h + 'h' + (m % 60 ? (m % 60) + 'm' : '');
  const d = Math.floor(h / 24);
  return d + 'd' + (h % 24 ? (h % 24) + 'h' : '');
}

// phaseSince renders a phase start compactly in local time: time-of-day for
// today, day+month plus time otherwise. Takes the clock so it stays pure — the
// today-or-not comparison is a date boundary, which a self-read clock would
// make untestable around midnight.
function phaseSince(ts, nowMs) {
  const d = new Date(ts);
  const time = d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
  if (d.toDateString() === new Date(nowMs).toDateString()) return time;
  return d.toLocaleDateString([], { day: 'numeric', month: 'short' }) + ' ' + time;
}

// HEALTH is the per-stack health rollup vocabulary (ADR-0027): the status
// strings the backend emits per stack, the keys CSS matches on [data-health],
// and the values the helpers below compare against. Hoisted so the literals
// live in one place instead of being repeated across app-helpers.js/index.html.
const HEALTH = {
  HEALTHY: 'healthy',
  UNHEALTHY: 'unhealthy',
  STARTING: 'starting',
  STOPPED: 'stopped',
  UNKNOWN: 'unknown',
};

// FOLD_START_MAX_MS bounds what the health timeline treats as a routine start:
// a starting phase at most this long that settled healthy is deploy/restart
// churn and is folded away; a slower start stays a full line — a service that
// takes longer than this to come up is information, not routine.
const FOLD_START_MAX_MS = 5 * 60 * 1000;

// foldPhases groups a service's healthwatch phases (newest first, ADR-0031)
// into the display items the timeline renders. Every deploy or restart records
// a `starting → healthy` pair per service, so an uneventful history is almost
// entirely that pattern repeated — folding it is what lets an actual incident
// stand alone (see UI_SPEC "Status history"). Rules:
//   - the current phase is always its own line; a routine start it rose from
//     is absorbed into it (`startedInMs`);
//   - consecutive routine cycles (healthy + short starting) collapse into one
//     `starts` summary item — count, span, worst start, correlated commits;
//   - the settled phase directly after an incident line stays expanded: how
//     long the service was good before it broke is the incident's context;
//   - anything involving unhealthy, a stop (unless on-demand — skipper stops
//     those by design, so their idle cycles are the routine), a slow start or
//     a start that never settled stays line-by-line.
// Pure: phases + {onDemand, nowMs} in, items out. Item shapes:
//   {kind:'phase', phase, endMs, current?, startedInMs?}
//   {kind:'starts', count, since, maxStartMs, commits, idle}
function foldPhases(phases, opts) {
  const o = opts || {};
  const n = phases.length;
  const sinceMs = phases.map(function (p) {
    return new Date(p.since).getTime();
  });
  const end = function (i) {
    return i === 0 ? o.nowMs : sinceMs[i - 1];
  };
  const dur = function (i) {
    return end(i) - sinceMs[i];
  };
  // settled OK: the status a routine cycle returns to. For an on-demand
  // service `stopped` is the intended idle (ADR-0027 amendment), so its
  // cycles settle there.
  const okStatus = function (status) {
    return status === HEALTH.HEALTHY || (!!o.onDemand && status === HEALTH.STOPPED);
  };
  const ok = function (i) {
    return okStatus(phases[i].status);
  };
  // A routine start: short, and the phase it led to (its newer neighbour)
  // settled OK. A start that led anywhere else is a failed start.
  const routineStart = function (i) {
    return phases[i].status === HEALTH.STARTING && i > 0 && ok(i - 1) && dur(i) < FOLD_START_MAX_MS;
  };
  const foldable = function (i) {
    return ok(i) || routineStart(i);
  };
  const expanded = function (i) {
    // One settled phase with its routine start folded in ("up in 22s").
    return { kind: 'phase', phase: phases[i], endMs: end(i), startedInMs: dur(i + 1) };
  };

  const items = [];
  // Head: the current phase, whatever it is.
  const head = { kind: 'phase', phase: phases[0], endMs: end(0), current: true };
  let i = 1;
  if (ok(0) && n > 1 && routineStart(1)) {
    head.startedInMs = dur(1);
    i = 2;
  }
  items.push(head);

  while (i < n) {
    if (!foldable(i)) {
      items.push({ kind: 'phase', phase: phases[i], endMs: end(i) });
      i++;
      continue;
    }
    // Maximal foldable run [i, j).
    let j = i;
    while (j < n && foldable(j)) j++;
    let k = i;
    const prev = items[items.length - 1];
    const prevNotable = prev.kind === 'phase' && !okStatus(prev.phase.status);
    if (prevNotable && ok(k) && k + 1 < j && phases[k + 1].status === HEALTH.STARTING) {
      items.push(expanded(k));
      k += 2;
    }
    if (k < j) {
      const seg = [];
      for (let s = k; s < j; s++) seg.push(s);
      const startIdxs = seg.filter(function (s) {
        return phases[s].status === HEALTH.STARTING;
      });
      const stoppedCount = seg.filter(function (s) {
        return phases[s].status === HEALTH.STOPPED;
      }).length;
      if (startIdxs.length === 1 && j - k === 2 && ok(k)) {
        // A lone cycle: an expanded line says the same as a summary of one.
        items.push(expanded(k));
      } else if (startIdxs.length === 0 && !(o.onDemand && stoppedCount >= 2)) {
        // No starts to summarize (a trailing baseline phase): plain lines.
        for (let s = k; s < j; s++) {
          items.push({ kind: 'phase', phase: phases[s], endMs: end(s) });
        }
      } else {
        const commits = [];
        seg.forEach(function (s) {
          const p = phases[s];
          if (p.deploy_correlated && p.commit && commits.indexOf(p.commit) < 0)
            commits.push(p.commit);
        });
        const idle = !!o.onDemand && stoppedCount > 0;
        items.push({
          kind: 'starts',
          // Idle cycles are counted by their settled stops; deploy/restart
          // cycles by their starts.
          count: idle ? stoppedCount : startIdxs.length,
          since: phases[j - 1].since,
          maxStartMs: startIdxs.reduce(function (max, s) {
            return Math.max(max, dur(s));
          }, 0),
          commits,
          idle,
        });
      }
    }
    i = j;
  }
  return items;
}

// healthClass maps one service to the same status vocabulary as the rollup, so
// the per-service dot colour matches the pill's tier.
function healthClass(s) {
  if (s.health === HEALTH.UNHEALTHY || s.state === 'restarting' || s.state === 'dead')
    return HEALTH.UNHEALTHY;
  if (s.health === HEALTH.STARTING || s.state === 'created') return HEALTH.STARTING;
  if (s.health === HEALTH.HEALTHY || s.state === 'running') return HEALTH.HEALTHY;
  return HEALTH.STOPPED;
}

// attentionStacks distils the live health snapshot (stack -> {status,services})
// down to just the stacks that need attention — currently only 'unhealthy',
// deliberately excluding the reporting-only states (starting is transient mid-
// deploy, stopped can be intended for on-demand stacks, unknown/healthy are not
// problems). Returned sorted by name as {stack,status}, the single source the
// header health beacon and the Deploys attention band both render from. Pure and
// snapshot-driven, so it surfaces an unhealthy stack even when its newest deploy
// row has aged out of the bounded event log (where the row-bound pill cannot).
function attentionStacks(healthSnap) {
  const snap = healthSnap || {};
  return Object.keys(snap)
    .filter(function (stack) {
      return snap[stack] && snap[stack].status === HEALTH.UNHEALTHY;
    })
    .sort()
    .map(function (stack) {
      return { stack: stack, status: snap[stack].status };
    });
}

// attentionLabel is the pluralised summary of attentionStacks' count, reused as
// the beacon title, its aria-label and the popover heading, so the three never
// drift apart.
function attentionLabel(n) {
  return n === 1 ? '1 stack unhealthy' : n + ' stacks unhealthy';
}

// rosterAttentionRank floats an enabled, currently-unhealthy stack to the top
// (rank 0) so the Stacks view answers "what needs attention" at a glance — the
// inventory-view counterpart to the Deploys attention band. Legitimate there (a
// roster is inventory, not a chronological log, so reordering carries no
// meaning to lose). Same "only unhealthy floats" rule as attentionStacks.
function rosterAttentionRank(entry, healthSnap) {
  const h = (healthSnap || {})[entry.name];
  return !entry.disabled && h && h.status === HEALTH.UNHEALTHY ? 0 : 1;
}

// rosterOrdered stably sorts a copy of the roster snapshot by that rank,
// preserving the backend's enabled-first / alphabetical order within each
// group. The caller applies it only at full render (view switch / roster
// snapshot), never on a live health poll, so rows never jump out from under an
// open panel.
function rosterOrdered(rosterSnap, healthSnap) {
  return (rosterSnap || []).slice().sort(function (a, b) {
    return rosterAttentionRank(a, healthSnap) - rosterAttentionRank(b, healthSnap);
  });
}

// levelClass clamps a log level to the known set (unknown → INFO).
function levelClass(level) {
  return ['DEBUG', 'INFO', 'WARN', 'ERROR'].indexOf(level) >= 0 ? level : 'INFO';
}

// logTime shows the time of day; lines from another day get a date
// prefix. The full timestamp is always in the tooltip.
function logTime(ts) {
  const d = new Date(ts);
  const t = d.toLocaleTimeString();
  if (d.toDateString() !== new Date().toDateString()) {
    return d.toLocaleDateString() + ' ' + t;
  }
  return t;
}

// reasonFromSnap derives why a paused stack is paused when it is not in the
// queue (the queue item carries an authoritative reason of its own).
function reasonFromSnap(s) {
  if (s.effective) return '';
  if (s.overridden || s.config === false) return 'stack';
  return 'global';
}

// waitedSince renders how long a queued stack has been waiting, from its ISO
// timestamp: one coarse unit, never a compound span (the queue row has room for
// a few characters). Takes the clock so it stays pure.
function waitedSince(since, nowMs) {
  const diffS = Math.floor((nowMs - new Date(since).getTime()) / 1000);
  if (diffS < 60) return diffS + 's';
  if (diffS < 3600) return Math.floor(diffS / 60) + 'm';
  if (diffS < 86400) return Math.floor(diffS / 3600) + 'h';
  return Math.floor(diffS / 86400) + 'd';
}

// snapshotIsFresh reports whether a versioned state payload may be applied,
// given the version last applied — ordering comes from the payload, not from
// arrival time. An unversioned payload always applies: there is nothing to
// compare, and dropping it would leave the view frozen.
function snapshotIsFresh(appliedVersion, payload) {
  const v = payload && payload.version;
  if (typeof v !== 'number') return true;
  return appliedVersion === null || v >= appliedVersion;
}

// orphanMeta is the right-hand note on an orphan row: "state only" when nothing
// runs, otherwise the container count.
function orphanMeta(o) {
  if (o.state_only) return 'state only';
  const n = (o.containers || []).length;
  return n + (n === 1 ? ' container' : ' containers');
}

// orphanStateClass maps a container's docker State to the health-pill dot
// vocabulary (healthy / unhealthy / stopped).
function orphanStateClass(state) {
  if (state === 'running') return 'healthy';
  if (state === 'restarting' || state === 'dead') return 'unhealthy';
  return 'stopped';
}

// containerMatchesQuery reports whether a container matches the lowercased query
// across its fields. Empty query never matches.
function containerMatchesQuery(c, q) {
  if (!q) return false;
  return [c.name, c.service, c.image, c.ports, c.status].some(function (v) {
    return (v || '').toLowerCase().indexOf(q) !== -1;
  });
}

// orphanMatchesQuery reports whether an orphan matches the query — its project
// fields (name, working_dir, config file, volumes) or any of its containers.
function orphanMatchesQuery(o, q) {
  if (!q) return false;
  const fields = [o.project, o.working_dir, o.config_file].concat(o.volumes || []);
  if (
    fields.some(function (v) {
      return (v || '').toLowerCase().indexOf(q) !== -1;
    })
  )
    return true;
  return (o.containers || []).some(function (c) {
    return containerMatchesQuery(c, q);
  });
}

// HOST_COLOR_COUNT is how many distinct per-host identity colours the palette
// provides (ADR-0048) — the app.css `[data-host-color="N"]` slots must match
// this count per theme.
const HOST_COLOR_COUNT = 6;

// hostColorIndex assigns a host its identity-colour slot automatically and
// deterministically from its name alone: a fixed FNV-1a hash of the name,
// wrapped to the palette size. Hashing the name (not the host's position in the
// set) means a host keeps the same colour regardless of host-set order, how
// many peers exist, or which instance is the primary — "host-b is always the
// same colour" holds everywhere it appears.
function hostColorIndex(name) {
  let hash = 0x811c9dc5; // FNV-1a 32-bit offset basis
  const s = name || '';
  for (let i = 0; i < s.length; i++) {
    hash ^= s.charCodeAt(i);
    hash = Math.imul(hash, 0x01000193); // FNV prime
  }
  return (hash >>> 0) % HOST_COLOR_COUNT;
}

// hostMonogram is the short uppercase label shown in a host's row chip — the
// initials of a hyphen/underscore/dot/space-separated name (host-a -> HA), or
// the first three letters of a single-token name (argoneon -> ARG, nuc -> NUC).
// The chip's colour disambiguates hosts that share a monogram; the full name is
// always in its title/tap-tip.
function hostMonogram(name) {
  const s = (name || '').trim();
  if (!s) return '';
  const parts = s.split(/[-_. ]+/).filter(Boolean);
  if (parts.length >= 2) {
    return parts
      .slice(0, 3)
      .map(function (p) {
        return p[0];
      })
      .join('')
      .toUpperCase();
  }
  return s.slice(0, 3).toUpperCase();
}

// assignHostColors maps each host name to a palette slot (0..HOST_COLOR_COUNT-1),
// guaranteeing that no two hosts share a slot while free slots remain — distinct
// hosts must stay distinguishable at a glance until the palette is genuinely
// exhausted (a hard rule). Each host prefers its deterministic name-hash slot
// (hostColorIndex), so a host keeps its colour across sessions and unrelated
// host-set changes; when two hosts want the same slot and slots are still free,
// the one later in a fixed name order is bumped forward to the next free slot by
// linear probing. Beyond HOST_COLOR_COUNT hosts the palette is exhausted and
// colours necessarily repeat (collectWarnings flags the config before that
// point). The name order is a plain sort, so the mapping is independent of host
// order, count, and which host is the primary. Returns a name -> slot object.
function assignHostColors(names) {
  const order = (names || []).slice().sort();
  const taken = new Set();
  const colors = {};
  for (let i = 0; i < order.length; i++) {
    const name = order[i];
    let slot = hostColorIndex(name);
    if (taken.size < HOST_COLOR_COUNT) {
      // Free slots remain, so never reuse one: probe forward from the preferred
      // slot until an unused slot is found.
      while (taken.has(slot)) slot = (slot + 1) % HOST_COLOR_COUNT;
    }
    taken.add(slot);
    colors[name] = slot;
  }
  return colors;
}

// hostFilterActive reports whether the Hosts filter is narrowing the view (a
// strict subset is selected) — the signal that lights the Hosts control.
function hostFilterActive(selectedCount, totalCount) {
  return totalCount > 0 && selectedCount > 0 && selectedCount < totalCount;
}

// reconcileHostFilter resolves a saved Hosts-filter selection (loaded from
// localStorage) against the current host set, returning null ("all hosts")
// or the array of names to select. A saved host no longer present is
// dropped; if that leaves nothing, or leaves the full current set, the
// filter normalizes back to null rather than restoring a stale or
// redundant subset.
function reconcileHostFilter(savedNames, currentNames) {
  if (!savedNames || !savedNames.length) return null;
  const restored = savedNames.filter(function (n) {
    return currentNames.indexOf(n) !== -1;
  });
  if (!restored.length || restored.length === currentNames.length) return null;
  return restored;
}

// resolvePeerView returns the fanned-in PeerView for a non-self host, or null
// for the primary itself (and when no peers are configured, or the host is
// unknown). The per-host resolvers below use it to read a peer's own fanned-in
// state (ADR-0048) instead of the primary's live snapshot, so peer rows render
// the same container/health/app-link detail the primary shows for its own
// stacks.
function resolvePeerView(peersSnap, selfHost, host) {
  if (!peersSnap || !host || host === selfHost) return null;
  return (
    (peersSnap.peers || []).find(function (p) {
      return p.name === host;
    }) || null
  );
}

// peerStacksMap reads one `{stacks:{<name>:…}}` section out of a peer's
// fanned-in state, defensively: a peer running a skipper that predates the
// section carries none of it, and the safe default is an empty map — never a
// throw, and never the primary's own snapshot standing in for a peer's.
function peerStacksMap(peer, section) {
  const s = peer.state && peer.state[section];
  return (s && s.stacks) || {};
}

// resolveHealthMap / resolveHealthwatchMap / resolveAppLinksMap resolve the
// per-stack map for a host: the caller's own live snapshot for self, else the
// peer's fanned-in state. Both carry the identical `{stacks:{<name>:…}}` shape,
// so a consumer keyed by stack name works unchanged for either.
function resolveHealthMap(peersSnap, selfHost, host, selfHealth) {
  const p = resolvePeerView(peersSnap, selfHost, host);
  return p ? peerStacksMap(p, 'health') : selfHealth;
}
function resolveHealthwatchMap(peersSnap, selfHost, host, selfHealthwatch) {
  const p = resolvePeerView(peersSnap, selfHost, host);
  return p ? peerStacksMap(p, 'healthwatch') : selfHealthwatch;
}
function resolveAppLinksMap(peersSnap, selfHost, host, selfAppLinks) {
  const p = resolvePeerView(peersSnap, selfHost, host);
  return p ? peerStacksMap(p, 'app_links') : selfAppLinks;
}

// resolveUpdates picks a host's registry update-check snapshot ({stacks,
// checked_at}, ADR-0054), riding each host's own `stacks` state exactly like
// resolveRepoWebURL: the primary's live snapshot for self, else the peer's
// fanned-in one. null when the host has none — the check is disabled there,
// has not run yet, or the peer runs a skipper predating the field.
function resolveUpdates(peersSnap, selfHost, host, selfUpdates) {
  const p = resolvePeerView(peersSnap, selfHost, host);
  if (!p) return selfUpdates || null;
  return (p.state && p.state.stacks && p.state.stacks.updates) || null;
}

// resolveRepoWebURL picks the forge browse URL a host's commit SHAs link
// through. Each host tracks its own deploy repo, so a peer's SHAs must link
// through that peer's forge, never the primary's. '' — no link at all — when
// the host derived no browse URL, or runs a skipper predating the field.
function resolveRepoWebURL(peersSnap, selfHost, host, selfRepoWebURL) {
  const p = resolvePeerView(peersSnap, selfHost, host);
  return p ? (p.state && p.state.stacks && p.state.stacks.repo_web_url) || '' : selfRepoWebURL;
}

// buildHostList is the effective host set as ordered descriptors: the primary
// (self) first — always reachable, never stale — then each peer in snapshot
// order, its reachable/stale flags coerced to booleans (an older peer's record
// may omit them).
function buildHostList(peersSnap, selfHost) {
  const out = [{ name: selfHost, url: '', self: true, reachable: true, stale: false }];
  if (peersSnap) {
    (peersSnap.peers || []).forEach(function (p) {
      out.push({
        name: p.name,
        url: p.url,
        self: false,
        reachable: !!p.reachable,
        stale: !!p.stale,
      });
    });
  }
  return out;
}

// logLineVisible reports whether a log line stays visible under the in-log
// search filter (ADR-0037): an empty query shows every line, otherwise the line
// must contain the query (case-insensitive). A non-empty query that matches is
// also a "hit" the caller highlights and counts.
function logLineVisible(text, q) {
  if (!q) return true;
  return (text || '').toLowerCase().indexOf(q.toLowerCase()) !== -1;
}

// clogStreamStatus maps an EventSource readyState after an error to the status
// line the container-log panel shows. EventSource retries a dropped connection
// on its own (CONNECTING), but a non-2xx response — a 404 for a stack that went
// away, a 429 when the server is already running its maximum number of log
// follows — closes it for good (CLOSED). Reporting "reconnecting…" for that
// case would promise a retry that never comes, so a closed stream reads as
// closed and points at the way to try again.
function clogStreamStatus(readyState) {
  // 2 === EventSource.CLOSED; the constant is not available under node --test.
  if (readyState === 2) {
    return { text: 'stream closed — reopen the log to retry', cls: 'err', closed: true };
  }
  return { text: 'reconnecting…', cls: 'err', closed: false };
}

// watchedSummary phrases the change-detection panel's lead line for a roster
// entry: what skipper watches for this stack, and — the question the panel
// exists to answer — why nothing has happened for it.
//
// The "unchanged since" claim is only made after a clean deploy. A stack whose
// last outcome was failed/queued/blocked has a change *pending*, so saying
// nothing changed since then would be exactly backwards.
const WATCHED_SETTLED = ['success', 'healed'];

// UNCHANGED_SINCE opens the settled lead, immediately followed by the commit.
// watchedLeadHTML finds that commit by this prefix to turn it into a link, so
// the two must agree — hence one shared constant rather than the phrase written
// twice.
const UNCHANGED_SINCE = 'Unchanged since ';
function watchedSummary(status, commit, count, disabled) {
  if (disabled) {
    return 'Parked with disabled: true — skipper neither watches nor deploys this stack.';
  }
  if (!count) {
    return 'Nothing tracked yet — this stack has not deployed, so every input counts as changed.';
  }
  const deploys =
    count === 1
      ? 'A deploy runs when this file changes:'
      : 'A deploy runs when any of these change:';
  if (WATCHED_SETTLED.indexOf(status) !== -1) {
    // A stack's very first deploy has no prior commit to diff against, so the
    // audit record carries none — the "unchanged" fact still holds, only the
    // reference point is the deploy itself rather than a SHA.
    const since = commit ? shortSHA(commit) : 'the last deploy';
    return UNCHANGED_SINCE + since + '. ' + deploys;
  }
  return deploys;
}

// AUDIT_BAD_STATUSES are the terminal outcomes that went wrong.
const AUDIT_BAD_STATUSES = ['failed', 'rolled_back', 'rolled_back_unhealthy', 'heal_exhausted'];

// AUDIT_FOLD_MIN is the shortest run of identical routine outcomes worth
// folding: below it the summary line replaces at most two rows while hiding
// their commits, which costs more than the line it saves.
const AUDIT_FOLD_MIN = 3;

// AUDIT_FOLD_INCIDENT_MIN is the same threshold for the repeats *below* an
// incident that stays expanded. It is lower because an incident row is two
// lines — status plus its error — so collapsing even two of them is already a
// clear gain, where two routine rows are not.
const AUDIT_FOLD_INCIDENT_MIN = 2;

// foldAuditRecords groups a stack's audit records (newest first, ADR-0033)
// into the display items the deploy-history panel renders. A long-lived stack
// converges the same way for weeks, so rendered verbatim its history is one
// outcome repeated — noise that buries the deploy that went wrong. This is the
// deploy-history counterpart of foldPhases above, and follows the same rules:
//   - the newest record is always its own line;
//   - runs of AUDIT_FOLD_MIN or more consecutive identical routine outcomes
//     collapse into one summary item;
//   - the record directly below an incident stays expanded — what ran last
//     before the failure is the failure's context.
// An incident is never folded *away*: the newest of a repeated failure always
// keeps its full row, error and all. Only its identical repeats below it fold
// (same status and same error text — a different cause is information), which
// is what a retry storm looks like: the same sentence printed nine times.
// Pure: records in, items out. Item shapes:
//   {kind:'record', record}
//   {kind:'run', status, records}
function foldAuditRecords(records) {
  const recs = records || [];
  const routine = function (i) {
    return AUDIT_BAD_STATUSES.indexOf(recs[i].status) === -1;
  };
  const sameIncident = function (a, b) {
    return recs[a].status === recs[b].status && (recs[a].error || '') === (recs[b].error || '');
  };
  const items = [];
  const record = function (i) {
    items.push({ kind: 'record', record: recs[i] });
  };
  // [from, to) as one summary once it is long enough to be worth a line, and as
  // plain records otherwise.
  const foldOrList = function (from, to, min) {
    if (to - from >= min) {
      items.push({ kind: 'run', status: recs[from].status, records: recs.slice(from, to) });
      return;
    }
    for (let s = from; s < to; s++) record(s);
  };

  let afterIncident = false;
  let i = 0;
  while (i < recs.length) {
    if (!routine(i)) {
      // The incident itself, then its identical repeats as one summary.
      let j = i + 1;
      while (j < recs.length && !routine(j) && sameIncident(i, j)) j++;
      record(i);
      foldOrList(i + 1, j, AUDIT_FOLD_INCIDENT_MIN);
      afterIncident = true;
      i = j;
      continue;
    }
    // The newest record always keeps its own line.
    if (i === 0) {
      record(i);
      i++;
      continue;
    }
    // Maximal run of one routine status. The first of it stays expanded when it
    // follows an incident: it is that incident's context.
    let j = i;
    while (j < recs.length && routine(j) && recs[j].status === recs[i].status) j++;
    let k = i;
    if (afterIncident) {
      record(k);
      k++;
      afterIncident = false;
    }
    foldOrList(k, j, AUDIT_FOLD_MIN);
    i = j;
  }
  return items;
}

// deployAnnouncement builds the screen-reader phrase for a terminal deploy
// outcome, so the a11y-live region can voice what a sighted user reads off the
// row (T2.8). Returns null for non-terminal statuses (deploying/queued/blocked/
// skipped) and for a missing stack — the caller only announces a real string.
function deployAnnouncement(status, stack) {
  if (!stack) return null;
  const phrase = {
    success: 'deployed successfully',
    healed: 'self-healed',
    failed: 'deploy failed',
    rolled_back: 'deploy failed, rolled back',
    rolled_back_unhealthy: 'rolled back, still unhealthy',
    heal_exhausted: 'self-heal failed',
  }[status];
  return phrase ? stack + ' ' + phrase : null;
}

// deployStatusLabel is the header indicator's title and aria-label. It says in
// words what the trail renders as chips, so the meaning survives on mobile,
// where those labels are hidden and only the dot and count chip stay
// (UI_SPEC §Responsive).
function deployStatusLabel(active, up) {
  if (active.length === 0) return 'idle';
  return 'deploying ' + active.join(', ') + (up.length ? ' · next ' + up.join(', ') : '');
}

// Dual-use export: in the browser this file loads as a plain <script>, so the
// functions above are already globals and `module` is undefined — the export is
// skipped. Under `node --test` `module` exists, so the helpers are exported for
// import. No bundler, no build step.
if (typeof module !== 'undefined' && module.exports) {
  // Some of these are internal to this file (parseImageRef, imageRepoName,
  // HOST_COLOR_COUNT, hostColorIndex — each used only by another helper here).
  // They are exported anyway so the unit layer can exercise them directly:
  // testing a fiddly reference parser through its callers only would leave its
  // own edge cases to inference.
  module.exports = {
    formatDuration,
    makeReconnector,
    RECONNECT_BASE_DELAY_MS,
    RECONNECT_MAX_DELAY_MS,
    OFFLINE_AFTER_FAILURES,
    formatTime,
    fullTime,
    classifyDiffLine,
    shortSHA,
    commitURL,
    parseImageRef,
    shortImageTag,
    imageDelta,
    imageRepoName,
    rosterVersion,
    statusText,
    statusIcon,
    auditStatusLabel,
    auditCountText,
    phaseDuration,
    phaseSince,
    HEALTH,
    FOLD_START_MAX_MS,
    foldPhases,
    AUDIT_BAD_STATUSES,
    AUDIT_FOLD_MIN,
    AUDIT_FOLD_INCIDENT_MIN,
    foldAuditRecords,
    healthClass,
    attentionStacks,
    attentionLabel,
    rosterAttentionRank,
    rosterOrdered,
    resolvePeerView,
    resolveHealthMap,
    resolveHealthwatchMap,
    resolveAppLinksMap,
    resolveRepoWebURL,
    resolveUpdates,
    buildHostList,
    levelClass,
    logLineLevel,
    rowClass,
    logTime,
    reasonFromSnap,
    waitedSince,
    snapshotIsFresh,
    orphanMeta,
    orphanStateClass,
    containerMatchesQuery,
    orphanMatchesQuery,
    logLineVisible,
    clogStreamStatus,
    watchedSummary,
    UNCHANGED_SINCE,
    deployAnnouncement,
    deployStatusLabel,
    HOST_COLOR_COUNT,
    hostColorIndex,
    hostMonogram,
    assignHostColors,
    hostFilterActive,
    reconcileHostFilter,
    LOG_SEVERITY_FILTERS,
    DEFAULT_LOG_FILTERS,
    logNarrative,
    logKind,
    logFacets,
    logMatchesFilters,
    logQuickVisible,
    logFiltersActive,
    parseLogFilters,
    isLogOutcome,
    logFilterSeverity,
    mergeLogView,
    INCIDENT_WINDOW_MS,
    recentIncidentCount,
    incidentBadgeLabel,
    incidentPresetActive,
  };
}
