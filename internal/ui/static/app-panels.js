// app-panels.js — the per-stack detail surfaces every view opens the same way
// (dev-docs/ui-design-concept.md, "one look and feel across every view"): the
// bound row panels — diff, files, heal, deploy history (ADR-0033), containers/
// health (ADR-0027/0031), deploy hooks (ADR-0038), watched files — plus the
// row-cell affordances that open them: the ⋯ overflow menu, the app-link
// popover, the container-logs and hooks buttons, and the stack icon. Cut out of
// the app script by view (ADR-0035 amendment) and attached as App.panels.
//
// Loads after app-state.js and before every view. Reads only the store and the
// per-host resolvers; a view passes in the row or cell it wants a panel on.
// The bootstrap calls init() at the spot the popover/menu/fold listeners used to
// register, so document-level listener order is unchanged.
App.panels = (function () {
  const S = App.state;
  const {
    healthMapFor,
    healthwatchMapFor,
    appLinksMapFor,
    repoWebURLFor,
    updatesFor,
    stackUpdatesFor,
  } = App.resolve;

  // The currently open app-link popover's wrapping element (multi-host case),
  // or null. One open at a time; a stale reference (its row was replaced by a
  // re-render) simply fails every future .contains() check and self-clears.
  let openLinkWrap = null;

  const CHECK_ICON =
    '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="3" stroke-linecap="round" stroke-linejoin="round"><path d="M5 13l4 4L19 7"/></svg>';

  // linkCell resolves a stack's discovered app-link hostnames on a host and
  // renders the cell (linkCellHTML in app-render.js owns the markup).
  function linkCell(stack, host) {
    return linkCellHTML(appLinksMapFor(host)[stack]);
  }

  // toggleAppLinkPopover opens wrap's popover, closing whichever was open
  // before (including wrap itself, so a second click closes it again).
  function toggleAppLinkPopover(wrap) {
    const wasOpen = wrap === openLinkWrap;
    closeAppLinkPopover();
    if (!wasOpen) {
      wrap.classList.add('open');
      openLinkWrap = wrap;
    }
  }
  function closeAppLinkPopover() {
    if (openLinkWrap) openLinkWrap.classList.remove('open');
    openLinkWrap = null;
  }

  // ── Row overflow menu (⋯) — T3.13 ──
  // The newest deploy row folds its secondary actions (deploy history, container
  // logs, deploy hooks) behind one ⋯ button, so the row rests at identity +
  // status + the jump action instead of a cluster of look-alike glyphs. Same
  // one-open-at-a-time behaviour as the app-link popover; the action buttons
  // inside keep their own classes, testids and click handlers — they are only
  // relocated, so nothing about opening the panels changes.
  const MORE_ICON =
    '<svg viewBox="0 0 16 16" fill="currentColor" aria-hidden="true"><circle cx="3" cy="8" r="1.5"/><circle cx="8" cy="8" r="1.5"/><circle cx="13" cy="8" r="1.5"/></svg>';
  let openMoreWrap = null;

  // ensureRowMore returns the row's ⋯ wrapper, creating it (button + empty
  // popover) on first use. The action buttons are appended into its .more-pop.
  function ensureRowMore(cell, stack) {
    let wrap = cell.querySelector('.row-more');
    if (!wrap) {
      wrap = document.createElement('span');
      wrap.className = 'row-more';
      wrap.innerHTML =
        '<button class="more-btn" type="button" data-testid="more-btn" aria-haspopup="true" aria-expanded="false"' +
        ' title="More actions" aria-label="' +
        escapeAttr('more actions for ' + stack) +
        '">' +
        MORE_ICON +
        '</button>' +
        '<div class="more-pop" data-testid="more-pop"></div>';
      cell.appendChild(wrap);
    }
    return wrap;
  }

  // moreMenuItem turns a relocated icon button into a labelled full-width menu
  // row: the icon keeps its meaning, the text names the action (so the row no
  // longer relies on look-alike glyphs), and any trailing count (hooks) stays put.
  function moreMenuItem(btn, label) {
    btn.classList.add('more-item');
    btn.removeAttribute('data-taptip'); // the visible label replaces the tap-tip
    const lbl = document.createElement('span');
    lbl.className = 'mi-label';
    lbl.textContent = label;
    const count = btn.querySelector('.hk-count');
    if (count) btn.insertBefore(lbl, count);
    else btn.appendChild(lbl);
    return btn;
  }

  // Keep the popover on-screen: it opens left-aligned under the ⋯, but flips to
  // right-aligned when that would spill past the viewport edge — the portrait-
  // phone case, where the stack cell sits close to the right edge.
  function positionMorePop(wrap) {
    const pop = wrap.querySelector('.more-pop');
    pop.classList.remove('align-right');
    const r = wrap.getBoundingClientRect();
    if (r.left + pop.offsetWidth > window.innerWidth - 8) pop.classList.add('align-right');
  }
  function toggleMoreMenu(wrap) {
    const wasOpen = wrap === openMoreWrap;
    closeMoreMenu();
    if (!wasOpen) {
      wrap.classList.add('open');
      const btn = wrap.querySelector('.more-btn');
      if (btn) btn.setAttribute('aria-expanded', 'true');
      positionMorePop(wrap); // measured after .open makes the popover laid-out
      openMoreWrap = wrap;
    }
  }
  function closeMoreMenu() {
    if (openMoreWrap) {
      openMoreWrap.classList.remove('open');
      const btn = openMoreWrap.querySelector('.more-btn');
      if (btn) btn.setAttribute('aria-expanded', 'false');
    }
    openMoreWrap = null;
  }

  // closeMoreMenuIf closes the ⋯ menu only if wrap is the one that is open — a
  // row losing its menu asks this rather than comparing the module's state.
  function closeMoreMenuIf(wrap) {
    if (openMoreWrap === wrap) closeMoreMenu();
  }

  // createHealPanel wraps the healed row's detail body in its element, bound to
  // the row (variant A) so it shares the teal status bar.
  function createHealPanel(drift, meta) {
    const el = document.createElement('div');
    el.className = 'heal-panel';
    el.dataset.testid = 'heal-panel';
    if (meta) {
      el.classList.add('bound');
      el.dataset.status = meta.status;
    }
    el.innerHTML = healPanelHTML(drift);
    return el;
  }

  function createFilesPanel(files, meta) {
    const el = document.createElement('div');
    el.className = 'files-list';
    el.dataset.testid = 'files-panel';
    // Bound to its row (variant A) when opened from a deploy row, so it carries
    // the same status left bar as the diff panel — keeps the bar continuous when
    // an error detail is stacked below it. Unbound in the log view (no meta).
    if (meta) {
      el.classList.add('bound');
      el.dataset.status = meta.status;
    }
    el.innerHTML = filesPanelHTML(files);
    return el;
  }

  // renderDiffPanel wraps the diff body in its element. meta (optional) carries
  // the deploy row's stack + status: when set, the panel binds to its row
  // (variant A — shared status bar/tint). Opened from a log line, meta is
  // absent and the panel stays unbound.
  function renderDiffPanel(diffs, commits, meta) {
    const el = document.createElement('div');
    el.className = 'diff-panel';
    el.dataset.testid = 'diff-panel';
    if (meta && meta.status) {
      el.classList.add('bound');
      el.dataset.status = meta.status;
    }
    // The diff panel only ever shows the local repo's commits — a peer's diff
    // arrives without commit metadata — so the local forge base is right here.
    el.innerHTML = diffPanelHTML(diffs, commits, meta, S.repoWebURL);
    return el;
  }

  // createLoadError builds the shared amber "couldn't load" line (T4.16) that
  // the lazy detail fetches (deploy history, diff, peer diff) show when a fetch
  // fails or answers 5xx. It is deliberately NOT the red .error-detail: the
  // deploy itself is fine — only the panel fetch failed — and it is distinct
  // from the muted genuine-empty line. onRetry runs when the user clicks Retry;
  // the caller re-runs its fetch and replaces this element with the result (or a
  // fresh load-error), so no reset bookkeeping is needed here.
  function createLoadError(message, onRetry) {
    const el = document.createElement('div');
    el.className = 'load-error';
    el.dataset.testid = 'load-error';
    el.innerHTML =
      '<svg class="le-ico" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" aria-hidden="true">' +
      '<path d="M12 8v5"/><circle cx="12" cy="16.6" r="1" fill="currentColor" stroke="none"/>' +
      '<path d="M10.3 3.9 2.7 17a2 2 0 0 0 1.7 3h15.2a2 2 0 0 0 1.7-3L13.7 3.9a2 2 0 0 0-3.4 0Z" stroke-linejoin="round"/>' +
      '</svg>' +
      '<span class="le-msg">' +
      escapeHtml(message) +
      '</span>' +
      '<button class="le-retry" type="button" data-testid="load-retry" aria-label="Retry loading">' +
      '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M3 12a9 9 0 1 0 3-6.7L3 8"/><path d="M3 3v5h5"/></svg>Retry</button>';
    const btn = el.querySelector('.le-retry');
    btn.addEventListener('click', function () {
      if (el.classList.contains('busy')) return; // a retry is already in flight
      el.classList.add('busy');
      el.querySelector('.le-msg').textContent = 'Retrying…';
      onRetry(el);
    });
    return el;
  }

  const diffCache = {};

  // fetchDiffs resolves callback(diffs, commits, err). err distinguishes a real
  // fetch failure (network drop / 5xx → an amber load-error, T4.16) from a
  // legitimate "no diff recorded" (2xx-empty or 404 evicted event → the file
  // list stands in). Without that third arg both looked identical to the caller.
  function fetchDiffs(eventId, callback) {
    if (diffCache[eventId]) {
      callback(diffCache[eventId].diffs, diffCache[eventId].commits, false);
      return;
    }
    fetch('/api/events/' + eventId + '/diffs')
      .then(function (r) {
        if (r.ok) return r.json();
        if (r.status >= 500) throw new Error('server'); // transient → load-error
        return null; // 404 etc: genuine no-diff
      })
      .then(function (data) {
        if (data && data.diffs) {
          diffCache[eventId] = { diffs: data.diffs, commits: data.commits || null };
          callback(data.diffs, diffCache[eventId].commits, false);
        } else {
          callback(null, null, false);
        }
      })
      .catch(function () {
        callback(null, null, true);
      });
  }

  // clogButton is the DOM-node form of clogBtnHTML (app-render.js) for the
  // deploy row's action cell.
  function clogButton(stack, service) {
    const tmp = document.createElement('template');
    tmp.innerHTML = clogBtnHTML(stack, service);
    return tmp.content.firstElementChild;
  }

  // ─── Deploy hooks (ADR-0038) ───

  function hooksFor(stack) {
    const e = S.rosterSnap.find(function (r) {
      return r.name === stack;
    });
    return e && e.hooks ? e.hooks : null;
  }
  function hooksBadgeButton(stack, hooks) {
    const tmp = document.createElement('template');
    tmp.innerHTML = hooksBadgeHTML(stack, hooks);
    return tmp.content.firstElementChild;
  }
  // Bound panel (variant A) listing the configured commands, from the snapshot.
  function createHooksPanel(stack) {
    const hooks = hooksFor(stack) || {};
    const group = function (title, cmds) {
      if (!cmds || !cmds.length) return '';
      const lines = cmds
        .map(function (c) {
          return `<div class="hooks-cmd" data-testid="hooks-cmd"><span class="hk-prompt">$</span> ${escapeHtml(c)}</div>`;
        })
        .join('');
      return `<div class="hooks-group"><div class="hk-title">${title}<span class="hk-num">${cmds.length}</span></div>${lines}</div>`;
    };
    const panel = document.createElement('div');
    panel.className = 'hooks-panel bound';
    panel.dataset.testid = 'hooks-panel';
    panel.innerHTML =
      `<div class="hooks-head"><span class="hk-stack">${escapeHtml(stack)}</span><span class="hk-label">deploy hooks</span></div>` +
      group('pre_deploy', hooks.pre_deploy) +
      group('post_deploy', hooks.post_deploy);
    return panel;
  }
  function closeHooksPanel(row) {
    const next = row.nextElementSibling;
    if (next && next.classList.contains('hooks-panel')) next.remove();
    row.classList.remove('hooks-open');
  }

  // Wraps the running-hook sub-label, shared by both views so it looks
  // identical in Deploys and Stacks.
  function hookPhaseNode(hr) {
    const phase = document.createElement('span');
    phase.className = 'hook-phase';
    phase.dataset.testid = 'hook-phase';
    phase.innerHTML = hookPhaseHTML(hr);
    return phase;
  }

  function iconURL(stack) {
    return '/api/icons/' + encodeURIComponent(stack);
  }

  // populateIcon fills a .stack-icon chip with the stack's image, swapping to a
  // monogram if the request fails (unknown stack, no match, or offline).
  function populateIcon(chip, stack) {
    if (!chip) return;
    chip.classList.remove('mono');
    chip.textContent = '';
    const img = document.createElement('img');
    img.alt = '';
    img.loading = 'lazy';
    img.addEventListener('error', function () {
      iconFallback(chip, stack);
    });
    img.src = iconURL(stack);
    chip.appendChild(img);
  }

  function iconFallback(chip, stack) {
    chip.classList.add('mono');
    chip.textContent = (stack || '?').charAt(0);
  }

  function closeHealthPanel(row) {
    const next = row.nextElementSibling;
    if (next && next.classList.contains('health-panel')) next.remove();
    row.classList.remove('health-open');
    row.removeAttribute('data-health');
  }

  // closeDiffPanel is the files/diff counterpart of closeHealthPanel. Together
  // they enforce one open panel per row: opening either panel first closes the
  // other, so a toggleable panel is always the row's direct next sibling.
  function closeDiffPanel(row) {
    const next = row.nextElementSibling;
    if (
      next &&
      (next.classList.contains('files-list') ||
        next.classList.contains('diff-panel') ||
        next.classList.contains('heal-panel') ||
        next.classList.contains('load-error'))
    )
      next.remove();
    row.classList.remove('diff-open');
  }

  // Counter behind every deploy-history panel's raw-list id (see the fold
  // toggle below); a panel is rebuilt on each open, so it only ever grows.
  let auditHistorySeq = 0;

  // closeAuditPanel is the deploy-history counterpart, part of the same
  // one-panel-per-row rule (ADR-0033): opening the history panel closes the
  // health/diff panel and vice versa.
  function closeAuditPanel(row) {
    const next = row.nextElementSibling;
    if (next && next.classList.contains('audit-panel')) next.remove();
    row.classList.remove('audit-open');
  }

  // fetchAudit loads one stack's durable deploy history. Not cached: the history
  // grows with every deploy, and a re-fetch against localhost is cheap, so a
  // fresh open always shows the latest rather than risking a stale panel.
  // fetchAudit resolves callback(records, err). err is true on a real fetch
  // failure (network / 5xx) so the panel can show an amber load-error (T4.16)
  // rather than the identical "No recorded deploys" line a genuinely-empty
  // history produces.
  function fetchAudit(stack, callback) {
    fetch('/api/audit?stack=' + encodeURIComponent(stack))
      .then(function (r) {
        if (r.ok) return r.json();
        if (r.status >= 500) throw new Error('server');
        return [];
      })
      .then(function (records) {
        callback(records || [], false);
      })
      .catch(function () {
        callback(null, true);
      });
  }

  // createAuditPanel returns the panel immediately with a loading line and fills
  // it once the fetch resolves — the same lazy pattern the diff panel uses. The
  // panel is removed-if-closed guard keeps a slow fetch from resurrecting it.
  function createAuditPanel(stack) {
    const el = document.createElement('div');
    el.className = 'audit-panel';
    el.dataset.testid = 'audit-panel';
    el.dataset.auditFor = stack;
    // No heading — the deploy count is the only header text.
    el.innerHTML =
      `<div class="ap-head"><span class="ap-count"></span></div>` +
      `<div class="ap-empty">Loading history…</div>`;
    fetchAudit(stack, function (records, err) {
      if (!el.parentNode) return; // closed (or replaced) while loading
      renderAuditRecords(el, stack, records, err);
    });
    return el;
  }

  // createWatchedPanel returns the change-detection panel: the input files whose
  // hashes decide whether this stack redeploys, and — after a clean deploy — the
  // commit nothing has changed since. It answers "I pushed and this stack did
  // nothing, why?", which every other surface leaves to state.yaml on the host.
  // The data rides the `stacks` snapshot, so unlike the history panel beside it
  // there is no fetch and no loading state.
  function createWatchedPanel(stack) {
    const entry =
      S.rosterSnap.find(function (r) {
        return r.name === stack;
      }) || {};
    const el = document.createElement('div');
    el.className = 'watched-panel';
    el.dataset.testid = 'watched-panel';
    el.dataset.watchedFor = stack;
    el.innerHTML = watchedPanelHTML(entry, S.repoWebURL);
    return el;
  }

  function renderAuditRecords(el, stack, records, err) {
    // Fetch failed (network / 5xx): an amber load-error with a retry that
    // re-runs the same fetch, distinct from a stack that genuinely has no
    // history (T4.16). The :has(> .load-error) rule flattens the panel bar.
    if (err) {
      el.innerHTML = '';
      el.appendChild(
        createLoadError("Couldn't load deploy history.", function () {
          el.innerHTML =
            '<div class="ap-head"><span class="ap-count"></span></div><div class="ap-empty">Loading history…</div>';
          fetchAudit(stack, function (recs, e) {
            if (!el.parentNode) return;
            renderAuditRecords(el, stack, recs, e);
          });
        }),
      );
      return;
    }
    const count = el.querySelector('.ap-count');
    if (count) count.textContent = auditCountText(records.length);
    if (!records.length) {
      const empty = el.querySelector('.ap-empty');
      if (empty) empty.textContent = 'No recorded deploys for this stack yet.';
      return;
    }
    // The fold toggle's aria-controls needs a document-unique id: several rows
    // can hold an open history at once, so the stack name would not do.
    const body = auditRowsHTML(records, S.repoWebURL, S.absoluteTime, {
      id: 'a' + ++auditHistorySeq,
    });
    // Replace the loading line with the rows (keep the head).
    const head = el.querySelector('.ap-head');
    el.innerHTML = '';
    if (head) el.appendChild(head);
    el.insertAdjacentHTML('beforeend', body);
  }

  // Feeds the ids the timeline's fold toggle points aria-controls at. A counter
  // rather than stack+service: the same service can hold an open panel on two
  // surfaces at once (a Deploys row and the roster card), and an id must be
  // unique across the whole document.
  let healthHistorySeq = 0;

  // host is optional: omitted (or the primary's own name) reads the live local
  // snapshot; a peer name reads that peer's fanned-in health (ADR-0048), so peer
  // rows render the same containers panel. The per-service log button is
  // host-aware: a peer's streams through the primary's peer-logs proxy.
  function createHealthPanel(stack, host) {
    const el = document.createElement('div');
    el.className = 'health-panel';
    el.dataset.testid = 'health-panel';
    el.dataset.healthFor = stack;
    if (host) el.dataset.host = host;
    const h = healthMapFor(host)[stack];
    const status = (h && h.status) || 'unknown';
    el.dataset.health = status; // drives the shared --hc colour (variant A)
    // The stack's update-check map (ADR-0054): per-service ⇡ markers on the
    // version chips below, and a count + freshness summary in the head.
    const upd = stackUpdatesFor(stack, host) || {};
    const updMeta = updateCheckMetaHTML(
      Object.keys(upd).length,
      (updatesFor(host) || {}).checked_at,
      Date.now(),
    );
    // Header echoes the stack + status so the panel names its own row.
    const head =
      `<div class="health-panel-head">` +
      `<span class="hp-head-label">health</span>` +
      `<span class="hp-head-who">${escapeHtml(stack)}</span>` +
      updMeta +
      `<span class="hp-head-pill"><span class="hdot"></span>${escapeHtml(status)}</span>` +
      `</div>`;
    const svcs = (h && h.services) || [];
    if (!svcs.length) {
      el.innerHTML = head + '<div class="hp-empty">No services reported for this stack.</div>';
      return el;
    }
    // Versions get their own column only when the snapshot carries images at all
    // (a stack of stopped containers, or a peer running an older skipper, reports
    // none) — an empty column would otherwise indent every line for nothing.
    const withVersions = svcs.some(function (s) {
      return !!s.image;
    });
    if (withVersions) el.classList.add('has-versions');
    el.innerHTML =
      head +
      svcs
        .map(function (s) {
          // One status per line: the classified per-service status (backend field,
          // healthClass as fallback for older snapshots). The raw `health:` value is
          // never shown — with a healthcheck it always equals the classified status,
          // so spelling it out only duplicated the coloured status. The state text
          // keeps the container fact the status doesn't carry (running/exited/…).
          const st = s.status || healthClass(s);
          // With the health watch on, the service line carries the current
          // phase's age and a status timeline below it (ADR-0031).
          const phases = (healthwatchMapFor(host)[stack] || {})[s.name];
          const age =
            phases && phases.length
              ? ` <span class="hp-for">· ${escapeHtml(phaseDuration(Date.now() - new Date(phases[0].since).getTime()))}</span>`
              : '';
          // An on-demand service is labelled so its stopped state reads as the
          // intended idle (skipper stops it after the deploy; the scheduler starts
          // it on request), not as something to worry about.
          const state = s.on_demand ? s.state + ' · on-demand' : s.state;
          const svcTitle = s.on_demand
            ? ' title="on-demand container: stopped by skipper after the deploy, started by the scheduler on request"'
            : '';
          // The running version, as the same chip the Deploys column and the roster
          // row use. A service without an image (nothing running) keeps an empty cell
          // so the lines stay aligned.
          const ver = withVersions
            ? `<span class="hp-ver" data-testid="health-version">${s.image ? serviceVersionHTML(s.name, s.image, false, upd[s.name]) : ''}</span>`
            : '';
          return (
            `<div class="hp-svc" data-testid="health-service"${svcTitle}>` +
            `<span class="hp-name">${escapeHtml(s.name)}</span>` +
            ver +
            `<span class="hp-state">${escapeHtml(state)}</span>` +
            `<span class="hp-status" data-health="${escapeAttr(st)}"><span class="hdot"></span>${escapeHtml(st)}${age}</span>` +
            clogBtnHTML(stack, s.name, host) +
            `</div>` +
            healthHistoryHTML(phases, repoWebURLFor(host), Date.now(), {
              onDemand: !!s.on_demand,
              id: 'h' + ++healthHistorySeq,
            })
          );
        })
        .join('');
    return el;
  }

  // init registers the document-level listeners the panels rely on: click-away
  // and Escape for the app-link popover and the ⋯ menu, and the fold toggle.
  function init() {
    document.addEventListener('click', function (e) {
      if (openLinkWrap && !openLinkWrap.contains(e.target)) closeAppLinkPopover();
      if (openMoreWrap && !openMoreWrap.contains(e.target)) closeMoreMenu();
    });
    document.addEventListener('keydown', function (e) {
      if (e.key === 'Escape') {
        closeAppLinkPopover();
        closeMoreMenu();
      }
    });

    // ── Fold toggle (health timeline + deploy history) ──
    // Both panels of the expand card fold routine repetition away — the timeline
    // its deploy/restart cycles (healthHistoryHTML), the history its runs of one
    // outcome (auditRowsHTML) — and both swap the folded view for the verbatim
    // list through this one toggle. A single delegated listener covers every
    // surface either panel appears on (deploy rows, roster card, peer detail);
    // the panels are rebuilt on each open/snapshot, so per-instance wiring would
    // re-bind constantly. Both live outside any row element, so no row-toggle
    // handler sees this click.
    document.addEventListener('click', function (e) {
      const btn = e.target.closest && e.target.closest('.hp-fold-toggle');
      if (!btn) return;
      const hist = btn.closest('.hp-history, .ap-history');
      if (!hist) return;
      const raw = hist.classList.toggle('show-raw');
      btn.setAttribute('aria-expanded', String(raw));
      btn.textContent = raw ? btn.dataset.foldLabel : btn.dataset.label;
    });
  }

  return {
    CHECK_ICON,
    linkCell,
    toggleAppLinkPopover,
    closeAppLinkPopover,
    ensureRowMore,
    moreMenuItem,
    toggleMoreMenu,
    closeMoreMenu,
    closeMoreMenuIf,
    createHealPanel,
    createFilesPanel,
    renderDiffPanel,
    createLoadError,
    fetchDiffs,
    clogButton,
    hooksFor,
    hooksBadgeButton,
    createHooksPanel,
    closeHooksPanel,
    hookPhaseNode,
    populateIcon,
    closeHealthPanel,
    closeDiffPanel,
    closeAuditPanel,
    createAuditPanel,
    createWatchedPanel,
    createHealthPanel,
    init,
  };
})();
