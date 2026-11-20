// skipper-cd UI render layer — pure HTML-string builders shared by the app
// shell and exercised in isolation by the unit layer (app-render.test.js).
//
// Like app-helpers.js this file is embedded and served same-origin
// (GET /app-render.js), loaded after app-helpers.js (whose functions it calls
// by bare name) and before the app files; in the browser its functions become globals,
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
// The tokens ride in one .td-val group so a chip too wide for its column
// breaks after the service label and never between `1.22.3` and `↑1.22.6` —
// half a version on each line reads as two versions.
function versionChipHTML(service, body, aria, title) {
  const label = service ? `<span class="td-svc">${escapeHtml(service)}</span>` : '';
  return `<span class="tag-delta" role="img" aria-label="${escapeAttr(aria)}" title="${escapeAttr(title)}">${label}<span class="td-val">${body}</span></span>`;
}

// imageDeltaChips renders the per-service image change(s) a deploy carried
// as `service old→new` chips for the row's Changes column — one per line, so the
// versions line up down the table and which service updated (and to what) reads
// at a glance without opening the diff. The data rides the event
// (image_changes); only services whose image ref actually changed are listed.
// Each chip names its service (a stack often has several), then the change: an
// empty Old is a service's first image (nothing on the left); an empty New is a
// service removed from the stack. Every changed service is listed — a deploy
// that touched five services says so, rather than hiding the rest behind a
// count the reader would have to open the diff to resolve.
function imageDeltaChips(changes) {
  return (changes || []).map(function (c) {
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
  });
}

// imageDeltaChipsHTML is imageDeltaChips as one string.
function imageDeltaChipsHTML(changes) {
  return imageDeltaChips(changes).join('');
}

// The change kinds a file_changes entry carries, in the order their chips are
// laid out: the stack's own definition first, then what it builds from, then
// the environment around it. A kind the backend adds later still renders — it
// lands after these.
const CHANGE_KIND_ORDER = ['compose', 'build', 'env', 'vars', 'watch', 'config'];

// How many service names a change chip prints before it counts the rest. Two
// names fit the column at every width; a file every service reads (one env file
// across five sidecars) must cost a counter, not five lines of row height.
const CHANGE_SERVICE_NAMES = 2;

// groupChangesByKind folds the event's file_changes into one entry per change
// kind: the services that kind reached, and whether any of its files was
// stack-wide. Two kinds of service go unlisted, both because the chip would say
// nothing the row does not already say:
//   - a service already named by an image chip, for the `compose` kind — on the
//     common bump row that chip IS that service's compose change;
//   - a stack-wide `compose` change (unresolvable, or the file's first deploy) —
//     that the stack's own definition changed is what the file count already
//     says; only the service attribution is new information. Every other kind
//     keeps its stack-wide chip: which project-wide input moved is the answer to
//     "why did this redeploy again".
function groupChangesByKind(fileChanges, imageChanges) {
  const named = {};
  (imageChanges || []).forEach(function (c) {
    named[c.service] = true;
  });
  const byKind = {};
  (fileChanges || []).forEach(function (fc) {
    const g = byKind[fc.kind] || (byKind[fc.kind] = { kind: fc.kind, services: [], wide: false });
    if (fc.wide) {
      if (fc.kind !== 'compose') g.wide = true;
      return;
    }
    (fc.services || []).forEach(function (svc) {
      if (fc.kind === 'compose' && named[svc]) return;
      if (g.services.indexOf(svc) === -1) g.services.push(svc);
    });
  });
  return Object.keys(byKind)
    .sort(function (a, b) {
      const ia = CHANGE_KIND_ORDER.indexOf(a);
      const ib = CHANGE_KIND_ORDER.indexOf(b);
      return (
        (ia === -1 ? CHANGE_KIND_ORDER.length : ia) - (ib === -1 ? CHANGE_KIND_ORDER.length : ib)
      );
    })
    .map(function (k) {
      return byKind[k];
    })
    .filter(function (g) {
      return g.wide || g.services.length > 0;
    });
}

// changeChips renders the per-kind change chips: which containers of the
// stack a deploy's changed files reached, for changes that carry no new image
// (a sidecar gaining an environment variable is the case this exists for). One
// chip per kind rather than per service, so a file every service reads costs a
// counter instead of a row-height line. A kind with no attributable service
// reads `stack-wide` — the honest answer for a project-wide input, never a
// guess.
function changeChips(fileChanges, imageChanges) {
  return groupChangesByKind(fileChanges, imageChanges).map(function (g) {
    const shown = g.services.slice(0, CHANGE_SERVICE_NAMES);
    const extra = g.services.length - shown.length;
    let body =
      `<span class="td-kind">${escapeHtml(g.kind)}</span>` +
      shown
        .map(function (svc) {
          return `<span class="td-svc">${escapeHtml(svc)}</span>`;
        })
        .join('');
    let who = shown.join(', ');
    if (extra > 0) {
      body += `<span class="td-count">+${extra} more</span>`;
      who += ' and ' + extra + ' more service' + (extra === 1 ? '' : 's');
    }
    if (g.wide) {
      body += `<span class="td-wide">stack-wide</span>`;
      who = who ? who + ', and stack-wide' : 'every service';
    }
    const aria = `${g.kind} changed for ${who}`;
    return (
      `<span class="tag-delta tag-change" role="img" aria-label="${escapeAttr(aria)}"` +
      ` title="${escapeAttr(aria)}"><span class="td-val">${body}</span></span>`
    );
  });
}

// changeChipsHTML is changeChips as one string.
function changeChipsHTML(fileChanges, imageChanges) {
  return changeChips(fileChanges, imageChanges).join('');
}

