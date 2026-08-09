(function () {
  const tbody = document.getElementById('tbody');
  const table = document.getElementById('deploy-table');
  const emptyState = document.getElementById('empty-state');
  const loadingState = document.getElementById('loading-state');
  const connDot = document.getElementById('conn-dot');
  const connText = document.getElementById('conn-text');
  const stackSearchBtn = document.getElementById('stack-search-btn');
  const a11yAnnounce = document.getElementById('a11y-announce');
  // ── A11y announcements + UI diagnostics (T2.8) ──

  // announce voices a message through the off-screen polite live region (T2.8).
  // The region is cleared first, then set on the next frame, so two identical
  // consecutive outcomes (e.g. two stacks both "deploy failed") still re-fire.
  function announce(msg) {
    if (!a11yAnnounce || !msg) return;
    a11yAnnounce.textContent = '';
    requestAnimationFrame(function () {
      a11yAnnounce.textContent = msg;
    });
  }
  // The `/api/events` stream replays the deploy-event backlog through the same
  // handler as live events, so announcements must be suppressed until that burst
  // has settled — otherwise every (re)connect would read the whole history aloud
  // (T2.8). Each connect arms a one-shot gate: the replay burst (rendered within
  // a frame or two) lands while it is closed, then it opens and stays open, so
  // every later live deploy announces. Not reset by incoming events — a live
  // deploy arriving soon after connect must not keep pushing the gate shut.
  //
  // The gate's state is mirrored onto the live region as data-announce-ready,
  // so "the replay burst is over" is observable instead of having to be waited
  // out: a test asserts on the attribute rather than sleeping past the timer.
  // uiNote records a diagnostic on the console and keeps a bounded copy on the
  // page. The console alone is not enough: nothing collects it, so a failure in
  // CI (or a user's report) arrives without the reason the UI already knew. The
  // e2e harness reads this buffer when a test fails; keeping it a plain array
  // means no listener has to be attached while the test runs, which matters —
  // subscribing to the console is itself enough to change the timing of the
  // race UC11 is chasing.
  const UI_NOTES_MAX = 50;
  window.__uiNotes = [];
  // level picks the console method, so a by-design drop stays `debug` while a
  // failure is a `warn` — both are recorded either way, because which one
  // explains a bug is not knowable in advance.
  function uiNote(level) {
    const args = Array.prototype.slice.call(arguments, 1);
    console[level].apply(console, args);
    window.__uiNotes.push(
      new Date().toISOString() +
        ' ' +
        args
          .map(function (a) {
            return a instanceof Error ? a.message : String(a);
          })
          .join(' '),
    );
    // Bounded: a page left open for hours must not grow this without limit.
    if (window.__uiNotes.length > UI_NOTES_MAX) window.__uiNotes.shift();
  }
  // The web fonts arriving is the page's one late layout reflow (their swap
  // re-wraps text); timestamping it lets a stray-click note be ordered against
  // the shift that caused it (T8).
  if (document.fonts && document.fonts.ready) {
    document.fonts.ready.then(function () {
      uiNote('debug', 'fonts: settled');
    });
  }

  // ANNOUNCE_SETTLE_MS is how long after a connect the gate stays shut: long
  // enough for the replay burst to render, short enough that a deploy landing
  // right after a reconnect is still voiced.
  const ANNOUNCE_SETTLE_MS = 700;
  let announceReady = false;
  let announceSettleTimer = null;
  // setAnnounceReady moves the gate and its published state together, so the
  // attribute can never disagree with the flag the announce path reads.
  function setAnnounceReady(ready) {
    announceReady = ready;
    if (a11yAnnounce) a11yAnnounce.dataset.announceReady = ready ? '1' : '0';
  }
  function armAnnounceGate() {
    setAnnounceReady(false);
    clearTimeout(announceSettleTimer);
    announceSettleTimer = setTimeout(function () {
      setAnnounceReady(true);
    }, ANNOUNCE_SETTLE_MS);
  }
  // Publish the closed state at boot, before the first connect arms the gate,
  // so the attribute is never absent while the flag says shut.
  setAnnounceReady(false);
  const deployStatus = document.getElementById('deploy-status');
  const dsActive = deployStatus.querySelector('.ds-active');
  const dsNext = document.getElementById('deploy-next');
  const dsCount = document.getElementById('deploy-count');
  const runDrawer = document.getElementById('run-drawer');
  const runSub = document.getElementById('run-sub');
  const runCount = document.getElementById('run-count');
  const runList = document.getElementById('run-list');
  const deployingRows = {};
  // Queued (paused) rows, keyed by stack, so a stack's pending row is replaced
  // rather than duplicated, and removed once it deploys or drains.
  const queuedRows = {};
  let hasRows = false;
  let absoluteTime = localStorage.getItem('timeMode') === 'absolute';
  // Whether the deploy table's Version column is shown. On by default; the
  // Deploys view-options toggle persists an off choice per browser. Only the
  // explicit 'off' hides it, so a first-time visitor sees the column.
  let imageDeltaOn = localStorage.getItem('imageDelta') !== 'off';

  // Autosync state, populated from the 'autosync' and 'queue' SSE events.
  let autosyncSnap = null; // GET /api/autosync shape
  let autosyncVersion = null; // version of the applied snapshot
  let queueSnap = { count: 0, pending: [] }; // GET /api/queue shape
  let queueByStack = {}; // stack name -> pending item

  // Run look-ahead, from the 'upcoming' SSE event: stacks that will deploy after
  // the one currently deploying, in deploy order.
  let upcomingSnap = [];

  // Live stack health, from the 'health' SSE snapshot: stack name -> { status,
  // services }. Rendered as a pill on the newest row of each stack (ADR-0027).
  let healthSnap = {};

  // Health-watch status history, from the 'healthwatch' SSE snapshot: stack ->
  // service -> phases (newest first, <= 10). Empty when the watchdog is off —
  // the health panel then renders without age/timeline (ADR-0031).
  let healthwatchSnap = {};

  // ── Disabled-stacks strip (ADR-0034) ──

  // Names parked with disabled: true, from the 'stacks' SSE snapshot (stack
  // discovery, ADR-0034). Rendered as a quiet chip line below the deploy table;
  // empty (and the line hidden) in host-list mode.
  let disabledSnap = [];

  function renderDisabledStacks() {
    const wrap = document.getElementById('disabled-stacks');
    const list = document.getElementById('disabled-list');
    list.textContent = '';
    if (!disabledSnap.length) {
      wrap.classList.remove('shown');
      return;
    }
    disabledSnap.forEach(function (name) {
      const chip = document.createElement('span');
      chip.className = 'dis-chip';
      chip.textContent = name;
      list.appendChild(chip);
    });
    wrap.classList.add('shown');
  }

  // The Stacks view roster: the full stack set (stack discovery, ADR-0034, or
  // the host stacks: list) with each stack's last outcome, from the
  // 'stacks' SSE snapshot. Inventory, not an event log — every declared stack
  // appears, including never-deployed and disabled ones. See
  // dev-docs/stack-roster-spec.md.
  let rosterSnap = [];

  // The registry update-check snapshot ({stacks, checked_at}, ADR-0054) from
  // the same 'stacks' snapshot, or null while the check is disabled or has not
  // run. Drives the amber ⇡ markers on version chips and the containers
  // panel's update summary.
  let updatesSnap = null;

  // The bad terminal audit records of the last 24h from the same 'stacks'
  // snapshot, driving the header incident badge. The badge re-filters the list
  // against the window on the relative-time tick, so the count ages out
  // between republishes.
  let incidentsSnap = [];

  // The deploy repo's forge browse URL from the same 'stacks' snapshot, or ''
  // when the server could derive none from repo_url. Every commit SHA the UI
  // prints links to its commit page through it (commitLinkHTML); without it the
  // SHAs stay plain text. A peer's SHAs use that peer's own value — its repo may
  // live on a different forge — so this one is the local set's base only.
  let repoWebURL = '';

  // ── Multi-host state + per-host resolvers (ADR-0048) ──

  // peersSnap is the 'peers' state payload
  // ({self, peers:[{name,url,reachable,stale,last_seen,state,deploys}]}) or null
  // on a single-host instance with no peers configured — in which case the whole
  // multi-host surface (Hosts control, Host column, peer rows) stays hidden and
  // the UI is exactly the single-host one. selfHost is the primary's own host
  // label, the identity every local row is tagged with. hostColors maps each
  // host name to its palette slot (assignHostColors, collision-avoiding).
  // hostSelected is the set of in-view host names (the Hosts filter); null means
  // all hosts. Persisted per browser (localStorage key hostFilter) and restored
  // once, the first time the peers snapshot arrives (see applyPeers) — the host
  // set isn't known any earlier — reconciled against it via reconcileHostFilter
  // so a saved host that no longer exists can't strand the view.
  let peersSnap = null;
  let selfHost = '';
  let hostColors = {};
  let hostSelected = null;
  let hostFilterRestored = false;

  // healthMapFor / healthwatchMapFor / appLinksMapFor / repoWebURLFor resolve
  // the per-stack map (or forge browse URL) for a host: the primary's own live
  // snapshot for self, else the peer's fanned-in state (ADR-0048), so peer rows
  // render the same container/health/app-link detail the primary shows for its
  // own stacks. Thin wrappers binding the module-scoped snapshots to the pure
  // resolve* helpers (app-helpers.js), which own — and unit-test — the
  // self-vs-peer fallback and the tolerance for older peers missing a section.
  function healthMapFor(host) {
    return resolveHealthMap(peersSnap, selfHost, host, healthSnap);
  }
  function healthwatchMapFor(host) {
    return resolveHealthwatchMap(peersSnap, selfHost, host, healthwatchSnap);
  }
  function appLinksMapFor(host) {
    return resolveAppLinksMap(peersSnap, selfHost, host, appLinksSnap);
  }
  function repoWebURLFor(host) {
    return resolveRepoWebURL(peersSnap, selfHost, host, repoWebURL);
  }
  function updatesFor(host) {
    return resolveUpdates(peersSnap, selfHost, host, updatesSnap);
  }
  // stackUpdatesFor resolves one stack's update map (service → {latest,
  // rebuilt}) on a host — the parameter the version-chip renderers take.
  function stackUpdatesFor(stack, host) {
    const u = updatesFor(host);
    return (u && u.stacks && u.stacks[stack]) || null;
  }
  // stackHealthFor resolves one stack's live-health entry on a host — the
  // parameter the roster cell renderers in app-render.js take.
  function stackHealthFor(stack, host) {
    return healthMapFor(host)[stack];
  }

  // The reserved stack key for nixos-rebuild deploys (invariant 4). It is a
  // pseudo-stack, not a Docker Compose project and not in the Stacks roster, so
  // it carries no container-logs button and no jump-to-Stacks button — only the
  // affordances that apply to it (its git diff + deploy history).
  const NIXOS_STACK = '_nixos';

  // ── App links (Traefik-routed hostnames) ──

  // Traefik-routed hostnames per stack, from the 'app_links' SSE snapshot
  // (dev-docs/traefik-app-links-spec.md): stack name -> hostnames, absent
  // when none were discovered. Feeds the roster row's link icon.
  let appLinksSnap = {};
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

  // ── Stacks roster rendering ──

  function renderRoster() {
    const list = document.getElementById('roster-list');
    // A `stacks` snapshot lands after every run, so a rebuild that drops the
    // open panel yanks the answer away from whoever is reading it — and the run
    // that republished is exactly when the panel has something new to say (the
    // deploy history gained a record, change detection a commit). So note which
    // row is open and re-open it after the rebuild, from the new snapshot.
    const openRow = list.querySelector(
      '.roster-row.audit-open, .roster-row.hooks-open, .roster-row.peer-row.diff-open',
    );
    const reopen = openRow && {
      stack: openRow.dataset.stack,
      host: openRow.dataset.host,
      kind: openRow.classList.contains('hooks-open')
        ? 'hooks'
        : openRow.classList.contains('peer-row')
          ? 'peer'
          : 'detail',
    };
    list.textContent = '';
    openLinkWrap = null; // rebuilt rows drop any open app-link popover too
    // Attention-ranked, stable (rosterOrdered, app-helpers.js): unhealthy first,
    // backend order kept within each group. Applied only here, at full render —
    // never on a live health poll — so rows never jump under an open panel.
    rosterOrdered(rosterSnap, healthSnap).forEach(function (entry) {
      const deploying = !!deployingRows[entry.name];
      // Time + commit only apply to a real past deploy.
      const showMeta = !entry.disabled && !deploying && !!entry.last_status;
      // Shared time mode; title carries the opposite (relative <-> absolute).
      const when =
        showMeta && entry.last_at
          ? absoluteTime
            ? fullTime(entry.last_at)
            : formatTime(entry.last_at)
          : '';
      const whenTitle =
        showMeta && entry.last_at
          ? absoluteTime
            ? formatTime(entry.last_at)
            : fullTime(entry.last_at)
          : '';
      const commit = showMeta ? entry.last_commit || '' : '';
      const row = document.createElement('div');
      row.className = entry.disabled ? 'roster-row disabled' : 'roster-row';
      row.dataset.testid = 'roster-row';
      row.dataset.stack = entry.name;
      row.dataset.host = selfHost; // local stacks belong to the primary host
      // Mark the row with its live health so CSS can give an unhealthy row the
      // same severity bar + tint as a failed deploy row (kept in sync live by
      // updateRosterHealth). Only enabled locals — disabled stacks aren't polled.
      const rowHealth = entry.disabled ? null : healthSnap[entry.name];
      if (rowHealth && rowHealth.status) row.dataset.health = rowHealth.status;
      row.innerHTML =
        `<span class="roster-stack"><span class="roster-ident">${hostChip(selfHost)}<span class="stack-icon" data-testid="stack-icon"></span><span class="roster-name" title="${escapeAttr(entry.name)}">${escapeHtml(entry.name)}</span></span>${rowActionClusterHTML(
          jumpBtnHTML('deploys', entry.name),
          entry.disabled ? '' : linkCell(entry.name),
          rosterRowActionsHTML(entry),
        )}</span>` +
        rosterVersionCellHTML(
          entry.name,
          rowHealth,
          entry.disabled,
          stackUpdatesFor(entry.name, ''),
        ) +
        `<span class="roster-status">${rosterStatusHTML(entry, !!deployingRows[entry.name])}${entry.disabled ? '' : rosterHealthPillHTML(entry.name, rowHealth)}${entry.disabled ? '' : outcomeStripHTML(entry.recent, Date.now()) + lastIncidentHTML(entry.last_incident, Date.now())}</span>` +
        `<span class="roster-when"${whenTitle ? ` title="${escapeAttr(whenTitle)}"` : ''}>${escapeHtml(when)}</span>` +
        commitLinkHTML(commit, { cls: 'roster-sha', base: repoWebURL, title: commit });
      populateIcon(row.querySelector('.stack-icon'), entry.name);
      list.appendChild(row);
    });
    renderPeerRosterRows(list); // append each peer's stacks, host-tagged + read-only
    reopenRosterPanel(list, reopen); // the open panel returns, carrying the new snapshot
    applyHostFilter(); // dots + host filtering on the rebuilt roster
    refilterRoster(); // a re-render replaces the rows; re-apply any active filter
    applyHookRun(); // re-paint a running hook's phase on the rebuilt roster rows
  }

  // reopenRosterPanel restores the panel renderRoster noted before the rebuild.
  // A stack that left the set (parked, or removed from the repo) simply has no
  // row to re-open on, which is the correct outcome — its panel is gone with it.
  function reopenRosterPanel(list, reopen) {
    if (!reopen) return;
    const row = [...list.querySelectorAll('.roster-row')].find(function (r) {
      return r.dataset.stack === reopen.stack && r.dataset.host === reopen.host;
    });
    if (!row) return;
    if (reopen.kind === 'peer') openPeerDetail(row);
    else if (reopen.kind === 'hooks') openRosterHooksPanel(row);
    else openRosterDetail(row);
  }

  // renderPeerRosterRows appends every peer's stacks to the roster, one read-only
  // row each (host chip + icon + name + app-link, status, last deploy, commit).
  // The app-link opens the peer's own routed app directly (the browser can reach
  // it); there is no jump/logs/hooks affordance, since the primary cannot act on
  // a peer's stack. A click expands the row's read-only detail (containers, from
  // the peer's fanned-in health). Rows are grouped per host after the local set.
  function renderPeerRosterRows(list) {
    if (!peersSnap) return;
    (peersSnap.peers || []).forEach(function (p) {
      const roster = (p.state && p.state.stacks && p.state.stacks.roster) || [];
      const peerRepo = repoWebURLFor(p.name); // a peer's commits live on its own forge
      roster.forEach(function (entry) {
        const showMeta = !entry.disabled && !!entry.last_status;
        const when =
          showMeta && entry.last_at
            ? absoluteTime
              ? fullTime(entry.last_at)
              : formatTime(entry.last_at)
            : '';
        const whenTitle =
          showMeta && entry.last_at
            ? absoluteTime
              ? formatTime(entry.last_at)
              : fullTime(entry.last_at)
            : '';
        const commit = showMeta ? entry.last_commit || '' : '';
        const row = document.createElement('div');
        row.className =
          'roster-row peer-row' +
          (entry.disabled ? ' disabled' : '') +
          (p.stale ? ' peer-stale' : '');
        row.dataset.testid = 'roster-row';
        row.dataset.stack = entry.name;
        row.dataset.host = p.name;
        row.dataset.status = entry.last_status || '';
        if (entry.last_commit) row.dataset.commit = entry.last_commit;
        row.innerHTML =
          `<span class="roster-stack"><span class="roster-ident">${hostChip(p.name)}<span class="stack-icon" data-testid="stack-icon"></span><span class="roster-name" title="${escapeAttr(entry.name)}">${escapeHtml(entry.name)}</span></span>${rowActionClusterHTML(
            entry.disabled ? '' : linkCell(entry.name, p.name),
          )}</span>` +
          rosterVersionCellHTML(
            entry.name,
            stackHealthFor(entry.name, p.name),
            entry.disabled,
            stackUpdatesFor(entry.name, p.name),
          ) +
          // deploying=false: deployingRows is the primary's own in-flight set,
          // never a peer's.
          `<span class="roster-status">${rosterStatusHTML(entry, false)}${entry.disabled ? '' : rosterHealthPillHTML(entry.name, stackHealthFor(entry.name, p.name))}${entry.disabled ? '' : outcomeStripHTML(entry.recent, Date.now()) + lastIncidentHTML(entry.last_incident, Date.now())}</span>` +
          `<span class="roster-when"${whenTitle ? ` title="${escapeAttr(whenTitle)}"` : ''}>${escapeHtml(when)}</span>` +
          commitLinkHTML(commit, { cls: 'roster-sha', base: peerRepo, title: commit });
        populateIcon(row.querySelector('.stack-icon'), entry.name);
        list.appendChild(row);
      });
    });
  }

  // ── Orphan compose projects (ADR-0036) ──

  // Orphans (ADR-0036): compose projects the discovered stack set no longer
  // accounts for, from the 'orphans' SSE snapshot — a collapsed section below
  // the table, hidden when empty. orphansOpen remembers expanded rows so the
  // per-poll re-render does not collapse them.
  let orphansSnap = [];
  let orphansOpen = new Set();
  let orphansSectionOpen = false; // the user's manual toggle of the section header

  function orphanContainerRow(c) {
    const row = document.createElement('div');
    row.className = 'orphan-cont';
    row.setAttribute('data-testid', 'orphan-cont');
    const dot = document.createElement('span');
    dot.className = 'oc-dot ' + orphanStateClass(c.state);
    dot.title = c.state || '';
    const name = document.createElement('span');
    name.className = 'oc-name';
    name.textContent = c.name || c.service || '';
    const image = document.createElement('span');
    image.className = 'oc-image';
    image.textContent = c.image || '';
    image.title = c.image || '';
    row.append(dot, name, image);
    if (c.ports) {
      const ports = document.createElement('span');
      ports.className = 'oc-ports';
      ports.textContent = c.ports;
      row.appendChild(ports);
    }
    const status = document.createElement('span');
    status.className = 'oc-status';
    status.textContent = c.status || c.state || '';
    row.appendChild(status);
    return row;
  }

  // orphanFactRow builds one project-level fact line (config file, volumes).
  function orphanFactRow(key, val, note) {
    const line = document.createElement('div');
    line.className = 'of-line';
    const k = document.createElement('span');
    k.className = 'of-key';
    k.textContent = key;
    const v = document.createElement('span');
    v.className = 'of-val';
    v.textContent = val;
    line.append(k, v);
    if (note) {
      const n = document.createElement('span');
      n.className = 'of-note';
      n.textContent = note;
      line.appendChild(n);
    }
    return line;
  }

  function renderOrphans() {
    const wrap = document.getElementById('orphans');
    const body = document.getElementById('orphans-body');
    const count = document.getElementById('orphans-count');
    body.textContent = '';
    // The badge shows matching orphans during a search, else the total.
    const q = (deployFilter.value || '').trim().toLowerCase();
    count.textContent = String(
      q
        ? orphansSnap.filter(function (o) {
            return orphanMatchesQuery(o, q);
          }).length
        : orphansSnap.length,
    );
    if (!orphansSnap.length) {
      wrap.classList.remove('shown');
      return;
    }
    orphansSnap.forEach(function (o) {
      const conts = o.containers || [];
      const item = document.createElement('div');
      item.className = 'orphan-item';
      item.setAttribute('data-testid', 'orphan-item');
      item.setAttribute('data-project', o.project);
      item.setAttribute('data-class', o.class);

      const row = document.createElement(conts.length ? 'button' : 'div');
      row.className = 'orphan-row';
      const caret = document.createElement('span');
      caret.className = 'orphan-caret' + (conts.length ? '' : ' blank');
      caret.textContent = '▶';
      caret.setAttribute('aria-hidden', 'true');
      const tag = document.createElement('span');
      tag.className = 'orphan-tag ' + (o.class === 'orphaned' ? 'orphaned' : 'unmanaged');
      tag.textContent = o.class;
      const name = document.createElement('span');
      name.className = 'orphan-name';
      name.textContent = o.project;
      const dir = document.createElement('span');
      dir.className = 'orphan-dir';
      dir.textContent = o.working_dir || '';
      dir.title = o.working_dir || '';
      const meta = document.createElement('span');
      meta.className = 'orphan-meta';
      meta.textContent = orphanMeta(o);
      row.append(caret, tag, name, dir, meta);

      const detail = document.createElement('div');
      detail.className = 'orphan-detail';
      const vols = o.volumes || [];
      if (o.config_file || vols.length) {
        const facts = document.createElement('div');
        facts.className = 'orphan-facts';
        if (o.config_file) facts.appendChild(orphanFactRow('config', o.config_file));
        if (vols.length)
          facts.appendChild(orphanFactRow('volumes', vols.join(', '), 'kept on prune'));
        detail.appendChild(facts);
      }
      // Search: on a container match, hide the non-matching containers so the
      // hit stands out.
      const anyContMatch =
        q &&
        conts.some(function (c) {
          return containerMatchesQuery(c, q);
        });
      conts.forEach(function (c) {
        const crow = orphanContainerRow(c);
        if (anyContMatch && !containerMatchesQuery(c, q)) crow.classList.add('filtered-out');
        detail.appendChild(crow);
      });

      // A non-matching orphan is hidden during search, not just left collapsed;
      // a matching one auto-expands on top of any manual expansion.
      const orphanMatch = !q || orphanMatchesQuery(o, q);
      item.classList.toggle('filtered-out', !orphanMatch);

      if (conts.length) {
        const startOpen = orphansOpen.has(o.project) || (!!q && orphanMatch);
        item.classList.toggle('open', startOpen);
        row.setAttribute('aria-expanded', startOpen ? 'true' : 'false');
        row.addEventListener('click', function () {
          const open = item.classList.toggle('open');
          row.setAttribute('aria-expanded', open ? 'true' : 'false');
          if (open) {
            orphansOpen.add(o.project);
          } else {
            orphansOpen.delete(o.project);
          }
        });
      }

      item.append(row, detail);
      body.appendChild(item);
    });
    wrap.classList.add('shown');
    applyOrphansSectionOpen();
  }

  // Open the section when the user opened it, or when a search matches an orphan
  // (so the hit isn't hidden behind a collapsed section).
  function applyOrphansSectionOpen() {
    const q = (deployFilter.value || '').trim().toLowerCase();
    const searchOpen =
      !!q &&
      orphansSnap.some(function (o) {
        return orphanMatchesQuery(o, q);
      });
    const open = orphansSectionOpen || searchOpen;
    document.getElementById('orphans').classList.toggle('open', open);
    document.getElementById('orphans-head').setAttribute('aria-expanded', open ? 'true' : 'false');
  }

  (function () {
    document.getElementById('orphans-head').addEventListener('click', function () {
      orphansSectionOpen = !orphansSectionOpen;
      applyOrphansSectionOpen();
    });
  })();

  // ── Deploys table: initial state + row detail panels ──

  // The deploy list starts as a skeleton (T4.17); this flips true the moment the
  // real picture is known — either rows arrived (showTable) or a snapshot
  // confirmed there are none (settleInitialState).
  let initialStateSettled = false;

  function showTable() {
    if (!hasRows) {
      hasRows = true;
      initialStateSettled = true; // rows arrived — the initial picture is known
      table.style.display = '';
      emptyState.style.display = 'none';
      loadingState.style.display = 'none';
      clearOfflineNotice(); // after the flag, so it does not restore the skeleton
    }
  }

  // settleInitialState runs once, the first time a snapshot is successfully
  // applied: it retires the skeleton and, only if that snapshot carried no
  // deploys, reveals the genuine-empty state. A failed snapshot does NOT settle
  // — the skeleton stays "connecting" and a live event or the next reconnect
  // resolves it, so a transient outage never masquerades as "no deployments".
  function settleInitialState() {
    if (initialStateSettled) return;
    initialStateSettled = true;
    loadingState.style.display = 'none';
    clearOfflineNotice();
    if (!hasRows) emptyState.style.display = '';
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
    el.innerHTML = diffPanelHTML(diffs, commits, meta, repoWebURL);
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

  // ── Container logs (ADR-0037) ──────────────────────────────────────────────
  // A live `docker compose logs` panel opened from a logs icon, per stack
  // (merged services) and per container. One log open at a time; the panel
  // streams from /api/container-logs via EventSource and trails the row/line it
  // was opened from. Controls: live/pause, auto-scroll, wrap, in-log search,
  // fullscreen.
  const CLOG_ICONS = {
    search:
      '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="11" cy="11" r="7"/><path d="M21 21l-4-4"/></svg>',
    wrap: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M4 6h16"/><path d="M4 12h13a3 3 0 0 1 0 6h-4"/><path d="M16 15l-3 3 3 3"/><path d="M4 18h6"/></svg>',
    scroll:
      '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 4v11"/><path d="M8 11l4 4 4-4"/><path d="M5 20h14"/></svg>',
    fs: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M8 3H5a2 2 0 0 0-2 2v3"/><path d="M16 3h3a2 2 0 0 1 2 2v3"/><path d="M8 21H5a2 2 0 0 1-2-2v-3"/><path d="M16 21h3a2 2 0 0 0 2-2v-3"/></svg>',
    filter:
      '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M3 4h18l-7 8v6l-4 2v-8z"/></svg>',
  };

  // clogButton is the DOM-node form of clogBtnHTML (app-render.js) for the
  // deploy row's action cell.
  function clogButton(stack, service) {
    const tmp = document.createElement('template');
    tmp.innerHTML = clogBtnHTML(stack, service);
    return tmp.content.firstElementChild;
  }

  // ─── Deploy hooks (ADR-0038) ───
  // Hook commands ride inline on the stacks snapshot (no fetch); hookRunSnap is
  // the currently-executing hook ({} when none) from the hookrun SSE snapshot.
  let hookRunSnap = {};

  function hooksFor(stack) {
    const e = rosterSnap.find(function (r) {
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

  // Paint the running-hook phase + pulse the badge on the stack's row in both
  // views; clear it when hookRunSnap has no stack.
  function applyHookRun() {
    document.querySelectorAll('.hook-phase').forEach(function (n) {
      n.remove();
    });
    document
      .querySelectorAll('.hooks-badge[data-hook-active], .more-btn[data-hook-active]')
      .forEach(function (b) {
        b.removeAttribute('data-hook-active');
      });
    const hr = hookRunSnap;
    if (!hr || !hr.stack) return;

    // Deploys view: the stack's newest row while deploying. The hooks badge now
    // lives inside the collapsed ⋯ menu, so the running-hook pulse rides the
    // visible ⋯ button instead (the badge itself is still marked for when it opens).
    const drow = Array.from(tbody.querySelectorAll('.event-row[data-stack]')).find(function (r) {
      return r.dataset.stack === hr.stack;
    });
    if (drow && drow.dataset.status === 'deploying') {
      const cell = drow.querySelector('.status-cell');
      if (cell) cell.appendChild(hookPhaseNode(hr));
      // Marked independently: a running hook is reported by the hookrun stream,
      // while the badge only exists once the roster snapshot has carried the
      // stack's hooks. Gating the ⋯ pulse on the badge would drop it whenever
      // the roster lags the hook.
      const b = drow.querySelector('.hooks-badge');
      if (b) b.dataset.hookActive = '1';
      const mb = drow.querySelector('.more-btn');
      if (mb) mb.dataset.hookActive = '1';
    }

    // Stacks view: the roster row while it shows the live deploying state.
    const rlist = document.getElementById('roster-list');
    if (rlist && deployingRows[hr.stack]) {
      const rrow = Array.from(
        rlist.querySelectorAll('.roster-row[data-stack]:not(.peer-row)'),
      ).find(function (r) {
        return r.dataset.stack === hr.stack;
      });
      if (rrow) {
        const rcell = rrow.querySelector('.roster-status');
        if (rcell) rcell.appendChild(hookPhaseNode(hr));
        // The hooks badge sits inline on the roster row, so the running-hook
        // pulse rides the badge itself.
        const rb = rrow.querySelector('.hooks-badge');
        if (rb) rb.dataset.hookActive = '1';
      }
    }
  }

  const clog = (function () {
    let panel = null,
      es = null,
      btn = null,
      body = null,
      key = null;
    let selected = [],
      curStack = '',
      curHost = ''; // selected = [] → whole stack; else the chosen service subset
    // 'container' streams docker compose logs; 'skipper' streams /api/logs
    // filtered to the stack — the hook output (ADR-0038), in the same panel.
    let mode = 'container';
    let follow = true,
      paused = false,
      query = '',
      tail = 200;
    let fsHolder = null; // marks the panel's row spot while it is fullscreen in <body>

    function toBottom() {
      if (body) body.scrollTop = body.scrollHeight;
    }
    function setStat(text, cls) {
      const s = panel && panel.querySelector('.clog-stat');
      if (s) {
        s.textContent = text;
        s.className = 'clog-stat' + (cls ? ' ' + cls : '');
      }
    }

    // A stream error, applied to both ends of the panel. The footer says what
    // happened; the live/pause pill has to follow, or a closed stream keeps a
    // green "live" next to a footer saying it is closed — and its toggle would
    // put "live · streaming" back on a stream that is gone.
    function applyStreamError(readyState) {
      if (!panel) return;
      const s = clogStreamStatus(readyState);
      setStat(s.text, s.cls);
      const live = panel.querySelector('.clog-live');
      if (!live) return;
      live.classList.toggle('dead', s.closed);
      live.querySelector('.clog-ltxt').textContent = s.closed
        ? 'closed'
        : paused
          ? 'paused'
          : 'live';
    }

    // The stack's services, from the (peer-aware) health snapshot at open time.
    function servicesFor() {
      const h = healthMapFor(curHost)[curStack];
      return (h && h.services) || [];
    }
    // A stack with fewer than two services has nothing to filter, so the
    // per-service control is suppressed (only in container mode, never hooks).
    function hasServiceFilter() {
      return mode === 'container' && servicesFor().length >= 2;
    }

    // The scope label in the header: whole stack, one service, a short list, or
    // an "N services" count once the list would get long.
    function scopeText() {
      if (!selected.length) return curStack + ' · all services';
      if (selected.length === 1) return curStack + ' / ' + selected[0];
      if (selected.length <= 3) return curStack + ' / ' + selected.join(' + ');
      return curStack + ' / ' + selected.length + ' services';
    }

    // Empty string when there is nothing to filter, so the head shows no filter
    // tool either.
    function svcRowHTML() {
      if (!hasServiceFilter()) return '';
      return clogSvcsHTML(servicesFor(), selected);
    }

    // Reflect the current selection onto the chips + scope label after a toggle.
    function syncChips() {
      if (!panel) return;
      panel.querySelectorAll('.clog-svcs .clog-chip').forEach(function (x) {
        const svc = x.dataset.svc;
        x.classList.toggle(
          'active',
          svc === '' ? selected.length === 0 : selected.indexOf(svc) !== -1,
        );
      });
      const sc = panel.querySelector('.clog-scope');
      if (sc) sc.textContent = '· ' + scopeText();
    }

    // decorate splits a compose log line into service prefix (merged view),
    // leading RFC3339 timestamp (--timestamps) and message, colouring each and
    // tinting error/warn lines. Everything is escaped — no HTML from the child.
    function decorate(data) {
      let svc = '',
        rest = data;
      if (selected.length !== 1) {
        // merged + multi keep the compose prefix; a single service drops it
        const m = rest.match(/^([^|]{1,60}?)\s+\|\s?(.*)$/);
        if (m) {
          svc = m[1];
          rest = m[2];
        }
      }
      let ts = '';
      const t = rest.match(/^(\S+)\s([\s\S]*)$/);
      if (t && /^\d{4}-\d\d-\d\dT[\d:.]+/.test(t[1])) {
        ts = t[1];
        rest = t[2];
      }
      let cls = '';
      if (/error|fatal|panic|\bfail/i.test(rest)) cls = 'clog-err';
      else if (/warn/i.test(rest)) cls = 'clog-warn';
      let html = '';
      if (svc) html += '<span class="clog-svc">' + escapeHtml(svc) + ' |</span> ';
      if (ts) html += '<span class="clog-ts">' + escapeHtml(ts) + '</span> ';
      html += cls ? '<span class="' + cls + '">' + escapeHtml(rest) + '</span>' : escapeHtml(rest);
      return html;
    }

    function highlight(root, q) {
      const ql = q.toLowerCase();
      const w = document.createTreeWalker(root, NodeFilter.SHOW_TEXT, null),
        ns = [];
      while (w.nextNode()) ns.push(w.currentNode);
      ns.forEach(function (n) {
        const text = n.nodeValue,
          low = text.toLowerCase();
        let idx = low.indexOf(ql);
        if (idx < 0) return;
        const frag = document.createDocumentFragment();
        let pos = 0;
        while (idx >= 0) {
          if (idx > pos) frag.appendChild(document.createTextNode(text.slice(pos, idx)));
          const mk = document.createElement('mark');
          mk.textContent = text.slice(idx, idx + q.length);
          frag.appendChild(mk);
          pos = idx + q.length;
          idx = low.indexOf(ql, pos);
        }
        if (pos < text.length) frag.appendChild(document.createTextNode(text.slice(pos)));
        n.parentNode.replaceChild(frag, n);
      });
    }
    function filterLine(ln) {
      if (ln.dataset.orig == null) ln.dataset.orig = ln.innerHTML;
      ln.innerHTML = ln.dataset.orig;
      ln.classList.remove('clog-out', 'clog-hit');
      if (!logLineVisible(ln.textContent, query)) {
        ln.classList.add('clog-out');
        return;
      }
      if (query) {
        ln.classList.add('clog-hit');
        highlight(ln, query);
      }
    }
    function applySearch(q) {
      query = q;
      let n = 0;
      body.querySelectorAll('.clog-ln').forEach(function (ln) {
        filterLine(ln);
        if (q && !ln.classList.contains('clog-out')) n++;
      });
      const hits = panel.querySelector('.clog-hits');
      if (hits) hits.textContent = q ? n + (n === 1 ? ' hit' : ' hits') : '';
      if (follow) toBottom();
    }

    function appendLine(data) {
      if (paused) return;
      const ln = document.createElement('span');
      ln.className = 'clog-ln';
      ln.innerHTML = decorate(data);
      if (query) filterLine(ln);
      body.appendChild(ln);
      while (body.children.length > 3000) body.removeChild(body.firstChild);
      if (query) {
        const hits = panel.querySelector('.clog-hits');
        if (hits) hits.textContent = body.querySelectorAll('.clog-ln.clog-hit').length + ' hits';
      }
      if (follow) toBottom();
    }

    // Render one /api/logs entry (skipper mode) via the shared renderLogLine,
    // tagged .clog-ln so the panel's search/wrap/scroll apply.
    function appendSkipperLine(entry) {
      if (paused) return;
      const ln = renderLogLine(entry);
      ln.classList.add('clog-ln');
      if (query) filterLine(ln);
      body.appendChild(ln);
      while (body.children.length > 3000) body.removeChild(body.firstChild);
      if (query) {
        const hits = panel.querySelector('.clog-hits');
        if (hits) hits.textContent = body.querySelectorAll('.clog-ln.clog-hit').length + ' hits';
      }
      if (follow) toBottom();
    }

    function connect() {
      if (es) {
        es.close();
        es = null;
      }
      setStat('live · streaming', paused ? 'paused' : '');
      if (mode === 'skipper') {
        // Hook log: skipper's own stream, filtered to the stack's attributed lines.
        es = new EventSource('/api/logs');
        es.addEventListener('log', function (ev) {
          if (!panel || !panel.isConnected) {
            close();
            return;
          }
          let entry;
          try {
            entry = JSON.parse(ev.data);
          } catch (_) {
            return;
          }
          if (((entry.attrs && entry.attrs.stack) || '') !== curStack) return;
          appendSkipperLine(entry);
        });
        es.onerror = function (ev) {
          applyStreamError(ev.target.readyState);
        };
        es.onopen = function () {
          if (panel) setStat(paused ? 'paused' : 'live · streaming', paused ? 'paused' : '');
        };
        return;
      }
      // A peer's logs stream through the primary's proxy (the browser can't reach
      // the peer cross-origin, ADR-0048); a local stack hits the endpoint directly.
      let url = curHost
        ? '/api/peers/' +
          encodeURIComponent(curHost) +
          '/container-logs/' +
          encodeURIComponent(curStack)
        : '/api/container-logs/' + encodeURIComponent(curStack);
      url += '?tail=' + tail;
      if (selected.length) url += '&services=' + selected.map(encodeURIComponent).join(',');
      es = new EventSource(url);
      es.onmessage = function (ev) {
        if (!panel || !panel.isConnected) {
          close();
          return;
        } // dropped by a re-render
        appendLine(ev.data);
      };
      es.onerror = function (ev) {
        applyStreamError(ev.target.readyState);
      };
      es.onopen = function () {
        if (panel) setStat(paused ? 'paused' : 'live · streaming', paused ? 'paused' : '');
      };
    }

    function buildPanel(scope) {
      const el = document.createElement('div');
      el.className = 'clog-panel';
      el.dataset.testid = 'clog-panel';
      el.innerHTML =
        '<div class="clog-head" data-taptip>' +
        '<span class="clog-title">' +
        CLOG_ICON +
        ' logs <span class="clog-scope">· ' +
        escapeHtml(scope) +
        '</span></span>' +
        '<span class="clog-live" data-testid="clog-live" role="button" tabindex="0" title="Live — click to pause"><span class="clog-dot"></span><span class="clog-ltxt">live</span></span>' +
        '<span class="clog-grow"></span>' +
        '<button class="clog-tool" data-clog="search" type="button" title="Search in log">' +
        CLOG_ICONS.search +
        '</button>' +
        (hasServiceFilter()
          ? '<button class="clog-tool" data-clog="svcfilter" type="button" title="Filter by service">' +
            CLOG_ICONS.filter +
            '</button>'
          : '') +
        '<button class="clog-tool" data-clog="wrap" type="button" title="Wrap long lines">' +
        CLOG_ICONS.wrap +
        '</button>' +
        '<button class="clog-tool on" data-clog="scroll" type="button" title="Auto-scroll — follow the tail">' +
        CLOG_ICONS.scroll +
        '</button>' +
        '<span class="clog-tail" data-testid="clog-tail">' +
        '<button data-tail="50" type="button">50</button>' +
        '<button data-tail="200" class="active" type="button">200</button>' +
        '<button data-tail="1000" type="button">1000</button>' +
        '</span>' +
        '<button class="clog-tool" data-clog="fs" type="button" title="Fullscreen">' +
        CLOG_ICONS.fs +
        '</button>' +
        '</div>' +
        svcRowHTML() +
        '<div class="clog-search clog-hide" data-testid="clog-search"><span class="clog-sic">' +
        CLOG_ICONS.search +
        '</span>' +
        '<input type="text" placeholder="Search in log…" autocomplete="off" spellcheck="false" aria-label="Search in log"><span class="clog-hits"></span></div>' +
        '<div class="clog-body" data-testid="clog-body"></div>' +
        '<div class="clog-foot"><span class="clog-stat">live · streaming</span></div>';
      return el;
    }

    function wire() {
      const live = panel.querySelector('.clog-live');
      live.addEventListener('click', function () {
        if (this.classList.contains('dead')) return; // nothing left to pause
        paused = !paused;
        this.classList.toggle('paused', paused);
        this.querySelector('.clog-ltxt').textContent = paused ? 'paused' : 'live';
        setStat(paused ? 'paused' : 'live · streaming', paused ? 'paused' : '');
      });
      live.addEventListener('keydown', function (e) {
        if (e.key === 'Enter' || e.key === ' ') {
          e.preventDefault();
          live.click();
        }
      });
      panel.querySelectorAll('.clog-tool[data-clog]').forEach(function (b) {
        b.addEventListener('click', function () {
          const k = b.dataset.clog;
          if (k === 'wrap') {
            b.classList.toggle('on', body.classList.toggle('wrap'));
          } else if (k === 'scroll') {
            follow = b.classList.toggle('on');
            if (follow) toBottom();
          } else if (k === 'fs') {
            fullscreen(!panel.classList.contains('clog-fullscreen'));
          } else if (k === 'svcfilter') {
            const row = panel.querySelector('.clog-svcs');
            if (row) b.classList.toggle('on', row.classList.toggle('clog-hide') === false);
          } else if (k === 'search') {
            const box = panel.querySelector('.clog-search');
            const show = box.classList.toggle('clog-hide') === false;
            b.classList.toggle('on', show);
            const inp = box.querySelector('input');
            if (show) inp.focus();
            else {
              inp.value = '';
              applySearch('');
            }
          }
        });
      });
      panel.querySelector('.clog-search input').addEventListener('input', function () {
        applySearch(this.value.trim());
      });
      panel.querySelectorAll('.clog-tail button').forEach(function (b) {
        b.addEventListener('click', function () {
          panel.querySelectorAll('.clog-tail button').forEach(function (x) {
            x.classList.remove('active');
          });
          b.classList.add('active');
          tail = parseInt(b.dataset.tail, 10) || 200;
          body.innerHTML = '';
          connect(); // re-pull the backlog at the new size
        });
      });
      // Service chips toggle membership in the selected set; "all" clears it.
      // Each change re-pulls the backlog at the new scope (like the tail buttons).
      panel.querySelectorAll('.clog-svcs .clog-chip').forEach(function (c) {
        c.addEventListener('click', function () {
          const svc = c.dataset.svc;
          if (svc === '') {
            selected = [];
          } else {
            const i = selected.indexOf(svc);
            if (i === -1) selected.push(svc);
            else selected.splice(i, 1);
          }
          syncChips();
          body.innerHTML = '';
          connect();
        });
      });
    }

    // The panel lives inside <main>, which has its own stacking context
    // (z-index:1), so a high z-index alone can't lift a fullscreen overlay above
    // the sticky header. Reparent it to <body> for the duration, leaving a
    // comment where it belongs so exit can restore it.
    function fullscreen(on) {
      const b = panel.querySelector('.clog-tool[data-clog="fs"]');
      if (on) {
        if (!fsHolder) {
          fsHolder = document.createComment('clog-fs');
          panel.before(fsHolder);
          document.body.appendChild(panel);
        }
        panel.classList.add('clog-fullscreen');
        if (b) b.classList.add('on');
        toBottom();
        return;
      }
      panel.classList.remove('clog-fullscreen');
      if (b) b.classList.remove('on');
      if (fsHolder && fsHolder.parentNode) {
        fsHolder.replaceWith(panel); // back to its row
        fsHolder = null;
      } else if (fsHolder) {
        fsHolder = null;
        close(); // the row was re-rendered away while fullscreen → tear down
      }
    }

    function open(button, stack, service, host) {
      const newKey = (host || '') + '\n' + stack + '\n' + (service || '');
      if (key === newKey) {
        close();
        return;
      } // same icon → toggle closed
      close();
      btn = button;
      key = newKey;
      curStack = stack;
      selected = service ? [service] : [];
      curHost = host || '';
      mode = 'container';
      follow = true;
      paused = false;
      query = '';
      btn.classList.add('on');
      panel = buildPanel(scopeText());
      const anchor =
        button.closest('.hp-svc') ||
        button.closest('.event-row') ||
        button.closest('.roster-row') ||
        button;
      anchor.after(panel);
      body = panel.querySelector('.clog-body');
      wire();
      connect();
    }

    // Open this stack's hook log inline in skipper mode (ADR-0038); toggle closed
    // on a second click.
    function openHookLog(button, stack) {
      const newKey = `${stack}\n#hook`;
      if (key === newKey) {
        close();
        return;
      }
      close();
      btn = button;
      key = newKey;
      curStack = stack;
      selected = [];
      curHost = '';
      mode = 'skipper';
      follow = true;
      paused = false;
      query = '';
      if (btn) btn.classList.add('on');
      panel = buildPanel(`${stack} · deploy hook`);
      const anchor = button.closest('.event-row') || button.closest('.roster-row') || button;
      anchor.after(panel);
      body = panel.querySelector('.clog-body');
      wire();
      // /api/logs replays its own backlog and has no tail param — hide the selector.
      const tailSel = panel.querySelector('.clog-tail');
      if (tailSel) tailSel.style.display = 'none';
      connect();
    }

    function close() {
      if (es) {
        es.close();
        es = null;
      }
      if (fsHolder) {
        if (fsHolder.parentNode) fsHolder.remove();
        fsHolder = null;
      }
      if (panel) {
        panel.remove();
        panel = null;
      }
      if (btn) {
        btn.classList.remove('on');
        btn = null;
      }
      body = null;
      key = null;
      query = '';
      mode = 'container';
      curHost = '';
      selected = [];
    }

    // Type-to-search: while a log is open, a printable key routes into the
    // in-log search, overriding the deploys/stacks type-to-search. Capture phase
    // + stopImmediatePropagation so it wins over those document keydown listeners.
    document.addEventListener(
      'keydown',
      function (e) {
        if (!panel) return; // no log open → leave default search
        if (e.defaultPrevented || e.metaKey || e.ctrlKey || e.altKey) return;
        const input = panel.querySelector('.clog-search input');
        if (e.target === input) return; // already in the log search → native typing
        const tag = (e.target && e.target.tagName) || '';
        if (
          tag === 'INPUT' ||
          tag === 'TEXTAREA' ||
          tag === 'SELECT' ||
          (e.target && e.target.isContentEditable)
        )
          return;
        if (e.key === 'Escape' || e.key.length !== 1 || e.key === ' ') return;
        const box = panel.querySelector('.clog-search');
        const searchBtn = panel.querySelector('.clog-tool[data-clog="search"]');
        if (box.classList.contains('clog-hide')) {
          box.classList.remove('clog-hide');
          if (searchBtn) searchBtn.classList.add('on');
        }
        input.focus();
        input.value += e.key; // focus mid-keydown doesn't reliably route the char
        applySearch(input.value.trim());
        e.preventDefault();
        e.stopImmediatePropagation();
      },
      true,
    );

    return {
      toggle: function (button) {
        open(
          button,
          button.dataset.clogStack,
          button.dataset.clogService || '',
          button.dataset.clogHost || '',
        );
      },
      openHookLog: function (button, stack) {
        openHookLog(button, stack);
      },
      close: close,
      escape: function () {
        if (panel && panel.classList.contains('clog-fullscreen')) {
          fullscreen(false);
          return true;
        }
        if (panel) {
          close();
          return true;
        }
        return false;
      },
    };
  })();

  // ── Deploy rows: stack icons, row build, per-stack affordances ──

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

  // pendingReason draws a stack's pause/block reason from the queue snapshot
  // for pendingTagHTML (app-render.js).
  function pendingReason(stack) {
    const item = queueByStack[stack];
    return item && item.reason;
  }

  function createRow(evt, isHistory) {
    const row = document.createElement('div');
    row.className = rowClass(evt.status, isHistory);
    row.dataset.testid = 'deploy-row';
    row.dataset.stack = evt.stack;
    row.dataset.status = evt.status;
    row.dataset.eventId = evt.id;
    row.dataset.host = selfHost; // local rows belong to the primary host
    if (evt.has_diffs) row.dataset.hasDiffs = '1';

    const absTs = fullTime(evt.timestamp);
    const relTs = formatTime(evt.timestamp);
    const pausedTag =
      evt.status === 'queued' || evt.status === 'blocked'
        ? pendingTagHTML(evt.status, pendingReason(evt.stack))
        : '';
    // A healed row has no changed files; its files cell instead carries the
    // self-heal badge, which expands the corrective-redeploy detail (ADR-0029).
    const filesCell =
      evt.status === 'healed' ? healPillHTML(evt.heal_drift) : filesHTML(evt.changed_files);
    // A healed row applied no image change (self-heal re-applies the same
    // version); every other row names which service(s) moved, and to what, in the
    // Version column. Stash the changes on the row so the view-options toggle can
    // fill/empty the column live without a reload.
    if (evt.status !== 'healed' && evt.image_changes && evt.image_changes.length) {
      row.dataset.imageChanges = JSON.stringify(evt.image_changes);
    }
    const delta = imageDeltaOn && evt.status !== 'healed' ? imageDeltaHTML(evt.image_changes) : '';
    row.innerHTML =
      `<span class="cell-time" data-testid="time-cell" data-ts="${escapeAttr(evt.timestamp)}" title="${escapeAttr(absoluteTime ? relTs : absTs)}">${absoluteTime ? absTs : relTs}</span>` +
      `<span class="cell-stack">${hostChip(selfHost)}<span class="stack-icon" data-testid="stack-icon"></span><span class="stack-name">${escapeHtml(evt.stack)}</span>${evt.stack === NIXOS_STACK ? '' : jumpBtnHTML('stacks', evt.stack)}${pausedTag}</span>` +
      `<span class="col-version">${delta}</span>` +
      `<span class="status-cell">${badgeHTML(evt.status)}${retryNoteHTML(evt)}</span>` +
      `<span class="cell-duration" data-testid="duration-cell">${formatDuration(evt.duration_ms)}</span>` +
      `<span class="col-files">${filesCell}</span>`;

    populateIcon(row.querySelector('.stack-icon'), evt.stack);
    return row;
  }

  function createErrorDetail(evt) {
    const el = document.createElement('div');
    el.className = 'error-detail';
    el.dataset.testid = 'error-panel';
    el.dataset.errorFor = evt.stack;
    if (evt.status) el.dataset.status = evt.status;
    el.textContent = evt.error;
    return el;
  }

  // Place a health pill on the newest row of each stack that has a health
  // status, and remove pills from older rows (health is a current per-stack
  // value; the deploy table is an event log — see ADR-0027). Cheap to re-run on
  // every deploy or health snapshot: a handful of rows.
  // Manage the two per-stack affordances that belong on the newest row of each
  // stack only: the live health pill (ADR-0027) and the deploy-history button
  // (ADR-0033). Both are current per-stack values, not per-event, so an older
  // row of the same stack carries neither. Cheap to re-run on every deploy or
  // health snapshot — a handful of rows.
  function updateStackAffordances() {
    const seen = {};
    // Peer rows are read-only mirrors — no health pill, history, logs or hooks
    // affordances (those drive local actions the primary cannot perform on a
    // peer), so they are excluded here.
    tbody.querySelectorAll('.event-row[data-stack]:not(.peer-row)').forEach(function (row) {
      const stack = row.dataset.stack;
      const cell = row.querySelector('.cell-stack');
      const statusCell = row.querySelector('.status-cell');
      if (!cell || !statusCell) return;
      const newest = !seen[stack];
      seen[stack] = true;

      // Health pill — only when a health status is known for the stack. Lives
      // in .status-cell (stacked under the SUCCESS/FAILED badge) rather than
      // .cell-stack, so it reads as row state next to the other state badge
      // instead of competing with the stack-name icon cluster.
      let pill = statusCell.querySelector('.health-pill');
      const h = newest ? healthSnap[stack] : null;
      if (h && h.status) {
        if (!pill) {
          // A real <button> (healthPillHTML) so the panel is keyboard-reachable
          // (Tab + Enter/Space), and the pill markup has one source of truth.
          statusCell.insertAdjacentHTML('beforeend', healthPillHTML(stack, h.status));
        } else {
          pill.dataset.health = h.status;
          pill.dataset.stack = stack;
          pill.querySelector('.hlabel').textContent = h.status;
          pill.title = stack + ' — ' + h.status;
        }
      } else if (pill) {
        pill.remove();
        closeHealthPanel(row);
      }

      // Secondary per-stack actions (history, container logs, deploy hooks) live
      // in a single ⋯ overflow menu on the newest row (T3.13), so the row rests
      // calm. Non-newest rows carry no menu; drop it (and close its panels).
      if (!newest) {
        const staleWrap = cell.querySelector('.row-more');
        if (staleWrap) {
          if (openMoreWrap === staleWrap) closeMoreMenu();
          staleWrap.remove();
          closeAuditPanel(row);
          closeHooksPanel(row);
        }
      } else {
        const pop = ensureRowMore(cell, stack).querySelector('.more-pop');

        // History — always present (independent of health).
        if (!pop.querySelector('.history-btn')) {
          const hist = document.createElement('button');
          hist.className = 'history-btn';
          hist.type = 'button';
          hist.dataset.testid = 'history-btn';
          hist.setAttribute('aria-label', 'deploy history for ' + stack);
          hist.innerHTML =
            '<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5"><circle cx="8" cy="8" r="6"/><path d="M8 4.5V8l2.4 1.5"/></svg>';
          pop.appendChild(moreMenuItem(hist, 'Deploy history'));
        }

        // Container logs — per stack (ADR-0037). Not for _nixos: it is not a
        // compose project, so it has no container logs.
        const clogBtn = pop.querySelector('.clog-btn:not(.hook-log-btn)');
        if (!clogBtn && stack !== NIXOS_STACK) {
          pop.appendChild(moreMenuItem(clogButton(stack, ''), 'Container logs'));
        } else if (stack === NIXOS_STACK && clogBtn) {
          clogBtn.remove();
        }

        // Deploy hooks — only when the stack declares any (ADR-0038).
        const hooks = hooksFor(stack);
        const hbadge = pop.querySelector('.hooks-badge');
        if (hooks && hookCount(hooks) > 0) {
          if (!hbadge)
            pop.appendChild(moreMenuItem(hooksBadgeButton(stack, hooks), 'Deploy hooks'));
        } else if (hbadge) {
          hbadge.remove();
          closeHooksPanel(row);
        }
      }
    });
    applyHookRun(); // re-paint a running hook's phase after rows/affordances changed
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
      rosterSnap.find(function (r) {
        return r.name === stack;
      }) || {};
    const el = document.createElement('div');
    el.className = 'watched-panel';
    el.dataset.testid = 'watched-panel';
    el.dataset.watchedFor = stack;
    el.innerHTML = watchedPanelHTML(entry, repoWebURL);
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
    const body = auditRowsHTML(records, repoWebURL, absoluteTime);
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

  // ── Health-timeline fold toggle ──
  // The timeline folds routine deploy/restart cycles away (healthHistoryHTML);
  // this swaps one service's folded view for its raw phase list and back. One
  // delegated listener for every surface the panel appears on (deploy rows,
  // roster card, peer detail) — the panel is rebuilt on each open/snapshot, so
  // per-instance wiring would re-bind constantly. The panel lives outside any
  // row element, so no row-toggle handler sees this click.
  document.addEventListener('click', function (e) {
    const btn = e.target.closest && e.target.closest('.hp-fold-toggle');
    if (!btn) return;
    const hist = btn.closest('.hp-history');
    if (!hist) return;
    const raw = hist.classList.toggle('show-raw');
    btn.setAttribute('aria-expanded', String(raw));
    btn.textContent = raw ? 'fold routine cycles' : btn.dataset.label;
  });

  // ── Deploying indicator + run drawer ──

  function updateDeployingIndicator() {
    const active = Object.keys(deployingRows);
    const up = upcomingSnap;
    if (active.length > 0) {
      deployStatus.className = 'deploy-status active';
      dsActive.textContent = active.join(', ');
      dsNext.innerHTML = up.length ? nextTrailHTML(up) : '';
      dsCount.textContent = up.length ? '+' + up.length : '';
    } else {
      deployStatus.className = 'deploy-status';
      dsActive.textContent = 'idle';
      dsNext.innerHTML = '';
      dsCount.textContent = '';
      setRunDrawer(false); // nothing running; close the panel if open
    }
    const label = deployStatusLabel(active, up);
    deployStatus.title = label;
    deployStatus.setAttribute('aria-label', label);
    renderRunPanel();
  }

  // renderRunPanel fills the run drawer from the active deploy(s) plus the
  // upcoming snapshot. Read-only — no switches.
  function renderRunPanel() {
    const active = Object.keys(deployingRows);
    const up = upcomingSnap;
    runSub.innerHTML = runSummaryHTML(active, up);
    const total = active.length + up.length;
    runCount.textContent = total ? total : '';
    runList.innerHTML = runListHTML(active, up);
  }

  // setRunDrawer opens/closes the run panel. It is mutually exclusive with the
  // autosync drawer and the view-options popover.
  function setRunDrawer(open) {
    if (open) {
      setViewOptions(false);
      setDrawer(false);
    }
    runDrawer.classList.toggle('open', open);
    deployStatus.setAttribute('aria-expanded', String(open));
    manageSurfaceFocus(runDrawer, deployStatus, open);
  }

  deployStatus.addEventListener('click', function () {
    if (!deployStatus.classList.contains('active')) return; // only openable during a run
    setRunDrawer(!runDrawer.classList.contains('open'));
  });
  deployStatus.addEventListener('keydown', function (e) {
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault();
      deployStatus.click();
    }
  });

  // registerSurface wires the shared dismiss behaviour for a pop-out surface (a
  // drawer or popover): Escape closes it, and a click outside it closes it. Each
  // surface used to copy-paste its own pair of global document handlers; this
  // centralizes them. `isOpen` reports whether the surface is showing, `close`
  // dismisses it, and `within` lists the elements (the surface plus its trigger)
  // whose clicks are internal and must not dismiss it.
  function registerSurface(opts) {
    document.addEventListener('keydown', function (e) {
      if (e.key === 'Escape') opts.close();
    });
    document.addEventListener('click', function (e) {
      if (!opts.isOpen()) return;
      // composedPath() is captured when the event is dispatched, so it still
      // names the surface even if the click's handler synchronously re-rendered
      // and detached e.target (e.g. toggling a host in the Hosts drawer rebuilds
      // its list) — a plain contains(e.target) would then miss and wrongly close.
      const path = typeof e.composedPath === 'function' ? e.composedPath() : [];
      if (
        opts.within.some(function (el) {
          return el.contains(e.target) || path.indexOf(el) !== -1;
        })
      )
        return;
      opts.close();
    });
  }

  // --- Pop-out focus management (T2.7) -----------------------------------
  // Keyboard users must be pulled into a drawer/popover when it opens and
  // returned to the opener when it closes; the role="dialog" drawers must also
  // trap Tab so it can't wander behind the open panel.
  const FOCUSABLE_SEL =
    'a[href],button:not([disabled]),input:not([disabled]),' +
    'select:not([disabled]),textarea:not([disabled]),[tabindex]:not([tabindex="-1"])';
  // Upper bound on frames the open-focus keeps re-trying while a surface settles
  // (see manageSurfaceFocus). Comfortably longer than the 0.22s open transition
  // (~14 frames at 60fps) so focus always lands within the animation.
  const FOCUS_SETTLE_FRAMES = 30;
  function focusablesIn(el) {
    return Array.prototype.filter.call(el.querySelectorAll(FOCUSABLE_SEL), function (n) {
      // Skip hidden controls (a collapsed vo-group, a display:none clear button).
      return n.offsetWidth > 0 || n.offsetHeight > 0 || n.getClientRects().length > 0;
    });
  }
  // focusRestsInside reports whether focus already sits on a real control within
  // `surface` — the bare surface fallback (its tabindex=-1 container) doesn't
  // count. Lets the open-focus bow out once the observer has moved focus inside.
  function focusRestsInside(surface) {
    const ae = document.activeElement;
    return !!ae && ae !== surface && surface.contains(ae);
  }
  // manageSurfaceFocus moves focus into `surface` on open and restores it to
  // `opener` on close. Restore only fires when focus is still inside the surface
  // (Escape / keyboard close) — an outside click already moved focus elsewhere,
  // so yanking it back would be wrong.
  //
  // On open the first attempt is synchronous, so focus has landed by the time
  // the caller's "surface is open" await resolves — no observer has to wait out
  // the open animation. Should the surface not be focusable yet (contents
  // rendered at open time, a live list still rebuilding, or visibility:hidden
  // still settling), it retries each frame until focus rests on a real control,
  // bounded by FOCUS_SETTLE_FRAMES. Retrying stops the instant focus is inside —
  // whether we placed it or the user moved there first — so it can neither be
  // lost to the open animation nor yank focus back from where the user went next.
  function manageSurfaceFocus(surface, opener, open) {
    if (open) {
      surface._opener = opener;
      let tries = FOCUS_SETTLE_FRAMES;
      (function settle() {
        if (!surface.classList.contains('open')) return; // toggled off already
        if (focusRestsInside(surface)) return; // observer moved focus in — don't override
        const target = focusablesIn(surface)[0] || surface;
        // preventScroll: the surface is mid-open, so its scroll box is still a
        // sliver — a scroll-into-view here leaves the drawer parked past its own
        // first rows, with the control the viewer came for cut off at the top.
        target.focus({ preventScroll: true });
        if (document.activeElement === target || --tries <= 0) return;
        requestAnimationFrame(settle);
      })();
    } else {
      const ae = document.activeElement;
      const returnFocus = surface._opener && (!ae || ae === document.body || surface.contains(ae));
      const opener = surface._opener;
      surface._opener = null;
      if (returnFocus && opener && typeof opener.focus === 'function')
        opener.focus({ preventScroll: true });
    }
  }
  // trapFocus keeps Tab/Shift+Tab within an open dialog (wraps at the ends).
  function trapFocus(dialog) {
    dialog.addEventListener('keydown', function (e) {
      if (e.key !== 'Tab' || !dialog.classList.contains('open')) return;
      const f = focusablesIn(dialog);
      if (!f.length) {
        e.preventDefault();
        dialog.focus();
        return;
      }
      const first = f[0],
        last = f[f.length - 1];
      if (e.shiftKey && document.activeElement === first) {
        e.preventDefault();
        last.focus();
      } else if (!e.shiftKey && document.activeElement === last) {
        e.preventDefault();
        first.focus();
      }
    });
  }
  trapFocus(runDrawer); // asDrawer/hostsDrawer are trapped in their own sections (declared later)

  registerSurface({
    isOpen: function () {
      return runDrawer.classList.contains('open');
    },
    close: function () {
      setRunDrawer(false);
    },
    within: [runDrawer, deployStatus],
  });

  // ── Deploy-event ingest: queued rows + handleEvent ──

  // refreshPendingTags re-renders the paused/blocked tag on every live pending
  // row. The pending deploy event and the queue snapshot (which carries the
  // authoritative reason) are not ordered — the snapshot is published after the
  // run's events, and after the history replay on connect — so a row can render
  // before its reason is known. This re-render makes the tag deterministic once
  // the snapshot lands.
  function refreshPendingTags() {
    Object.keys(queuedRows).forEach(function (stack) {
      const row = queuedRows[stack];
      const tag = row.querySelector('.paused-tag');
      if (tag) tag.outerHTML = pendingTagHTML(row.dataset.status, pendingReason(stack));
    });
  }

  // removeQueuedRow drops a stack's pending (queued) row and any panel opened
  // below it. The close helpers cover every panel type a queued row can carry
  // (files/diff, health, deploy history), so a drained row never strands a
  // panel in the table. No-op when the stack has no queued row.
  function removeQueuedRow(stack) {
    const row = queuedRows[stack];
    if (!row) return;
    closeDiffPanel(row);
    closeHealthPanel(row);
    closeAuditPanel(row);
    row.remove();
    delete queuedRows[stack];
  }

  function handleEvent(evt, isHistory) {
    // A skipped deploy means an unchanged stack — no signal worth showing, so
    // it is never rendered (skipped events are live-only, never in history).
    if (evt.status === 'skipped') return;

    showTable();

    // Voice terminal outcomes to screen readers as they land live. The
    // post-connect replay burst lands while the gate is closed, so a reconnect
    // never announces the backlog. (T2.8)
    if (announceReady) announce(deployAnnouncement(evt.status, evt.stack));

    if (evt.status === 'queued' || evt.status === 'blocked') {
      // Both are pending, leave-dirty states re-emitted every sync/reconcile
      // tick (queued = autosync paused; blocked = a failed depends_on
      // dependency, ADR-0032). Collapse repeats into one live row per stack,
      // keyed so it clears when the stack finally deploys.
      removeQueuedRow(evt.stack);
      const qrow = createRow(evt, isHistory);
      tbody.insertBefore(qrow, tbody.firstChild);
      queuedRows[evt.stack] = qrow;
      return;
    }

    // Any real deploy progress supersedes this stack's pending row.
    removeQueuedRow(evt.stack);

    if (evt.status === 'deploying') {
      const row = createRow(evt, isHistory);
      tbody.insertBefore(row, tbody.firstChild);
      deployingRows[evt.stack] = row;
      updateDeployingIndicator();
      return;
    }

    if (
      (evt.status === 'success' ||
        evt.status === 'failed' ||
        evt.status === 'rolled_back' ||
        evt.status === 'rolled_back_unhealthy') &&
      deployingRows[evt.stack]
    ) {
      const existing = deployingRows[evt.stack];
      existing.className = rowClass(evt.status, isHistory);
      existing.dataset.status = evt.status;
      existing.dataset.eventId = evt.id;
      if (evt.has_diffs) existing.dataset.hasDiffs = '1';
      // The terminal event alone knows whether this success retries a rollback,
      // so the note lands as the row settles (like the Version column below).
      existing.querySelector('.status-cell').innerHTML = badgeHTML(evt.status) + retryNoteHTML(evt);
      existing.querySelector('.cell-duration').textContent = formatDuration(evt.duration_ms);
      const tc = existing.querySelector('.cell-time');
      tc.dataset.ts = evt.timestamp;
      tc.textContent = absoluteTime ? fullTime(evt.timestamp) : formatTime(evt.timestamp);
      tc.title = absoluteTime ? formatTime(evt.timestamp) : fullTime(evt.timestamp);
      if (evt.changed_files && evt.changed_files.length > 0) {
        existing.querySelector('.col-files').innerHTML = filesHTML(evt.changed_files);
      }
      // The terminal event carries image_changes (the preceding deploying event
      // does not), so fill the Version column as the row settles — otherwise a
      // live deploy would show it only after a reload. Stash and cell are both
      // written from this event, so a re-emitted one stays idempotent and the
      // toggle can never restore a delta the cell no longer shows.
      if (evt.image_changes && evt.image_changes.length) {
        existing.dataset.imageChanges = JSON.stringify(evt.image_changes);
      } else {
        delete existing.dataset.imageChanges;
      }
      const versionCell = existing.querySelector('.col-version');
      if (versionCell)
        versionCell.innerHTML = imageDeltaOn ? imageDeltaHTML(evt.image_changes) : '';
      if (evt.error) {
        existing.after(createErrorDetail(evt));
      }
      delete deployingRows[evt.stack];
      updateDeployingIndicator();
      return;
    }

    const row = createRow(evt, isHistory);
    tbody.insertBefore(row, tbody.firstChild);
    if (evt.error) {
      row.after(createErrorDetail(evt));
    }
  }

  setInterval(function () {
    if (!absoluteTime) {
      tbody.querySelectorAll('.cell-time').forEach(function (cell) {
        const abs = cell.dataset.ts;
        if (abs) cell.textContent = formatTime(abs);
      });
    }
    // The incident badge's 24h window ages out on the same tick — the stacks
    // snapshot only republishes after runs, and the count must not wait for one.
    renderIncidentBadge();
  }, 30000);

  // EventSource auto-reconnects after a transient drop (readyState CONNECTING),
  // but a *fatal* stream error — a non-2xx response or a bad content-type — puts
  // it in CLOSED for good and it never retries. Without our own retry the
  // indicator would then sit on `reconnecting` forever, so we retry ourselves
  // with a capped backoff that onopen resets once a connection is good.
  const eventsReconnect = makeReconnector(
    function () {
      connect();
    },
    setTimeout,
    clearTimeout,
  );

  // ── Multi-host fan-in (ADR-0048) ──
  // A peer instance's read data, merged into the local views and tagged by host.
  // Everything here is inert on a single-host instance (peersSnap null): the
  // Hosts control, Host column and peer rows never appear.

  // hostList is the effective host set: the primary (self) first, then each
  // peer — buildHostList (app-helpers.js) with the live peers snapshot bound.
  function hostList() {
    return buildHostList(peersSnap, selfHost);
  }

  // isHostSelected reports whether a host is in view. hostSelected null means the
  // filter is off (all hosts shown); otherwise it is the selected-name set.
  function isHostSelected(name) {
    return hostSelected === null || hostSelected.has(name);
  }

  // recomputeHostColors reassigns palette slots whenever the host set changes,
  // keeping the no-two-hosts-share-a-colour guarantee (assignHostColors).
  function recomputeHostColors() {
    if (!peersSnap) {
      hostColors = {};
      return;
    }
    hostColors = assignHostColors(
      hostList().map(function (h) {
        return h.name;
      }),
    );
  }

  // hostChip supplies hostChipHTML (app-render.js) with the host's colour slot
  // from the live assignment.
  function hostChip(hostName) {
    return hostChipHTML(hostName, hostColors[hostName]);
  }
  // hostChipKeydown activates a focused host chip from the keyboard (T2.10),
  // mirroring the click delegation on the deploy feed and roster.
  function hostChipKeydown(e) {
    if (e.key !== 'Enter' && e.key !== ' ') return;
    const chip = e.target.closest && e.target.closest('.host-mono');
    if (!chip) return;
    e.preventDefault();
    const r = chip.closest('[data-host]');
    if (r) toggleHostFilterTo(r.dataset.host);
  }

  // createPeerRow builds one read-only deploy row from a peer's audit record.
  // Peer rows carry no *local* drill-down (no diff/hook/history/log fetch), but a
  // click opens a compact read-only detail (commit + file count + status) with a
  // link to the peer's own UI for the full diff — a glance that never dead-ends.
  //
  // newest marks the newest deploy row for a (host, stack): only it carries the
  // live health pill (health is a current per-stack value, not per-event — the
  // same rule updateStackAffordances applies to local rows).
  function createPeerRow(rec, hostName, stale, newest) {
    const row = document.createElement('div');
    row.className = rowClass(rec.status, true) + ' peer-row' + (stale ? ' peer-stale' : '');
    row.dataset.testid = 'deploy-row';
    row.dataset.stack = rec.stack;
    row.dataset.status = rec.status;
    row.dataset.host = hostName;
    if (rec.commit_sha) row.dataset.commit = rec.commit_sha;
    row.dataset.changed = String(rec.changed_files || 0);
    if (rec.error) row.dataset.error = rec.error;
    if (rec.id) row.dataset.peerEventId = String(rec.id); // for the proxied peer diff
    const absTs = fullTime(rec.timestamp);
    const relTs = formatTime(rec.timestamp);
    const n = rec.changed_files || 0;
    const files =
      n > 0 ? '<span class="peer-files">' + n + ' file' + (n === 1 ? '' : 's') + '</span>' : '';
    const h = newest ? healthMapFor(hostName)[rec.stack] : null;
    const pill = h && h.status ? healthPillHTML(rec.stack, h.status) : '';
    row.innerHTML =
      '<span class="cell-time" data-testid="time-cell" data-ts="' +
      escapeAttr(rec.timestamp) +
      '" title="' +
      escapeAttr(absoluteTime ? relTs : absTs) +
      '">' +
      (absoluteTime ? absTs : relTs) +
      '</span>' +
      '<span class="cell-stack">' +
      hostChip(hostName) +
      '<span class="stack-icon" data-testid="stack-icon"></span><span class="stack-name">' +
      escapeHtml(rec.stack) +
      '</span></span>' +
      // Empty Version cell: the fan-in's audit records carry no image_changes, but
      // the peer row shares the deploy grid, so the cell must exist to stay aligned.
      '<span class="col-version"></span>' +
      '<span class="status-cell">' +
      badgeHTML(rec.status) +
      pill +
      '</span>' +
      '<span class="cell-duration" data-testid="duration-cell">' +
      formatDuration(rec.duration_ms) +
      '</span>' +
      '<span class="col-files">' +
      files +
      '</span>';
    if (rec.error) row.title = rec.stack + ' — ' + rec.error;
    populateIcon(row.querySelector('.stack-icon'), rec.stack);
    return row;
  }

  // createPeerDetailPanel is what a peer row expands to on click: the read-only
  // facts the fan-in carries (commit, changed-file count, error) plus the peer's
  // diff, loaded inline through the primary's proxy. A click never dead-ends,
  // even on a mirror row.
  function createPeerDetailPanel(row) {
    const host = row.dataset.host;
    const el = document.createElement('div');
    el.className = 'peer-detail bound';
    el.dataset.testid = 'peer-detail';
    el.dataset.status = row.dataset.status || '';
    el.dataset.peerFor = host;
    let facts = '';
    if (row.dataset.commit) {
      facts +=
        '<span class="pd-fact">commit ' +
        commitLinkHTML(row.dataset.commit, {
          cls: 'pd-sha',
          base: repoWebURLFor(host),
          title: row.dataset.commit,
        }) +
        '</span>';
    }
    if (row.dataset.changed !== undefined) {
      const n = parseInt(row.dataset.changed, 10) || 0;
      facts += '<span class="pd-fact">' + n + ' file' + (n === 1 ? '' : 's') + ' changed</span>';
    }
    const eventId = row.dataset.peerEventId;
    let html =
      '<div class="pd-head">' +
      hostChip(host) +
      '<span class="pd-note">peer deploy · read-only mirror</span></div>' +
      (facts ? '<div class="pd-facts">' + facts + '</div>' : '');
    if (row.dataset.error)
      html += '<div class="pd-error">' + escapeHtml(row.dataset.error) + '</div>';
    // The diff is fetched from the peer through the primary's proxy (the browser
    // can't reach the peer cross-origin) and rendered here on expand. (The
    // per-host "open its own UI" affordance lives in the Hosts drawer, not here.)
    if (eventId)
      html +=
        '<div class="pd-diff" data-testid="peer-diff"><div class="diff-loading">Loading diff…</div></div>';
    el.innerHTML = html;
    // Containers: the peer's fanned-in health for this stack, rendered with the
    // same panel the primary uses for its own stacks so peers reach health
    // parity. Inserted above the diff; omitted when the peer reported no health
    // record for the stack (a never-deployed or health-less stack).
    const stack = row.dataset.stack;
    if (stack && healthMapFor(host)[stack]) {
      const hp = createHealthPanel(stack, host);
      const diffSlot = el.querySelector('.pd-diff');
      if (diffSlot) el.insertBefore(hp, diffSlot);
      else el.appendChild(hp);
    }
    if (eventId) loadPeerDiff(el, host, eventId);
    return el;
  }

  // loadPeerDiff fetches a peer deploy's diff via the primary's proxy
  // (/api/peers/{host}/events/{id}/diffs) and renders it into the panel's diff
  // slot. A peer that has evicted the event answers 404 — genuine "no diff", so
  // the slot is dropped and the peer-UI link stands in. An unreachable peer
  // (502 / network) is a transient failure: keep the slot and show an amber
  // load-error with a retry (T4.16) rather than silently vanishing.
  function loadPeerDiff(panel, host, id) {
    fetch('/api/peers/' + encodeURIComponent(host) + '/events/' + encodeURIComponent(id) + '/diffs')
      .then(function (r) {
        if (r.ok) return r.json();
        if (r.status >= 500) throw new Error('unreachable'); // 502 peer unreachable
        return null; // 404: event evicted, genuine no-diff
      })
      .then(function (data) {
        const slot = panel.querySelector('.pd-diff');
        if (!slot) return;
        if (data && data.diffs && Object.keys(data.diffs).length > 0) {
          slot.textContent = '';
          slot.appendChild(renderDiffPanel(data.diffs, null, {})); // files only — the commit is already in the facts row
        } else {
          slot.remove(); // no diff to show; the peer-UI link remains
        }
      })
      .catch(function () {
        const slot = panel.querySelector('.pd-diff');
        if (!slot) return;
        slot.textContent = '';
        slot.appendChild(
          createLoadError('Couldn’t reach ' + host + ' for the diff.', function () {
            loadPeerDiff(panel, host, id); // re-query the (still-present) slot and refetch
          }),
        );
      });
  }

  // togglePeerDetail opens or closes a peer row's read-only detail panel.
  function togglePeerDetail(row) {
    const next = row.nextElementSibling;
    if (next && next.classList.contains('peer-detail')) {
      next.remove();
      row.classList.remove('diff-open');
      return;
    }
    openPeerDetail(row);
  }

  // openPeerDetail opens a peer row's read-only detail card. Shared with the
  // re-open after a roster rebuild, so a restored panel is built exactly like a
  // clicked one — and the restore never has to rely on the row being closed.
  function openPeerDetail(row) {
    row.classList.add('diff-open'); // reuse the bound status bar/tint
    row.after(createPeerDetailPanel(row));
  }

  // insertRowByTime places a row into the merged feed at its chronological spot:
  // before the first existing row that is older. ISO timestamps compare
  // lexicographically, so a string compare is a chronological one.
  function insertRowByTime(row, ts) {
    const rows = tbody.querySelectorAll('.event-row');
    for (let i = 0; i < rows.length; i++) {
      const cell = rows[i].querySelector('.cell-time');
      const ets = cell ? cell.dataset.ts : '';
      if (ets && ets < ts) {
        tbody.insertBefore(row, rows[i]);
        return;
      }
    }
    tbody.appendChild(row);
  }

  // renderPeerRows rebuilds every peer row from the current snapshot, interleaved
  // into the local rows by timestamp. Local rows and their panels are never
  // touched; only peer rows (which have no panels) are removed and rebuilt.
  function renderPeerRows() {
    tbody.querySelectorAll('.peer-row, .peer-detail').forEach(function (r) {
      r.remove();
    });
    if (!peersSnap) return;
    const items = [];
    (peersSnap.peers || []).forEach(function (p) {
      (p.deploys || []).forEach(function (rec) {
        if (rec.status === 'skipped') return; // an unchanged stack is not shown, as locally
        items.push({ rec: rec, host: p.name, stale: !!p.stale });
      });
    });
    // Newest first, so each insert-above-first-older builds a correct merge.
    items.sort(function (a, b) {
      return a.rec.timestamp < b.rec.timestamp ? 1 : a.rec.timestamp > b.rec.timestamp ? -1 : 0;
    });
    if (items.length) showTable();
    // The first row seen per (host, stack) is the newest — it carries the live
    // health pill (a current per-stack value), like the local newest row.
    const seenNewest = {};
    items.forEach(function (it) {
      const perHost = seenNewest[it.host] || (seenNewest[it.host] = {});
      const newest = !perHost[it.rec.stack];
      perHost[it.rec.stack] = true;
      insertRowByTime(createPeerRow(it.rec, it.host, it.stale, newest), it.rec.timestamp);
    });
  }

  // setLeadingChip replaces (or clears) a stack cell's leading host chip in place.
  function setLeadingChip(cell, hostName) {
    const existing = cell.querySelector('.host-mono');
    if (existing) existing.remove();
    if (hostName) cell.insertAdjacentHTML('afterbegin', hostChip(hostName));
  }

  // retagLocalRows fills in the host dot + data-host on local rows created before
  // the first peers snapshot set selfHost (deploy history and the roster are both
  // painted just before it), across both merged views.
  function retagLocalRows() {
    tbody.querySelectorAll('.event-row[data-stack]:not(.peer-row)').forEach(function (row) {
      row.dataset.host = selfHost;
      const cell = row.querySelector('.cell-stack');
      if (cell) setLeadingChip(cell, selfHost);
    });
    document
      .getElementById('roster-list')
      .querySelectorAll('.roster-row[data-stack]:not(.peer-row)')
      .forEach(function (row) {
        row.dataset.host = selfHost;
        // The chip leads the identity group, not the cell — the cell's first
        // child is that group.
        const cell = row.querySelector('.roster-ident');
        if (cell) setLeadingChip(cell, selfHost);
      });
  }

  // refreshPeerRosterRows rebuilds only the peer roster rows (which carry no
  // panels), leaving local roster rows and any open audit/health panel intact —
  // the roster's equivalent of renderPeerRows for the deploy feed.
  function refreshPeerRosterRows() {
    const list = document.getElementById('roster-list');
    list.querySelectorAll('.roster-row.peer-row, .peer-detail').forEach(function (r) {
      r.remove();
    });
    renderPeerRosterRows(list);
  }

  // renderHosts paints the Hosts control (header badge + drawer list). Hidden
  // entirely when no peers are configured.
  function renderHosts() {
    const btn = document.getElementById('hosts-btn');
    const hosts = hostList();
    const hasPeers = !!(peersSnap && (peersSnap.peers || []).length);
    btn.classList.toggle('enabled', hasPeers);
    if (!hasPeers) return;

    const selCount = hosts.filter(function (h) {
      return isHostSelected(h.name);
    }).length;
    document.getElementById('hosts-count').textContent = selCount + '/' + hosts.length;
    btn.classList.toggle('filtered', hostFilterActive(selCount, hosts.length));
    const anyOffline = hosts.some(function (h) {
      return !h.self && !h.reachable;
    });
    btn.classList.toggle('has-offline', anyOffline);
    document.getElementById('hosts-sub').textContent = anyOffline
      ? 'Some hosts are unreachable — showing last-known data.'
      : 'Filter the merged view by host.';

    const list = document.getElementById('hosts-list');
    list.innerHTML = '';
    hosts.forEach(function (h) {
      const row = document.createElement('div');
      row.className = 'host-row' + (isHostSelected(h.name) ? ' selected' : '');
      row.dataset.host = h.name;
      row.dataset.testid = 'host-row';
      // Multi-select filter row: a keyboard-operable checkbox (T2.10). The
      // check glyph is decorative; aria-checked carries the selection state.
      row.setAttribute('role', 'checkbox');
      row.tabIndex = 0;
      row.setAttribute('aria-checked', String(isHostSelected(h.name)));
      row.setAttribute('aria-label', h.name + (h.self ? ' (this host)' : ''));
      const slot = hostColors[h.name];
      const attr = slot === undefined ? '' : ' data-host-color="' + slot + '"';
      const dotClass = h.self || h.reachable ? 'up' : h.stale ? 'stale' : 'down';
      const dotTitle = h.self
        ? 'this host'
        : h.reachable
          ? 'reachable'
          : h.stale
            ? 'unreachable — showing last-known'
            : 'unreachable';
      const link =
        !h.self && h.url
          ? '<a class="host-link" href="' +
            escapeAttr(h.url) +
            '" target="_blank" rel="noopener" title="Open ' +
            escapeAttr(h.name) +
            ' UI" data-testid="host-link">' +
            LINK_ICON +
            '</a>'
          : '';
      const selfBadge = h.self ? '<span class="hr-self">self</span>' : '';
      row.innerHTML =
        '<span class="host-check">' +
        CHECK_ICON +
        '</span>' +
        '<span class="hr-name"><span class="host-tag"' +
        attr +
        '><span class="host-name">' +
        escapeHtml(h.name) +
        '</span></span>' +
        selfBadge +
        '</span>' +
        '<span class="host-dot ' +
        dotClass +
        '" title="' +
        escapeAttr(dotTitle) +
        '"></span>' +
        '<span class="host-link-slot">' +
        link +
        '</span>';
      list.appendChild(row);
    });
  }

  // hostHideRows hides deselected-host rows in one container (the deploy feed or
  // the roster), carrying each row's visibility onto its trailing panel(s) — the
  // same carry-over the stack filter uses, so host + stack filters compose.
  function hostHideRows(container) {
    let visible = true;
    Array.prototype.forEach.call(container.children, function (el) {
      const tid = el.dataset && el.dataset.testid;
      if (tid === 'deploy-row' || tid === 'roster-row') visible = isHostSelected(el.dataset.host);
      el.classList.toggle('host-hidden', !visible);
    });
  }

  // applyHostFilter shows/hides rows by their host across both merged views,
  // toggles the per-row host dots, and raises the stale banner for any in-view
  // peer that is unreachable.
  function applyHostFilter() {
    const table = document.getElementById('deploy-table');
    const stacksView = document.getElementById('stacks-view');
    const banner = document.getElementById('host-stale-banner');
    const roster = document.getElementById('roster-list');
    if (!peersSnap || !(peersSnap.peers || []).length) {
      table.classList.remove('show-hosts', 'host-filter-active');
      stacksView.classList.remove('show-hosts', 'host-filter-active');
      banner.classList.remove('show');
      return;
    }
    // Dots stay visible whenever peers are configured — with a single host in
    // view they are redundant as an indicator, but they double as the click
    // target to clear the filter, so hiding them would strand the user.
    table.classList.add('show-hosts');
    stacksView.classList.add('show-hosts');

    // While a filter is narrowing the view, highlight the (now single-host) dots
    // so it is visible that a filter is on and re-clicking a dot clears it.
    const hosts = hostList();
    const active = hostFilterActive(
      hosts.filter(function (h) {
        return isHostSelected(h.name);
      }).length,
      hosts.length,
    );
    table.classList.toggle('host-filter-active', active);
    stacksView.classList.toggle('host-filter-active', active);

    hostHideRows(tbody);
    hostHideRows(roster);

    const staleSel = (peersSnap.peers || []).filter(function (p) {
      return isHostSelected(p.name) && (p.stale || !p.reachable);
    });
    if (staleSel.length) {
      const names = staleSel
        .map(function (p) {
          return p.name;
        })
        .join(', ');
      document.getElementById('host-stale-text').textContent =
        staleSel.length === 1
          ? names + ' is unreachable — showing its last-known deploys.'
          : names + ' are unreachable — showing last-known deploys.';
      banner.classList.add('show');
    } else {
      banner.classList.remove('show');
    }
    renderHosts(); // keep the header badge counts in step with the filter

    // Persist the selection per browser; "all hosts" clears the key so a later
    // reload with no saved value behaves the same as a fresh instance.
    if (hostSelected === null) localStorage.removeItem('hostFilter');
    else localStorage.setItem('hostFilter', Array.from(hostSelected).join(','));
  }

  // toggleHostFilterTo is the dot-click shortcut: isolate the merged view to one
  // host, or — if that host is already the only one in view — clear the filter
  // back to all. Complements the multi-select Hosts drawer.
  function toggleHostFilterTo(name) {
    if (!name) return;
    const isolated = hostSelected !== null && hostSelected.size === 1 && hostSelected.has(name);
    hostSelected = isolated ? null : new Set([name]);
    renderPeerRows();
    refreshPeerRosterRows();
    applyHostFilter();
    refilterDeploys();
    refilterRoster();
  }

  // schedulePeerReflow re-interleaves peer rows after local deploy rows land.
  // Local rows are inserted at the top (newest first); a burst of replayed
  // history — or a fresh live deploy — therefore lands above the peer rows that
  // were already merged in, so the peer rows must be re-slotted by timestamp
  // afterwards. Coalesced to one pass per frame so a history replay reflows once.
  let peerReflowScheduled = false;
  function schedulePeerReflow() {
    if (!peersSnap || peerReflowScheduled) return;
    peerReflowScheduled = true;
    setTimeout(function () {
      peerReflowScheduled = false;
      renderPeerRows();
      applyHostFilter();
      refilterDeploys();
    }, 0);
  }

  // applyPeers ingests a fresh 'peers' snapshot end to end.
  function applyPeers(d) {
    peersSnap = d || null;
    selfHost = (peersSnap && peersSnap.self) || '';
    if (!hostFilterRestored && peersSnap) {
      hostFilterRestored = true; // once only — later refreshes must not override interactive picks
      const saved = (localStorage.getItem('hostFilter') || '').split(',').filter(Boolean);
      const restored = reconcileHostFilter(
        saved,
        hostList().map(function (h) {
          return h.name;
        }),
      );
      hostSelected = restored ? new Set(restored) : null;
    }
    recomputeHostColors();
    retagLocalRows();
    renderPeerRows(); // deploy feed peer rows
    refreshPeerRosterRows(); // roster peer rows (local roster rows + panels untouched)
    applyHostFilter(); // dots + host filtering across both views; also renderHosts
    refilterDeploys(); // re-apply any active stack search to the new peer rows
    refilterRoster(); // and to the new peer roster rows
  }

  // applyState routes one state snapshot (by its state-event name) to the same
  // handling whether it is the baseline the stream sends on (re)connect or a
  // later live state event. State payloads are snapshots (full replacements), so
  // applying one is idempotent. 'deploy' is not here — deploy events are the
  // append-style history, streamed over SSE, not part of the state snapshot.
  function applyState(name, d) {
    switch (name) {
      case 'autosync':
        applyAutosyncSnapshot(d);
        break;
      case 'queue':
        queueSnap = d;
        queueByStack = {};
        (queueSnap.pending || []).forEach(function (it) {
          queueByStack[it.stack] = it;
        });
        // A stack that left the pending set without a deploy event (e.g. resumed
        // then found unchanged) must lose its now-stale queued row.
        Object.keys(queuedRows).forEach(function (stack) {
          if (!queueByStack[stack]) removeQueuedRow(stack);
        });
        refreshPendingTags();
        renderAutosync();
        break;
      case 'upcoming':
        upcomingSnap = (d && d.upcoming) || [];
        updateDeployingIndicator();
        break;
      case 'hookrun':
        hookRunSnap = d || {};
        applyHookRun();
        break;
      case 'health':
        healthSnap = (d && d.stacks) || {};
        updateStackAffordances();
        updateRosterHealth();
        renderHealthAttention(); // beacon + band read the same snapshot
        break;
      case 'healthwatch':
        healthwatchSnap = (d && d.stacks) || {};
        break;
      case 'stacks':
        disabledSnap = (d && d.disabled) || [];
        rosterSnap = (d && d.roster) || [];
        repoWebURL = (d && d.repo_web_url) || '';
        updatesSnap = (d && d.updates) || null;
        incidentsSnap = (d && d.incidents_24h) || [];
        renderDisabledStacks();
        renderRoster();
        updateStackAffordances(); // roster carries the hooks — (de)show the badge
        renderIncidentBadge();
        break;
      case 'app_links':
        appLinksSnap = (d && d.stacks) || {};
        updateAppLinks();
        break;
      case 'orphans':
        orphansSnap = (d && d.orphans) || [];
        // Under an active search, re-run the filter so new data obeys the query;
        // otherwise a plain render.
        if (deployFilterWrap.classList.contains('has-value')) applyDeployFilter();
        else renderOrphans();
        break;
      case 'peers':
        applyPeers(d);
        break;
    }
  }

  // ── SSE connection (/api/events) ──

  // The live stream handle, or null before the first connect. Kept out here so
  // the wake-up path (resumeStreams) can tell a surviving stream from a dead one.
  let eventsSource = null;

  const STREAM_OFFLINE_MSG = "Can't reach skipper — the deploy stream is offline.";

  // Once attempts keep failing, the skeleton stops promising a connection and
  // says so. Its spinner and shimmer rows mean "rows are on their way", which is
  // exactly wrong when nothing can reach the server — a page left on a suspended
  // device or off the network would otherwise sit on "Connecting…" forever.
  // Reuses the load-error component rather than inventing a second failure look;
  // its Retry reconnects on the spot instead of making the user reload.
  let offlineNotice = null;

  function showOfflineNotice() {
    if (initialStateSettled) return; // the table is up — the indicator carries it
    if (offlineNotice) offlineNotice.remove(); // replace one spent on "Retrying…"
    offlineNotice = createLoadError(STREAM_OFFLINE_MSG, function () {
      resumeStreams();
    });
    // Beside the skeleton, never inside it: the skeleton is aria-hidden
    // decoration, and this is a real message carrying a real control. A
    // focusable button in an aria-hidden subtree is reachable by keyboard but
    // absent from the accessibility tree.
    loadingState.style.display = 'none';
    loadingState.parentElement.insertBefore(offlineNotice, loadingState.nextSibling);
  }

  function clearOfflineNotice() {
    if (!offlineNotice) return;
    offlineNotice.remove();
    offlineNotice = null;
    // Put the skeleton back if the picture is still unknown — a connection came
    // up and the rows (or the synced marker) are on their way.
    if (!initialStateSettled) loadingState.style.display = '';
  }

  // The stream carries its own baseline: it subscribes, then sends the current
  // state, then the deltas. Reading the baseline over a second channel would
  // reopen the gap a change can vanish into (ADR-0039 amendment). Re-run on
  // every reconnect.
  function connect() {
    connDot.className = 'indicator-dot warn';
    connText.textContent = 'connecting';
    connDot.parentElement.dataset.state = 'connecting';
    connDot.parentElement.title = 'connecting';
    armAnnounceGate(); // re-hydrate silently; the replay burst must not announce (T2.8)
    autosyncVersion = null; // the version restarts with the server; the baseline re-seeds it
    openStream();
  }

  function openStream() {
    // Drop any previous stream before opening the next. Usually it is already
    // CLOSED (close() is then a no-op), but a stream a suspension left behind
    // can still be holding a connection — reopening over it would leak one.
    if (eventsSource) eventsSource.close();
    const es = new EventSource('/api/events');
    eventsSource = es; // the wake-up path reads its readyState; handlers keep `es`

    es.onopen = function () {
      eventsReconnect.reset(); // a good connection resets the backoff and "offline"
      clearOfflineNotice();
      connDot.className = 'indicator-dot';
      connText.textContent = 'connected';
      connDot.parentElement.dataset.state = 'connected';
      connDot.parentElement.title = 'connected';
    };

    es.addEventListener('deploy', function (e) {
      const evt = JSON.parse(e.data);
      handleEvent(evt, false);
      refilterDeploys();
      updateStackAffordances(); // a new/mutated row may change which row is newest
      schedulePeerReflow(); // re-slot peer rows below/among the just-inserted local row
      if (activeView === 'stacks') refreshRosterRow(evt.stack); // reflect in-flight/settled state live
    });

    // End-of-replay marker (T4.17): the deploy history has finished replaying.
    // If it carried rows, handleEvent already retired the skeleton; if it was
    // empty, this is what reveals the genuine-empty state (settleInitialState is
    // one-shot, so reconnect replays are no-ops).
    es.addEventListener('synced', function () {
      settleInitialState();
    });

    // Live state changes reuse the same apply path as the snapshot fetch.
    [
      'autosync',
      'queue',
      'upcoming',
      'hookrun',
      'health',
      'healthwatch',
      'stacks',
      'app_links',
      'orphans',
      'peers',
    ].forEach(function (name) {
      es.addEventListener(name, function (e) {
        applyState(name, JSON.parse(e.data));
      });
    });

    es.onerror = function () {
      eventsReconnect.failed();
      if (eventsReconnect.isOffline()) showOfflineNotice();
      connDot.className = 'indicator-dot err';
      connText.textContent = 'reconnecting';
      connDot.parentElement.dataset.state = 'reconnecting';
      connDot.parentElement.title = 'reconnecting';
      // CLOSED means the browser gave up (fatal error) and will not retry on its
      // own — schedule our own reconnect. CONNECTING means it is already retrying,
      // so leave it be and let onopen recover the indicator.
      if (es.readyState === EventSource.CLOSED) eventsReconnect.schedule();
    };
  }

  // ── Deploys view controls: time mode, version column, stack filter ──

  // Time mode (default: relative). One shared mode; a toggle in both the Deploys
  // and Stacks popovers flips it and updates both views.
  const timeModeBtns = [
    document.getElementById('time-mode'),
    document.getElementById('roster-time-mode'),
  ];

  function applyTimeMode() {
    timeModeBtns.forEach(function (btn) {
      if (!btn) return;
      btn.classList.toggle('active', absoluteTime);
      btn.title = absoluteTime ? 'Switch to relative time' : 'Switch to absolute time';
    });
    tbody.querySelectorAll('.cell-time').forEach(function (cell) {
      const ts = cell.dataset.ts;
      if (!ts) return;
      cell.textContent = absoluteTime ? fullTime(ts) : formatTime(ts);
      cell.title = absoluteTime ? formatTime(ts) : fullTime(ts);
    });
  }

  timeModeBtns.forEach(function (btn) {
    if (!btn) return;
    btn.addEventListener('click', function () {
      absoluteTime = !absoluteTime;
      localStorage.setItem('timeMode', absoluteTime ? 'absolute' : 'relative');
      applyTimeMode();
      // Kept out of applyTimeMode: its setup-time call runs before the roster
      // block is initialized (would TDZ). Reached only on a click here.
      if (activeView === 'stacks') renderRoster();
    });
  });

  applyTimeMode();

  // Per-service image delta toggle (Deploys popover). Reflects the toggle state,
  // collapses the whole Version column when off (.no-version — an empty column
  // would keep taking width, and its header would lie), and fills/empties every
  // already-rendered row's cell from its stashed image_changes, so flipping it
  // takes effect live without a reload. New rows honour imageDeltaOn in
  // createRow / the settle path.
  const imageDeltaBtn = document.getElementById('image-delta-toggle');
  const deployTable = document.getElementById('deploy-table');

  function applyImageDelta() {
    if (imageDeltaBtn) {
      imageDeltaBtn.classList.toggle('active', imageDeltaOn);
      imageDeltaBtn.title = imageDeltaOn ? 'Hide the Version column' : 'Show the Version column';
    }
    if (deployTable) deployTable.classList.toggle('no-version', !imageDeltaOn);
    tbody.querySelectorAll('.event-row[data-stack]').forEach(function (row) {
      const cell = row.querySelector('.col-version');
      if (!cell) return;
      cell.innerHTML =
        imageDeltaOn && row.dataset.imageChanges
          ? imageDeltaHTML(JSON.parse(row.dataset.imageChanges))
          : '';
    });
  }

  if (imageDeltaBtn) {
    imageDeltaBtn.addEventListener('click', function () {
      imageDeltaOn = !imageDeltaOn;
      // Store only the off choice; absence means the default (on), so a cleared
      // browser and a first-time visitor both get the delta.
      if (imageDeltaOn) localStorage.removeItem('imageDelta');
      else localStorage.setItem('imageDelta', 'off');
      applyImageDelta();
    });
  }

  applyImageDelta();

  // Deploys filter (type-to-search) — a substring filter over the deploy rows by
  // stack name. There is no header control: the bar is hidden until the user
  // starts typing in the deploys view, then folds down above the table. It hides
  // rows whose data-stack doesn't match (and the panels trailing them); an empty
  // result shows a "No stack matches" note.
  const deployFilterWrap = document.getElementById('deploy-filter-wrap');
  const deployFilter = document.getElementById('deploy-filter');
  const deployFilterClear = document.getElementById('deploy-filter-clear');
  const deployFilterCount = document.getElementById('deploy-filter-count');
  const deployFilterEmpty = document.getElementById('deploy-filter-empty');
  const deployStatusFilterEl = document.getElementById('deploy-status-filter');

  // Active status chips (UI_SPEC.md "Status filter"). Deliberately NOT
  // persisted — a sticky exclusion filter silently hiding rows across sessions
  // is exactly the failure mode the Logs view's persisted severity chips
  // demonstrated on 2026-08-05.
  let deployStatusFilter = [];

  // renderDeployStatusChips rebuilds the chip row from the statuses present
  // among the rendered rows (with counts); an active status whose rows have
  // aged out stays rendered at 0 so the narrowing remains clearable.
  function renderDeployStatusChips() {
    const counts = {};
    tbody.querySelectorAll('.event-row[data-status]').forEach(function (row) {
      const s = row.dataset.status;
      if (s) counts[s] = (counts[s] || 0) + 1;
    });
    deployStatusFilter.forEach(function (s) {
      if (counts[s] === undefined) counts[s] = 0;
    });
    deployStatusFilterEl.innerHTML = deployStatusChipsHTML(counts, deployStatusFilter);
  }

  function applyDeployFilter() {
    const q = (deployFilter.value || '').trim().toLowerCase();
    const statuses = deployStatusFilter;
    deployFilterWrap.classList.toggle('has-value', q.length > 0);
    let total = 0,
      shown = 0,
      visible = false;
    const kids = tbody.children;
    for (let i = 0; i < kids.length; i++) {
      const el = kids[i];
      if (el.dataset && el.dataset.testid === 'deploy-row') {
        total++;
        // The name query and the status chips narrow independently; a row
        // shows only when it passes both.
        visible =
          el.dataset.stack.toLowerCase().indexOf(q) !== -1 &&
          (statuses.length === 0 || statuses.indexOf(el.dataset.status) !== -1);
        if (visible) shown++;
      }
      // A panel (files/diff/error) trails its row and shares its visibility.
      el.classList.toggle('filtered-out', !visible);
    }
    // The same query drives orphan search; re-render, and count orphans into
    // hits/total.
    renderOrphans();
    if (q) {
      for (let j = 0; j < orphansSnap.length; j++) {
        total++;
        if (orphanMatchesQuery(orphansSnap[j], q)) shown++;
      }
    }
    const narrowing = q.length > 0 || statuses.length > 0;
    deployFilterCount.textContent = narrowing ? shown + '/' + total : '';
    const none = narrowing && total > 0 && shown === 0;
    deployFilterEmpty.classList.toggle('show', none);
    // With chips active the all-hidden note generalises — "no stack matches"
    // would blame the name query for rows the status chips hid.
    if (statuses.length > 0) {
      deployFilterEmpty.textContent = 'Nothing matches the active filters.';
    } else {
      deployFilterEmpty.innerHTML = 'No stack matches “<span id="deploy-filter-empty-q"></span>”.';
      document.getElementById('deploy-filter-empty-q').textContent = q;
    }
    renderDeployStatusChips();
  }

  // Re-run the filter after live/replayed rows land so new stacks obey it too
  // — and keep the chip counts current while the bar is open.
  function refilterDeploys() {
    if (deployFilterWrap.classList.contains('has-value') || deployStatusFilter.length > 0) {
      applyDeployFilter();
    } else if (deployFilterWrap.classList.contains('revealed')) {
      renderDeployStatusChips();
    }
  }

  function revealDeployFilter(on) {
    deployFilterWrap.classList.toggle('revealed', on);
    if (on) renderDeployStatusChips();
    syncStackSearchBtn();
  }

  function clearDeployFilter(hide) {
    deployFilter.value = '';
    deployStatusFilter = []; // Esc/clear drops the chips with the query
    applyDeployFilter();
    if (hide) {
      revealDeployFilter(false);
      deployFilter.blur();
    }
  }

  // Chip clicks: toggle the status in/out of the active set.
  deployStatusFilterEl.addEventListener('click', function (e) {
    const chip = e.target.closest('.status-chip');
    if (!chip) return;
    const s = chip.dataset.status;
    const i = deployStatusFilter.indexOf(s);
    if (i === -1) deployStatusFilter.push(s);
    else deployStatusFilter.splice(i, 1);
    applyDeployFilter();
  });

  // presetDeployStatusFilter is the incident badge's landing (UI_SPEC.md
  // "Incident badge"): reveal the bar with the given chips pre-selected and
  // the name query cleared, so the click lands on exactly the promised rows.
  function presetDeployStatusFilter(statuses) {
    deployFilter.value = '';
    deployStatusFilter = statuses.slice();
    revealDeployFilter(true);
    applyDeployFilter();
  }

  deployFilter.addEventListener('input', applyDeployFilter);
  deployFilter.addEventListener('keydown', function (e) {
    if (e.key === 'Escape') {
      e.stopPropagation();
      if (deployFilter.value || deployStatusFilter.length > 0)
        clearDeployFilter(false); // first Esc clears query and chips together
      else {
        revealDeployFilter(false);
        deployFilter.blur();
      } // second Esc folds away
    }
  });
  deployFilter.addEventListener('blur', function (e) {
    // A click on a status chip blurs the input BEFORE the click lands; folding
    // here would collapse the bar mid-click and take the chip with it.
    if (e.relatedTarget && deployStatusFilterEl.contains(e.relatedTarget)) return;
    // Fold away only when nothing narrows — folding with chips active would
    // leave rows hidden by a filter that is no longer on screen.
    if (!deployFilter.value && deployStatusFilter.length === 0) revealDeployFilter(false);
  });
  deployFilterClear.addEventListener('click', function () {
    clearDeployFilter(false);
    deployFilter.focus();
  });

  // Mobile entry point: the "Search stacks" row in the deploys view-options
  // popover reveals the filter and focuses it (raising the on-screen keyboard).
  // Hidden on desktop, where type-to-search covers the same job. setViewOptions
  // is a hoisted function declaration further down in this scope.
  document.getElementById('deploy-search-open').addEventListener('click', function () {
    setViewOptions(false);
    revealDeployFilter(true);
    deployFilter.focus();
  });

  // Type-to-search: a printable key while viewing deploys reveals the bar and
  // seeds it. Ignored while typing in any field, off the deploys view, or before
  // any row exists (nothing to filter).
  document.addEventListener('keydown', function (e) {
    if (e.defaultPrevented || e.metaKey || e.ctrlKey || e.altKey) return;
    const tag = (e.target && e.target.tagName) || '';
    if (
      tag === 'INPUT' ||
      tag === 'TEXTAREA' ||
      tag === 'SELECT' ||
      (e.target && e.target.isContentEditable)
    )
      return;
    if (activeView !== 'deploys' || !hasRows) return;
    if (e.key === 'Escape') {
      if (deployFilterWrap.classList.contains('revealed')) clearDeployFilter(true);
      return;
    }
    if (e.key.length === 1 && e.key !== ' ') {
      revealDeployFilter(true);
      deployFilter.focus();
      deployFilter.value += e.key; // focus mid-keydown doesn't reliably route the char
      applyDeployFilter();
      e.preventDefault();
    }
  });

  // ── Stacks view: click-a-row-for-history + search (mirrors the deploys filter) ──
  const rosterList = document.getElementById('roster-list');
  const rosterFilterWrap = document.getElementById('roster-filter-wrap');
  const rosterFilter = document.getElementById('roster-filter');
  const rosterFilterClear = document.getElementById('roster-filter-clear');
  const rosterFilterCount = document.getElementById('roster-filter-count');
  const rosterFilterEmpty = document.getElementById('roster-filter-empty');
  const rosterFilterEmptyQ = document.getElementById('roster-filter-empty-q');

  // Toggle a stack's deploy-history panel (ADR-0033), reusing the deploy table's
  // audit panel. One open at a time; the panel trails its row as a sibling, so
  // clicks inside it don't re-trigger the row.
  function closeRosterPanels() {
    rosterList
      .querySelectorAll('.audit-panel, .health-panel, .hooks-panel, .watched-panel')
      .forEach(function (p) {
        p.remove();
      });
    rosterList
      .querySelectorAll('.roster-row.audit-open, .roster-row.hooks-open')
      .forEach(function (r) {
        r.classList.remove('audit-open', 'hooks-open');
      });
  }

  // updateAppLinks patches each already-rendered row's link icon in place from
  // the current appLinksSnap, without a full renderRoster() rebuild — app_links
  // updates on the health-poll cadence, far more often than the 'stacks'
  // snapshot, and a rebuild at that rate would tear down and refetch an open
  // card every few seconds (see reopenRosterPanel) for a single icon.
  function updateAppLinks() {
    rosterList.querySelectorAll('.roster-row[data-stack]:not(.peer-row)').forEach(function (row) {
      const cell = row.querySelector('.roster-stack');
      if (!cell) return;
      const old = cell.querySelector('.link-wrap');
      if (old) old.remove();
      if (row.classList.contains('disabled')) return;
      const html = linkCell(row.dataset.stack);
      if (html) {
        // Back into the action cluster, ahead of the logs icon — the render
        // order. A plain append to the cell would drop the link *outside* the
        // cluster (it is rebuilt on every health poll, the cluster is not), so
        // a narrow row would strand it on its own line again.
        const logs = cell.querySelector('.clog-btn');
        if (logs) logs.insertAdjacentHTML('beforebegin', html);
        else (cell.querySelector('.row-actions') || cell).insertAdjacentHTML('beforeend', html);
      }
    });
  }

  // updateRosterHealth patches each local roster row's live-health pill and
  // Version cell in place from the health snapshot (peer rows refresh via
  // refreshPeerRosterRows), so a health change — or the first poll after the
  // roster rendered — shows on the Stacks overview without a full re-render,
  // which at poll cadence would tear down and refetch an open card (see
  // reopenRosterPanel). Mirrors updateStackAffordances' pill logic for the roster.
  function updateRosterHealth() {
    rosterList.querySelectorAll('.roster-row[data-stack]:not(.peer-row)').forEach(function (row) {
      const statusCell = row.querySelector('.roster-status');
      if (!statusCell || row.classList.contains('disabled')) return;
      const verCell = row.querySelector('.col-version');
      // The versions ride the health snapshot, so the cell is refreshed here
      // rather than only at render — on connect the roster arrives first.
      if (verCell)
        verCell.innerHTML = rosterVersionInnerHTML(
          row.dataset.stack,
          stackHealthFor(row.dataset.stack, row.dataset.host),
          stackUpdatesFor(row.dataset.stack, row.dataset.host),
        );
      const h = healthSnap[row.dataset.stack];
      // Keep the row's health marker (drives the severity bar + tint) in sync
      // in place — no re-sort, so a row never jumps out from under an open panel.
      if (h && h.status) row.dataset.health = h.status;
      else row.removeAttribute('data-health');
      let pill = statusCell.querySelector('.health-pill');
      if (h && h.status) {
        if (!pill) {
          // Keep the render order (badge → pill → strip → incident line): a
          // pill arriving with the first poll after a roster render must land
          // where a full render would have put it, not at the cell's end.
          const anchor = statusCell.querySelector('.outcome-strip, .last-incident');
          const html = healthPillHTML(row.dataset.stack, h.status);
          if (anchor) anchor.insertAdjacentHTML('beforebegin', html);
          else statusCell.insertAdjacentHTML('beforeend', html);
          return;
        }
        pill.dataset.health = h.status;
        pill.title = row.dataset.stack + ' — ' + h.status;
        pill.querySelector('.hlabel').textContent = h.status;
      } else if (pill) {
        pill.remove();
      }
    });
  }

  rosterList.addEventListener('keydown', hostChipKeydown); // T2.10 keyboard host chip
  rosterList.addEventListener('click', function (e) {
    if (e.target.closest('.sha-link')) return; // the commit link opens the forge; no panel toggle
    // ⋯ overflow menu: toggle open/closed. Picking an action closes the menu,
    // then falls through to that action's own handler below (the relocated
    // buttons still carry their .clog-btn/.hooks-badge classes). Mirrors Deploys.
    const moreBtn = e.target.closest('.more-btn');
    if (moreBtn) {
      toggleMoreMenu(moreBtn.closest('.row-more'));
      return;
    }
    if (e.target.closest('.more-item')) closeMoreMenu();

    // Host chip: quick-filter the merged view to this row's host (toggle). Before
    // the peer-row guard and panel logic so it works on every row's chip.
    const hostChip = e.target.closest('.host-mono');
    if (hostChip) {
      const r = hostChip.closest('[data-host]');
      if (r) toggleHostFilterTo(r.dataset.host);
      return;
    }

    // Cross-view jump button: hand off to Deploys. Handled before the row's
    // own click (which opens its history panel) so a tap never opens both.
    const jumpBtn = e.target.closest('.jump-btn');
    if (jumpBtn) {
      jumpToStack(jumpBtn.dataset.jumpView, jumpBtn.dataset.jumpStack);
      return;
    }
    // Hook-log icon: a .clog-btn, so match it before the container-logs handler.
    const hookLog = e.target.closest('.hook-log-btn');
    if (hookLog) {
      clog.openHookLog(hookLog, hookLog.dataset.hookLog);
      return;
    }
    // Deploy hooks (ADR-0038): toggle the bound hooks panel (configured commands),
    // like the Deploys hooks badge. On the roster the badge only acts from inside
    // the ⋯ menu; a click elsewhere on the row opens the history panel instead.
    const hbadge = e.target.closest('.hooks-badge');
    if (hbadge) {
      const hrow = hbadge.closest('.roster-row');
      if (hrow) {
        const hnext = hrow.nextElementSibling;
        if (hnext && hnext.classList.contains('hooks-panel')) {
          closeHooksPanel(hrow);
        } else {
          closeRosterPanels(); // one panel per row
          openRosterHooksPanel(hrow);
        }
      }
      return;
    }
    // Container-logs button (ADR-0037) — per stack on the row, per container on a
    // health-panel service line. Handled first so it never toggles the history panel.
    const clogB = e.target.closest('.clog-btn');
    if (clogB) {
      clog.toggle(clogB);
      return;
    }
    // App-link icon (dev-docs/traefik-app-links-spec.md): a single-host anchor
    // navigates on its own; a multi-host button toggles its popover. Either way
    // it must not also toggle the row's history panel.
    const linkPopA = e.target.closest('.link-pop a');
    if (linkPopA) {
      closeAppLinkPopover();
      return;
    }
    const linkTrigger = e.target.closest('.link-wrap > .link-btn');
    if (linkTrigger && linkTrigger.tagName === 'BUTTON') {
      toggleAppLinkPopover(linkTrigger.closest('.link-wrap'));
      return;
    }
    if (e.target.closest('.link-btn')) return;
    const row = e.target.closest('.roster-row');
    if (!row || !rosterList.contains(row)) return;
    if (row.classList.contains('peer-row')) {
      togglePeerDetail(row);
      return;
    } // read-only detail + link
    const next = row.nextElementSibling;
    const isOpen =
      next && (next.classList.contains('audit-panel') || next.classList.contains('health-panel'));
    closeRosterPanels();
    if (!isOpen) openRosterDetail(row);
  });

  // openRosterDetail opens a local row's bound panels as one card. Shared with
  // the re-open after a roster rebuild, so a restored panel is built exactly
  // like a clicked one.
  function openRosterDetail(row) {
    row.classList.add('audit-open');
    const stack = row.dataset.stack;
    // Containers (health, if known) above deploy history, bound as one card.
    let anchor = row;
    const h = healthSnap[stack];
    if (h && h.services && h.services.length) {
      const health = createHealthPanel(stack);
      anchor.after(health);
      anchor = health;
    }
    const audit = createAuditPanel(stack);
    anchor.after(audit);
    // Change detection last: it explains why the history above ends where it
    // does — which inputs are watched, and that none has changed since.
    audit.after(createWatchedPanel(stack));
    refilterRoster(); // freshly opened panels obey an active filter
  }

  // openRosterHooksPanel opens the configured-hooks panel (ADR-0038) on a row.
  function openRosterHooksPanel(row) {
    row.classList.add('hooks-open');
    row.after(createHooksPanel(row.dataset.stack));
    refilterRoster(); // a freshly opened panel obeys an active filter
  }

  function applyRosterFilter() {
    const q = (rosterFilter.value || '').trim().toLowerCase();
    rosterFilterWrap.classList.toggle('has-value', q.length > 0);
    let total = 0,
      shown = 0,
      visible = false;
    const kids = rosterList.children;
    for (let i = 0; i < kids.length; i++) {
      const el = kids[i];
      if (el.dataset && el.dataset.testid === 'roster-row') {
        total++;
        visible = el.dataset.stack.toLowerCase().indexOf(q) !== -1;
        if (visible) shown++;
      }
      // A trailing history panel shares its row's visibility.
      el.classList.toggle('filtered-out', !visible);
    }
    rosterFilterCount.textContent = q ? shown + '/' + total : '';
    rosterFilterEmpty.classList.toggle('show', q.length > 0 && total > 0 && shown === 0);
    rosterFilterEmptyQ.textContent = q;
  }
  // Re-run after the roster re-renders (a new snapshot) so new stacks obey it too.
  function refilterRoster() {
    if (rosterFilterWrap.classList.contains('has-value')) applyRosterFilter();
  }
  function revealRosterFilter(on) {
    rosterFilterWrap.classList.toggle('revealed', on);
    syncStackSearchBtn();
  }
  function clearRosterFilter(hide) {
    rosterFilter.value = '';
    applyRosterFilter();
    if (hide) {
      revealRosterFilter(false);
      rosterFilter.blur();
    }
  }
  rosterFilter.addEventListener('input', applyRosterFilter);
  rosterFilter.addEventListener('keydown', function (e) {
    if (e.key === 'Escape') {
      e.stopPropagation();
      if (rosterFilter.value)
        clearRosterFilter(false); // first Esc clears
      else {
        revealRosterFilter(false);
        rosterFilter.blur();
      } // second Esc folds away
    }
  });
  rosterFilter.addEventListener('blur', function () {
    if (!rosterFilter.value) revealRosterFilter(false);
  });
  rosterFilterClear.addEventListener('click', function () {
    clearRosterFilter(false);
    rosterFilter.focus();
  });

  // Mobile entry point: the "Search stacks" row in the stacks view-options popover.
  document.getElementById('roster-search-open').addEventListener('click', function () {
    setViewOptions(false);
    revealRosterFilter(true);
    rosterFilter.focus();
  });

  // Hooks into the Logs panel's own wiring, which is set up further down the
  // file. Declared here, ahead of their first use, so no earlier call can hit
  // them uninitialised; each is inert until then.
  let logSearchIsOpen = function () {
    return false;
  };
  let logSearchToggle = function () {};
  // Re-applies the in-log filter after a re-render, so fresh lines obey it.
  let logSearchApply = function () {};
  // Drops the Logs panel out of fullscreen. A view switch calls it because
  // fullscreen is a viewport-filling overlay: left on, it covers whichever view
  // the viewer moved to.
  let exitLogFullscreen = function () {};

  // What the trigger below searches depends on the view, so its label does too.
  const SEARCH_LABEL_STACKS = 'Search stacks';
  const SEARCH_LABEL_LOG = 'Search in log';

  // T3.11 — always-visible search trigger. The stack filter used to be
  // desktop-invisible (type-to-search only, an easter egg); this header magnifier
  // opens the same bar for the active view — on Logs too, where it opens the
  // in-log search: one magnifier, same place, whichever view is up. Hidden only
  // on mobile (the popover entry covers it there) via CSS. syncStackSearchBtn
  // reflects the bar's open state on the trigger.
  function syncStackSearchBtn() {
    const onLogs = activeView === 'logs';
    const open = onLogs
      ? logSearchIsOpen()
      : activeView === 'stacks'
        ? rosterFilterWrap.classList.contains('revealed')
        : deployFilterWrap.classList.contains('revealed');
    stackSearchBtn.classList.toggle('active', open);
    stackSearchBtn.setAttribute('aria-expanded', String(open));
    const label = onLogs ? SEARCH_LABEL_LOG : SEARCH_LABEL_STACKS;
    stackSearchBtn.title = label;
    stackSearchBtn.setAttribute('aria-label', label);
  }
  stackSearchBtn.addEventListener('click', function () {
    if (activeView === 'deploys') {
      if (deployFilterWrap.classList.contains('revealed')) clearDeployFilter(true);
      else {
        revealDeployFilter(true);
        deployFilter.focus();
      }
    } else if (activeView === 'stacks') {
      if (rosterFilterWrap.classList.contains('revealed')) clearRosterFilter(true);
      else {
        revealRosterFilter(true);
        rosterFilter.focus();
      }
    } else if (activeView === 'logs') {
      logSearchToggle();
    }
  });

  // Type-to-search on the stacks view, matching the deploys behaviour.
  document.addEventListener('keydown', function (e) {
    if (e.defaultPrevented || e.metaKey || e.ctrlKey || e.altKey) return;
    const tag = (e.target && e.target.tagName) || '';
    if (
      tag === 'INPUT' ||
      tag === 'TEXTAREA' ||
      tag === 'SELECT' ||
      (e.target && e.target.isContentEditable)
    )
      return;
    if (activeView !== 'stacks' || !rosterSnap.length) return;
    if (e.key === 'Escape') {
      if (rosterFilterWrap.classList.contains('revealed')) clearRosterFilter(true);
      return;
    }
    if (e.key.length === 1 && e.key !== ' ') {
      revealRosterFilter(true);
      rosterFilter.focus();
      rosterFilter.value += e.key;
      applyRosterFilter();
      e.preventDefault();
    }
  });

  // Live update of a single roster row's status (in-flight → settled) without a
  // full re-render, so an open history panel survives a deploy event.
  function refreshRosterRow(name) {
    const row = rosterList.querySelector(
      `.roster-row[data-stack="${window.CSS && CSS.escape ? CSS.escape(name) : name}"]:not(.peer-row)`,
    );
    if (!row) return;
    const entry = rosterSnap.find(function (x) {
      return x.name === name;
    });
    const cell = row.querySelector('.roster-status');
    if (entry && cell) cell.innerHTML = rosterStatusHTML(entry, !!deployingRows[name]);
    applyHookRun(); // rosterStatusHTML replaces the cell — re-paint a running hook's phase
  }

  // ── Theme toggle ──

  // Colour-scheme toggle (dark is the default; light is opt-in per browser).
  // Which palette (Catppuccin/Nord/Solarized/Gruvbox/Rosé Pine) is configured
  // server-side via ui_theme and baked into <html data-theme> — this toggle
  // only ever flips dark/light within that palette.
  const themeToggle = document.getElementById('theme-toggle');
  let lightTheme = localStorage.getItem('colorScheme') === 'light';

  function applyTheme() {
    document.documentElement.classList.toggle('light', lightTheme);
    themeToggle.classList.toggle('active', lightTheme);
    themeToggle.title = lightTheme ? 'Switch to dark theme' : 'Switch to light theme';
  }

  themeToggle.addEventListener('click', function () {
    lightTheme = !lightTheme;
    localStorage.setItem('colorScheme', lightTheme ? 'light' : 'dark');
    applyTheme();
  });

  applyTheme();

  // Theme picker — this browser only, and opt-in. It is wired up only when the
  // deployment enables the switcher (data-theme-switcher="on", baked in from
  // ui_theme_switcher); otherwise the picker is hidden, no override is applied,
  // and the block below never runs. Choosing a theme here never changes the
  // environment's configured ui_theme (data-server-theme, baked in at serve
  // time); it only persists a local override applied instantly (no reload:
  // every theme's CSS is always present, switching data-theme is enough).
  // Picking the server's own theme clears the override, so the page goes back
  // to following whatever the environment is configured for.
  if (document.documentElement.getAttribute('data-theme-switcher') === 'on') {
    const THEME_LABELS = {
      catppuccin: 'Catppuccin',
      nord: 'Nord',
      solarized: 'Solarized',
      gruvbox: 'Gruvbox',
      'rose-pine': 'Rosé Pine',
      flake: 'Flake',
    };
    const serverTheme = document.documentElement.getAttribute('data-server-theme');
    const themeSelect = document.getElementById('theme-select');
    const themeNotice = document.getElementById('theme-notice');
    const themeNoticeText = document.getElementById('theme-notice-text');
    let themeNoticeTimer = null;

    themeSelect.value = document.documentElement.getAttribute('data-theme');

    function updateThemeNotice() {
      clearTimeout(themeNoticeTimer);
      const override = localStorage.getItem('themeOverride');
      if (!override || override === serverTheme) {
        themeNotice.hidden = true;
        return;
      }
      themeNoticeText.textContent =
        'Showing ' +
        (THEME_LABELS[override] || override) +
        ' in this browser — this environment is configured for ' +
        (THEME_LABELS[serverTheme] || serverTheme) +
        '.';
      themeNotice.hidden = false;
      themeNoticeTimer = setTimeout(function () {
        themeNotice.hidden = true;
      }, 6000);
    }

    themeSelect.addEventListener('change', function () {
      const chosen = themeSelect.value;
      document.documentElement.setAttribute('data-theme', chosen);
      if (chosen === serverTheme) {
        localStorage.removeItem('themeOverride');
      } else {
        localStorage.setItem('themeOverride', chosen);
      }
      updateThemeNotice();
    });

    document.getElementById('theme-notice-close').addEventListener('click', function () {
      clearTimeout(themeNoticeTimer);
      themeNotice.hidden = true;
    });

    updateThemeNotice(); // show on load if a saved override already differs from the server theme
  }

  // ── First-run header tour (T3.15) ──
  // The header is glyph-only by design; on a fresh browser only, captions under
  // each control plus the banner teach the mapping. "Got it" (or Esc) marks it
  // seen — the class hides the captions/banner and persists so it never returns.
  // Purely localStorage-gated (no timers), so the shown/dismissed states are
  // deterministic. Returning browsers get .header-tour-seen pre-paint (see head).
  (function () {
    const root = document.documentElement;
    const dismiss = document.getElementById('header-tour-dismiss');
    if (!dismiss || root.classList.contains('header-tour-seen')) return;
    function endTour() {
      if (root.classList.contains('header-tour-seen')) return;
      root.classList.add('header-tour-seen');
      try {
        localStorage.setItem('headerTourSeen', '1');
      } catch (e) {
        // Private-mode storage can throw; the class already hid the tour for
        // this session, so log and carry on rather than leaving it half-applied.
        uiNote('warn', 'could not persist headerTourSeen', e);
      }
      // Keyboard users lose the vanished button; land them back in the header.
      const nav = document.querySelector('.view-toggle button.active');
      if (nav) nav.focus();
    }
    dismiss.addEventListener('click', endTour);
    document.addEventListener('keydown', function (e) {
      if (e.key === 'Escape') endTour();
    });
  })();

  // ── Autosync controls ──
  // State comes from the 'autosync' and 'queue' SSE events (initial snapshots
  // are sent on connect). The header control shows global state + pending count
  // and opens the drawer; the drawer holds the global switch, the ordered queue,
  // and a filterable per-stack switch list. Toggles POST to /api/autosync; the
  // server pushes back an 'autosync' event so other tabs stay in sync.
  const asBtn = document.getElementById('autosync-btn');
  const asCountEl = document.getElementById('autosync-count');
  const asDrawer = document.getElementById('autosync-drawer');
  const asSub = document.getElementById('autosync-sub');
  const asGlobalSw = document.getElementById('autosync-global-sw');
  const asGlobalNote = document.getElementById('autosync-global-note');
  const asQueueList = document.getElementById('autosync-queue-list');
  const asStackList = document.getElementById('autosync-stack-list');
  const asQCount = document.getElementById('autosync-qcount');
  const asFilter = document.getElementById('autosync-filter');
  const asFilterWrap = document.getElementById('autosync-filter-wrap');
  const asFilterClear = document.getElementById('autosync-filter-clear');

  function autosyncStacks() {
    return (autosyncSnap && autosyncSnap.stacks) || [];
  }

  function stackByName(name) {
    return autosyncStacks().filter(function (s) {
      return s.name === name;
    })[0];
  }

  function anyPaused() {
    if (!autosyncSnap) return false;
    if (!autosyncSnap.global) return true;
    return autosyncStacks().some(function (s) {
      return !s.effective;
    });
  }

  // autosyncPost writes a toggle and applies the snapshot it answers with.
  //
  // A failure here is invisible in the interface: the switch simply does not
  // move, which is indistinguishable from a click that never landed. It is
  // therefore announced on the console — the same treatment a dropped stale
  // snapshot gets — so an operator (and a failing test) can tell "the write was
  // refused" from "the control is dead". The switch is deliberately left
  // showing server state rather than the attempted one: the write did not
  // happen, so showing it as though it had would be the worse lie.
  function autosyncPost(scope, stack, enabled) {
    const what = scope === 'stack' ? scope + ' ' + stack : scope;
    // Recorded before the request, not after: it marks that a click actually
    // reached this handler. An empty note buffer on a failed toggle then means
    // the click never got here at all, which no later message could tell apart
    // from a request that vanished (T8).
    uiNote('debug', 'autosync: toggling', what, '->', enabled);
    fetch('/api/autosync', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ scope: scope, stack: stack, enabled: enabled }),
    })
      .then(function (r) {
        if (!r.ok) {
          uiNote('warn', 'autosync: toggle refused for', what, '— HTTP', r.status);
          return null;
        }
        return r.json();
      })
      .then(function (snap) {
        if (snap) applyAutosyncSnapshot(snap);
      })
      .catch(function (err) {
        uiNote('warn', 'autosync: toggle for', what, 'did not reach the server —', err);
      });
  }

  // applyAutosyncSnapshot installs a snapshot unless it is older than the one
  // already applied — the POST response and the SSE broadcast of the same change
  // can overtake each other, which would make a switch snap back
  // (dev-docs/autosync-spec.md, "Snapshot ordering").
  function applyAutosyncSnapshot(snap) {
    if (!snapshotIsFresh(autosyncVersion, snap)) {
      // A drop is invisible by design, so leave a trace of it.
      uiNote('debug', 'autosync: dropped a stale snapshot', snap.version, '<', autosyncVersion);
      return;
    }
    autosyncSnap = snap;
    if (typeof snap.version === 'number') autosyncVersion = snap.version;
    renderAutosync();
  }

  // patchStackRow updates an existing row's cells without touching the row or
  // switch nodes themselves — see renderRowList for why that matters.
  function patchStackRow(row, s, pos) {
    const item = queueByStack[s.name];
    const qpos = row.querySelector('.qpos');
    qpos.className = pos !== null ? 'qpos' : 'qpos blank';
    qpos.textContent = autosyncPosText(pos, item);
    row.querySelector('.stack-name').innerHTML =
      escapeHtml(s.name) + autosyncReasonChipHTML(s, item);
    row.querySelector('.stack-detail').innerHTML = autosyncDetailHTML(s, item, Date.now());
    const sw = row.querySelector('.sw');
    sw.classList.toggle('on', s.effective);
    sw.setAttribute('aria-checked', String(s.effective));
    sw.title = autosyncSwitchTitle(s);
  }

  // renderRowList paints a stack list, patching in place whenever the rows and
  // their order are unchanged.
  //
  // Rebuilding wholesale would be simpler, but a snapshot can land in the middle
  // of a click: if the switch node is replaced between mousedown and mouseup the
  // browser fires the `click` on their common ancestor instead, the delegated
  // handler finds no `.sw` under it, and the toggle is silently lost. Every SSE
  // tick used to swap the whole list, so an autosync/queue event arriving at the
  // wrong moment ate the click (and any keyboard focus with it).
  function renderRowList(el, rows) {
    const key = rows
      .map(function (r) {
        return r.stack.name;
      })
      .join('\n');
    if (el.dataset.rowKey === key && el.children.length === rows.length) {
      rows.forEach(function (r, i) {
        patchStackRow(el.children[i], r.stack, r.pos);
      });
      return;
    }
    el.dataset.rowKey = key;
    const now = Date.now();
    el.innerHTML = rows
      .map(function (r) {
        return autosyncRowHTML(r.stack, r.pos, queueByStack[r.stack.name], now);
      })
      .join('');
  }

  function renderAutosyncBtn() {
    const count = queueSnap.count || 0;
    asBtn.classList.toggle('has-pending', count > 0);
    asBtn.classList.toggle('paused', anyPaused());
    if (autosyncSnap) asBtn.dataset.global = String(autosyncSnap.global);
    asCountEl.textContent = count;
    asBtn.title =
      count > 0
        ? `${count} stack${count === 1 ? '' : 's'} queued — autosync paused`
        : 'Autosync — all in sync';
  }

  function renderSub() {
    const paused = autosyncStacks().filter(function (s) {
      return !s.effective;
    }).length;
    const q = queueSnap.count || 0;
    if (q === 0 && paused === 0) asSub.textContent = 'All stacks in sync.';
    else if (q === 0)
      asSub.textContent = `${paused} stack${paused === 1 ? '' : 's'} paused · nothing waiting.`;
    else
      asSub.innerHTML = `<b>${q}</b> update${q === 1 ? '' : 's'} waiting · ${paused} stack${paused === 1 ? '' : 's'} paused.`;
  }

  // renderGlobal paints the global switch — and, the first time a snapshot
  // arrives, arms it. Before that the drawer has no server state: the switch
  // would show the markup's optimistic "on" and a click could not compute the
  // opposite value, so it posted nothing and the control read as broken. While
  // `data-ready` is "false" it is inert (aria-disabled, and pointer-events off
  // in CSS, so a click waits for the state instead of falling into the void).
  function renderGlobal() {
    if (!autosyncSnap) return;
    asDrawer.dataset.ready = 'true';
    asGlobalSw.removeAttribute('aria-disabled');
    asGlobalSw.classList.toggle('on', autosyncSnap.global);
    asGlobalSw.setAttribute('aria-checked', String(autosyncSnap.global));
    asGlobalNote.textContent = autosyncSnap.global
      ? 'on · deploys apply automatically'
      : 'off · stacks pause unless individually enabled';
  }

  function renderQueueList() {
    const pending = queueSnap.pending || [];
    asQCount.textContent = pending.length ? `(${pending.length})` : '';
    if (pending.length === 0) {
      asQueueList.dataset.rowKey = '';
      asQueueList.innerHTML =
        '<div class="qempty">Nothing queued — resumed deploys apply immediately.</div>';
      return;
    }
    renderRowList(
      asQueueList,
      pending.map(function (it) {
        const s = stackByName(it.stack) || {
          name: it.stack,
          effective: false,
          overridden: false,
          config: null,
        };
        return { stack: s, pos: it.position };
      }),
    );
  }

  function renderStackList() {
    const query = (asFilter.value || '').trim().toLowerCase();
    asFilterWrap.classList.toggle('has-value', query.length > 0);
    const stacks = autosyncStacks();
    const list = stacks.filter(function (s) {
      return s.name.toLowerCase().indexOf(query) !== -1;
    });
    if (list.length === 0) {
      asStackList.dataset.rowKey = '';
      asStackList.innerHTML =
        `<div class="qempty">` +
        (stacks.length ? `No stack matches “${escapeHtml(query)}”.` : 'No stacks.') +
        `</div>`;
      return;
    }
    renderRowList(
      asStackList,
      list.map(function (s) {
        return { stack: s, pos: null };
      }),
    );
  }

  function renderAutosync() {
    renderAutosyncBtn();
    renderGlobal();
    renderQueueList();
    renderStackList();
    renderSub();
  }

  function setDrawer(open) {
    if (open) {
      setViewOptions(false);
      setRunDrawer(false);
    } // surfaces are mutually exclusive
    asDrawer.classList.toggle('open', open);
    asBtn.classList.toggle('open', open);
    asBtn.setAttribute('aria-expanded', String(open));
    // While the max-height transition runs, geometry inside the drawer is in
    // flux: content taller than the current clip shows a transient scrollbar,
    // which re-wraps text and moves the right-aligned switches. data-settled
    // marks the end of that window so a test (or any caller) can wait for the
    // drawer to stop moving instead of clicking into the transition (T8).
    asDrawer.dataset.settled = 'false';
    // Always open at the top: the drawer is taller than it is tall enough to
    // show, so a leftover scroll offset from the previous visit would hide the
    // global switch — the row the whole surface is about.
    if (open) asDrawer.scrollTop = 0;
    manageSurfaceFocus(asDrawer, asBtn, open);
  }
  asDrawer.addEventListener('transitionend', function (e) {
    if (e.target === asDrawer && e.propertyName === 'max-height') {
      asDrawer.dataset.settled = String(asDrawer.classList.contains('open'));
    }
  });
  trapFocus(asDrawer);

  asBtn.addEventListener('click', function () {
    setDrawer(!asDrawer.classList.contains('open'));
  });
  asBtn.addEventListener('keydown', function (e) {
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault();
      asBtn.click();
    }
  });

  asGlobalSw.addEventListener('click', function () {
    if (autosyncSnap) autosyncPost('global', '', !autosyncSnap.global);
  });
  asGlobalSw.addEventListener('keydown', function (e) {
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault();
      asGlobalSw.click();
    }
  });

  // missGeometry compacts where a stray click landed relative to the switch it
  // should have hit: the pointer position, the switch's current box, and the
  // font-load state — a swap of a `font-display: swap` face reflows the drawer,
  // so `loading` at click time marks the layout as mid-shift. All synchronous
  // reads inside a handler that already ran, so recording them cannot perturb
  // the race being measured (T8).
  function missGeometry(e, sw) {
    let s = '[';
    if (typeof e.clientX === 'number') s += 'at ' + e.clientX + ',' + e.clientY + ' ';
    if (sw) {
      const r = sw.getBoundingClientRect();
      s += 'sw ' + Math.round(r.left) + ',' + Math.round(r.top);
      s += ' ' + Math.round(r.width) + 'x' + Math.round(r.height) + ' ';
    }
    return s + 'fonts ' + (document.fonts ? document.fonts.status : 'n/a') + ']';
  }

  // Per-stack switches are event-delegated so re-rendering the lists is cheap.
  function toggleFromEvent(e) {
    const target = e.target;
    const sw = target.closest ? target.closest('.sw[data-stack]') : null;
    if (!sw) {
      // Only interesting when the click landed *inside a stack row* yet missed
      // that row's switch — a click aimed at the control that did not reach it —
      // or anywhere in the drawer while fonts are still loading, when a swap can
      // shift the layout under a click already in flight. The rest of the drawer
      // (filter, background, queue lines) is legitimately non-switch surface,
      // and noting those would evict the diagnostics this buffer exists for.
      // See T8.
      const row = target.closest ? target.closest('.stack-row') : null;
      if (row && row.querySelector('.sw[data-stack]')) {
        uiNote(
          'debug',
          'autosync: click inside row',
          row.getAttribute('data-stack'),
          'missed its switch — hit',
          String(target.className || target.nodeName),
          missGeometry(e, row.querySelector('.sw[data-stack]')),
        );
      } else if (document.fonts && document.fonts.status === 'loading') {
        uiNote(
          'debug',
          'autosync: stray click while fonts were loading — hit',
          String(target.className || target.nodeName),
          missGeometry(e, asStackList.querySelector('.sw[data-stack]')),
        );
      }
      return false;
    }
    autosyncPost('stack', sw.getAttribute('data-stack'), !sw.classList.contains('on'));
    return true;
  }
  asDrawer.addEventListener('click', function (e) {
    toggleFromEvent(e);
  });
  asDrawer.addEventListener('keydown', function (e) {
    if (e.key === 'Enter' || e.key === ' ') {
      if (toggleFromEvent(e)) e.preventDefault();
    }
  });

  asFilter.addEventListener('input', renderStackList);
  asFilter.addEventListener('keydown', function (e) {
    if (e.key === 'Escape' && asFilter.value) {
      e.stopPropagation();
      asFilter.value = '';
      renderStackList();
    }
  });
  asFilterClear.addEventListener('click', function () {
    asFilter.value = '';
    renderStackList();
    asFilter.focus();
  });

  registerSurface({
    isOpen: function () {
      return asDrawer.classList.contains('open');
    },
    close: function () {
      setDrawer(false);
    },
    within: [asDrawer, asBtn],
  });

  renderAutosync(); // initial paint before the first SSE snapshot arrives

  // ── Hosts drawer (multi-host fan-in) ──
  const hostsBtn = document.getElementById('hosts-btn');
  const hostsDrawer = document.getElementById('hosts-drawer');

  function setHostsDrawer(open) {
    if (open) {
      setViewOptions(false);
      setRunDrawer(false);
      setDrawer(false);
    } // surfaces are mutually exclusive
    hostsDrawer.classList.toggle('open', open);
    hostsBtn.classList.toggle('open', open);
    hostsBtn.setAttribute('aria-expanded', String(open));
    manageSurfaceFocus(hostsDrawer, hostsBtn, open);
  }
  trapFocus(hostsDrawer);

  hostsBtn.addEventListener('click', function () {
    setHostsDrawer(!hostsDrawer.classList.contains('open'));
  });

  // Toggling a host: the filter is the set of in-view hosts. Deselecting the last
  // host is a no-op (an empty merged feed is never useful) — one host stays on.
  function toggleHost(name) {
    const hosts = hostList().map(function (h) {
      return h.name;
    });
    if (hostSelected === null) hostSelected = new Set(hosts); // materialize "all" before narrowing
    if (hostSelected.has(name)) {
      if (hostSelected.size <= 1) return; // keep at least one host in view
      hostSelected.delete(name);
    } else {
      hostSelected.add(name);
    }
    if (hostSelected.size === hosts.length) hostSelected = null; // back to "all"
    renderPeerRows(); // membership unchanged, but keeps rows consistent after refilter
    applyHostFilter();
    refilterDeploys();
  }

  hostsDrawer.addEventListener('click', function (e) {
    if (e.target.closest('.host-link')) return; // the external link opens normally
    const row = e.target.closest('.host-row');
    if (row) toggleHost(row.dataset.host);
  });
  // Keyboard-operate the host-row checkboxes (T2.10). toggleHost rebuilds the
  // list, so re-focus the same host's fresh row afterwards to keep the caret put.
  hostsDrawer.addEventListener('keydown', function (e) {
    if (e.key !== 'Enter' && e.key !== ' ') return;
    if (e.target.closest('.host-link')) return;
    const row = e.target.closest('.host-row');
    if (!row) return;
    e.preventDefault();
    const host = row.dataset.host;
    toggleHost(host);
    const again = hostsDrawer.querySelector(
      '.host-row[data-host="' + (window.CSS && CSS.escape ? CSS.escape(host) : host) + '"]',
    );
    if (again) again.focus();
  });

  document.getElementById('hosts-all-btn').addEventListener('click', function () {
    hostSelected = null;
    applyHostFilter();
    refilterDeploys();
  });

  registerSurface({
    isOpen: function () {
      return hostsDrawer.classList.contains('open');
    },
    close: function () {
      setHostsDrawer(false);
    },
    within: [hostsDrawer, hostsBtn],
  });

  // ── Logs view ──
  // The log stream connects lazily on first activation and stays open;
  // the connection indicator remains bound to /api/events.
  const logPane = document.getElementById('log-pane');
  const followBtn = document.getElementById('follow-logs');
  const viewButtons = document.querySelectorAll('#view-toggle button');

  // The log view keeps a bounded client-side buffer (the backend replays at
  // most its own ring on connect) and renders a sliding window of it. The
  // order is chronological — newest line at the bottom, like `journalctl -f`
  // and like the container-log panel this view borrows its chrome from, so
  // "auto-scroll" points at the edge the tail is actually on. The window
  // starts at logPageSize lines and grows by logPageSize each time the user
  // scrolls to the older (top) edge.
  const logPageSize = 500;
  const maxLogBuffer = 2000;
  // Outcome lines kept past buffer eviction (mirrors internal/logbuf's pinned
  // set), so a busy run's child output can never push the line that says how a
  // deploy ended out of the pane before it was read.
  const maxPinnedLog = 50;
  const logEntries = []; // chronological, oldest → newest
  const pinnedLogEntries = []; // narrated outcome lines, exempt from eviction

  // The render window reads this merged view — evicted outcome lines back in
  // chronological position ahead of the ring — never logEntries directly.
  function logView() {
    return mergeLogView(pinnedLogEntries, logEntries);
  }
  let logVisible = logPageSize; // number of newest entries currently rendered
  let followLogs = localStorage.getItem('followLogs') !== 'false';
  const savedView = localStorage.getItem('activeView');
  let activeView = savedView === 'logs' || savedView === 'stacks' ? savedView : 'deploys';
  let logSource = null;

  // ── Logs-view quick filters ──
  // Severity threshold + kind set + stack set, persisted per browser so a
  // narrowed view survives a reload (and a PWA relaunch). Kept in one object
  // so persisting is one write and the pure predicate takes one argument.
  const LOG_FILTERS_KEY = 'logQuickFilters';
  let logFilters = parseLogFilters(localStorage.getItem(LOG_FILTERS_KEY));
  // Lines that arrived while auto-scroll was off, so the "N new lines" pill
  // can offer to jump without moving the viewport under the reader.
  let pendingLogLines = 0;

  function renderLogLine(entry) {
    const line = document.createElement('div');
    line.className = 'log-line';
    line.dataset.testid = 'log-line';
    line.dataset.level = logLineLevel(entry);
    // The quick filters test these three facets; stashing them here keeps a
    // re-filter a DOM walk instead of a second pass over the buffer.
    const facets = logFacets(entry);
    line.dataset.sev = facets.level;
    line.dataset.kind = facets.kind;
    if (facets.stack) line.dataset.stack = facets.stack;
    if (!logMatchesFilters(facets, logFilters)) line.classList.add('log-out');
    line.innerHTML = logLineHTML(entry);
    // A changed file carries its diff on the record (the console prints it the
    // same way); render it under the line so the two surfaces read alike. The
    // capture layer has already clamped it, so the block is bounded.
    const attrs = entry.attrs || {};
    if (attrs.diff) line.insertAdjacentHTML('beforeend', logDiffBlockHTML(attrs.diff));
    return line;
  }

  // handleDiffToggle handles the two collapse/expand controls every diff panel
  // carries — the per-file header and the multi-commit "N commits" pill —
  // wherever the panel is rendered (deploy table or log view). Returns true when
  // it consumed the click so the caller stops. Shared by both click handlers.
  function handleDiffToggle(target) {
    const fileHeader = target.closest('.diff-file-header');
    if (fileHeader) {
      fileHeader.classList.toggle('expanded');
      return true;
    }
    const cpill = target.closest('.commits-pill');
    if (cpill) {
      const chead = cpill.closest('.diff-head');
      const clist = chead && chead.querySelector('.diff-commit-list');
      if (clist) clist.classList.toggle('expanded');
      return true;
    }
    return false;
  }

  // Diff pill in the log view — same fetch/render/toggle behaviour as the
  // files pill in the deploy table, but anchored below the log line.
  logPane.addEventListener('click', function (e) {
    if (handleDiffToggle(e.target)) return;

    const pill = e.target.closest('.log-diff-pill');
    if (!pill) return;
    const line = pill.closest('.log-line');
    const existing = line.nextElementSibling;
    if (
      existing &&
      (existing.classList.contains('diff-panel') ||
        existing.classList.contains('files-list') ||
        existing.classList.contains('load-error'))
    ) {
      existing.remove();
      return;
    }
    function openLogDiff() {
      fetchDiffs(pill.dataset.eventId, function (diffs, commits, err) {
        if (err) {
          // Fetch failed — surface it instead of the genuine "No diff recorded"
          // line, with a retry that re-runs just this fetch (T4.16).
          line.after(
            createLoadError("Couldn't load the diff.", function (le) {
              le.remove();
              openLogDiff();
            }),
          );
        } else if (diffs && Object.keys(diffs).length > 0) {
          line.after(renderDiffPanel(diffs, commits, null));
        } else {
          const note = document.createElement('div');
          note.className = 'files-list';
          note.textContent = 'No diff recorded for this deploy.';
          line.after(note);
        }
      });
    }
    openLogDiff();
  });

  // The newest line lives at the bottom; "follow" and fresh renders pin the
  // view to that edge.
  function scrollToNewest() {
    logPane.scrollTop = logPane.scrollHeight;
    clearPendingLines();
  }

  // ── Quick filters ──
  const logQuickBar = document.getElementById('log-quick-bar');
  const logQuickCount = document.getElementById('log-quick-count');
  const logStackChips = document.getElementById('log-stack-chips');
  const logStackSep = document.getElementById('log-stack-sep');
  const logNewPill = document.getElementById('log-newpill');

  function persistLogFilters() {
    try {
      localStorage.setItem(LOG_FILTERS_KEY, JSON.stringify(logFilters));
    } catch (_) {
      // Private mode / quota: the filter still applies for this session.
    }
  }

  // Hide or reveal every rendered line for the current filters, then refresh
  // the count. `.log-out` is the quick filters' own hide class, separate from
  // the in-log search's — the two narrow independently and a line must stay
  // hidden while either one excludes it.
  function applyLogQuickFilters() {
    logPane.querySelectorAll('.log-line').forEach(function (line) {
      const facets = {
        level: line.dataset.sev,
        kind: line.dataset.kind,
        stack: line.dataset.stack || '',
      };
      line.classList.toggle('log-out', !logMatchesFilters(facets, logFilters));
    });
    updateLogQuickCount();
  }

  function updateLogQuickCount() {
    const lines = logPane.querySelectorAll('.log-line');
    if (!logFiltersActive(logFilters)) {
      logQuickCount.textContent = '';
      return;
    }
    const shown = logPane.querySelectorAll('.log-line:not(.log-out)').length;
    logQuickCount.textContent = shown + ' of ' + lines.length;
  }

  function renderLogQuickChips() {
    logQuickBar.querySelectorAll('[data-sev]').forEach(function (b) {
      const on = b.dataset.sev === logFilters.sev;
      b.classList.toggle('active', on);
      b.setAttribute('aria-pressed', String(on));
    });
    logQuickBar.querySelectorAll('[data-kind]').forEach(function (b) {
      const on = logFilters.kinds.indexOf(b.dataset.kind) !== -1;
      b.classList.toggle('active', on);
      b.setAttribute('aria-pressed', String(on));
    });
    logStackChips.textContent = '';
    logFilters.stacks.forEach(function (name) {
      const chip = document.createElement('button');
      chip.type = 'button';
      chip.className = 'clog-chip active stack-chip';
      chip.dataset.testid = 'log-stack-chip';
      chip.dataset.stack = name;
      chip.title = 'Remove this stack filter';
      chip.append(name);
      const x = document.createElement('span');
      x.className = 'chip-x';
      x.setAttribute('aria-hidden', 'true');
      x.textContent = '×';
      chip.appendChild(x);
      logStackChips.appendChild(chip);
    });
    logStackSep.hidden = logFilters.stacks.length === 0;
  }

  // One entry point for every filter change: persist, repaint the chips,
  // re-filter what is rendered. Following the tail keeps following it — the
  // pane's position should not depend on which filter was last touched.
  function setLogFilters(next) {
    logFilters = next;
    persistLogFilters();
    renderLogQuickChips();
    applyLogQuickFilters();
    if (followLogs) scrollToNewest();
  }

  function toggleLogStackFilter(name) {
    const stacks = logFilters.stacks.slice();
    const i = stacks.indexOf(name);
    if (i === -1) stacks.push(name);
    else stacks.splice(i, 1);
    setLogFilters(Object.assign({}, logFilters, { stacks: stacks }));
  }

  logQuickBar.addEventListener('click', function (e) {
    const chip = e.target.closest('.clog-chip');
    if (!chip) return;
    if (chip.dataset.sev) {
      setLogFilters(Object.assign({}, logFilters, { sev: chip.dataset.sev }));
      return;
    }
    if (chip.dataset.kind) {
      const kinds = logFilters.kinds.slice();
      const i = kinds.indexOf(chip.dataset.kind);
      if (i === -1) kinds.push(chip.dataset.kind);
      else kinds.splice(i, 1);
      setLogFilters(Object.assign({}, logFilters, { kinds: kinds }));
      return;
    }
    if (chip.dataset.stack) toggleLogStackFilter(chip.dataset.stack);
  });

  // A stack prefix in a line is the fastest way to narrow to that stack.
  logPane.addEventListener('click', function (e) {
    const prefix = e.target.closest('.log-stack');
    if (!prefix) return;
    e.stopPropagation(); // never also toggle the line's diff panel
    toggleLogStackFilter(prefix.dataset.stack);
  });
  logPane.addEventListener('keydown', function (e) {
    if (e.key !== 'Enter' && e.key !== ' ') return;
    const prefix = e.target.closest('.log-stack');
    if (!prefix) return;
    e.preventDefault();
    toggleLogStackFilter(prefix.dataset.stack);
  });

  // ── "N new lines" pill ──
  function countPendingLine() {
    pendingLogLines++;
    logNewPill.textContent =
      '↓ ' + pendingLogLines + (pendingLogLines === 1 ? ' new line' : ' new lines');
    logNewPill.hidden = false;
  }

  function clearPendingLines() {
    pendingLogLines = 0;
    logNewPill.hidden = true;
  }

  logNewPill.addEventListener('click', function () {
    setFollowLogs(true);
  });

  // setFollowLogs is the one place follow changes: it persists the choice and
  // repaints the control. keepScroll is for the scroll handler itself — it
  // already knows where the pane is, and jumping there again from inside a
  // scroll event would fight the user's own scrolling.
  function setFollowLogs(on, opts) {
    if (followLogs === on) return;
    followLogs = on;
    localStorage.setItem('followLogs', String(followLogs));
    if (opts && opts.keepScroll) {
      followBtn.classList.toggle('on', followLogs);
      followBtn.setAttribute('aria-pressed', String(followLogs));
      clearPendingLines(); // reaching the tail by scrolling settles the pill too
      return;
    }
    applyFollow();
  }

  // Remove a rendered line together with any diff/files panel opened below it,
  // so trimming the window never leaves an orphaned panel behind.
  function removeLogLine(line) {
    const next = line.nextElementSibling;
    if (next && (next.classList.contains('diff-panel') || next.classList.contains('files-list'))) {
      next.remove();
    }
    line.remove();
  }

  // Keep at most logVisible rendered lines, dropping from the older (top)
  // edge. The dropped entries stay in the buffer, so scrolling back can
  // reveal them again.
  function trimLogDom() {
    const lines = logPane.querySelectorAll('.log-line');
    const over = lines.length - logVisible;
    for (let i = 0; i < over; i++) {
      removeLogLine(lines[i]);
    }
  }

  // Rebuild the pane from the buffer for the current order and window size.
  // Used on view activation, sort toggle and other non-incremental changes.
  function renderLogWindow() {
    logPane.innerHTML = '';
    const view = logView();
    if (view.length === 0) {
      const empty = document.createElement('div');
      empty.className = 'log-empty';
      empty.id = 'log-empty';
      empty.textContent = 'Awaiting log output...';
      logPane.appendChild(empty);
      return;
    }
    const count = Math.min(logVisible, view.length);
    const slice = view.slice(view.length - count); // oldest → newest
    const frag = document.createDocumentFragment();
    slice.forEach(function (e) {
      frag.appendChild(renderLogLine(e));
    });
    logPane.appendChild(frag);
    applyLogQuickFilters();
    scrollToNewest();
    logSearchApply();
  }

  // Reveal another page of older entries at the older (top) edge, preserving
  // the reading position. No-op once the whole buffer is rendered.
  function loadMoreLogs() {
    const view = logView();
    const have = Math.min(logVisible, view.length);
    const target = Math.min(logVisible + logPageSize, view.length);
    if (target <= have) return;
    logVisible = target;
    const older = view.slice(view.length - target, view.length - have);
    const frag = document.createDocumentFragment();
    older.forEach(function (e) {
      frag.appendChild(renderLogLine(e));
    }); // oldest → newest
    const prevHeight = logPane.scrollHeight,
      prevTop = logPane.scrollTop;
    logPane.insertBefore(frag, logPane.firstChild);
    logPane.scrollTop = prevTop + (logPane.scrollHeight - prevHeight);
    applyLogQuickFilters();
  }

  // A single fresh (newest) line arrived over the stream.
  function appendNewestToDom(entry) {
    const empty = logPane.querySelector('.log-empty');
    if (empty) empty.remove();
    const node = renderLogLine(entry);
    logPane.appendChild(node);
    trimLogDom();
    logSearchApply(); // a fresh line obeys an active in-log filter
    if (followLogs) {
      scrollToNewest();
    } else if (!node.classList.contains('log-out')) {
      // Reading history: don't move the viewport, but say what landed. Only
      // lines that survive the quick filters count — offering to jump to
      // arrivals the pane would not show is worse than staying quiet.
      countPendingLine();
    }
    updateLogQuickCount();
  }

  function pushLog(entry) {
    logEntries.push(entry);
    const overflow = logEntries.length - maxLogBuffer;
    if (overflow > 0) logEntries.splice(0, overflow);
    // A reconnect can replay lines already held; the ID guard keeps the pinned
    // set strictly increasing so nothing is pinned twice.
    if (
      isLogOutcome(entry) &&
      (!pinnedLogEntries.length || pinnedLogEntries[pinnedLogEntries.length - 1].id < entry.id)
    ) {
      pinnedLogEntries.push(entry);
      const over = pinnedLogEntries.length - maxPinnedLog;
      if (over > 0) pinnedLogEntries.splice(0, over);
    }
    // While the logs view is hidden, or its live pill is paused, we only
    // buffer; the pane is rebuilt from the buffer on reactivation or unpause.
    if (activeView === 'logs' && !logsPaused) appendNewestToDom(entry);
  }

  // Live/pause pill (clog-live), matching the container-log panel's: pausing
  // freezes the pane without dropping anything — pushLog keeps buffering, and
  // unpausing rebuilds the window to catch up in one go.
  const logsLive = document.getElementById('logs-live');
  const logsStat = document.getElementById('logs-stat');
  let logsPaused = false;

  function setLogsStat(text, cls) {
    logsStat.textContent = text;
    logsStat.className = 'clog-stat' + (cls ? ' ' + cls : '');
  }

  logsLive.addEventListener('click', function () {
    logsPaused = !logsPaused;
    logsLive.classList.toggle('paused', logsPaused);
    logsLive.querySelector('.clog-ltxt').textContent = logsPaused ? 'paused' : 'live';
    if (logsPaused) {
      setLogsStat('paused', 'paused');
      return;
    }
    setLogsStat('live · streaming');
    renderLogWindow(); // catch up everything buffered while paused
  });
  logsLive.addEventListener('keydown', function (e) {
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault();
      logsLive.click();
    }
  });

  // SCROLL_EDGE_PX is how close to an edge counts as being at it — one line
  // height's worth of slack, so a pane sitting a pixel off the bottom after a
  // render still reads as "at the tail".
  const SCROLL_EDGE_PX = 40;

  logPane.addEventListener('scroll', function () {
    if (logPane.scrollTop <= SCROLL_EDGE_PX) loadMoreLogs();
    const atTail =
      logPane.scrollTop + logPane.clientHeight >= logPane.scrollHeight - SCROLL_EDGE_PX;
    // Scrolling away from the tail disengages follow: without this, every
    // arriving line yanks the viewport back and reading history is impossible.
    // Scrolling back to the tail re-arms it, so the control matches where the
    // pane actually is rather than needing to be toggled by hand.
    if (atTail !== followLogs) setFollowLogs(atTail, { keepScroll: true });
  });

  // The log stream has no connection indicator (see the events indicator for
  // that), so a fatal error must recover silently. Like /api/events, EventSource
  // auto-reconnects after a transient drop but gives up for good on a fatal error
  // (non-2xx / bad content-type, readyState CLOSED); without our own retry the
  // pane would then stop receiving lines with nothing on screen to explain it.
  // Backoff is the same mechanism the events stream uses, with its own state.
  const logsReconnect = makeReconnector(
    function () {
      connectLogs();
    },
    setTimeout,
    clearTimeout,
  );

  function connectLogs() {
    if (logSource) return;
    logSource = new EventSource('/api/logs');
    logSource.addEventListener('log', function (e) {
      pushLog(JSON.parse(e.data));
    });
    logSource.onopen = function () {
      logsReconnect.reset(); // a good connection resets the backoff
      setLogsStat(logsPaused ? 'paused' : 'live · streaming', logsPaused ? 'paused' : '');
    };
    logSource.onerror = function () {
      // CONNECTING: the browser is retrying itself (Last-Event-ID replays the
      // gap on reconnect), so leave it. CLOSED: it gave up — clear the guard and
      // retry ourselves with a capped backoff.
      if (logSource.readyState === EventSource.CLOSED) {
        logSource = null;
        setLogsStat('reconnecting…', 'err');
        logsReconnect.schedule();
      }
    };
  }

  // ── Stream wake-up ──

  // An OS that freezes a backgrounded tab — an installed PWA, a locked phone —
  // tears the streams down and drops or throttles the retry timer that would
  // have rebuilt them, so a page can come back to the foreground stuck on
  // `reconnecting` with nothing left to wake it. The wake-up drives the
  // reconnect itself instead of trusting that timer. `online` is the same
  // recovery for the case where the network, not the tab, was what went away.

  function streamIsOpen(src) {
    return src !== null && src.readyState === EventSource.OPEN;
  }

  function resumeStreams() {
    eventsReconnect.resume(streamIsOpen(eventsSource));
    // Drop a log stream the suspension killed even when the Logs view is not
    // active: connectLogs takes a non-null handle as "already connected", so one
    // left behind dead would make the view come up silent when it is next
    // opened. Reconnecting is what stays view-gated — that view is what opens
    // the stream, and applyView connects it on arrival.
    if (logSource && !streamIsOpen(logSource)) {
      logSource.close();
      logSource = null;
    }
    if (activeView === 'logs') logsReconnect.resume(streamIsOpen(logSource));
  }

  document.addEventListener('visibilitychange', function () {
    if (document.visibilityState === 'visible') resumeStreams();
  });
  window.addEventListener('online', resumeStreams);

  // ── View switching ──

  function applyView() {
    clog.close(); // a container-log panel belongs to its row; drop it on a view switch
    if (activeView !== 'logs') exitLogFullscreen(); // it would overlay the new view
    document.body.classList.toggle('view-logs', activeView === 'logs');
    document.body.classList.toggle('view-stacks', activeView === 'stacks');
    viewButtons.forEach(function (btn) {
      btn.classList.toggle('active', btn.dataset.view === activeView);
    });
    if (activeView === 'logs') {
      connectLogs();
      renderLogWindow();
    }
    if (activeView === 'stacks') renderRoster();
    syncStackSearchBtn(); // the trigger is view-aware + logs-hidden (T3.11)
  }

  // View-specific options (time mode / sort / follow) live in a popover behind
  // the view buttons so the header never gains or loses controls on a view
  // switch. Clicking the already-active view button toggles that popover;
  // clicking the other button switches views (and closes the popover).
  const viewSwitch = document.getElementById('view-switch');
  const viewOptions = document.getElementById('view-options');

  function setViewOptions(open) {
    if (open) {
      setDrawer(false);
      setRunDrawer(false);
    } // surfaces are mutually exclusive
    viewOptions.classList.toggle('open', open);
    let activeBtn = null;
    viewButtons.forEach(function (btn) {
      // Keep aria-expanded honest on every button — only the active one can be
      // open, the rest are always false — so the caret cue (CSS keys off it)
      // never reads "open" on a stale non-active button.
      const isActive = btn.dataset.view === activeView;
      btn.setAttribute('aria-expanded', String(isActive && open));
      if (isActive) activeBtn = btn;
    });
    // role=menu popover: focus the first option on open, return to the active
    // view button on keyboard close (no hard trap — Escape closes it). (T2.7)
    manageSurfaceFocus(viewOptions, activeBtn, open);
  }

  viewButtons.forEach(function (btn) {
    btn.addEventListener('click', function () {
      if (btn.dataset.view === activeView) {
        // Active button toggles its options popover.
        if (document.querySelector(`.vo-group[data-view="${activeView}"]`)) {
          setViewOptions(!viewOptions.classList.contains('open'));
        }
        return;
      }
      activeView = btn.dataset.view;
      localStorage.setItem('activeView', activeView);
      setViewOptions(false);
      applyView();
    });
  });

  // jumpToStack drives the cross-view jump-btn (deploys <-> stacks): switch to
  // targetView, then scroll to and briefly flash that stack's row there. In
  // the deploys view a stack can have many rows (it's an event log), so the
  // newest one — first in DOM order — is the landing target; the stacks view
  // has exactly one row per stack (it's inventory). Silently does nothing
  // past the view switch when the stack has no row in the target view (e.g.
  // "never deployed"). Clears the target view's own search filter first: a
  // leftover query from before the jump could otherwise leave the landing row
  // `.filtered-out` (display: none) — switched to, but invisible and with no
  // indication why.
  function jumpToStack(targetView, stack) {
    if (targetView !== activeView) {
      activeView = targetView;
      localStorage.setItem('activeView', activeView);
      setViewOptions(false);
      applyView();
    }
    if (targetView === 'stacks') clearRosterFilter(true);
    else clearDeployFilter(true);
    const escaped = window.CSS && CSS.escape ? CSS.escape(stack) : stack;
    const target =
      targetView === 'stacks'
        ? rosterList.querySelector(`.roster-row[data-stack="${escaped}"]`)
        : tbody.querySelector(`.event-row[data-stack="${escaped}"]`);
    if (!target) return;
    flashRow(target);
  }

  // flashRow scrolls a row into view and flashes the accent jump highlight —
  // shared by the cross-view jump and the retry note's rollback landing.
  function flashRow(target) {
    target.scrollIntoView({ behavior: 'smooth', block: 'center' });
    target.classList.remove('jump-target');
    void target.offsetWidth; // restart the animation if it's already landed once
    target.classList.add('jump-target');
    setTimeout(function () {
      target.classList.remove('jump-target');
    }, 1800);
  }

  // openRollbackFor is the retry note's target (UI_SPEC.md "Rollback
  // linkage"): flash the superseded rollback's own row while its event is
  // still rendered, else open the note's row's deploy-history panel — the
  // durable record the rollback always keeps.
  function openRollbackFor(row, rollbackId) {
    if (rollbackId) {
      const escaped = window.CSS && CSS.escape ? CSS.escape(rollbackId) : rollbackId;
      const target = tbody.querySelector(`.event-row[data-event-id="${escaped}"]`);
      if (target) {
        flashRow(target);
        return;
      }
    }
    if (!row) return;
    const next = row.nextElementSibling;
    if (next && next.classList.contains('audit-panel')) return; // already open
    closeHealthPanel(row);
    closeHooksPanel(row);
    closeDiffPanel(row); // one panel per row
    row.classList.add('audit-open');
    row.after(createAuditPanel(row.dataset.stack));
  }

  // ─── Live-health attention surface (ADR-0027 extension) ───
  // Two always-consistent affordances that lift a currently-unhealthy stack out
  // of the chronological deploy log — where its row-bound health pill can sit far
  // down, or vanish once the row ages out of the bounded log: a header BEACON
  // (present in every view) and, in the Deploys view, an attention BAND pinned
  // above the log. Both render from attentionStacks(healthSnap) and jump to the
  // stack's newest row on activate (degrading to a plain view switch when the
  // stack has no row, exactly like jumpBtn).
  // ─── Incident badge (UI_SPEC.md "Incident badge") ───
  // The beacon answers "what is unhealthy NOW"; this badge answers "what went
  // wrong RECENTLY" — the axis a recovered rollback disappears on. Counts
  // incidentsSnap (declared with the other 'stacks' snapshot state above),
  // re-filtered against the window on the client clock (the 30s tick), and
  // lands on the Deploys view with the bad-outcome status chips pre-selected.
  const BAD_OUTCOME_STATUSES = ['failed', 'rolled_back', 'rolled_back_unhealthy', 'heal_exhausted'];
  const incidentBadge = document.getElementById('incident-badge');
  const incidentBadgeCount = document.getElementById('incident-badge-count');

  function renderIncidentBadge() {
    const n = recentIncidentCount(incidentsSnap, Date.now());
    incidentBadge.hidden = n === 0;
    if (n === 0) return;
    incidentBadgeCount.textContent = String(n);
    const label = incidentBadgeLabel(n);
    incidentBadge.title = label;
    incidentBadge.setAttribute('aria-label', label);
  }

  incidentBadge.addEventListener('click', function () {
    // Toggle: a second click on the badge, with its own preset still the whole
    // filter, takes the filter back off rather than re-applying it.
    if (
      activeView === 'deploys' &&
      incidentPresetActive(deployStatusFilter, deployFilter.value, BAD_OUTCOME_STATUSES)
    ) {
      clearDeployFilter(true);
      return;
    }
    if (activeView !== 'deploys') {
      activeView = 'deploys';
      localStorage.setItem('activeView', activeView);
      setViewOptions(false);
      applyView();
    }
    presetDeployStatusFilter(BAD_OUTCOME_STATUSES);
  });

  const healthBeaconWrap = document.getElementById('health-beacon-wrap');
  const healthBeacon = document.getElementById('health-beacon');
  const healthBeaconIcon = healthBeacon.querySelector('.hb-icon');
  const healthBeaconCount = document.getElementById('health-beacon-count');
  const beaconPop = document.getElementById('beacon-pop');
  const attentionBand = document.getElementById('attention-band');
  healthBeaconIcon.innerHTML = WARN_ICON;

  function setBeaconPop(open) {
    beaconPop.classList.toggle('open', open);
    healthBeacon.setAttribute('aria-expanded', String(open));
  }

  function renderHealthBeacon(att) {
    const n = att.length;
    healthBeacon.hidden = n === 0;
    // Collapse the wrapper too, so a hidden beacon leaves no status-area gap.
    healthBeaconWrap.hidden = n === 0;
    if (n === 0) {
      setBeaconPop(false);
      beaconPop.innerHTML = '';
      return;
    }
    const label = attentionLabel(n);
    healthBeaconCount.textContent = String(n);
    healthBeacon.title = label;
    healthBeacon.setAttribute('aria-label', label);
    beaconPop.innerHTML = beaconPopHTML(att);
  }

  function renderAttentionBand(att) {
    const n = att.length;
    attentionBand.hidden = n === 0;
    if (n === 0) {
      attentionBand.innerHTML = '';
      return;
    }
    attentionBand.innerHTML = attentionBandHTML(att);
    attentionBand.querySelectorAll('.attention-row').forEach(function (row) {
      populateIcon(row.querySelector('.stack-icon'), row.dataset.stack);
    });
  }

  // Single entry point, called from applyState's 'health' case — the snapshot
  // bootstrap and the live SSE stream both route through it.
  function renderHealthAttention() {
    const att = attentionStacks(healthSnap);
    renderHealthBeacon(att);
    renderAttentionBand(att);
  }

  healthBeacon.addEventListener('click', function (e) {
    e.stopPropagation();
    setBeaconPop(!beaconPop.classList.contains('open'));
  });
  beaconPop.addEventListener('click', function (e) {
    const item = e.target.closest('.beacon-item');
    if (!item) return;
    setBeaconPop(false);
    // Land in the current list view when it has stack rows (Stacks -> roster
    // row), else fall back to Deploys — so a jump from Stacks doesn't yank the
    // user out of the view they're triaging in.
    jumpToStack(activeView === 'stacks' ? 'stacks' : 'deploys', item.dataset.stack);
  });
  attentionBand.addEventListener('click', function (e) {
    const row = e.target.closest('.attention-row');
    if (!row) return;
    jumpToStack('deploys', row.dataset.stack);
  });

  registerSurface({
    isOpen: function () {
      return beaconPop.classList.contains('open');
    },
    close: function () {
      setBeaconPop(false);
    },
    within: [healthBeacon, beaconPop],
  });

  registerSurface({
    isOpen: function () {
      return viewOptions.classList.contains('open');
    },
    close: function () {
      setViewOptions(false);
    },
    within: [viewSwitch], // the switch wraps both buttons and the options popover
  });

  function applyFollow() {
    followBtn.classList.toggle('on', followLogs);
    followBtn.setAttribute('aria-pressed', String(followLogs));
    if (followLogs) scrollToNewest();
    else clearPendingLines(); // the pill only speaks for an armed-then-disarmed follow
  }

  followBtn.addEventListener('click', function () {
    setFollowLogs(!followLogs);
  });

  renderLogQuickChips(); // paint the persisted filter before the first lines land
  applyFollow();
  applyView();

  // ── Deploys table click delegation (row panels) ──

  // Files panel — open/close as full-width sibling below the row.
  // When diffs are available, shows a diff panel; otherwise shows file list.
  tbody.addEventListener('keydown', hostChipKeydown); // T2.10 keyboard host chip
  tbody.addEventListener('click', function (e) {
    if (e.target.closest('.sha-link')) return; // the commit link opens the forge; no panel toggle
    // ⋯ overflow menu: toggle it open/closed. Picking an action inside closes the
    // menu, then falls through to that action's own handler below (the relocated
    // buttons still carry their .history-btn/.clog-btn/.hooks-badge classes).
    const moreBtn = e.target.closest('.more-btn');
    if (moreBtn) {
      toggleMoreMenu(moreBtn.closest('.row-more'));
      return;
    }
    if (e.target.closest('.more-item')) closeMoreMenu();

    // Host chip: quick-filter the merged view to this row's host (toggle). Before
    // any panel logic so a tap on the chip never also opens the row's diff panel.
    const hostChip = e.target.closest('.host-mono');
    if (hostChip) {
      const r = hostChip.closest('[data-host]');
      if (r) toggleHostFilterTo(r.dataset.host);
      return;
    }

    // Diff panel collapse/expand controls (per-file header, multi-commit pill).
    if (handleDiffToggle(e.target)) return;

    // Cross-view jump button: hand off to Stacks. Handled before the files
    // logic so a tap never opens the row's diff panel.
    const jumpBtn = e.target.closest('.jump-btn');
    if (jumpBtn) {
      jumpToStack(jumpBtn.dataset.jumpView, jumpBtn.dataset.jumpStack);
      return;
    }
    // Retry note: open the rollback this success supersedes. Before the files
    // logic so a tap never also opens the row's own diff panel.
    const retry = e.target.closest('.retry-note');
    if (retry) {
      openRollbackFor(retry.closest('.event-row'), retry.dataset.rollbackId);
      return;
    }
    // Hook-log icon: a .clog-btn, so match it before the container-logs handler.
    const hookLog = e.target.closest('.hook-log-btn');
    if (hookLog) {
      clog.openHookLog(hookLog, hookLog.dataset.hookLog);
      return;
    }

    // Hooks badge: toggle the panel; before the files logic so a tap never also
    // opens the row's diff panel.
    const hbadge = e.target.closest('.hooks-badge');
    if (hbadge) {
      const hrow = hbadge.closest('.event-row');
      if (hrow) {
        const hnext = hrow.nextElementSibling;
        if (hnext && hnext.classList.contains('hooks-panel')) {
          closeHooksPanel(hrow);
        } else {
          closeHealthPanel(hrow);
          closeDiffPanel(hrow);
          closeAuditPanel(hrow); // one panel per row
          hrow.classList.add('hooks-open');
          hrow.after(createHooksPanel(hrow.dataset.stack));
        }
      }
      return;
    }

    // Container-logs button (ADR-0037): toggle the live log panel. Handled first
    // so a tap never also opens the row's diff/health/audit panel.
    const clogB = e.target.closest('.clog-btn');
    if (clogB) {
      clog.toggle(clogB);
      return;
    }

    // History button: toggle the per-stack deploy-history panel (ADR-0033).
    // Handled before the files logic so a tap never opens the row's diff panel.
    const histBtn = e.target.closest('.history-btn');
    if (histBtn) {
      const arow = histBtn.closest('.event-row');
      if (arow) {
        const anext = arow.nextElementSibling;
        if (anext && anext.classList.contains('audit-panel')) {
          closeAuditPanel(arow);
        } else {
          closeHealthPanel(arow);
          closeHooksPanel(arow); // one panel per row
          closeDiffPanel(arow);
          arow.classList.add('audit-open');
          arow.after(createAuditPanel(arow.dataset.stack));
        }
      }
      return;
    }

    // Stack-health pill: toggle its per-service panel. Handled before the files
    // logic so a tap on the pill never opens the row's files/diff panel (mobile).
    const hpill = e.target.closest('.health-pill');
    if (hpill) {
      const hrow = hpill.closest('.event-row');
      // A peer row's pill is read-only: open its containers detail (host-scoped),
      // never the local health panel (which would key the peer's stack name into
      // the primary's own health).
      if (hrow && hrow.classList.contains('peer-row')) {
        togglePeerDetail(hrow);
        return;
      }
      if (hrow) {
        const hnext = hrow.nextElementSibling;
        if (hnext && hnext.classList.contains('health-panel')) {
          closeHealthPanel(hrow);
        } else {
          closeDiffPanel(hrow);
          closeHooksPanel(hrow); // one panel per row
          closeAuditPanel(hrow);
          hrow.dataset.health = hpill.dataset.health || 'unknown'; // tint the row (variant A)
          hrow.classList.add('health-open');
          hrow.after(createHealthPanel(hrow.dataset.stack));
        }
      }
      return;
    }

    // Self-heal badge: toggle the corrective-redeploy detail panel. A healed row
    // has no files pill, so it is handled on its own (ADR-0029).
    const healPill = e.target.closest('.heal-pill');
    const healRow = healPill
      ? healPill.closest('.event-row')
      : e.target.closest('.event-row.healed-row');
    if (healRow) {
      const healBtn = healPill || healRow.querySelector('.heal-pill');
      if (!healBtn) return;
      const hnext = healRow.nextElementSibling;
      if (hnext && hnext.classList.contains('heal-panel')) {
        closeDiffPanel(healRow);
      } else {
        closeHealthPanel(healRow);
        closeHooksPanel(healRow); // one panel per row
        closeAuditPanel(healRow);
        closeDiffPanel(healRow);
        healRow.classList.add('diff-open'); // shares the panel's status bar
        const drift = JSON.parse(healBtn.dataset.drift || '[]');
        healRow.after(
          createHealPanel(drift, { stack: healRow.dataset.stack, status: healRow.dataset.status }),
        );
      }
      return;
    }

    const pill = e.target.closest('.files-pill');
    const row = pill ? pill.closest('.event-row') : e.target.closest('.event-row');
    if (!row) return;
    if (row.classList.contains('peer-row')) {
      togglePeerDetail(row);
      return;
    } // read-only detail, not a local diff
    const pillEl = pill || row.querySelector('.files-pill');
    if (!pillEl) {
      // A row with no changed-files panel (e.g. a deploy with nothing hashed)
      // still has relevant info — its deploy history. Never dead-end a click:
      // toggle the audit panel instead of doing nothing.
      const anext = row.nextElementSibling;
      if (anext && anext.classList.contains('audit-panel')) {
        closeAuditPanel(row);
      } else {
        closeHealthPanel(row);
        closeHooksPanel(row);
        closeDiffPanel(row);
        row.classList.add('audit-open');
        row.after(createAuditPanel(row.dataset.stack));
      }
      return;
    }
    const existing = row.nextElementSibling;
    if (
      existing &&
      (existing.classList.contains('files-list') ||
        existing.classList.contains('diff-panel') ||
        existing.classList.contains('load-error'))
    ) {
      closeDiffPanel(row);
      return;
    }
    closeHealthPanel(row);
    closeHooksPanel(row); // one panel per row
    closeAuditPanel(row);

    const eventId = row.dataset.eventId;
    const hasDiffs = row.dataset.hasDiffs === '1';
    // Bind the panel to its row (variant A): the shared status bar/tint keys off
    // the row's status, so pass it to the panel and mark the row open.
    const meta = { stack: row.dataset.stack, status: row.dataset.status };

    // Mark the row open for any panel (diff or plain file list) so it keeps the
    // shared status bar; the trailing panel binds to it via --dc.
    row.classList.add('diff-open');
    if (hasDiffs && eventId) {
      // runDiffFetch drops a loading placeholder after the row and fills it once
      // the fetch resolves. Named so the load-error's Retry can re-run it.
      const runDiffFetch = function () {
        const loading = document.createElement('div');
        loading.className = 'diff-panel bound';
        loading.dataset.status = meta.status;
        loading.innerHTML = '<div class="diff-loading">Loading diffs...</div>';
        row.after(loading);
        fetchDiffs(eventId, function (diffs, commits, err) {
          // The placeholder gone means the panel was closed (or replaced by the
          // health panel) while loading — don't resurrect it.
          if (!loading.parentNode) return;
          loading.remove();
          if (err) {
            // Fetch failed: an amber load-error instead of a silent drop to the
            // file list, so a network hiccup isn't mistaken for "no diff"
            // (T4.16). Retry re-runs the fetch from the loading placeholder.
            row.after(
              createLoadError("Couldn't load the diff.", function (le) {
                le.remove();
                runDiffFetch();
              }),
            );
          } else if (diffs && Object.keys(diffs).length > 0) {
            row.after(renderDiffPanel(diffs, commits, meta));
          } else {
            const files = JSON.parse(pillEl.dataset.files);
            row.after(createFilesPanel(files, meta));
          }
        });
      };
      runDiffFetch();
    } else {
      const files = JSON.parse(pillEl.dataset.files);
      row.after(createFilesPanel(files, meta));
    }
  });

  // Paint the deployed skipper-cd build identity in the header once on load.
  // Feature-branch builds show the branch (which release semver alone cannot
  // distinguish); release/Nix/dev builds show the version. The short commit is
  // appended whenever the build path knows it.
  fetch('/api/version')
    .then(function (r) {
      return r.json();
    })
    .then(function (d) {
      if (!d || !d.version) return;
      const el = document.getElementById('brand-version');
      if (!el) return;
      const base = d.version === 'dev' ? 'dev' : 'v' + d.version;
      const head = d.branch && d.branch !== 'main' ? d.branch : base;
      const text = d.commit ? head + ' · ' + d.commit : head;
      el.textContent = text;
      // The label may be clipped (ellipsis) on a long branch name, and it is
      // hidden entirely below 1000px — so the identity also rides the logo,
      // where a hover tooltip or a tap-tip reaches it at every width.
      el.title = text;
      const icon = document.querySelector('.brand-icon');
      if (icon) icon.title = text;
    })
    .catch(function () {});

  // ── Tap-tips: touch tooltips (data-taptip opt-in) ──

  // Tap-reveal tooltips for touch (titles never show on touch): a tap flashes
  // the title in a bubble. Any control under a `data-taptip` ancestor (or
  // carrying the attribute itself) opts in; mouse/pen keep the native tooltip.
  (function setupTapTips() {
    const tip = document.createElement('div');
    tip.className = 'tap-tip';
    tip.setAttribute('aria-hidden', 'true');
    document.body.appendChild(tip);
    let hideTimer = null;
    function hide() {
      tip.classList.remove('show');
    }
    function flash(el) {
      const text = el.getAttribute('title');
      if (!text) return;
      tip.textContent = text;
      // Render off-screen first to measure, then centre under the control and
      // clamp to the viewport so it never overflows the edges.
      tip.style.left = '-9999px';
      tip.style.top = '0px';
      tip.classList.add('show');
      const r = el.getBoundingClientRect();
      const margin = 6;
      let left = r.left + r.width / 2 - tip.offsetWidth / 2;
      left = Math.max(margin, Math.min(left, window.innerWidth - tip.offsetWidth - margin));
      tip.style.left = left + 'px';
      tip.style.top = r.bottom + 6 + 'px';
      clearTimeout(hideTimer);
      hideTimer = setTimeout(hide, 1600);
    }
    document.addEventListener(
      'pointerdown',
      function (e) {
        if (e.pointerType && e.pointerType !== 'touch') return; // mouse/pen: native tooltip
        const el = e.target.closest && e.target.closest('[title]');
        if (!el) return;
        if (el.closest('.view-options')) return; // popover rows already show visible labels
        if (!el.closest('[data-taptip]')) return; // deliberate opt-in, not "any [title]"
        flash(el);
      },
      { passive: true },
    );
    window.addEventListener('scroll', hide, { passive: true });
  })();

  connect();

  // ── PWA: service worker + update prompt (ADR-0023) ──

  // Register the service worker for installability + app-shell caching, and
  // prompt to reload when a newer version has been deployed. Failure-tolerant:
  // an insecure context (plain HTTP) simply skips it, the page keeps working as
  // a normal site. See docs/pwa.md.
  if ('serviceWorker' in navigator) {
    window.addEventListener('load', function () {
      registerServiceWorker();
    });
  }

  // The update banner: shown once a new service worker has installed and is
  // waiting. Tapping Reload asks that worker to activate; the resulting
  // controllerchange reloads the page onto the fresh shell.
  // Deduped by worker identity: the same waiting worker never re-prompts (its
  // statechange and the on-load reg.waiting check can both reach here), but a
  // later deploy's new worker can prompt again even after a dismissal.
  let promptedWorker = null;
  // Set when the user accepts the prompt. Together with a worker already
  // controlling the page at load, it tells a real version handover (reload onto
  // it) apart from the first-install clients.claim() (must not bounce a fresh
  // visit) — see the controllerchange listener.
  let updateAccepted = false;
  function showUpdateBanner(worker) {
    if (promptedWorker === worker) return;
    promptedWorker = worker;
    const banner = document.getElementById('update-banner');
    const reload = document.getElementById('update-banner-reload');
    const close = document.getElementById('update-banner-close');
    reload.onclick = function () {
      updateAccepted = true;
      reload.disabled = true;
      worker.postMessage({ type: 'SKIP_WAITING' });
    };
    close.onclick = function () {
      banner.hidden = true;
    };
    banner.hidden = false;
  }

  function registerServiceWorker() {
    // Reload once when a waiting worker takes over: either the user accepted the
    // prompt (here or, via hadController, in another tab). A controllerchange
    // with neither — no controller at load and no acceptance — is just the
    // first-install clients.claim(), which must not bounce a fresh visit.
    const hadController = !!navigator.serviceWorker.controller;
    let reloading = false;
    navigator.serviceWorker.addEventListener('controllerchange', function () {
      if (reloading || (!updateAccepted && !hadController)) return;
      reloading = true;
      window.location.reload();
    });

    navigator.serviceWorker
      .register('/sw.js')
      .then(function (reg) {
        // A worker that finished installing while the page was closed is already
        // waiting on load — updatefound won't fire for it, so check explicitly.
        if (reg.waiting && navigator.serviceWorker.controller) {
          showUpdateBanner(reg.waiting);
        }
        reg.addEventListener('updatefound', function () {
          const sw = reg.installing;
          if (!sw) return;
          sw.addEventListener('statechange', function () {
            // installed + an existing controller ⇒ an update is ready; without a
            // controller this is the first install, which needs no prompt.
            if (sw.state === 'installed' && navigator.serviceWorker.controller) {
              showUpdateBanner(sw);
            }
          });
        });
        // Check for a new worker now and whenever the tab regains focus, so a
        // long-lived standalone PWA notices a deploy without a manual reload.
        reg.update().catch(function () {});
        document.addEventListener('visibilitychange', function () {
          if (document.visibilityState === 'visible') reg.update().catch(function () {});
        });
      })
      .catch(function () {});
  }

  // ── skipper Logs view controls: the view is styled as one big clog-panel
  // (like the Stacks/Deploys container-log panel, just page-sized), so its
  // search/wrap/auto-scroll/fullscreen tools live inline in its own header —
  // not in the view-options popover, which no longer carries a logs group.
  // Search reveals the same filter bar the deploys/stacks views use (seeded by
  // type-to-search on desktop). Fullscreen fills below the header so it stays
  // reachable to toggle back off.
  (function () {
    const view = document.getElementById('log-view');
    const wrapBtn = document.getElementById('log-wrap');
    const fsBtn = document.getElementById('log-fs');
    const searchBtn = document.getElementById('log-search-open');
    const wrap = document.getElementById('log-filter-wrap');
    const input = document.getElementById('log-filter');
    const count = document.getElementById('log-filter-count');

    function highlightLine(line, q) {
      const ql = q.toLowerCase();
      const w = document.createTreeWalker(line, NodeFilter.SHOW_TEXT, null),
        ns = [];
      while (w.nextNode()) ns.push(w.currentNode);
      ns.forEach(function (n) {
        const text = n.nodeValue,
          low = text.toLowerCase();
        let idx = low.indexOf(ql);
        if (idx < 0) return;
        const frag = document.createDocumentFragment();
        let pos = 0;
        while (idx >= 0) {
          if (idx > pos) frag.appendChild(document.createTextNode(text.slice(pos, idx)));
          const mk = document.createElement('mark');
          mk.textContent = text.slice(idx, idx + q.length);
          frag.appendChild(mk);
          pos = idx + q.length;
          idx = low.indexOf(ql, pos);
        }
        if (pos < text.length) frag.appendChild(document.createTextNode(text.slice(pos)));
        n.parentNode.replaceChild(frag, n);
      });
    }
    function applyLogSearch() {
      const q = input.value.trim();
      wrap.classList.toggle('has-value', q.length > 0);
      let n = 0;
      logPane.querySelectorAll('.log-line').forEach(function (line) {
        if (line.dataset.origHtml == null) line.dataset.origHtml = line.innerHTML;
        line.innerHTML = line.dataset.origHtml;
        line.classList.remove('clog-out');
        if (!logLineVisible(line.textContent, q)) {
          line.classList.add('clog-out');
          return;
        }
        // A line the quick filters exclude is not on screen, so it must not be
        // counted as a hit — the two filters narrow independently and the count
        // has to describe what a viewer can actually see.
        if (q && !line.classList.contains('log-out')) {
          n++;
          highlightLine(line, q);
        }
      });
      count.textContent = q ? n + (n === 1 ? ' hit' : ' hits') : '';
    }
    function revealLogFilter(on) {
      wrap.classList.toggle('revealed', on);
      searchBtn.classList.toggle('on', on);
      syncStackSearchBtn(); // the header magnifier opens this same bar
    }
    function clearLogFilter(hide) {
      input.value = '';
      applyLogSearch();
      if (hide) {
        revealLogFilter(false);
        input.blur();
      }
    }
    // Re-apply after the log window re-renders (new lines) so they obey the filter.
    logSearchApply = function () {
      if (wrap.classList.contains('revealed') && input.value.trim()) applyLogSearch();
    };

    input.addEventListener('input', applyLogSearch);
    input.addEventListener('keydown', function (e) {
      if (e.key !== 'Escape') return;
      e.stopPropagation();
      if (input.value) clearLogFilter(false);
      else {
        revealLogFilter(false);
        input.blur();
      }
    });
    input.addEventListener('blur', function () {
      if (!input.value) revealLogFilter(false);
    });
    // Clicking again closes it (and clears the query), matching the
    // container-log panel's search tool. The header magnifier drives the same
    // toggle on this view, so both read and leave the one state.
    function toggleLogSearch() {
      if (wrap.classList.contains('revealed')) {
        clearLogFilter(true);
        return;
      }
      revealLogFilter(true);
      input.focus();
    }
    logSearchToggle = toggleLogSearch;
    logSearchIsOpen = function () {
      return wrap.classList.contains('revealed');
    };
    searchBtn.addEventListener('click', toggleLogSearch);

    wrapBtn.addEventListener('click', function () {
      wrapBtn.classList.toggle('on', logPane.classList.toggle('wrap'));
    });

    function setLogFullscreen(on) {
      view.classList.toggle('clog-fullscreen', on);
      fsBtn.classList.toggle('on', on);
    }
    exitLogFullscreen = function () {
      setLogFullscreen(false);
    };
    fsBtn.addEventListener('click', function () {
      setLogFullscreen(!view.classList.contains('clog-fullscreen'));
    });

    // Type-to-search: a printable key while viewing logs reveals the filter and
    // seeds it (mirrors the deploys/stacks type-to-search); Esc exits fullscreen
    // then the filter.
    document.addEventListener('keydown', function (e) {
      if (activeView !== 'logs' || e.defaultPrevented || e.metaKey || e.ctrlKey || e.altKey) return;
      if (e.target === input) return;
      const tag = (e.target && e.target.tagName) || '';
      if (
        tag === 'INPUT' ||
        tag === 'TEXTAREA' ||
        tag === 'SELECT' ||
        (e.target && e.target.isContentEditable)
      )
        return;
      if (e.key === 'Escape') {
        if (view.classList.contains('clog-fullscreen')) setLogFullscreen(false);
        else if (wrap.classList.contains('revealed')) clearLogFilter(true);
        return;
      }
      if (e.key.length === 1 && e.key !== ' ') {
        revealLogFilter(true);
        input.focus();
        input.value += e.key;
        applyLogSearch();
        e.preventDefault();
      }
    });
  })();

  document.addEventListener('keydown', function (e) {
    if (e.key === 'Escape') clog.escape();
  });
})();
