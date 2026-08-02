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

// RECONNECT_BASE_DELAY_MS is where a stream's manual retry backoff starts, and
// RECONNECT_MAX_DELAY_MS is its cap. EventSource retries a transient drop by
// itself, but gives up for good on a fatal error (non-2xx, bad content-type),
// which is what these retries are for.
const RECONNECT_BASE_DELAY_MS = 1000;
const RECONNECT_MAX_DELAY_MS = 30000;

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
  return {
    schedule: function () {
      if (timer !== null) return; // a retry is already pending
      timer = setTimer(function () {
        timer = null;
        connect();
      }, delay);
      delay = Math.min(delay * 2, RECONNECT_MAX_DELAY_MS);
    },
    reset: function () {
      delay = RECONNECT_BASE_DELAY_MS;
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
  };
}
