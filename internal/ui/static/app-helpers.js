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
  if (line.startsWith('+++') || line.startsWith('---') || line.startsWith('diff ') || line.startsWith('index ')) return 'diff-meta';
  if (line.startsWith('@@')) return 'diff-hunk';
  if (line.startsWith('+')) return 'diff-add';
  if (line.startsWith('-')) return 'diff-del';
  return '';
}

// shortSHA truncates a commit SHA to its 7-char short form.
function shortSHA(sha) { return (sha || '').slice(0, 7); }

// statusText renders a deploy status as the short label shown on the diff
// panel's echo pill (mirrors badge wording without the stacked layout).
function statusText(status) {
  if (status === 'rolled_back') return 'rolled back';
  if (status === 'rolled_back_unhealthy') return 'rolled back · unhealthy';
  if (status === 'heal_exhausted') return 'self-heal · failed';
  return status || '';
}

// auditStatusLabel spells the two stacked-badge statuses on one line — the
// history rows are compact, so the two-line badge wording is flattened here.
function auditStatusLabel(status) {
  if (status === 'rolled_back') return 'rolled back';
  if (status === 'rolled_back_unhealthy') return 'rolled back · unhealthy';
  if (status === 'heal_exhausted') return 'self-heal failed';
  return status;
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
// today, day+month plus time otherwise.
function phaseSince(ts) {
  const d = new Date(ts);
  const time = d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
  if (d.toDateString() === new Date().toDateString()) return time;
  return d.toLocaleDateString([], { day: 'numeric', month: 'short' }) + ' ' + time;
}

// healthClass maps one service to the same status vocabulary as the rollup, so
// the per-service dot colour matches the pill's tier.
function healthClass(s) {
  if (s.health === 'unhealthy' || s.state === 'restarting' || s.state === 'dead') return 'unhealthy';
  if (s.health === 'starting' || s.state === 'created') return 'starting';
  if (s.health === 'healthy' || s.state === 'running') return 'healthy';
  return 'stopped';
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
  if (fields.some(function (v) { return (v || '').toLowerCase().indexOf(q) !== -1; })) return true;
  return (o.containers || []).some(function (c) { return containerMatchesQuery(c, q); });
}

// logLineVisible reports whether a log line stays visible under the in-log
// search filter (ADR-0037): an empty query shows every line, otherwise the line
// must contain the query (case-insensitive). A non-empty query that matches is
// also a "hit" the caller highlights and counts.
function logLineVisible(text, q) {
  if (!q) return true;
  return (text || '').toLowerCase().indexOf(q.toLowerCase()) !== -1;
}

// Dual-use export: in the browser this file loads as a plain <script>, so the
// functions above are already globals and `module` is undefined — the export is
// skipped. Under `node --test` `module` exists, so the helpers are exported for
// import. No bundler, no build step.
if (typeof module !== 'undefined' && module.exports) {
  module.exports = {
    formatDuration, formatTime, fullTime, classifyDiffLine, shortSHA,
    statusText, auditStatusLabel, phaseDuration, phaseSince, healthClass,
    levelClass, logTime, reasonFromSnap, orphanMeta, orphanStateClass,
    containerMatchesQuery, orphanMatchesQuery, logLineVisible,
  };
}