// changeCellHTML is a deploy row's Change cell: the first chip (image bump first) plus "+N".
function changeCellHTML(imageChanges, fileChanges) {
  const chips = imageDeltaChips(imageChanges).concat(changeChips(fileChanges, imageChanges));
  if (!chips.length) return '';
  const extra = chips.length - 1;
  const more =
    extra > 0
      ? `<span class="ver-count" data-testid="change-more" title="${extra} more change${extra === 1 ? '' : 's'} — open the row for every change">+${extra}</span>`
      : '';
  return `<span class="svc-delta" data-testid="svc-delta">${chips[0]}${more}</span>`;
}

// changeAttributionHTML renders one file's attribution for a panel line: the
// services that file reaches, `stack-wide` when it reaches all of them, or `no
// service changed` for an edit that touched no service definition. Empty when
// the event carries no attribution for the file (an older event, a peer's diff).
function changeAttributionHTML(fileChanges, path) {
  const fc = (fileChanges || []).find(function (c) {
    return c.file === path;
  });
  if (!fc) return '';
  const services = fc.services || [];
  let label = 'no service changed';
  let title = `${fc.kind} change, touching no service definition`;
  if (services.length) {
    label = services.join(' · ');
    title = `${fc.kind} change, reaching ${services.join(', ')}`;
  } else if (fc.wide) {
    label = 'stack-wide';
    title = `${fc.kind} change, reaching every service`;
  }
  return (
    `<span class="fc-svc${services.length ? '' : ' fc-wide'}" data-testid="file-services"` +
    ` title="${escapeAttr(title)}">${escapeHtml(label)}</span>`
  );
}

const personGlyph =
  '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="8" r="4"/><path d="M4 21a8 8 0 0 1 16 0"/></svg>';
const clockGlyph =
  '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="9"/><path d="M12 7v5l3.5 2"/></svg>';

// renderCommitHead builds the diff panel's header: the newest commit's
// message/author/time plus a collapsible list for a multi-commit range.
// Returns '' without commits. repoBase is the forge browse URL the SHA chips
// link through; '' renders them inert.
function renderCommitHead(commits, repoBase) {
  // shaHTML renders one `.m-sha` chip of this header.
  function shaHTML(sha, testid) {
    return commitLinkHTML(sha, { cls: 'm-sha', base: repoBase, title: sha, testid: testid });
  }
  if (!commits || !commits.length) return '';
  const head = commits[0];
  const multi = commits.length > 1;
  let html =
    '<div class="diff-head" data-testid="diff-head">' +
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
  html += line + '</div>';
  if (multi) html += commitListHTML(commits, repoBase, false);
  return html + '</div>';
}

// BADGE_STATUSES are the deploy statuses skipper emits (internal/events). A
// peer's records are relayed verbatim, so anything else is untrusted: it gets
// the neutral badge-unknown class and its label is escaped.
const BADGE_STATUSES = [
  STATUS.DEPLOYING,
  STATUS.SUCCESS,
  STATUS.FAILED,
  STATUS.SKIPPED,
  STATUS.ROLLED_BACK,
  STATUS.ROLLED_BACK_UNHEALTHY,
  STATUS.QUEUED,
  STATUS.HEALED,
  STATUS.HEAL_EXHAUSTED,
  STATUS.BLOCKED,
  STATUS.HELD,
  STATUS.REMOVED,
];

