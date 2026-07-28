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
  };
}
