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
function serviceVersionHTML(service, image, labelled) {
  const tag = shortImageTag(image);
  const body = `<span class="td-cur">${escapeHtml(tag)}</span>`;
  return versionChipHTML(
    labelled === false ? '' : service,
    body,
    `${service} running ${tag}`,
    `${service}: ${image}`,
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

// healthHistoryHTML renders one service's status timeline from the
// healthwatch snapshot (ADR-0031): newest first, each accepted phase with its
// start, how long it held, and the deploy commit when correlated. Returns ''
// when the watchdog is off — and for a service with only its baseline phase,
// where a one-line timeline would just repeat the inline age. The caller
// supplies the service's phases, the forge base for the commit chips
// (repoBase — the timeline may belong to a peer) and the clock (nowMs).
function healthHistoryHTML(phases, repoBase, nowMs) {
  if (!phases || phases.length < 2) return '';
  let html = '<div class="hp-history" data-testid="health-history">';
  for (let i = 0; i < phases.length; i++) {
    const p = phases[i];
    const end = i === 0 ? nowMs : new Date(phases[i - 1].since).getTime();
    const dur = phaseDuration(end - new Date(p.since).getTime());
    html +=
      `<div class="hp-phase" data-testid="health-phase" data-health="${escapeAttr(p.status)}">` +
      `<span class="hdot"></span>` +
      `<span class="hp-pstatus">${escapeHtml(p.status)}</span>` +
      `<span>${escapeHtml(phaseSince(p.since))}</span>` +
      `<span>${escapeHtml(i === 0 ? 'for ' + dur : dur)}</span>` +
      (p.deploy_correlated && p.commit
        ? commitLinkHTML(p.commit, {
            cls: 'hp-commit',
            base: repoBase,
            testid: 'health-phase-commit',
            title: 'deployed just before this phase began',
          })
        : '') +
      `</div>`;
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
// updateRosterHealth fills it in then).
function rosterVersionInnerHTML(stack, health) {
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
  return serviceVersionHTML(v.service, v.image) + more;
}

// rosterVersionCellHTML wraps that in the cell every row emits — including a
// parked stack, whose cell stays empty (it is never polled, so it has no running
// version) so the grid still lines up.
function rosterVersionCellHTML(stack, health, disabled) {
  const inner = disabled ? '' : rosterVersionInnerHTML(stack, health);
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
    filesHTML,
    healPillHTML,
    healthHistoryHTML,
    clogBtnHTML,
    healthPillHTML,
    hookCount,
    hooksBadgeHTML,
    jumpBtnHTML,
    pendingTagHTML,
    hostChipHTML,
    linkCellHTML,
    rosterRowActionsHTML,
    rosterVersionInnerHTML,
    rosterVersionCellHTML,
    rosterStatusHTML,
    rosterHealthPillHTML,
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
  };
}
