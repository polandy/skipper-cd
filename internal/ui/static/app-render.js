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
  };
}