function badgeHTML(status) {
  const known = BADGE_STATUSES.indexOf(status) !== -1;
  const cls = `badge badge-${known ? status : 'unknown'}`;
  // deploying keeps its animated spinner as the leading glyph; every other
  // status gets an icon from statusIcon (T3.14).
  if (status === STATUS.DEPLOYING)
    return `<span class="${cls}" data-testid="status-badge"><span class="spinner"></span>deploying</span>`;
  const icon = statusIcon(status);
  if (status === STATUS.ROLLED_BACK_UNHEALTHY) {
    // The worst terminal state: a warning icon on a solid danger fill (T3.14)
    // — the loudest chip in the status column, not the smallest. The label
    // still stacks two lines (one line overflows the column), wrapped in
    // .badge-lbl so the badge stays a row for the leading icon.
    return `<span class="${cls}" data-testid="status-badge">${icon}<span class="badge-lbl"><span>rolled back</span><span>unhealthy</span></span></span>`;
  }
  if (status === STATUS.HEAL_EXHAUSTED) {
    // Same solid two-line treatment as rolled_back_unhealthy (ADR-0029, T3.14).
    return `<span class="${cls}" data-testid="status-badge">${icon}<span class="badge-lbl"><span>self-heal</span><span>failed</span></span></span>`;
  }
  const label = status === STATUS.ROLLED_BACK ? 'rolled back' : escapeHtml(status);
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
// a toggle (`.hp-fold-toggle`, handled in app-panels.js). Returns '' when the
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
// The small per-row buttons and chips shared by the roster and the detail
// panels. Pure string builders; all click handling stays in the views.

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

// Fishing hook — distinct from the container-logs icon it sits beside.
const HOOK_ICON =
  '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M15 5v7a4 4 0 0 1-8 0"/><path d="M4.5 10.5 7 13l2.5-2.5"/></svg>';
function hookCount(hooks) {
  return (hooks.pre_deploy || []).length + (hooks.post_deploy || []).length;
}
// ── Table rows ──
// One renderer for every row of both tables — local and peer, Deploys and
// Stacks. The caller resolves the state-bound parts (host chip, per-host
// lookups, the live flags) into the model; this file only lays them out.

const ROW_KIND = Object.freeze({ deploy: 'deploy', roster: 'roster' });

// tableRowHTML renders the cells of one row of the given kind.
//
// deploy model: host (chip HTML), stack, cause (the line under the name, or
// ''), ts, absolute, version (inner HTML), state (inner HTML), durationMs,
// commit, repoBase. Like a roster row it carries no controls.
//
// roster model: host, stack, health (inner HTML, rosterHealthHTML), version
// (the whole cell, rosterVersionCellHTML), state (the last-deploy status,
// rosterStatusHTML), lastAt, absolute, commit, repoBase. The row carries no
// controls: a click anywhere opens its detail panel, where the actions live.
function tableRowHTML(kind, m) {
  return kind === ROW_KIND.roster ? rosterRowCellsHTML(m) : deployRowCellsHTML(m);
}

// ROW_TOGGLE_ATTRS make a row's stack name the keyboard way into its detail
// (the row itself holds other controls, so it cannot be the button); App.detail
// keeps aria-expanded in step.
const ROW_TOGGLE_ATTRS = ' role="button" tabindex="0" aria-expanded="false"';

function stackIconHTML() {
  return '<span class="stack-icon" data-testid="stack-icon"></span>';
}

// deployRowCellsHTML lays out Stack · Result · Change · When — the positions of
// the Stacks row's Stack · Health · Version · Last deploy.
function deployRowCellsHTML(m) {
  const cause = m.cause
    ? `<span class="cell-cause" data-testid="cause-line" title="${escapeAttr(m.cause)}">${escapeHtml(m.cause)}</span>`
    : '';
  return (
    `<span class="cell-who"><span class="cell-stack">${m.host}${stackIconHTML()}<span class="stack-name row-toggle"${ROW_TOGGLE_ATTRS}>${escapeHtml(m.stack)}</span></span>${cause}</span>` +
    `<span class="status-cell">${m.state}</span>` +
    `<span class="col-version">${m.version}</span>` +
    deployWhenHTML(m)
  );
}

// deployShaHTML is the short commit in a deploy row's When cell.
function deployShaHTML(sha, repoBase) {
  return commitLinkHTML(sha, { cls: 'cell-sha', base: repoBase, title: sha, testid: 'row-sha' });
}

// deployWhenHTML is a deploy row's When cell: relative time, muted duration, short commit.
function deployWhenHTML(m) {
  const t = timeLabelPair(m.ts, m.absolute);
  const sha = m.commit ? deployShaHTML(m.commit, m.repoBase) : '';
  return (
    '<span class="cell-when">' +
    `<span class="cell-time" data-testid="time-cell" data-ts="${escapeAttr(m.ts)}" title="${escapeAttr(t.title)}">${escapeHtml(t.text)}</span>` +
    `<span class="cell-duration" data-testid="duration-cell">${m.durationMs > 0 ? formatDuration(m.durationMs) : ''}</span>` +
    sha +
    '</span>'
  );
}

function rosterRowCellsHTML(m) {
  const t = timeLabelPair(m.lastAt, m.absolute);
  return (
    `<span class="roster-stack"><span class="roster-ident">${m.host}${stackIconHTML()}<span class="roster-name row-toggle" title="${escapeAttr(m.stack)}"${ROW_TOGGLE_ATTRS}>${escapeHtml(m.stack)}</span></span></span>` +
    `<span class="roster-health" data-testid="roster-health">${m.health}</span>` +
    m.version +
    `<span class="roster-last"><span class="roster-status">${m.state}</span>` +
    `<span class="roster-when"${t.title ? ` title="${escapeAttr(t.title)}"` : ''}>${escapeHtml(t.text)}</span>` +
    commitLinkHTML(m.commit, { cls: 'roster-sha', base: m.repoBase, title: m.commit }) +
    '</span>'
  );
}

// hostChipHTML renders a row's leading host-identity chip inside the stack
// cell — a colour-tinted monogram for the row's host (a labelled chip, not a
// dot: a dot already means deploy status). Empty (and hidden by CSS) on a
// single-host instance; CSS shows it only when more than one host is in view.
// The caller supplies the host's colour slot from the live assignment
// (app-hosts.js hostChip wraps that lookup).
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

// rosterVersionInnerHTML fills a Stacks row's Version cell from the live health
// snapshot: the running version of the service the stack is named after, plus a
// muted "+N" for the rest — the glance. The full per-service list lives one
// click away in the containers panel, so the row stays a single line (unlike
// the Deploys Version column, which lists every changed service: a deploy
// touches a few services, while a stack HAS all of them).
// health is the stack's live-health entry on the row's host (App.resolve
// .stackHealthFor wraps the lookup); empty while none exists for the stack
// (parked, stopped, or the first health poll still outstanding —
// updateRosterHealth fills it in then). updates is the stack's update-check
// map (service → {latest, rebuilt}, ADR-0054) — it also picks the cell's
// service: one with an available update outranks the lead, so a marked stack
// always shows WHICH service moved and the header badge's stack count can be
// counted off the list it points at (see rosterVersion).
function rosterVersionInnerHTML(stack, health, updates) {
  const v = rosterVersion(stack, health && health.services, updates);
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

// rosterStatusHTML picks the row's last-deploy status: a live in-flight deploy
// wins (deploying — a peer row always passes false, since the in-flight set is
// the primary's own), then the parked/never-deployed synthetic flags, else the
// last terminal badge, followed by the held chip when the stack's change is
// held. Retrying a held change is the detail verdict's action, not the row's.
function rosterStatusHTML(entry, deploying) {
  if (deploying) return badgeHTML(STATUS.DEPLOYING);
  if (entry.disabled) return `<span class="roster-flag">disabled</span>`;
  if (!entry.last_status) return `<span class="roster-flag">never deployed</span>`;
  return badgeHTML(entry.last_status) + heldChipHTML(entry.name, entry.held);
}

// heldChipHTML marks a stack whose change is held (ADR-0062): its new version
// failed after it started, so skipper waits for a new commit instead of
// re-failing it every reconcile tick. A change that failed before it started
// and is still retried on its own carries retry_at and reads "backoff"
// (ADR-0064). The tooltip says when and how it failed, and for a backoff when
// the next attempt is due. Empty when nothing is held.
function heldChipHTML(stack, held) {
  if (!held) return '';
  const backoff = !!held.retry_at;
  const when = held.since ? ' ' + formatTime(held.since) + ' (' + fullTime(held.since) + ')' : '';
  const commit = held.commit ? ' at ' + held.commit.slice(0, 7) : '';
  let title;
  if (backoff) {
    const tries = held.attempts === 1 ? '1 attempt' : held.attempts + ' attempts';
    title =
      'The change' +
      commit +
      ' failed before it started (' +
      tries +
      ' so far),' +
      when +
      '. Next attempt ' +
      fullTime(held.retry_at) +
      '; if it keeps failing, it is held.';
  } else {
    const how = held.attempts
      ? 'failed ' + held.attempts + ' times before it started, last'
      : outcomeLabel(held.status);
    title =
      'The change' +
      commit +
      ' ' +
      how +
      when +
      '. It is not retried until a new commit changes ' +
      stack +
      '.';
  }
  const label = backoff ? 'backoff' : 'held';
  return `<span class="held-line"><span class="held-chip" data-testid="held-chip" title="${escapeAttr(title)}">${statusIcon(STATUS.HELD)}${label}</span></span>`;
}

// rosterHealthHTML is a Stacks row's Health cell: the stack's live health as
// a dot and a word — what it is now, beside the Last deploy cell saying what
// happened then. Not a control (the row's click opens the panel). Empty while
// the host reports no health for the stack.
function rosterHealthHTML(health) {
  if (!health || !health.status) return '';
  return `<span class="health-pill" data-testid="health-pill" data-health="${escapeAttr(health.status)}"><span class="hdot"></span><span class="hlabel">${escapeHtml(health.status)}</span></span>`;
}

// outcomeLabel says a terminal status in words for the strip tooltips and the
// last-incident line — same phrasing as the badges.
function outcomeLabel(status) {
  if (status === STATUS.ROLLED_BACK) return 'rolled back';
  if (status === STATUS.ROLLED_BACK_UNHEALTHY) return 'rolled back · unhealthy';
  if (status === STATUS.HEAL_EXHAUSTED) return 'self-heal failed';
  return status;
}

// outcomeStripHTML is the detail panel's Last deploy mini-history: the stack's last
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
  STATUS.FAILED,
  STATUS.ROLLED_BACK_UNHEALTHY,
  STATUS.ROLLED_BACK,
  STATUS.HEAL_EXHAUSTED,
  STATUS.HEALED,
  STATUS.SUCCESS,
  STATUS.DEPLOYING,
  STATUS.QUEUED,
  STATUS.BLOCKED,
  STATUS.REMOVED,
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

// rosterUpdateChipHTML renders the Stacks filter bar's updates-only chip
// (UI_SPEC.md "Updates filter"): the single toggle that narrows the roster to
// the stacks a registry update is waiting on. It borrows the Deploys status
// filter's chip, in the queued amber the update markers already use —
// data-status="queued" is what the shared .status-chip.active rule colours by,
// not a deploy status of its own. Empty when nothing is advertised, so the row
// collapses and the bar looks exactly as it did before the check existed.
function rosterUpdateChipHTML(count, active) {
  if (!count) return '';
  const on = !!active;
  return (
    `<button type="button" class="clog-chip status-chip${on ? ' active' : ''}" ` +
    `data-status="queued" data-testid="roster-update-chip" aria-pressed="${on}" ` +
    `title="Show only stacks a registry update is available for">` +
    `\u21e1 updates<span class="sc-count">${count}</span></button>`
  );
}

// repeatNoteHTML marks a row whose outcome collapsed a run of identical
// repeats (ADR-0056) — a standing failure re-emitted on every reconcile tick is
// one record carrying the count, so the row says "×199" where it used to print
// the same sentence 199 times. It borrows the fold line's ↻ glyph: both mean
// "this stands for more than one occurrence". Empty for a one-off outcome.
function repeatNoteHTML(rec) {
  const note = repeatNote(rec);
  if (!note) return '';
  return (
    `<span class="repeat-note" data-testid="repeat-note" title="${escapeAttr(note.title)}">` +
    `<span class="hp-fold-glyph">↻</span>${escapeHtml(note.label)}</span>`
  );
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

// AUTOSYNC_REASONS are the pause reasons a reason chip has a colour for.
const AUTOSYNC_REASONS = ['global', 'stack'];

// autosyncReasonChipHTML tags a paused stack with why it is paused — the queue
// item's own reason when it has one, else derived from the snapshot entry.
// An unknown reason is escaped onto the neutral reason-unknown class.
function autosyncReasonChipHTML(entry, item) {
  const reason = item ? item.reason : reasonFromSnap(entry);
  if (entry.effective || !reason) return '';
  const cls = AUTOSYNC_REASONS.indexOf(reason) !== -1 ? reason : 'unknown';
  return `<span class="reason reason-${cls}">${escapeHtml(reason)}</span>`;
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
    `<div class="wp-head"><span class="wp-label">Change detection</span></div>` +
    `<div class="wp-lead" data-testid="watched-lead">${watchedLeadHTML(entry, items.length, repoBase)}</div>` +
    (items.length ? `<ul class="wp-files">${items.join('')}</ul>` : '')
  );
}

// filesPanelHTML renders the changed-file list of a deploy, one path per line,
// each with the services that file reaches when the event attributed it.
function filesPanelHTML(files, fileChanges) {
  return files
    .map(function (f) {
      return (
        `<span class="file-path">${escapeHtml(f)}</span>` + changeAttributionHTML(fileChanges, f)
      );
    })
    .join('<br>');
}

// diffContentHTML renders one file's unified diff, each line tagged with the
// CSS class classifyDiffLine derives from it. The leading git header block
// (diff --git, index, ---, +++) is dropped: the section header names the file.
// Lines are joined without a separator — they are block elements, and a '\n'
// between them would add a blank line under `white-space: pre`.
function diffContentHTML(diff) {
  const lines = diff.split('\n');
  let start = 0;
  while (start < lines.length && classifyDiffLine(lines[start]) === 'diff-meta') start++;
  return lines
    .slice(start)
    .map(function (line) {
      const cls = classifyDiffLine(line);
      return `<span class="diff-line${cls ? ' ' + cls : ''}">${escapeHtml(line)}</span>`;
    })
    .join('');
}

// diffStatHTML is a file's added/removed line counts.
function diffStatHTML(diff) {
  const st = diffStat(diff);
  return `<span class="df-stat"><span class="df-add">+${st.add}</span><span class="df-del">−${st.del}</span></span>`;
}

// diffPanelHTML renders the diff panel's body: the commit header plus one
// collapsible section per file. A lone file starts expanded — there is nothing
// to choose between. repoBase is passed through to the header's SHA links.
function diffPanelHTML(diffs, commits, meta, repoBase) {
  const files = Object.keys(diffs);
  const singleFile = files.length === 1;
  const fileChanges = meta && meta.fileChanges;
  return (
    renderCommitHead(commits, repoBase) +
    files
      .map(function (f) {
        const name = f.split('/').pop() || f;
        return (
          `<div class="diff-file-section">` +
          `<div class="diff-file-header${singleFile ? ' expanded' : ''}">` +
          `<svg viewBox="0 0 10 10"><path d="M3 1l4 4-4 4" fill="none" stroke="currentColor" stroke-width="1.5"/></svg>` +
          `<span>${escapeHtml(name)}</span>${diffStatHTML(diffs[f])}${changeAttributionHTML(fileChanges, f)}</div>` +
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

// rolloutPhaseHTML renders the rolling chip shown on a deploying row while a
// zero-downtime rollout cuts a service over; numbered only among several.
function rolloutPhaseHTML(rr) {
  const n = rr.total > 1 ? ' (' + rr.index + '/' + rr.total + ')' : '';
  return `<span class="hk-dot"></span>rolling ${escapeHtml(rr.service)}${n}`;
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
    const noun = AUDIT_FOLD_NOUN[run.status] || auditStatusLabel(run.status);
    const what =
      AUDIT_BAD_STATUSES.indexOf(run.status) === -1
        ? 'routine outcomes'
        : 'identical repeats of the failure above';
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
      `title="${what}, folded — the full history is one click away">` +
      `<span class="hp-fold-glyph">↻</span>` +
      `<span><span class="hp-count">${run.records.length}</span> more ` +
      `${escapeHtml(noun)} since ${escapeHtml(time(oldest.timestamp))}` +
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
        // The repeat marker rides the error cell rather than taking a column of
        // its own: it belongs to what failed, and the row's grid stays put.
        const rep = repeatNoteHTML(r);
        const err =
          r.error || rep
            ? `<span class="ar-err"${r.error ? ` title="${escapeAttr(r.error)}"` : ''}>${rep}${escapeHtml(r.error || '')}</span>`
            : '';
        return (
          `<div class="audit-row"${withTestid ? ' data-testid="audit-row"' : ''} data-status="${escapeAttr(r.status)}">` +
          `<span class="ar-time" data-ts="${escapeAttr(r.timestamp)}" title="${escapeAttr(absolute ? rel : abs)}">${escapeHtml(absolute ? abs : rel)}</span>` +
          `<span class="ar-status">${badgeHTML(r.status)}</span>` +
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
    `<button class="hp-fold-toggle" type="button" data-testid="audit-fold-toggle" ` +
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

// ── Detail panel (docs/dev/ui-redesign-plan.md §3.2) ──
// The panel a selected row opens: a head naming the stack with its labelled
// actions, the verdict, a facts strip and a tab strip over one pane. Pure
// string builders; app-roster.js resolves the state and wires the clicks
// (every control carries data-action, or is a tab).

const CLOSE_ICON =
  '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M6 6l12 12M18 6L6 18"/></svg>';
const JUMP_ICON =
  '<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" aria-hidden="true"><circle cx="8" cy="8" r="6"/><path d="M10.3 5.7l-1.4 3.2-3.2 1.4 1.4-3.2z" stroke-linejoin="round"/></svg>';

// detailActionHTML is one labelled head button. attrs is the raw attribute
// string the action's handler reads (already escaped by the caller).
function detailActionHTML(action, testid, attrs, icon, label, extra) {
  return `<button class="dp-act" type="button" data-action="${action}" data-testid="${testid}"${attrs}>${icon}<span class="dp-lbl">${label}</span>${extra || ''}</button>`;
}

// detailAppLinkHTML is the head's Open app action: a plain link for one
// discovered hostname, a button over the app-link popover for several.
function detailAppLinkHTML(links) {
  if (!links || !links.length) return '';
  if (links.length === 1) {
    return `<span class="link-wrap"><a class="dp-act" data-action="app" data-testid="app-link-btn" href="${escapeAttr('https://' + links[0])}" target="_blank" rel="noopener" title="${escapeAttr('Open ' + links[0])}">${LINK_ICON}<span class="dp-lbl">Open app</span></a></span>`;
  }
  const items = links
    .map(function (h) {
      return `<a href="${escapeAttr('https://' + h)}" target="_blank" rel="noopener">${LINK_ICON}${escapeHtml(h)}</a>`;
    })
    .join('');
  return `<span class="link-wrap">${detailActionHTML('app', 'app-link-btn', ` title="${links.length} app links"`, LINK_ICON, links.length + ' apps')}<div class="link-pop" data-testid="app-link-pop">${items}</div></span>`;
}

// JUMP_LABEL names the head's jump action by the view it leads to.
const JUMP_LABEL = Object.freeze({ deploys: 'Show in Deploys', stacks: 'Show in Stacks' });

// detailHeadHTML is the panel head: icon, name, host chip, badge and meta,
// then the actions that apply, always in the order Open app · Logs · Hooks ·
// Show in … · close. m: stack, host (chip HTML), badge (status badge HTML),
// meta (age · duration text), links (app-link hostnames), logs (the stack runs
// containers), logHost (a peer's name, so the logs stream through the
// primary's proxy), hooks ({pre_deploy, post_deploy} or null), hooksOpen (the
// Hooks pane is showing), jump (offer the jump), jumpView (its target view,
// default deploys).
function detailHeadHTML(m) {
  const stack = escapeAttr(m.stack);
  let actions = detailAppLinkHTML(m.links);
  if (m.logs) {
    const host = m.logHost ? ` data-clog-host="${escapeAttr(m.logHost)}"` : '';
    actions += detailActionHTML(
      'logs',
      'clog-btn',
      ` data-clog-stack="${stack}" data-clog-service=""${host} title="Container logs of every service"`,
      CLOG_ICON,
      'Logs',
    );
  }
  if (m.hooks && hookCount(m.hooks) > 0) {
    const pre = (m.hooks.pre_deploy || []).length,
      post = (m.hooks.post_deploy || []).length;
    actions += detailActionHTML(
      'hooks',
      'hooks-badge',
      ` data-hooks-stack="${stack}" aria-pressed="${m.hooksOpen ? 'true' : 'false'}" title="${escapeAttr(`pre-deploy hooks: ${pre}, post-deploy hooks: ${post}`)}"`,
      HOOK_ICON,
      'Hooks',
      `<span class="dp-n">${pre}+${post}</span>`,
    );
  }
  if (m.jump) {
    const view = m.jumpView || 'deploys';
    actions += detailActionHTML(
      'jump',
      'jump-btn',
      ` data-jump-view="${escapeAttr(view)}" data-jump-stack="${stack}"`,
      JUMP_ICON,
      JUMP_LABEL[view],
    );
  }
  actions += `<button class="dp-act dp-close" type="button" data-action="close" data-testid="detail-close" title="Close" aria-label="${escapeAttr('Close ' + m.stack)}">${CLOSE_ICON}</button>`;
  return (
    '<div class="dp-head">' +
    `<div class="dp-title">${stackIconHTML()}<span class="dp-name" title="${stack}">${escapeHtml(m.stack)}</span>${m.host || ''}${m.badge || ''}${m.meta ? `<span class="dp-meta">${escapeHtml(m.meta)}</span>` : ''}</div>` +
    `<div class="dp-actions">${actions}</div>` +
    '</div>'
  );
}

// VERDICT_GLYPH leads each verdict line by tone, so the line reads before its
// words do.
const VERDICT_GLYPH = Object.freeze({
  danger: '✗',
  rollback: '↺',
  warn: '!',
  ok: '✓',
  muted: '·',
});

// verdictHTML renders the verdict lines (stackVerdict, app-helpers.js) with
// their one action each: Retry once on a held change — only with
// opts.canRetry, since a peer's panel is read-only, and with an empty live
// region (retry-msg) that names a refusal — Logs of the service an unhealthy
// line names (through opts.logHost for a peer), and Open rollback on a retry
// after a rollback.
function verdictHTML(lines, stack, opts) {
  const o = opts || {};
  const body = lines
    .map(function (l) {
      let action = '';
      let msg = '';
      if (l.action && l.action.kind === 'retry' && o.canRetry) {
        action = `<button type="button" class="retry-btn" data-testid="retry-btn" data-retry-stack="${escapeAttr(stack)}">Retry once</button>`;
        msg = '<div class="dp-v-msg" role="status" data-testid="retry-msg"></div>';
      } else if (l.action && l.action.kind === 'rollback') {
        const id = l.action.id ? ` data-rollback-id="${escapeAttr(l.action.id)}"` : '';
        action = detailActionHTML('rollback', 'rollback-link', id, '↺', 'Open rollback');
      } else if (l.action && l.action.kind === 'logs') {
        const host = o.logHost ? ` data-clog-host="${escapeAttr(o.logHost)}"` : '';
        action = detailActionHTML(
          'logs',
          'clog-btn',
          ` data-clog-stack="${escapeAttr(stack)}" data-clog-service="${escapeAttr(l.action.service)}"${host}`,
          CLOG_ICON,
          'Logs',
        );
      }
      const head = l.head ? `<b>${escapeHtml(l.head)}</b>${l.text ? ' ' : ''}` : '';
      const sub = l.sub ? `<div class="dp-v-sub">${escapeHtml(l.sub)}</div>` : '';
      return (
        `<div class="dp-v" data-tone="${escapeAttr(l.tone)}" data-kind="${escapeAttr(l.kind)}">` +
        `<span class="dp-v-ico" aria-hidden="true">${VERDICT_GLYPH[l.tone] || ''}</span>` +
        `<div class="dp-v-body"><div class="dp-v-text">${head}${escapeHtml(l.text || '')}</div>${sub}${msg}</div>` +
        action +
        '</div>'
      );
    })
    .join('');
  return `<div class="dp-verdict" data-testid="detail-verdict">${body}</div>`;
}

// factsHTML is the panel's facts strip; each fact's html is the caller's
// markup (escaped where it carries data).
function factsHTML(facts) {
  const cells = facts
    .map(function (f) {
      return `<div class="dp-fact"><div class="dp-k">${escapeHtml(f.label)}</div><div class="dp-fv">${f.html}</div></div>`;
    })
    .join('');
  return `<div class="dp-facts" data-testid="detail-facts">${cells}</div>`;
}

// stackFactsHTML is a stack's facts: its running version, its last deploy
// (age, commit, the outcome dots and the incident they paper over), its
// health as a count, and its hooks. o: repoBase, nowMs (kept pure).
function stackFactsHTML(entry, health, updates, o) {
  const running = rosterVersionInnerHTML(entry.name, health, updates) || '—';
  let last = 'never';
  if (entry.last_at) {
    last =
      escapeHtml(phaseDuration(o.nowMs - new Date(entry.last_at).getTime()) + ' ago') +
      (entry.last_commit
        ? ' · ' +
          commitLinkHTML(entry.last_commit, {
            cls: 'dp-sha',
            base: o.repoBase,
            title: entry.last_commit,
          })
        : '') +
      outcomeStripHTML(entry.recent, o.nowMs) +
      lastIncidentHTML(entry.last_incident, o.nowMs);
  }
  // On-demand services are stopped on purpose; they are counted apart so
  // the healthy count is out of the services meant to run.
  const services = (health && health.services) || [];
  const onDemand = services.filter(function (s) {
    return s.on_demand;
  }).length;
  const healthy = services.filter(function (s) {
    return !s.on_demand && healthClass(s) === HEALTH.HEALTHY;
  }).length;
  const healthText = services.length
    ? healthy +
      ' of ' +
      (services.length - onDemand) +
      ' healthy' +
      (onDemand ? ' · ' + onDemand + ' on-demand' : '')
    : '—';
  const hooks = entry.hooks || {};
  const hooksText = hookCount(hooks)
    ? (hooks.pre_deploy || []).length + ' pre · ' + (hooks.post_deploy || []).length + ' post'
    : 'none';
  return factsHTML([
    { label: 'Running', html: running },
    { label: 'Last deploy', html: last },
    { label: 'Health', html: escapeHtml(healthText) },
    { label: 'Hooks', html: escapeHtml(hooksText) },
  ]);
}

// tabsHTML is the panel's tab strip (stackTabs, app-helpers.js). A count turns
// red when the tab holds a problem; a tab without a count shows a red "!"
// instead, so the problem still shows. idBase names the panel's tab and pane
// ids; only the selected tab (else the first) is in the Tab order.
function tabsHTML(tabs, active, idBase) {
  const stop = tabs.some(function (t) {
    return t.id === active;
  })
    ? active
    : tabs.length && tabs[0].id;
  const btns = tabs
    .map(function (t) {
      let n = '';
      if (t.count !== null && t.count !== undefined) {
        n = `<span class="dp-n${t.bad ? ' bad' : ''}">${t.count}</span>`;
      } else if (t.bad) {
        n = '<span class="dp-n bad">!</span>';
      }
      const id = escapeAttr(idBase + '-tab-' + t.id);
      const sel = t.id === active ? 'true' : 'false';
      const tabindex = t.id === stop ? '0' : '-1';
      return `<button class="dp-tab" type="button" role="tab" id="${id}" aria-controls="${escapeAttr(idBase)}-pane" data-testid="detail-tab" data-tab="${escapeAttr(t.id)}" aria-selected="${sel}" tabindex="${tabindex}">${escapeHtml(t.label)}${n}</button>`;
    })
    .join('');
  return `<div class="dp-tabs" role="tablist">${btns}</div>`;
}

// tabPaneHTML is the empty pane under tabsHTML's strip, labelled by the
// selected tab, or by label when the head selects it instead (Hooks).
function tabPaneHTML(tabs, active, idBase, label) {
  if (!tabs.length) return '<div class="dp-pane"></div>';
  const inStrip = tabs.some(function (t) {
    return t.id === active;
  });
  const name = inStrip
    ? `aria-labelledby="${escapeAttr(idBase + '-tab-' + active)}"`
    : `aria-label="${escapeAttr(label || '')}"`;
  return `<div class="dp-pane" role="tabpanel" id="${escapeAttr(idBase)}-pane" tabindex="0" ${name}></div>`;
}

// ── Deploys detail panel: one deploy event in the Stacks detail's anatomy ──

// DEPLOY_OUTPUT_LINES is how many lines of a deploy's error the Output tab
// shows before it clamps.
const DEPLOY_OUTPUT_LINES = 6;

// DEPLOY_DETAIL_ID prefixes the deploy detail's tab and pane ids; the view
// keeps one detail open at a time.
const DEPLOY_DETAIL_ID = 'deploy-detail';

// deployFactsHTML is a deploy's facts: its commit (from the lazily loaded
// diff, else the peer record's SHA, else —), the first change chip plus how
// many more, its duration and its changed-file count. o: commits, repoBase.
function deployFactsHTML(evt, o) {
  const opts = o || {};
  const head = opts.commits && opts.commits[0];
  let commit = '—';
  if (head) {
    commit =
      commitLinkHTML(head.sha, {
        cls: 'dp-sha',
        testid: 'commit-sha',
        base: opts.repoBase,
        title: head.sha,
      }) + (head.subject ? `<span class="dp-subject">${escapeHtml(head.subject)}</span>` : '');
  } else if (evt.commit_sha) {
    commit = commitLinkHTML(evt.commit_sha, {
      cls: 'dp-sha',
      testid: 'commit-sha',
      base: opts.repoBase,
      title: evt.commit_sha,
    });
  }
  const chips = imageDeltaChips(evt.image_changes).concat(
    changeChips(evt.file_changes, evt.image_changes),
  );
  // Every chip: the row shows one plus "+N" and points here for the rest.
  const change = chips.length ? chips.join('') : 'none';
  const files = changedFileCount(evt);
  return factsHTML([
    { label: 'Commit', html: commit },
    { label: 'Change', html: change },
    { label: 'Duration', html: escapeHtml(formatDuration(evt.duration_ms)) },
    { label: 'Files', html: files ? files + (files === 1 ? ' file' : ' files') : 'none' },
  ]);
}

// deployPanelHTML is the inside of a deploy row's detail panel: the head
// (badge, age · duration, Logs · Show in Stacks · close), the verdict when the
// outcome needs one, the facts, the tab strip and an empty pane the caller
// fills (deployPaneHTML). o: host (chip HTML), logs, logHost, jump, nowMs,
// repoBase, commits (the loaded diff's), queueReason, tab (the open one).
function deployPanelHTML(evt, o) {
  const opts = o || {};
  const ago = evt.timestamp
    ? phaseDuration(opts.nowMs - new Date(evt.timestamp).getTime()) + ' ago'
    : '';
  const meta = [ago, evt.duration_ms > 0 ? formatDuration(evt.duration_ms) : '']
    .filter(Boolean)
    .join(' · ');
  const lines = deployVerdict(evt, { queueReason: opts.queueReason });
  const tabs = deployTabs(evt, { commits: (opts.commits || []).length });
  return (
    detailHeadHTML({
      stack: evt.stack,
      host: opts.host,
      badge: badgeHTML(evt.status),
      meta: meta,
      logs: opts.logs,
      logHost: opts.logHost,
      jump: opts.jump,
      jumpView: 'stacks',
    }) +
    (lines.length ? verdictHTML(lines, evt.stack, { logHost: opts.logHost }) : '') +
    deployFactsHTML(evt, opts) +
    (tabs.length ? tabsHTML(tabs, opts.tab, DEPLOY_DETAIL_ID) : '') +
    tabPaneHTML(tabs, opts.tab, DEPLOY_DETAIL_ID)
  );
}

// deployOutputHTML is the Output tab: the deploy's full error text, clamped
// beyond DEPLOY_OUTPUT_LINES lines behind a Show all / Show less toggle.
function deployOutputHTML(error, expanded) {
  const n = String(error || '').split('\n').length;
  const long = n > DEPLOY_OUTPUT_LINES;
  const cls = long && !expanded ? 'dp-output clamped' : 'dp-output';
  const id = DEPLOY_DETAIL_ID + '-output';
  const toggle = long
    ? `<button type="button" class="dp-more-btn" data-action="output-more" aria-expanded="${!!expanded}" aria-controls="${id}">${expanded ? 'Show less' : 'Show all ' + n + ' lines'}</button>`
    : '';
  return `<pre class="${cls}" id="${id}" data-testid="deploy-output">${escapeHtml(error)}</pre>${toggle}`;
}

// driftListHTML lists the containers a self-heal found drifted.
function driftListHTML(drift) {
  return (
    '<ul class="heal-drift-list" data-testid="heal-drift">' +
    (drift || [])
      .map(function (d) {
        return (
          `<li><span class="hd-name">${escapeHtml(d.name)}</span>` +
          `<span class="hd-status hd-${escapeAttr(d.status)}">${escapeHtml(d.status)}</span></li>`
        );
      })
      .join('') +
    '</ul>'
  );
}

// commitListHTML lists a push's commits, newest first, each with its SHA;
// expanded shows it at once rather than behind the diff head's pill.
function commitListHTML(commits, repoBase, expanded) {
  return (
    `<ul class="diff-commit-list${expanded ? ' expanded' : ''}" data-testid="diff-commit-list">` +
    (commits || [])
      .map(function (c) {
        return (
          `<li>${commitLinkHTML(c.sha, { cls: 'm-sha', base: repoBase, title: c.sha })}<span>` +
          `<span class="cl-subject">${escapeHtml(c.subject || '')}</span> ` +
          `<span class="cl-meta">— ${escapeHtml(c.author || '')}${c.date ? ', ' + escapeHtml(formatTime(c.date)) : ''}</span></span></li>`
        );
      })
      .join('') +
    '</ul>'
  );
}

// deployPaneHTML is one tab's content. d: diffs and commits (the loaded
// diff), loading (the fetch is still running), repoBase, expanded (Output
// shows every line). A load error is the caller's: it carries a Retry.
function deployPaneHTML(tab, evt, d) {
  const data = d || {};
  switch (tab) {
    case DEPLOY_TAB.changes: {
      if (data.loading) return '<div class="diff-loading">Loading diffs…</div>';
      if (data.diffs && Object.keys(data.diffs).length) {
        return `<div class="diff-panel" data-testid="diff-panel">${diffPanelHTML(data.diffs, null, { fileChanges: evt.file_changes }, data.repoBase)}</div>`;
      }
      if (Array.isArray(evt.changed_files) && evt.changed_files.length) {
        return `<div class="files-list" data-testid="files-panel">${filesPanelHTML(evt.changed_files, evt.file_changes)}</div>`;
      }
      return '<div class="diff-loading">No diff recorded.</div>';
    }
    case DEPLOY_TAB.drift:
      return driftListHTML(evt.heal_drift);
    case DEPLOY_TAB.output:
      return deployOutputHTML(evt.error, data.expanded);
    case DEPLOY_TAB.commits:
      return commitListHTML(data.commits, data.repoBase, true);
    default:
      return '';
  }
}

// Dual-use export, same pattern as app-helpers.js: skipped in the browser
// (the functions are already globals), used by `node --test`.
if (typeof module !== 'undefined' && module.exports) {
  module.exports = {
    escapeHtml,
    deployFactsHTML,
    deployPanelHTML,
    deployOutputHTML,
    deployPaneHTML,
    driftListHTML,
    commitListHTML,
    ROW_KIND,
    tableRowHTML,
    deployWhenHTML,
    deployShaHTML,
    escapeAttr,
    commitLinkHTML,
    versionChipHTML,
    imageDeltaChipsHTML,
    changeChipsHTML,
    changeCellHTML,
    changeAttributionHTML,
    renderCommitHead,
    badgeHTML,
    serviceVersionHTML,
    updateCheckMetaHTML,
    healthHistoryHTML,
    healthStripHTML,
    clogBtnHTML,
    hookCount,
    hostChipHTML,
    rosterVersionInnerHTML,
    rosterVersionCellHTML,
    rosterUpdateChipHTML,
    rosterStatusHTML,
    heldChipHTML,
    rosterHealthHTML,
    outcomeLabel,
    outcomeStripHTML,
    lastIncidentHTML,
    repeatNoteHTML,
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
    filesPanelHTML,
    diffContentHTML,
    diffPanelHTML,
    hookPhaseHTML,
    rolloutPhaseHTML,
    logLineHTML,
    logDiffBlockHTML,
    auditRowsHTML,
    clogSvcsHTML,
    detailHeadHTML,
    verdictHTML,
    factsHTML,
    tabsHTML,
    tabPaneHTML,
    stackFactsHTML,
  };
}
