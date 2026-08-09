// skipper-cd UI render layer — pure HTML-string builders shared by the app
// shell and exercised in isolation by the unit layer (app-render.test.js).
//
// Like app-helpers.js this file is embedded and served same-origin
// (GET /app-render.js), loaded after app-helpers.js (whose functions it calls
// by bare name) and before app.js; in the browser its functions become globals,
// under `node --test` it exports them — no bundler, no build step (ADR-0035).
//
// Keep every function pure: input in, HTML string out — no DOM reads, no
// module-scope state. A renderer that needs app state takes it as a parameter
// (renderCommitHead's repoBase); that is what lets it run under node.

// Under node the app-helpers functions are not globals yet — pull them in and
// make them so, mirroring how the browser sees them. Guarded: in the browser
// `module` is undefined and app-helpers.js has already installed the globals.
if (typeof module !== 'undefined' && module.exports) {
  Object.assign(globalThis, require('./app-helpers.js'));
}

// escapeHtml escapes text for interpolation into an HTML template: the same
// three characters (& < >) the DOM's text-node serialization escapes. Takes
// anything stringable; null/undefined render as ''.
function escapeHtml(s) {
  return String(s == null ? '' : s)
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;');
}

// escapeAttr escapes for use inside a double-quoted HTML attribute:
// like escapeHtml, but quotes must be encoded too.
function escapeAttr(s) {
  return escapeHtml(s).replace(/"/g, '&quot;');
}

// commitLinkHTML renders one commit SHA — the single treatment behind every
// SHA the UI shows (roster, deploy history, diff header, health timeline,
// peer detail), so they all link the same way or not at all. Given the forge
// base it emits an <a> to that commit's page opened in a new tab (the UI is a
// live dashboard — navigating away would drop the stream); without one it
// emits the plain element the UI showed before links existed, so a repo_url
// no browse URL can be derived from degrades to the previous rendering.
// The element keeps its caller's class and only gains `sha-link` when it is
// actually a link, so the inert case cannot pick up a link's affordance.
// opts: {cls, base, title, testid}. The label is always the short SHA — every
// site prints that form; only the href carries the full one.
function commitLinkHTML(sha, opts) {
  const o = opts || {};
  const label = shortSHA(sha);
  const href = commitURL(o.base, sha);
  const cls = href ? (o.cls ? o.cls + ' ' : '') + 'sha-link' : o.cls || '';
  const attrs =
    (cls ? ` class="${escapeAttr(cls)}"` : '') +
    (o.testid ? ` data-testid="${escapeAttr(o.testid)}"` : '') +
    (o.title ? ` title="${escapeAttr(o.title)}"` : '');
  if (!href) return `<span${attrs}>${escapeHtml(label)}</span>`;
  return `<a${attrs} href="${escapeAttr(href)}" target="_blank" rel="noopener noreferrer">${escapeHtml(label)}</a>`;
}

// versionChipHTML is THE version chip — every version in the UI is rendered
// with it, so one reads the same wherever it appears: a Deploys row's change
// (old→new), a Stacks row's lead version, a containers-panel line. Callers
// supply the version tokens (body) plus the accessible phrasing; the chip owns
// the frame and the service label. An empty service drops that label, for a
// caller whose surrounding line already names the service.
function versionChipHTML(service, body, aria, title) {
  const label = service ? `<span class="td-svc">${escapeHtml(service)}</span>` : '';
  return `<span class="tag-delta" role="img" aria-label="${escapeAttr(aria)}" title="${escapeAttr(title)}">${label}${body}</span>`;
}

// imageDeltaHTML renders the per-service image change(s) a deploy carried as
// `service old→new` chips for the row's Version column — one per line, so the
// versions line up down the table and which service updated (and to what) reads
// at a glance without opening the diff. The data rides the event
// (image_changes); only services whose image ref actually changed are listed.
// Each chip names its service (a stack often has several), then the change: an
// empty Old is a service's first image (nothing on the left); an empty New is a
// service removed from the stack. Every changed service is listed — a deploy
// that touched five services says so, rather than hiding the rest behind a
// count the reader would have to open the diff to resolve.
function imageDeltaHTML(changes) {
  if (!changes || changes.length === 0) return '';
  const shown = changes
    .map(function (c) {
      // Each chip carries a role=img + aria-label so a screen reader announces
      // the change as one phrase ("web updated from 1.25 to 1.26"), and a title
      // with the full old→new reference (registry + repo + digest are dropped
      // from the visible chip but kept here — progressive disclosure).
      let body, aria;
      if (!c.old) {
        // First image for this service — nothing to compare against.
        body = `<span class="td-new">${escapeHtml(shortImageTag(c.new))}</span>`;
        aria = `${c.service} set to ${c.new}`;
      } else if (!c.new) {
        // Service removed from the stack.
        body = `<span class="td-old">${escapeHtml(shortImageTag(c.old))}</span><span class="td-arr" aria-hidden="true">→</span><span class="td-gone">removed</span>`;
        aria = `${c.service} removed (was ${c.old})`;
      } else {
        const d = imageDelta(c.old, c.new);
        if (d.tag) {
          // Same tag, only the pinned digest moved: a rebuild. Two hex digests
          // are near-impossible to eyeball, so show a ↻ rebuilt marker with the
          // shared tag; the full digests live in the title/aria.
          body = `<span class="td-ctx">${escapeHtml(d.tag)}</span><span class="td-rebuilt" aria-hidden="true">↻</span>`;
          aria = `${c.service} rebuilt, tag ${d.tag} unchanged`;
        } else {
          // A tag bump: show the tags that differ.
          body = `<span class="td-old">${escapeHtml(d.from)}</span><span class="td-arr" aria-hidden="true">→</span><span class="td-new">${escapeHtml(d.to)}</span>`;
          aria = `${c.service} updated from ${d.from} to ${d.to}`;
        }
      }
      const title = `${c.service}: ${c.old || '(first image)'} → ${c.new || '(removed)'}`;
      return versionChipHTML(c.service, body, aria, title);
    })
    .join('');
  return `<span class="svc-delta" data-testid="svc-delta">${shown}</span>`;
}

const personGlyph =
  '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="8" r="4"/><path d="M4 21a8 8 0 0 1 16 0"/></svg>';
const clockGlyph =
  '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="9"/><path d="M12 7v5l3.5 2"/></svg>';

// renderCommitHead builds the diff panel's header: an optional row-echo
// (stack + status, present only when opened from a deploy row — meta set) and,
// when commit metadata is available, the newest commit's message/author/time
// plus a collapsible list for a multi-commit range. Returns '' when there is
// nothing to show. repoBase is the forge browse URL the SHA chips link
// through; '' renders them inert.
function renderCommitHead(commits, meta, repoBase) {
  // shaHTML renders one `.m-sha` chip of this header.
  function shaHTML(sha, testid) {
    return commitLinkHTML(sha, { cls: 'm-sha', base: repoBase, title: sha, testid: testid });
  }
  const hasEcho = !!(meta && meta.stack);
  const hasCommits = !!(commits && commits.length);
  if (!hasEcho && !hasCommits) return '';
  let html = '<div class="diff-head" data-testid="diff-head">';
  if (hasEcho) {
    html +=
      `<div class="diff-head-echo">` +
      `<span class="dh-who">${escapeHtml(meta.stack)}</span>` +
      `<span class="dh-label">deploy diff</span>` +
      `<span class="dh-pill"><span class="hdot"></span>${escapeHtml(statusText(meta.status))}</span>` +
      `</div>`;
  }
  if (hasCommits) {
    const head = commits[0];
    const multi = commits.length > 1;
    html +=
      `<div class="diff-commit">` +
      `<svg class="commit-glyph" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="3.2"/><path d="M12 3v5.8M12 15.2V21"/></svg>` +
      `<span class="dh-subject">${escapeHtml(head.subject || '')}</span></div>`;
    let line = '<div class="diff-meta-line">';
    if (multi) {
      const oldest = commits[commits.length - 1];
      line +=
        `<span class="m-range">${shaHTML(oldest.sha)}<span class="m-arr">→</span>${shaHTML(head.sha)}</span>` +
        `<span class="m-sep">·</span>` +
        `<button class="commits-pill" data-testid="commits-pill">${commits.length} commits</button>` +
        `<span class="m-sep">·</span>`;
    }
    line += `<span class="m-item">${personGlyph}${escapeHtml(head.author || '')}</span>`;
    if (head.date) {
      line += `<span class="m-sep">·</span><span class="m-item" data-testid="commit-time" title="${escapeAttr(fullTime(head.date))}">${clockGlyph}${escapeHtml(formatTime(head.date))}</span>`;
    }
    if (!multi) {
      line += `<span class="m-sep">·</span>` + shaHTML(head.sha, 'commit-sha');
    }
    line += '</div>';
    html += line;
    if (multi) {
      html +=
        `<ul class="diff-commit-list" data-testid="diff-commit-list">` +
        commits
          .map(function (c) {
            return (
              `<li>${shaHTML(c.sha)}<span>` +
              `<span class="cl-subject">${escapeHtml(c.subject || '')}</span> ` +
              `<span class="cl-meta">— ${escapeHtml(c.author || '')}${c.date ? ', ' + escapeHtml(formatTime(c.date)) : ''}</span></span></li>`
            );
          })
          .join('') +
        `</ul>`;
    }
  }
  html += '</div>';
  return html;
}

function badgeHTML(status) {
  const cls = `badge badge-${status}`;
  // deploying keeps its animated spinner as the leading glyph; every other
  // status gets an icon from statusIcon (T3.14).
  if (status === 'deploying')
    return `<span class="${cls}" data-testid="status-badge"><span class="spinner"></span>deploying</span>`;
  const icon = statusIcon(status);
  if (status === 'rolled_back_unhealthy') {
    // The worst terminal state: a warning icon on a solid danger fill (T3.14)
    // — the loudest chip in the status column, not the smallest. The label
    // still stacks two lines (one line overflows the column), wrapped in
    // .badge-lbl so the badge stays a row for the leading icon.
    return `<span class="${cls}" data-testid="status-badge">${icon}<span class="badge-lbl"><span>rolled back</span><span>unhealthy</span></span></span>`;
  }
  if (status === 'heal_exhausted') {
    // Same solid two-line treatment as rolled_back_unhealthy (ADR-0029, T3.14).
    return `<span class="${cls}" data-testid="status-badge">${icon}<span class="badge-lbl"><span>self-heal</span><span>failed</span></span></span>`;
  }
  const label = status === 'rolled_back' ? 'rolled back' : status;
  return `<span class="${cls}" data-testid="status-badge">${icon}${label}</span>`;
}

// serviceVersionHTML is the same chip in its current-value mode: one token, no
// arrow, and deliberately neutral \u2014 the add-green in a delta chip means "this
// is the new version", while a running version is a fact, not a change. The
// title carries the full reference the visible token drops (registry, repo,
// digest). Pass labelled=false in the containers panel, whose line already
// names the service in its own column (the aria/title still name it).
//
// upd, when set, is the service's registry update-check result (ADR-0054,
// {latest, rebuilt}): the chip gains an amber \u21e1 token \u2014 the newer same-shape
// tag, or "rebuilt" when the running tag itself was republished. Queued amber
// on purpose: the app's colour for "waiting to be applied", which an available
// update is; applying it stays a git commit.
function serviceVersionHTML(service, image, labelled, upd) {
  const tag = shortImageTag(image);
  let body = `<span class="td-cur">${escapeHtml(tag)}</span>`;
  let aria = `${service} running ${tag}`;
  let title = `${service}: ${image}`;
  if (upd && upd.latest) {
    body += `<span class="td-upd"><span aria-hidden="true">\u21e1</span>${escapeHtml(upd.latest)}</span>`;
    aria = `${service} running ${tag}, ${upd.latest} available upstream`;
    title += ` \u2014 upstream ${upd.latest} available`;
  } else if (upd && upd.rebuilt) {
    body += `<span class="td-upd"><span aria-hidden="true">\u21e1</span>rebuilt</span>`;
    aria = `${service} running ${tag}, image rebuilt upstream`;
    title += ` \u2014 tag ${tag} was rebuilt upstream (digest moved)`;
  }
  return versionChipHTML(labelled === false ? '' : service, body, aria, title);
}

// updateCheckMetaHTML is the containers-panel head's update summary (ADR-0054):
// how many of the stack's services have an available update and how fresh the
// registry check is. Empty when nothing is advertised \u2014 the panel head stays
// as quiet as before the feature existed. Takes the clock (nowMs) so it stays
// pure and unit-testable.
function updateCheckMetaHTML(count, checkedAt, nowMs) {
  if (!count) return '';
  const age = checkedAt ? phaseDuration(nowMs - new Date(checkedAt).getTime()) : '';
  const freshness = age ? ` \u00b7 checked ${age} ago` : '';
  return (
    `<span class="hp-head-check" data-testid="update-check-meta" ` +
    `title="read-only registry check \u2014 applying an update stays a git commit">` +
    `<span class="hc-count">\u21e1 ${count} update${count === 1 ? '' : 's'}</span>${escapeHtml(freshness)}</span>`
  );
}

function filesHTML(files) {
  if (!files || files.length === 0) return '<span class="cell-duration">\u2014</span>';
  // escapeAttr also encodes quotes - JSON is full of them and this lands
  // inside a double-quoted attribute.
  const encoded = escapeAttr(JSON.stringify(files));
  return (
    `<button class="files-pill" data-testid="files-pill" data-files="${encoded}">` +
    `<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5"><path d="M2 4h5l2 2h5v7H2z"/></svg>` +
    `${files.length} file${files.length > 1 ? 's' : ''}</button>`
  );
}

// healPillHTML renders the self-heal "badge" that stands in for the files pill
// on a healed row (a heal has no changed files). Clicking it expands a panel
// explaining the corrective redeploy and listing the services that had
// drifted (ADR-0029). The drift rides the event, so it is stashed on the pill.
function healPillHTML(drift) {
  const encoded = escapeAttr(JSON.stringify(drift || []));
  return (
    `<button class="heal-pill" data-testid="heal-pill" data-drift="${encoded}">` +
    `<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5"><path d="M8 3v10M3 8h10"/></svg>` +
    `self-heal</button>`
  );
}

// FOLD_COMMIT_CHIPS_MAX caps the commit chips on a folded-cycles summary line:
// beyond a handful the chips outgrow the line they annotate, so the rest is a
// `+N` counter and the raw list behind the toggle carries them per phase.
const FOLD_COMMIT_CHIPS_MAX = 3;

// healthStripHTML renders one service's phase history as a segmented bar,
// oldest → newest — the same reading direction as the roster's outcome strip,
// whose at-a-glance job it mirrors for health. Segment width is the phase's
// duration (flex-grow in seconds; CSS min-width keeps a short start visible as
// a sliver), colour is the status, and the title carries the full phase line.
// Decoration over the timeline below, which holds the same data as text — so
// the strip is aria-hidden, like the outcome strip's dots.
function healthStripHTML(phases, nowMs) {
  if (!phases || phases.length < 2) return '';
  let html = '<div class="hp-strip" data-testid="health-strip" aria-hidden="true">';
  for (let i = phases.length - 1; i >= 0; i--) {
    const end = i === 0 ? nowMs : new Date(phases[i - 1].since).getTime();
    const ms = end - new Date(phases[i].since).getTime();
    const dur = phaseDuration(ms);
    const title = `${phases[i].status} · ${phaseSince(phases[i].since, nowMs)} · ${i === 0 ? 'for ' + dur : dur}`;
    html += `<span data-health="${escapeAttr(phases[i].status)}" style="flex-grow:${Math.max(1, Math.round(ms / 1000))}" title="${escapeAttr(title)}"></span>`;
  }
  return html + '</div>';
}

// healthHistoryHTML renders one service's status timeline from the
// healthwatch snapshot (ADR-0031): the strip above, then the folded phase
// items (foldPhases, app-helpers.js) — routine deploy/restart churn collapsed
// into the settled lines and a summary, incidents kept line-by-line with their
// start, how long they held, and the deploy commit when correlated. When
// folding collapsed anything, the raw newest-first list stays reachable behind
// a toggle (`.hp-fold-toggle`, handled in app.js). Returns '' when the
// watchdog is off — and for a service with only its baseline phase, where a
// one-line timeline would just repeat the inline age. The caller supplies the
// service's phases, the forge base for the commit chips (repoBase — the
// timeline may belong to a peer), the clock (nowMs) and opts: {onDemand} folds
// idle cycles too (skipper stops those containers by design), {id} is a
// page-unique slug the toggle's aria-controls points at.
function healthHistoryHTML(phases, repoBase, nowMs, opts) {
  if (!phases || phases.length < 2) return '';
  const o = opts || {};

  // One phase as a timeline line — the shape the timeline has always used,
  // plus the folded "up in Xs" when a routine start was absorbed into it.
  // Raw-list lines drop the testid so counting testids never sees a phase twice.
  const phaseLine = function (p, endMs, current, startedInMs, withTestid) {
    const dur = phaseDuration(endMs - new Date(p.since).getTime());
    return (
      `<div class="hp-phase"${withTestid ? ' data-testid="health-phase"' : ''} data-health="${escapeAttr(p.status)}">` +
      `<span class="hdot"></span>` +
      `<span class="hp-pstatus">${escapeHtml(p.status)}</span>` +
      `<span>${escapeHtml(phaseSince(p.since, nowMs))}</span>` +
      `<span>${escapeHtml(current ? 'for ' + dur : dur)}</span>` +
      (startedInMs !== undefined
        ? `<span class="hp-upin">· up in ${escapeHtml(phaseDuration(startedInMs))}</span>`
        : '') +
      (p.deploy_correlated && p.commit
        ? commitLinkHTML(p.commit, {
            cls: 'hp-commit',
            base: repoBase,
            testid: withTestid ? 'health-phase-commit' : undefined,
            title: 'deployed just before this phase began',
          })
        : '') +
      `</div>`
    );
  };

  // The summary of a run of routine cycles: count, covered span, worst start,
  // and the deploys that landed inside it as commit chips (capped).
  const startsLine = function (it) {
    const noun = it.idle
      ? it.count === 1
        ? 'idle cycle'
        : 'idle cycles'
      : it.count === 1
        ? 'more start'
        : 'more starts';
    const up =
      it.maxStartMs > 0
        ? ` · up in ${it.count === 1 ? '' : '≤'}${phaseDuration(it.maxStartMs)}`
        : '';
    const rest = it.commits.length - FOLD_COMMIT_CHIPS_MAX;
    const chips =
      it.commits
        .slice(0, FOLD_COMMIT_CHIPS_MAX)
        .map(function (sha) {
          return commitLinkHTML(sha, {
            cls: 'hp-commit',
            base: repoBase,
            testid: 'health-fold-commit',
            title: 'deployed at one of these starts',
          });
        })
        .join('') + (rest > 0 ? `<span class="hp-commit">+${rest}</span>` : '');
    return (
      `<div class="hp-phase hp-fold" data-testid="health-fold" title="routine cycles, folded — each start settled within ${escapeAttr(phaseDuration(FOLD_START_MAX_MS))}">` +
      `<span class="hp-fold-glyph">${it.idle ? '⏾' : '↻'}</span>` +
      `<span><span class="hp-count">${it.count}</span> ${noun} since ${escapeHtml(phaseSince(it.since, nowMs))}${escapeHtml(up)}</span>` +
      chips +
      `</div>`
    );
  };

  const items = foldPhases(phases, { onDemand: o.onDemand, nowMs });
  const folded = items
    .map(function (it) {
      if (it.kind === 'starts') return startsLine(it);
      return phaseLine(it.phase, it.endMs, !!it.current, it.startedInMs, true);
    })
    .join('');

  let html =
    '<div class="hp-history" data-testid="health-history">' +
    healthStripHTML(phases, nowMs) +
    '<div class="hp-folded">' +
    folded +
    '</div>';
  const collapsedAny = items.some(function (it) {
    return it.kind === 'starts' || it.startedInMs !== undefined;
  });
  if (collapsedAny) {
    // aria-controls names the region the toggle reveals, so a screen reader
    // follows the swap; it needs a page-unique id, which only the caller can
    // supply (opts.id — host + stack + service). Omitted when it cannot.
    const rawID = o.id ? `hp-raw-${o.id}` : '';
    const controls = rawID ? ` aria-controls="${escapeAttr(rawID)}"` : '';
    html +=
      `<button class="hp-fold-toggle" type="button" data-testid="health-fold-toggle" aria-expanded="false"${controls} data-label="all ${phases.length} phases" data-fold-label="fold routine cycles">all ${phases.length} phases</button>` +
      `<div class="hp-raw"${rawID ? ` id="${escapeAttr(rawID)}"` : ''}>` +
      phases
        .map(function (p, i) {
          const end = i === 0 ? nowMs : new Date(phases[i - 1].since).getTime();
          return phaseLine(p, end, i === 0, undefined, false);
        })
        .join('') +
      '</div>';
  }
  return html + '</div>';
}

// ── Row-affordance widgets ──
// The small per-row buttons and chips shared by the Deploys feed, the roster
// and peer rows. Pure string builders; the DOM-node wrappers (clogButton,
// hooksBadgeButton) and all click handling stay in app.js.

// clogBtnHTML builds the logs icon that opens a container log (ADR-0037), used
// from templates (health-panel lines, roster rows). host is optional: a peer's
// name tags the button with data-clog-host so clog.toggle streams through the
// primary's peer proxy (ADR-0048) instead of the local container-logs endpoint.
const CLOG_ICON =
  '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M5 6h14M5 11h14M5 16h9"/><circle cx="17" cy="16" r="1.4" fill="currentColor" stroke="none"/></svg>';
function clogBtnHTML(stack, service, host) {
  const label = service
    ? 'logs for ' + stack + ' / ' + service
    : 'logs for ' + stack + ' (all services)';
  const hostAttr = host ? ` data-clog-host="${escapeAttr(host)}"` : '';
  return `<button class="clog-btn" type="button" data-testid="clog-btn" data-taptip data-clog-stack="${escapeAttr(stack)}" data-clog-service="${escapeAttr(service || '')}"${hostAttr} title="${escapeAttr(label)}" aria-label="${escapeAttr(label)}">${CLOG_ICON}</button>`;
}

// healthPillHTML is the string form of the live-health pill (ADR-0027) — the
// same markup updateStackAffordances builds imperatively for a local Deploys
// row, so peer rows and roster rows show an identical at-a-glance pill. A click
// opens the row's containers panel (routed per row type in the click handlers).
function healthPillHTML(stack, status) {
  return (
    '<button class="health-pill" type="button" data-testid="health-pill" data-health="' +
    escapeAttr(status) +
    '" data-stack="' +
    escapeAttr(stack) +
    '" title="' +
    escapeAttr(stack + ' — ' + status) +
    '"><span class="hdot"></span><span class="hlabel">' +
    escapeHtml(status) +
    '</span></button>'
  );
}

// Fishing hook — distinct from the container-logs icon it sits beside.
const HOOK_ICON =
  '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M15 5v7a4 4 0 0 1-8 0"/><path d="M4.5 10.5 7 13l2.5-2.5"/></svg>';
function hookCount(hooks) {
  return (hooks.pre_deploy || []).length + (hooks.post_deploy || []).length;
}
// hooksBadgeHTML is the deploy-hooks affordance (ADR-0038) on a stack's row.
function hooksBadgeHTML(stack, hooks) {
  const pre = (hooks.pre_deploy || []).length,
    post = (hooks.post_deploy || []).length;
  const label = `pre-deploy hook: ${pre}\npost-deploy hook: ${post}`;
  // "2+1" rather than the sum, so the split is visible.
  return `<button class="hooks-badge" type="button" data-testid="hooks-badge" data-taptip data-hooks-stack="${escapeAttr(stack)}" title="${escapeAttr(label)}" aria-label="${escapeAttr(label)}">${HOOK_ICON}<span class="hk-count">${pre}+${post}</span></button>`;
}

// jumpBtnHTML renders the cross-view navigation affordance next to a stack
// name: a small button that switches to targetView and scrolls to that
// stack's row there (see jumpToStack). Always rendered — whether a landing
// row actually exists depends on live data (e.g. a stack with no deploy
// history yet has no Deploys-view row), so jumpToStack degrades to a plain
// view switch when it finds nothing to land on.
function jumpBtnHTML(targetView, stack) {
  const label = targetView === 'stacks' ? 'View in Stacks' : 'View in Deploys';
  return (
    `<button type="button" class="jump-btn" data-testid="jump-btn" data-taptip data-jump-view="${targetView}" data-jump-stack="${escapeAttr(stack)}" title="${escapeAttr(label)}" aria-label="${escapeAttr(label + ': ' + stack)}">` +
    '<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" aria-hidden="true"><circle cx="8" cy="8" r="6"/><path d="M10.3 5.7l-1.4 3.2-3.2 1.4 1.4-3.2z" stroke-linejoin="round"/></svg>' +
    '</button>'
  );
}

// rowActionClusterHTML groups a stack cell's affordance glyphs (jump, app
// link, container logs, hooks) into one non-wrapping box. The cell itself
// wraps on a narrow screen, and without the box each glyph is its own flex
// item: the row broke *between* icons, leaving one stranded on a second line.
// As a cluster the affordances move to the next line whole. Empty when the row
// has none, so the cell's gap adds no phantom space after the name.
function rowActionClusterHTML(...parts) {
  const inner = parts.filter(Boolean).join('');
  return inner ? `<span class="row-actions">${inner}</span>` : '';
}

// pendingTagHTML renders the tag on a pending deploy row. Queued rows show
// "paused[: reason]"; blocked rows (ADR-0032) show the dependency reason
// ("blocked by <dep>") directly. The caller draws reason from the queue
// snapshot; it embeds a stack name — repo-controlled in stack-discovery mode
// (ADR-0034) — so it must render as text, never as markup.
function pendingTagHTML(status, reason) {
  if (status === 'blocked') {
    return `<span class="paused-tag">${escapeHtml(reason || 'blocked')}</span>`;
  }
  return `<span class="paused-tag">${reason ? 'paused: ' + escapeHtml(reason) : 'paused'}</span>`;
}

// hostChipHTML renders a row's leading host-identity chip inside the stack
// cell — a colour-tinted monogram for the row's host (a labelled chip, not a
// dot: a dot already means deploy status). Empty (and hidden by CSS) on a
// single-host instance; CSS shows it only when more than one host is in view.
// The caller supplies the host's colour slot from the live assignment
// (app.js hostChip wraps that lookup).
function hostChipHTML(hostName, slot) {
  if (!hostName) return '';
  const attr = slot === undefined ? '' : ' data-host-color="' + slot + '"';
  // title → native hover tooltip; data-taptip → the tap-tip bubble on touch,
  // so the full hostname appears whether the chip is hovered or tapped.
  // role/tabindex/aria-label make the quick-filter chip keyboard-operable
  // (T2.10) — Enter/Space fire the same toggle as a click (hostChipKeydown).
  return (
    '<span class="host-mono" role="button" tabindex="0" aria-label="Filter view to host ' +
    escapeAttr(hostName) +
    '"' +
    attr +
    ' data-taptip title="' +
    escapeAttr(hostName) +
    '">' +
    escapeHtml(hostMonogram(hostName)) +
    '</span>'
  );
}

// ── Roster cell renderers ──
// The Stacks-view row cells shared by local and peer rows. Pure string
// builders: the caller resolves the per-host state each one reads (the
// stack's discovered app-link hostnames, its live-health entry, whether a
// deploy is in flight) and passes it in.

const LINK_ICON =
  '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M14 4h6v6M10 14L20 4M18 13v6a1 1 0 01-1 1H5a1 1 0 01-1-1V6a1 1 0 011-1h6"/></svg>';

// linkCellHTML renders the app-link icon for a stack: nothing when no host
// was discovered, a plain external link for exactly one, or a button that
// opens a popover listing each when there are several. hosts is the stack's
// discovered hostnames (app.js linkCell wraps the per-host lookup).
function linkCellHTML(hosts) {
  if (!hosts || !hosts.length) return '';
  if (hosts.length === 1) {
    const url = 'https://' + hosts[0];
    return `<span class="link-wrap"><a class="link-btn" data-testid="app-link-btn" data-taptip href="${escapeAttr(url)}" target="_blank" rel="noopener" title="${escapeAttr('Open ' + hosts[0])}">${LINK_ICON}</a></span>`;
  }
  const items = hosts
    .map(function (h) {
      return `<a href="${escapeAttr('https://' + h)}" target="_blank" rel="noopener">${LINK_ICON}${escapeHtml(h)}</a>`;
    })
    .join('');
  const label = hosts.length + ' app links';
  return `<span class="link-wrap"><button class="link-btn" type="button" data-testid="app-link-btn" data-taptip title="${escapeAttr(label)}" aria-label="${escapeAttr(label)}">${LINK_ICON}</button><div class="link-pop" data-testid="app-link-pop">${items}</div></span>`;
}

// rosterRowActionsHTML surfaces an enabled roster row's secondary actions
// inline in the stack cell, beside the jump + app-link icons: the container-
// logs icon (ADR-0037) always, the deploy-hooks badge (ADR-0038) only when the
// stack declares hooks. Formerly folded behind the same ⋯ overflow menu the
// Deploys row uses (T3.13b), but on the roster the row-body click already opens
// the health + history panel, so the menu usually wrapped a single action
// (logs) — an extra click for no density gain. Disabled stacks carry neither.
function rosterRowActionsHTML(entry) {
  if (entry.disabled) return '';
  let html = clogBtnHTML(entry.name, ''); // container logs
  if (entry.hooks && hookCount(entry.hooks) > 0) html += hooksBadgeHTML(entry.name, entry.hooks);
  return html;
}

// rosterVersionInnerHTML fills a Stacks row's Version cell from the live health
// snapshot: the running version of the service the stack is named after, plus a
// muted "+N" for the rest — the glance. The full per-service list lives one
// click away in the containers panel, so the row stays a single line (unlike
// the Deploys Version column, which lists every changed service: a deploy
// touches a few services, while a stack HAS all of them).
// health is the stack's live-health entry on the row's host (app.js
// stackHealthFor wraps the lookup); empty while none exists for the stack
// (parked, stopped, or the first health poll still outstanding —
// updateRosterHealth fills it in then). updates is the stack's update-check
// map (service → {latest, rebuilt}, ADR-0054) — the lead chip carries its own
// service's marker; a non-lead service's update surfaces in the containers
// panel and the panel-head count, not on the row.
function rosterVersionInnerHTML(stack, health, updates) {
  const v = rosterVersion(stack, health && health.services);
  if (!v) return '';
  // No lead service: naming one of several equals would be arbitrary, so the
  // cell only says how many there are and defers to the panel.
  if (!v.service) {
    const label = v.more + (v.more === 1 ? ' service' : ' services');
    return `<span class="ver-count" title="No single main service — open the row for every version">${label}</span>`;
  }
  const more =
    v.more > 0
      ? `<span class="ver-count" title="${escapeAttr(v.more + ' more service' + (v.more === 1 ? '' : 's') + ' — open the row for every version')}">+${v.more}</span>`
      : '';
  return serviceVersionHTML(v.service, v.image, true, updates && updates[v.service]) + more;
}

// rosterVersionCellHTML wraps that in the cell every row emits — including a
// parked stack, whose cell stays empty (it is never polled, so it has no running
// version) so the grid still lines up.
function rosterVersionCellHTML(stack, health, disabled, updates) {
  const inner = disabled ? '' : rosterVersionInnerHTML(stack, health, updates);
  return `<span class="col-version" data-testid="roster-version">${inner}</span>`;
}

// rosterStatusHTML picks the row's status: a live in-flight deploy wins
// (deploying — a peer row always passes false, since the in-flight set is the
// primary's own), then the parked/never-deployed synthetic flags, else the
// last terminal badge.
function rosterStatusHTML(entry, deploying) {
  if (deploying) return badgeHTML('deploying');
  if (entry.disabled) return `<span class="roster-flag">disabled</span>`;
  if (!entry.last_status) return `<span class="roster-flag">never deployed</span>`;
  return badgeHTML(entry.last_status);
}

// rosterHealthPillHTML is the live-health pill shown inline in a roster row's
// status cell (under the deploy badge), so the Stacks overview surfaces current
// container health without expanding — for local stacks and, host-scoped, for
// peers alike. Empty when the host reports no health for the stack.
function rosterHealthPillHTML(stack, health) {
  return health && health.status ? healthPillHTML(stack, health.status) : '';
}

// outcomeLabel says a terminal status in words for the strip tooltips and the
// last-incident line — same phrasing as the badges.
function outcomeLabel(status) {
  if (status === 'rolled_back') return 'rolled back';
  if (status === 'rolled_back_unhealthy') return 'rolled back · unhealthy';
  if (status === 'heal_exhausted') return 'self-heal failed';
  return status;
}

// outcomeStripHTML is the roster status cell's mini-history: the stack's last
// terminal outcomes as small status-coloured dots, oldest → newest left →
// right, so the strip reads as a timeline running into the badge — a success
// can no longer paper over the rollback right behind it. recent arrives
// newest-first on the roster entry (`stacks` snapshot, audit order); the strip
// reverses it. Decoration, not a control: the row's own click opens the
// deploy-history panel where every dot's full record lives, and the dots are
// aria-hidden (badge + incident line carry the same facts in words). Empty
// with fewer than two records — a lone dot only repeats the badge. Takes the
// clock (nowMs) for the dot tooltips, so it stays pure.
function outcomeStripHTML(recent, nowMs) {
  const recs = recent || [];
  if (recs.length < 2) return '';
  const dots = recs
    .slice()
    .reverse()
    .map(function (r) {
      const age = phaseDuration(nowMs - new Date(r.at).getTime());
      const title =
        outcomeLabel(r.status) +
        ' · ' +
        age +
        ' ago' +
        (r.commit ? ' · ' + shortSHA(r.commit) : '');
      return `<span class="outcome-dot" data-testid="outcome-dot" data-status="${escapeAttr(r.status)}" title="${escapeAttr(title)}"></span>`;
    })
    .join('');
  return `<span class="outcome-strip" data-testid="outcome-strip" aria-hidden="true">${dots}</span>`;
}

// lastIncidentHTML names the newest bad outcome when later successes have
// taken the badge — the line that keeps a recovered rollback readable without
// opening anything. The server omits last_incident when the badge already says
// it (or no bad record is retained), so an empty render needs no client rule.
// The full timestamp rides the title; the glyph matches the narrated log's.
function lastIncidentHTML(incident, nowMs) {
  if (!incident || !incident.status) return '';
  const glyph = incident.status.indexOf('rolled_back') === 0 ? '↺' : '✗';
  const age = phaseDuration(nowMs - new Date(incident.at).getTime());
  return `<span class="last-incident" data-testid="last-incident" title="${escapeAttr(fullTime(incident.at))}">${glyph} ${escapeHtml(outcomeLabel(incident.status))} · ${escapeHtml(age)} ago</span>`;
}

// DEPLOY_STATUS_ORDER fixes the Deploys status-filter chip order — worst
// first, so the chips a triage reaches for lead the row. A status outside the
// list (a future addition) still renders, sorted alphabetically after these.
const DEPLOY_STATUS_ORDER = [
  'failed',
  'rolled_back_unhealthy',
  'rolled_back',
  'heal_exhausted',
  'healed',
  'success',
  'deploying',
  'queued',
  'blocked',
];

// deployStatusChipsHTML renders the status-filter chip row under the Deploys
// search input (UI_SPEC.md "Status filter"): one toggle chip per status
// present among the rendered rows, each with its per-status count, in the
// quick-filter chip idiom the Log view established. counts maps status →
// row count; active is the selected set — an active status whose rows have
// aged out still renders (count 0), or the narrowing could no longer be
// cleared from where it shows.
function deployStatusChipsHTML(counts, active) {
  const known = DEPLOY_STATUS_ORDER.filter(function (s) {
    return counts[s] !== undefined;
  });
  const rest = Object.keys(counts)
    .filter(function (s) {
      return DEPLOY_STATUS_ORDER.indexOf(s) === -1;
    })
    .sort();
  return known
    .concat(rest)
    .map(function (s) {
      const on = active.indexOf(s) !== -1;
      return `<button type="button" class="clog-chip status-chip${on ? ' active' : ''}" data-status="${escapeAttr(s)}" aria-pressed="${on}">${escapeHtml(outcomeLabel(s))}<span class="sc-count">${counts[s]}</span></button>`;
    })
    .join('');
}

// retryNoteHTML pairs a success row with the rollback it supersedes
// (follows_rollback on the event — UI_SPEC.md "Rollback linkage"): a small
// rollback-tinted note under the badge, so the success names the rollback it
// redeems instead of papering over it. A real button: activating it jumps to
// the rollback's own row when still rendered, else opens the row's
// deploy-history panel (the click handler's job). data-rollback-id rides along
// when the rollback event is still in the bounded history.
function retryNoteHTML(ev) {
  if (!ev || !ev.follows_rollback) return '';
  const id = ev.rollback_event_id
    ? ` data-rollback-id="${escapeAttr(String(ev.rollback_event_id))}"`
    : '';
  return `<button type="button" class="retry-note" data-testid="retry-note"${id} title="This success redeploys a change that was rolled back — open the rollback">↺ after rollback</button>`;
}

// autosyncDetailHTML is the second line of an autosync drawer row: what the
// stack is waiting on when it is queued (item), else its resting state. entry
// is the stack's autosync snapshot entry, item its pending queue entry (if any).
function autosyncDetailHTML(entry, item, nowMs) {
  if (item) {
    const n = item.changed_files ? item.changed_files.length : 0;
    return `<span class="warn">${n} file${n === 1 ? '' : 's'}</span> · waiting <span data-testid="wait-cell">${waitedSince(item.since, nowMs)}</span>`;
  }
  if (!entry.effective) return 'paused · no changes';
  return 'synced';
}

// autosyncPosText renders the row's leading cell. pos is a number only in the
// queue list; the all-stacks list passes null and marks a queued stack with a
// dot instead.
function autosyncPosText(pos, queued) {
  if (pos !== null) return String(pos);
  return queued ? '●' : '';
}

// autosyncReasonChipHTML tags a paused stack with why it is paused — the queue
// item's own reason when it has one, else derived from the snapshot entry.
function autosyncReasonChipHTML(entry, item) {
  const reason = item ? item.reason : reasonFromSnap(entry);
  return !entry.effective && reason ? `<span class="reason reason-${reason}">${reason}</span>` : '';
}

// autosyncSwitchTitle is the row switch's tooltip — it names the action the
// click performs, not the current state.
function autosyncSwitchTitle(entry) {
  return `${entry.effective ? 'Pause' : 'Resume'} autosync`;
}

// autosyncRowHTML renders one stack row of the autosync drawer, in either list:
// pos is the queue position, or null in the all-stacks list; item is the stack's
// pending queue entry, if any.
function autosyncRowHTML(entry, pos, item, nowMs) {
  const inQueue = pos !== null;
  const posCell = `<div class="qpos${inQueue ? '' : ' blank'}">${autosyncPosText(pos, item)}</div>`;
  const rowTestid = inQueue ? 'queue-item' : 'stack-item';
  // The stack switch is the interactive control the tests toggle; only tag it
  // in the all-stacks list so a queued stack does not expose two of them.
  const swTestid = inQueue ? '' : ' data-testid="stack-switch"';
  return (
    `<div class="stack-row" data-testid="${rowTestid}" data-stack="${escapeAttr(entry.name)}">` +
    posCell +
    `<div class="stack-meta">` +
    `<div class="stack-name">${escapeHtml(entry.name)}${autosyncReasonChipHTML(entry, item)}</div>` +
    `<div class="stack-detail">${autosyncDetailHTML(entry, item, nowMs)}</div>` +
    `</div>` +
    `<div class="sw${entry.effective ? ' on' : ''}"${swTestid} data-taptip role="switch" aria-checked="${entry.effective}"` +
    ` tabindex="0" data-stack="${escapeAttr(entry.name)}" title="${autosyncSwitchTitle(entry)}"></div>` +
    `</div>`
  );
}

// SHIP_ICON is the badge on the run drawer's active row.
const SHIP_ICON =
  '<svg class="ds-ico" viewBox="0 0 24 24" fill="none" aria-hidden="true"><path d="M4 13h16l-2 4H6z" stroke="currentColor" stroke-width="2" stroke-linejoin="round"/><rect x="7.5" y="8" width="3.5" height="4" rx="0.5" stroke="currentColor" stroke-width="1.7"/><rect x="13" y="8" width="3.5" height="4" rx="0.5" stroke="currentColor" stroke-width="1.7"/><path d="M3 20q2-1.6 4 0t4 0 4 0 4 0" stroke="currentColor" stroke-width="2" stroke-linecap="round"/></svg>';

// nextTrailHTML renders the header's look-ahead trail "→ a · b · +N", capping
// the names shown so a long run does not widen the header without bound.
function nextTrailHTML(up) {
  const MAX = 3;
  const shown = up.slice(0, MAX);
  let html =
    '<span class="arrow">→</span>' +
    shown
      .map(function (n, i) {
        return `<span class="up${i > 0 ? ' more' : ''}">${escapeHtml(n)}</span>`;
      })
      .join('<span class="sep">·</span>');
  const extra = up.length - shown.length;
  if (extra > 0) {
    html += `<span class="sep">·</span><span class="up more">+${extra}</span>`;
  }
  return html;
}

// runSummaryHTML is the run drawer's subtitle: what is deploying now and how
// much of the run is left.
function runSummaryHTML(active, up) {
  if (active.length === 0 && up.length === 0) return 'Nothing deploying.';
  if (active.length > 0) {
    return (
      `<b>${escapeHtml(active.join(', '))}</b> deploying` +
      (up.length ? ' · ' + up.length + ' more this run' : ' · last in this run')
    );
  }
  return up.length + ' stack' + (up.length === 1 ? '' : 's') + ' upcoming';
}

// runRowHTML renders one row of the run drawer. The badge is the ship glyph for
// the active stack and the queue position for the ones waiting behind it.
function runRowHTML(name, badge, detail, isActive) {
  return (
    `<div class="run-row${isActive ? ' active' : ''}" data-stack="${escapeAttr(name)}">` +
    `<span class="run-badge${isActive ? ' ship' : ''}">${badge}</span>` +
    `<div><div class="run-name">${escapeHtml(name)}</div>` +
    `<div class="run-detail">${detail}</div></div>` +
    `</div>`
  );
}

// runListHTML renders the whole run drawer body: the active stack(s) lead with
// the ship badge, the upcoming ones follow in deploy order.
function runListHTML(active, up) {
  const rows = active
    .map(function (n) {
      return runRowHTML(n, SHIP_ICON, 'deploying now', true);
    })
    .concat(
      up.map(function (n, i) {
        return runRowHTML(n, String(i + 1), i === 0 ? 'next' : 'then', false);
      }),
    );
  return rows.length ? rows.join('') : '<div class="qempty">Nothing deploying right now.</div>';
}

// WARN_ICON marks the live-health attention surface: the header beacon's glyph
// and the attention band's heading.
const WARN_ICON =
  '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M10.3 3.9 1.8 18a2 2 0 0 0 1.7 3h16.9a2 2 0 0 0 1.7-3L13.7 3.9a2 2 0 0 0-3.4 0Z"/><path d="M12 9v4M12 17h.01"/></svg>';

// beaconPopHTML renders the header beacon's popover: the shared summary plus one
// button per unhealthy stack, each carrying the stack name the click handler
// jumps to.
function beaconPopHTML(att) {
  return (
    '<div class="bp-head">' +
    escapeHtml(attentionLabel(att.length)) +
    '</div>' +
    att
      .map(function (a) {
        return (
          '<button type="button" class="beacon-item" data-testid="health-beacon-item" data-stack="' +
          escapeAttr(a.stack) +
          '">' +
          '<span class="bi-dot" data-health="' +
          escapeAttr(a.status) +
          '"></span>' +
          '<span class="bi-name">' +
          escapeHtml(a.stack) +
          '</span></button>'
        );
      })
      .join('')
  );
}

// attentionBandHTML renders the Deploys view's attention band: a counted heading
// and one row per unhealthy stack. The .stack-icon spans stay empty — the caller
// fills them, because the icons load asynchronously.
function attentionBandHTML(att) {
  return (
    '<div class="att-head">' +
    WARN_ICON +
    '<span class="att-title">Needs attention</span>' +
    '<span class="att-count">' +
    att.length +
    '</span></div>' +
    att
      .map(function (a) {
        return (
          '<button type="button" class="attention-row" data-testid="attention-row" data-stack="' +
          escapeAttr(a.stack) +
          '">' +
          '<span class="stack-icon" data-testid="stack-icon"></span>' +
          '<span class="att-name">' +
          escapeHtml(a.stack) +
          '</span>' +
          '<span class="health-pill att-pill" data-health="' +
          escapeAttr(a.status) +
          '"><span class="hdot"></span><span class="hlabel">' +
          escapeHtml(a.status) +
          '</span></span>' +
          '</button>'
        );
      })
      .join('')
  );
}

// watchedLeadHTML renders the change-detection lead, linking the commit it
// names. watchedSummary stays a pure, unit-tested string: only the SHA token it
// already produced is swapped for a link, after escaping — a short SHA is
// `[0-9a-f]{7}`, so it survives escaping unchanged and matches unambiguously.
// The lead only names a commit in its settled form (UNCHANGED_SINCE, shared
// with the helper); any other phrasing misses the lookup and stays plain text.
function watchedLeadHTML(entry, fileCount, repoBase) {
  const text = escapeHtml(
    watchedSummary(entry.last_status || '', entry.last_commit || '', fileCount, entry.disabled),
  );
  const commit = entry.last_commit || '';
  if (!commit) return text;
  const token = UNCHANGED_SINCE + shortSHA(commit);
  if (text.indexOf(token) === -1) return text;
  return text.replace(
    token,
    UNCHANGED_SINCE + commitLinkHTML(commit, { base: repoBase, title: commit }),
  );
}

// watchedPanelHTML renders the change-detection panel's body: the lead, plus the
// input files whose hashes decide whether the stack redeploys.
function watchedPanelHTML(entry, repoBase) {
  // The stack's own settings are hashed too, but under a synthetic key rather
  // than a file (ADR-0043 moved that config host-side). It gets its own,
  // clearly non-path entry so nobody goes looking for a file that isn't there.
  const items = (entry.watched || []).map(function (f) {
    return `<li class="wp-file" data-testid="watched-file">${escapeHtml(f)}</li>`;
  });
  if (entry.watched_config) {
    items.push(
      '<li class="wp-config" data-testid="watched-config">' +
        'plus this stack&rsquo;s settings in the host <code>skipper.yml</code></li>',
    );
  }
  return (
    `<div class="wp-head"><span class="wp-label">change detection</span></div>` +
    `<div class="wp-lead" data-testid="watched-lead">${watchedLeadHTML(entry, items.length, repoBase)}</div>` +
    (items.length ? `<ul class="wp-files">${items.join('')}</ul>` : '')
  );
}

// healPanelHTML renders the healed row's detail body: a fixed explanation (no
// git change → no diff) and, when known, the drifted services the corrective
// redeploy reacted to.
function healPanelHTML(drift) {
  let html =
    '<div class="heal-summary">Self-heal restored this stack to its deployed running state — a corrective <code>docker compose up -d</code>. Nothing in git changed, so there is no diff.</div>';
  if (drift && drift.length > 0) {
    html +=
      '<div class="heal-drift-label">Drifted when it ran</div>' +
      `<ul class="heal-drift-list">` +
      drift
        .map(function (d) {
          return (
            `<li><span class="hd-name">${escapeHtml(d.name)}</span>` +
            `<span class="hd-status hd-${escapeAttr(d.status)}">${escapeHtml(d.status)}</span></li>`
          );
        })
        .join('') +
      `</ul>`;
  }
  return html;
}

// filesPanelHTML renders the changed-file list of a deploy, one path per line.
function filesPanelHTML(files) {
  return files
    .map(function (f) {
      return `<span class="file-path">${escapeHtml(f)}</span>`;
    })
    .join('<br>');
}

// diffContentHTML renders one file's unified diff, each line tagged with the
// CSS class classifyDiffLine derives from it.
function diffContentHTML(diff) {
  return diff
    .split('\n')
    .map(function (line) {
      const cls = classifyDiffLine(line);
      return `<span class="diff-line${cls ? ' ' + cls : ''}">${escapeHtml(line)}</span>`;
    })
    .join('\n');
}

// diffPanelHTML renders the diff panel's body: the commit header plus one
// collapsible section per file. A lone file starts expanded — there is nothing
// to choose between. repoBase is passed through to the header's SHA links.
function diffPanelHTML(diffs, commits, meta, repoBase) {
  const files = Object.keys(diffs);
  const singleFile = files.length === 1;
  return (
    renderCommitHead(commits, meta, repoBase) +
    files
      .map(function (f) {
        const name = f.split('/').pop() || f;
        return (
          `<div class="diff-file-section">` +
          `<div class="diff-file-header${singleFile ? ' expanded' : ''}">` +
          `<svg viewBox="0 0 10 10"><path d="M3 1l4 4-4 4" fill="none" stroke="currentColor" stroke-width="1.5"/></svg>` +
          `<span>${escapeHtml(name)}</span></div>` +
          `<div class="diff-content">${diffContentHTML(diffs[f])}</div>` +
          `</div>`
        );
      })
      .join('')
  );
}

// hookPhaseHTML renders the hook-phase chip shown on a deploying row: which
// phase is running, its position when the stack has several hooks in that
// phase, and the button that opens the hook's output in the log (ADR-0038).
function hookPhaseHTML(hr) {
  const n = hr.total > 1 ? ' ' + hr.index + '/' + hr.total : '';
  return (
    `<span class="hk-dot"></span>${escapeHtml(hr.phase)} hook${n}` +
    `<button class="clog-btn hook-log-btn" type="button" data-testid="clog-btn" data-taptip ` +
    `data-hook-log="${escapeAttr(hr.stack)}" title="View this hook's output in the log" ` +
    `aria-label="View hook log">${CLOG_ICON}</button>`
  );
}

// logLineHTML renders one log line's spans. Child-process output (level `cmd`)
// gets a command prefix instead of a level badge and carries no attrs blob —
// its message is already the raw output.
function logLineHTML(entry) {
  const attrs = entry.attrs || {};
  // The stack attr says which stack a line belongs to — a prominent prefix
  // rather than one pair buried in the attrs blob. Hook output carries one too
  // (ADR-0038) so it reads like a deploy line and the filter matches it;
  // docker/git output has none.
  // In the Logs view the prefix doubles as the control that filters to that
  // stack (the CSS and the click handler are scoped to #log-pane, so the
  // hook-log panel's reuse of this renderer stays inert).
  const stack = attrs.stack
    ? `<span class="log-stack" data-testid="stack-prefix" data-stack="${escapeAttr(attrs.stack)}"` +
      ` role="button" tabindex="0" title="Filter the log to this stack">[${escapeHtml(attrs.stack)}]</span>`
    : '';
  const msg = `<span class="log-msg">${escapeHtml(entry.msg)}</span>`;
  let html = `<span class="log-time" title="${escapeAttr(fullTime(entry.time))}">${escapeHtml(logTime(entry.time))}</span>`;
  if (logLineLevel(entry) === 'cmd') {
    return (
      html +
      stack +
      `<span class="log-cmd" data-testid="cmd-prefix">[${escapeHtml(attrs.cmd)}]</span>` +
      msg
    );
  }
  // A message the console narrates is narrated here too, so the two surfaces
  // read alike; anything else keeps the level badge + message + attrs blob.
  const story = logNarrative(entry);
  if (story) return html + narratedLineHTML(story, attrs);

  html +=
    `<span class="log-level ${levelClass(entry.level)}" data-testid="level-badge">${escapeHtml(entry.level)}</span>` +
    stack +
    msg;
  // Deploy completion lines carry the deploy event's ID — render a pill that
  // loads the deploy's diff below the line on demand.
  if (attrs.event_id) {
    html += `<button class="files-pill log-diff-pill" data-testid="diff-pill" data-event-id="${escapeAttr(attrs.event_id)}">diff</button>`;
  }
  const pairs = Object.keys(attrs)
    .filter(function (k) {
      return k !== 'stack' && k !== 'event_id';
    })
    .map(function (k) {
      return `${escapeHtml(k)}=${escapeHtml(attrs[k])}`;
    });
  if (pairs.length > 0) html += `<span class="log-attrs">${pairs.join(' ')}</span>`;
  return html;
}

// narratedLineHTML renders one narrated line's parts: the status glyph, the
// stack (still the filter control), the narrated text and its trailing detail.
// The parts a line does not have are simply absent — a missing stack must not
// leave an empty column, or the lines stop aligning.
function narratedLineHTML(story, attrs) {
  let html = `<span class="log-glyph ${story.tone ? 'tone-' + story.tone : ''}" data-testid="log-glyph" aria-hidden="true">${escapeHtml(story.glyph)}</span>`;
  if (story.stack) {
    // Only a real stack attr is a filter control; a synthesised label like
    // "peer argoneon" is not one of the log's stacks. A control keeps the
    // bracketed form the unnarrated lines use — one pane, one shape for the
    // same affordance — while the label is rendered plain, so the two cannot
    // be mistaken for each other.
    html += attrs.stack
      ? `<span class="log-stack" data-testid="stack-prefix" data-stack="${escapeAttr(attrs.stack)}"` +
        ` role="button" tabindex="0" title="Filter the log to this stack">[${escapeHtml(attrs.stack)}]</span>`
      : `<span class="log-stack">${escapeHtml(story.stack)}</span>`;
  }
  if (story.text) {
    html += `<span class="log-msg ${story.textTone ? 'tone-' + story.textTone : ''}">${escapeHtml(story.text)}</span>`;
  }
  if (story.segments) {
    html += story.segments
      .map(function (s) {
        return `<span class="log-seg ${s.tone ? 'tone-' + s.tone : 'log-dim'}">${escapeHtml(s.text)}</span>`;
      })
      .join('<span class="log-dim">·</span>');
  }
  if (story.dim) html += `<span class="log-dim">${escapeHtml(story.dim)}</span>`;
  if (attrs.event_id) {
    html += `<button class="files-pill log-diff-pill" data-testid="diff-pill" data-event-id="${escapeAttr(attrs.event_id)}">diff</button>`;
  }
  return html;
}

// logDiffBlockHTML renders a changed file's diff under its line, in the same
// classes the diff panel uses so the two renderings of one diff look alike.
// The content arrives already clamped by the capture layer (internal/logbuf),
// which appends its own "… (N lines omitted)" marker — rendered as a plain
// line, since it is not part of the diff.
function logDiffBlockHTML(diff) {
  const lines = String(diff).replace(/\n$/, '').split('\n');
  return (
    `<div class="log-diff" data-testid="log-diff">` +
    lines
      .map(function (l) {
        return `<span class="diff-line ${classifyDiffLine(l)}">${escapeHtml(l)}</span>`;
      })
      .join('') +
    `</div>`
  );
}

// AUDIT_FOLD_NOUN names what a folded run of one routine outcome contains, so
// the summary line reads as a sentence rather than "9 × success".
const AUDIT_FOLD_NOUN = {
  success: ['successful deploy', 'successful deploys'],
  healed: ['self-heal', 'self-heals'],
};

// auditRowsHTML renders the deploy-history rows of one stack. absolute picks
// which of the two timestamps leads and which becomes the tooltip, following
// the app-wide time toggle. repoBase links the SHAs; '' renders them inert.
// Runs of routine outcomes are folded away (foldAuditRecords, app-helpers.js)
// exactly as the health timeline in the same card folds routine cycles — a
// stack that converged the same way for a month is otherwise that one line
// repeated, burying the deploy that did not. Whenever folding collapsed
// anything, the verbatim list stays behind a toggle; opts.id is the
// page-unique slug its aria-controls points at.
function auditRowsHTML(records, repoBase, absolute, opts) {
  const o = opts || {};
  const time = function (ts) {
    return absolute ? fullTime(ts) : formatTime(ts);
  };

  // The summary of one folded run: how many, since when, how many files they
  // changed, and the deploys inside it as commit chips (capped like the health
  // timeline's, whose chips these are).
  const foldLine = function (run) {
    const noun = AUDIT_FOLD_NOUN[run.status] || [
      auditStatusLabel(run.status),
      auditStatusLabel(run.status),
    ];
    const oldest = run.records[run.records.length - 1];
    const files = run.records.reduce(function (sum, rec) {
      return sum + (rec.changed_files || 0);
    }, 0);
    const commits = run.records
      .map(function (rec) {
        return rec.commit_sha;
      })
      .filter(Boolean);
    const rest = commits.length - FOLD_COMMIT_CHIPS_MAX;
    const chips =
      commits
        .slice(0, FOLD_COMMIT_CHIPS_MAX)
        .map(function (sha) {
          return commitLinkHTML(sha, {
            cls: 'hp-commit',
            base: repoBase,
            testid: 'audit-fold-commit',
            title: 'deployed in one of these runs',
          });
        })
        .join('') + (rest > 0 ? `<span class="hp-commit">+${rest}</span>` : '');
    return (
      `<div class="hp-phase hp-fold ar-fold" data-testid="audit-fold" data-status="${escapeAttr(run.status)}" ` +
      `title="routine outcomes, folded — the full history is one click away">` +
      `<span class="hp-fold-glyph">↻</span>` +
      `<span><span class="hp-count">${run.records.length}</span> more ` +
      `${escapeHtml(run.records.length === 1 ? noun[0] : noun[1])} since ${escapeHtml(time(oldest.timestamp))}` +
      `${files ? escapeHtml(' · ' + files + ' file' + (files > 1 ? 's' : '')) : ''}</span>` +
      chips +
      `</div>`
    );
  };

  const rows = function (recs, withTestid) {
    return recs
      .map(function (r) {
        const abs = fullTime(r.timestamp),
          rel = formatTime(r.timestamp);
        const sha = r.commit_sha
          ? commitLinkHTML(r.commit_sha, { cls: 'ar-sha', base: repoBase, title: r.commit_sha })
          : '<span class="ar-sha">—</span>';
        const files = r.changed_files
          ? escapeHtml(r.changed_files + ' file' + (r.changed_files > 1 ? 's' : ''))
          : '—';
        const err = r.error
          ? `<span class="ar-err" title="${escapeAttr(r.error)}">${escapeHtml(r.error)}</span>`
          : '';
        return (
          `<div class="audit-row"${withTestid ? ' data-testid="audit-row"' : ''} data-status="${escapeAttr(r.status)}">` +
          `<span class="ar-time" data-ts="${escapeAttr(r.timestamp)}" title="${escapeAttr(absolute ? rel : abs)}">${escapeHtml(absolute ? abs : rel)}</span>` +
          `<span class="ar-status"><span class="adot"></span>${escapeHtml(auditStatusLabel(r.status))}</span>` +
          `<span class="ar-dur">${escapeHtml(formatDuration(r.duration_ms))}</span>` +
          sha +
          `<span class="ar-files">${files}</span>` +
          err +
          `</div>`
        );
      })
      .join('');
  };

  const items = foldAuditRecords(records);
  const folded = items
    .map(function (it) {
      return it.kind === 'run' ? foldLine(it) : rows([it.record], true);
    })
    .join('');
  if (
    !items.some(function (it) {
      return it.kind === 'run';
    })
  ) {
    return folded;
  }
  // aria-controls names the list the toggle reveals, so a screen reader
  // follows the swap; the id must be document-unique (several rows can hold an
  // open history at once), which only the caller can guarantee.
  const rawID = o.id ? `ap-raw-${o.id}` : '';
  const controls = rawID ? ` aria-controls="${escapeAttr(rawID)}"` : '';
  return (
    `<div class="ap-history"><div class="ap-folded">${folded}</div>` +
    `<button class="hp-fold-toggle ap-fold-toggle" type="button" data-testid="audit-fold-toggle" ` +
    `aria-expanded="false"${controls} data-label="all ${records.length} deploys" ` +
    `data-fold-label="fold routine outcomes">all ${records.length} deploys</button>` +
    `<div class="ap-raw"${rawID ? ` id="${escapeAttr(rawID)}"` : ''}>${rows(records, false)}</div>` +
    `</div>`
  );
}

// clogSvcsHTML renders the container-log drawer's service filter: an "all" chip
// plus one per service, the selected ones active. The caller decides whether a
// filter is warranted at all — a single-service stack has nothing to filter.
function clogSvcsHTML(services, selected) {
  const chips = services
    .map(function (s) {
      const on = selected.indexOf(s.name) !== -1;
      return (
        `<button class="clog-chip${on ? ' active' : ''}" type="button" ` +
        `data-svc="${escapeAttr(s.name)}">${escapeHtml(s.name)}</button>`
      );
    })
    .join('');
  return (
    '<div class="clog-svcs clog-hide" data-testid="clog-svcs">' +
    '<span class="clog-svcs-lbl">service</span>' +
    `<button class="clog-chip${selected.length ? '' : ' active'}" type="button" data-svc="">all</button>` +
    chips +
    '</div>'
  );
}

// Dual-use export, same pattern as app-helpers.js: skipped in the browser
// (the functions are already globals), used by `node --test`.
if (typeof module !== 'undefined' && module.exports) {
  module.exports = {
    escapeHtml,
    escapeAttr,
    commitLinkHTML,
    versionChipHTML,
    imageDeltaHTML,
    renderCommitHead,
    badgeHTML,
    serviceVersionHTML,
    updateCheckMetaHTML,
    filesHTML,
    healPillHTML,
    healthHistoryHTML,
    healthStripHTML,
    clogBtnHTML,
    healthPillHTML,
    hookCount,
    hooksBadgeHTML,
    jumpBtnHTML,
    rowActionClusterHTML,
    pendingTagHTML,
    hostChipHTML,
    linkCellHTML,
    rosterRowActionsHTML,
    rosterVersionInnerHTML,
    rosterVersionCellHTML,
    rosterStatusHTML,
    rosterHealthPillHTML,
    outcomeLabel,
    outcomeStripHTML,
    lastIncidentHTML,
    retryNoteHTML,
    DEPLOY_STATUS_ORDER,
    deployStatusChipsHTML,
    autosyncDetailHTML,
    autosyncPosText,
    autosyncReasonChipHTML,
    autosyncSwitchTitle,
    autosyncRowHTML,
    nextTrailHTML,
    runSummaryHTML,
    runRowHTML,
    runListHTML,
    SHIP_ICON,
    beaconPopHTML,
    attentionBandHTML,
    WARN_ICON,
    watchedLeadHTML,
    watchedPanelHTML,
    healPanelHTML,
    filesPanelHTML,
    diffContentHTML,
    diffPanelHTML,
    hookPhaseHTML,
    logLineHTML,
    logDiffBlockHTML,
    auditRowsHTML,
    clogSvcsHTML,
  };
}
