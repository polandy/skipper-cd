(function () {
  // Shared state comes from app-state.js (loaded before this file); S is the
  // store every view reads and applyState / applyPeers / handleEvent below
  // write. The views that have moved out are imported from App.<view>.
  const S = App.state;
  const clog = App.clog;
  const autosync = App.autosync;
  const logs = App.logs;
  const roster = App.roster;
  const {
    renderDisabledStacks,
    renderRoster,
    updateAppLinks,
    updateRosterHealth,
    refreshRosterRow,
    clearRosterFilter,
    presetRosterUpdateFilter,
    countRosterUpdates,
  } = roster;
  const hosts = App.hosts;
  const { hostChip, hostChipKeydown, togglePeerDetail, toggleHostFilterTo, schedulePeerReflow } =
    hosts;
  const panels = App.panels;
  const {
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
    createHealthPanel,
  } = panels;
  // What app.js still owes the views already cut out: the chrome, deploy and
  // logs functions they call, under the namespace each will own once it moves.
  // Function declarations hoist, so the stubs can sit ahead of the definitions.
  App.chrome = {
    uiNote,
    registerSurface,
    manageSurfaceFocus,
    trapFocus,
    setViewOptions,
    syncStackSearchBtn,
    showTable,
    renderUpdateBadge,
    jumpToStack,
    activeView: function () {
      return activeView;
    },
  };
  App.deploys = { setRunDrawer, insertRowByTime, refilterDeploys, isDeploying };
  App.stream = { streamIsOpen, applyHookRun };
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
  // isDeploying answers the roster's question about the deploys table's state.
  function isDeploying(stack) {
    return !!deployingRows[stack];
  }
  // Queued (paused) rows, keyed by stack, so a stack's pending row is replaced
  // rather than duplicated, and removed once it deploys or drains.
  const queuedRows = {};
  let hasRows = false;

  // The reserved stack key for nixos-rebuild deploys (invariant 4). It is a
  // pseudo-stack, not a Docker Compose project and not in the Stacks roster, so
  // it carries no container-logs button and no jump-to-Stacks button — only the
  // affordances that apply to it (its git diff + deploy history).
  const NIXOS_STACK = '_nixos';

  // ── Row panels, ⋯ menu, app-link popover, icons ── live in app-panels.js
  // (App.panels); their document-level listeners are wired here, at the spot
  // they used to register.
  panels.init();

  // ── Orphan compose projects (ADR-0036) ──

  // orphansOpen remembers expanded rows so the per-poll re-render does not
  // collapse them.
  let orphansOpen = new Set();
  let orphansSectionOpen = false; // the user's manual toggle of the section header
  let announcedOrphans = new Set(); // orphaned projects already surfaced, so a manual close sticks

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

  // orphanedProjects are the projects skipper once deployed and the stack set no
  // longer accounts for — the ones a removal leaves behind. Unmanaged projects
  // (never deployed by skipper) are inventory, not news.
  function orphanedProjects() {
    return S.orphansSnap
      .filter(function (o) {
        return o.class === 'orphaned';
      })
      .map(function (o) {
        return o.project;
      });
  }

  // A newly appearing orphan opens the section once, so a stack that left the
  // repo is not hidden behind a collapsed header. Tracked per project: a manual
  // close then sticks until the next one shows up.
  function openSectionForNewOrphans() {
    const current = orphanedProjects();
    const fresh = current.some(function (p) {
      return !announcedOrphans.has(p);
    });
    announcedOrphans = new Set(current);
    if (fresh) orphansSectionOpen = true;
  }

  function renderOrphans() {
    const wrap = document.getElementById('orphans');
    const body = document.getElementById('orphans-body');
    const count = document.getElementById('orphans-count');
    body.textContent = '';
    const orphaned = orphanedProjects();
    openSectionForNewOrphans(orphaned);
    // The pill carries the orphaned colour only when one is present — an
    // all-unmanaged list is inventory and stays muted.
    count.classList.toggle('has-orphaned', orphaned.length > 0);
    // The badge shows matching orphans during a search, else the total.
    const q = (deployFilter.value || '').trim().toLowerCase();
    count.textContent = String(
      q
        ? S.orphansSnap.filter(function (o) {
            return orphanMatchesQuery(o, q);
          }).length
        : S.orphansSnap.length,
    );
    if (!S.orphansSnap.length) {
      wrap.classList.remove('shown');
      return;
    }
    S.orphansSnap.forEach(function (o) {
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
      S.orphansSnap.some(function (o) {
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

  // Paint the running-hook phase + pulse the badge on the stack's row in both
  // views; clear it when S.hookRunSnap has no stack.
  function applyHookRun() {
    document.querySelectorAll('.hook-phase').forEach(function (n) {
      n.remove();
    });
    document
      .querySelectorAll('.hooks-badge[data-hook-active], .more-btn[data-hook-active]')
      .forEach(function (b) {
        b.removeAttribute('data-hook-active');
      });
    const hr = S.hookRunSnap;
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

  // ── Deploy rows: stack icons, row build, per-stack affordances ──

  // pendingReason draws a stack's pause/block reason from the queue snapshot
  // for pendingTagHTML (app-render.js).
  function pendingReason(stack) {
    const item = S.queueByStack[stack];
    return item && item.reason;
  }

  function createRow(evt, isHistory) {
    const row = document.createElement('div');
    row.className = rowClass(evt.status, isHistory);
    row.dataset.testid = 'deploy-row';
    row.dataset.stack = evt.stack;
    row.dataset.status = evt.status;
    row.dataset.eventId = evt.id;
    row.dataset.host = S.selfHost; // local rows belong to the primary host
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
    const delta =
      S.imageDeltaOn && evt.status !== 'healed' ? imageDeltaHTML(evt.image_changes) : '';
    row.innerHTML =
      `<span class="cell-time" data-testid="time-cell" data-ts="${escapeAttr(evt.timestamp)}" title="${escapeAttr(S.absoluteTime ? relTs : absTs)}">${S.absoluteTime ? absTs : relTs}</span>` +
      `<span class="cell-stack">${hostChip(S.selfHost)}<span class="stack-icon" data-testid="stack-icon"></span><span class="stack-name">${escapeHtml(evt.stack)}</span>${evt.stack === NIXOS_STACK ? '' : jumpBtnHTML('stacks', evt.stack)}${pausedTag}</span>` +
      `<span class="col-version">${delta}</span>` +
      `<span class="status-cell">${badgeHTML(evt.status)}${retryNoteHTML(evt)}${repeatNoteHTML(evt)}</span>` +
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
      const h = newest ? S.healthSnap[stack] : null;
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
          closeMoreMenuIf(staleWrap);
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

  // ── Deploying indicator + run drawer ──

  function updateDeployingIndicator() {
    const active = Object.keys(deployingRows);
    const up = S.upcomingSnap;
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
    const up = S.upcomingSnap;
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
      autosync.setDrawer(false);
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

  // A repeat collapses into the event it absorbed (ADR-0056), so the incoming
  // event replaces one already on screen rather than adding to it. Without this
  // the row would be drawn twice — once at the old count, once at the new.
  function removeSupersededRow(evt) {
    if (!evt.supersedes_id) return;
    const row = tbody.querySelector('.event-row[data-event-id="' + evt.supersedes_id + '"]');
    if (!row) return;
    closeDiffPanel(row);
    closeHealthPanel(row);
    closeAuditPanel(row);
    const detail = row.nextElementSibling;
    if (detail && detail.classList.contains('error-detail')) detail.remove();
    row.remove();
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
    removeSupersededRow(evt);

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
      existing.querySelector('.status-cell').innerHTML =
        badgeHTML(evt.status) + retryNoteHTML(evt) + repeatNoteHTML(evt);
      existing.querySelector('.cell-duration').textContent = formatDuration(evt.duration_ms);
      const tc = existing.querySelector('.cell-time');
      tc.dataset.ts = evt.timestamp;
      tc.textContent = S.absoluteTime ? fullTime(evt.timestamp) : formatTime(evt.timestamp);
      tc.title = S.absoluteTime ? formatTime(evt.timestamp) : fullTime(evt.timestamp);
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
        versionCell.innerHTML = S.imageDeltaOn ? imageDeltaHTML(evt.image_changes) : '';
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
    if (!S.absoluteTime) {
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

  // ── Multi-host fan-in (ADR-0048) ── lives in app-hosts.js (App.hosts). The
  // dispatcher keeps the store writes; two table/roster primitives it renders
  // through stay here with their owners.

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

  // applyPeers ingests a fresh 'peers' snapshot: store it, then let the hosts
  // view apply it end to end.
  function applyPeers(d) {
    S.peersSnap = d || null;
    S.selfHost = (S.peersSnap && S.peersSnap.self) || '';
    hosts.applyPeers();
  }

  // applyState routes one state snapshot (by its state-event name) to the same
  // handling whether it is the baseline the stream sends on (re)connect or a
  // later live state event. State payloads are snapshots (full replacements), so
  // applying one is idempotent. 'deploy' is not here — deploy events are the
  // append-style history, streamed over SSE, not part of the state snapshot.
  function applyState(name, d) {
    switch (name) {
      case 'autosync':
        autosync.applyAutosyncSnapshot(d);
        break;
      case 'queue':
        S.queueSnap = d;
        S.queueByStack = {};
        (S.queueSnap.pending || []).forEach(function (it) {
          S.queueByStack[it.stack] = it;
        });
        // A stack that left the pending set without a deploy event (e.g. resumed
        // then found unchanged) must lose its now-stale queued row.
        Object.keys(queuedRows).forEach(function (stack) {
          if (!S.queueByStack[stack]) removeQueuedRow(stack);
        });
        refreshPendingTags();
        autosync.renderAutosync();
        break;
      case 'upcoming':
        S.upcomingSnap = (d && d.upcoming) || [];
        updateDeployingIndicator();
        break;
      case 'hookrun':
        S.hookRunSnap = d || {};
        applyHookRun();
        break;
      case 'health':
        S.healthSnap = (d && d.stacks) || {};
        updateStackAffordances();
        updateRosterHealth();
        renderHealthAttention(); // beacon + band read the same snapshot
        break;
      case 'healthwatch':
        S.healthwatchSnap = (d && d.stacks) || {};
        break;
      case 'stacks':
        S.disabledSnap = (d && d.disabled) || [];
        S.rosterSnap = (d && d.roster) || [];
        S.repoWebURL = (d && d.repo_web_url) || '';
        S.updatesSnap = (d && d.updates) || null;
        S.incidentsSnap = (d && d.incidents_24h) || [];
        renderDisabledStacks();
        renderRoster();
        updateStackAffordances(); // roster carries the hooks — (de)show the badge
        renderIncidentBadge();
        break;
      case 'app_links':
        S.appLinksSnap = (d && d.stacks) || {};
        updateAppLinks();
        break;
      case 'orphans':
        S.orphansSnap = (d && d.orphans) || [];
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
    S.autosyncVersion = null; // the version restarts with the server; the baseline re-seeds it
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
      btn.classList.toggle('active', S.absoluteTime);
      btn.title = S.absoluteTime ? 'Switch to relative time' : 'Switch to absolute time';
    });
    tbody.querySelectorAll('.cell-time').forEach(function (cell) {
      const ts = cell.dataset.ts;
      if (!ts) return;
      cell.textContent = S.absoluteTime ? fullTime(ts) : formatTime(ts);
      cell.title = S.absoluteTime ? formatTime(ts) : fullTime(ts);
    });
  }

  timeModeBtns.forEach(function (btn) {
    if (!btn) return;
    btn.addEventListener('click', function () {
      S.absoluteTime = !S.absoluteTime;
      localStorage.setItem('timeMode', S.absoluteTime ? 'absolute' : 'relative');
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
  // takes effect live without a reload. New rows honour S.imageDeltaOn in
  // createRow / the settle path.
  const imageDeltaBtn = document.getElementById('image-delta-toggle');
  const deployTable = document.getElementById('deploy-table');

  function applyImageDelta() {
    if (imageDeltaBtn) {
      imageDeltaBtn.classList.toggle('active', S.imageDeltaOn);
      imageDeltaBtn.title = S.imageDeltaOn ? 'Hide the Version column' : 'Show the Version column';
    }
    if (deployTable) deployTable.classList.toggle('no-version', !S.imageDeltaOn);
    tbody.querySelectorAll('.event-row[data-stack]').forEach(function (row) {
      const cell = row.querySelector('.col-version');
      if (!cell) return;
      cell.innerHTML =
        S.imageDeltaOn && row.dataset.imageChanges
          ? imageDeltaHTML(JSON.parse(row.dataset.imageChanges))
          : '';
    });
  }

  if (imageDeltaBtn) {
    imageDeltaBtn.addEventListener('click', function () {
      S.imageDeltaOn = !S.imageDeltaOn;
      // Store only the off choice; absence means the default (on), so a cleared
      // browser and a first-time visitor both get the delta.
      if (S.imageDeltaOn) localStorage.removeItem('imageDelta');
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
      for (let j = 0; j < S.orphansSnap.length; j++) {
        total++;
        if (orphanMatchesQuery(S.orphansSnap[j], q)) shown++;
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

  // ── Stacks view ── lives in app-roster.js (App.roster); wired here, at the
  // spot the block occupied, so its type-to-search stays behind the deploys one.
  roster.init();

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
      ? logs.logSearchIsOpen()
      : activeView === 'stacks'
        ? roster.filterRevealed()
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
      if (roster.filterRevealed()) clearRosterFilter(true);
      else {
        roster.openFilter();
      }
    } else if (activeView === 'logs') {
      logs.logSearchToggle();
    }
  });

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

  // ── Autosync controls ── live in app-autosync.js (App.autosync); wired here,
  // at the spot the block occupied, once the surfaces it registers with exist.
  autosync.init();

  // ── Hosts drawer ── lives in app-hosts.js (App.hosts); wired here, at the
  // spot the block occupied.
  hosts.init();

  // ── Logs view ── lives in app-logs.js (App.logs); wired here, at the spot
  // the block occupied, once the view state below exists.
  const viewButtons = document.querySelectorAll('#view-toggle button');
  const savedView = localStorage.getItem('activeView');
  let activeView = savedView === 'logs' || savedView === 'stacks' ? savedView : 'deploys';
  logs.init();

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
    logs.resume();
  }

  document.addEventListener('visibilitychange', function () {
    if (document.visibilityState === 'visible') resumeStreams();
  });
  window.addEventListener('online', resumeStreams);

  // ── View switching ──

  function applyView() {
    clog.close(); // a container-log panel belongs to its row; drop it on a view switch
    if (activeView !== 'logs') logs.exitLogFullscreen(); // it would overlay the new view
    document.body.classList.toggle('view-logs', activeView === 'logs');
    document.body.classList.toggle('view-stacks', activeView === 'stacks');
    viewButtons.forEach(function (btn) {
      btn.classList.toggle('active', btn.dataset.view === activeView);
    });
    if (activeView === 'logs') {
      logs.connectLogs();
      logs.renderLogWindow();
    }
    if (activeView === 'stacks') renderRoster();
    renderUpdateBadge(); // its .active state is view-dependent (Stacks only)
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
      autosync.setDrawer(false);
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
        ? document
            .getElementById('roster-list')
            .querySelector(`.roster-row[data-stack="${escaped}"]`)
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
  // above the log. Both render from attentionStacks(S.healthSnap) and jump to the
  // stack's newest row on activate (degrading to a plain view switch when the
  // stack has no row, exactly like jumpBtn).
  // ─── Incident badge (UI_SPEC.md "Incident badge") ───
  // The beacon answers "what is unhealthy NOW"; this badge answers "what went
  // wrong RECENTLY" — the axis a recovered rollback disappears on. Counts
  // S.incidentsSnap (declared with the other 'stacks' snapshot state above),
  // re-filtered against the window on the client clock (the 30s tick), and
  // lands on the Deploys view with the bad-outcome status chips pre-selected.
  const BAD_OUTCOME_STATUSES = ['failed', 'rolled_back', 'rolled_back_unhealthy', 'heal_exhausted'];
  const incidentBadge = document.getElementById('incident-badge');
  const incidentBadgeCount = document.getElementById('incident-badge-count');

  function renderIncidentBadge() {
    const n = recentIncidentCount(S.incidentsSnap, Date.now());
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

  // ─── Update badge (UI_SPEC.md "Updates filter") ───
  // The registry check (ADR-0054) marks individual version chips; this badge is
  // the fleet-level answer — how many stacks are waiting on one — and the only
  // always-visible way into the Stacks view's updates-only filter, which would
  // otherwise sit behind a filter bar nobody opens. It counts the rendered
  // roster rows rather than the snapshot, so the number is always the number of
  // rows the filter hands back. A filter, never a trigger: applying an update
  // stays a git commit.
  const updateBadge = document.getElementById('update-badge');
  const updateBadgeCount = document.getElementById('update-badge-count');

  function renderUpdateBadge() {
    const n = countRosterUpdates();
    updateBadge.hidden = n === 0;
    updateBadge.classList.toggle(
      'active',
      activeView === 'stacks' && roster.updateFilterPresetActive(),
    );
    if (n === 0) return;
    updateBadgeCount.textContent = String(n);
    const label = updateBadgeLabel(n);
    updateBadge.title = label;
    updateBadge.setAttribute('aria-label', label);
  }

  updateBadge.addEventListener('click', function () {
    // Toggle: a second click, with its own preset still the whole filter, takes
    // the narrowing back off rather than re-applying it.
    if (activeView === 'stacks' && roster.updateFilterPresetActive()) {
      clearRosterFilter(true);
      return;
    }
    if (activeView !== 'stacks') {
      activeView = 'stacks';
      localStorage.setItem('activeView', activeView);
      setViewOptions(false);
      applyView();
    }
    presetRosterUpdateFilter();
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
    const att = attentionStacks(S.healthSnap);
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

  logs.renderLogQuickChips(); // paint the persisted filter before the first lines land
  logs.applyFollow();
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
    if (logs.handleDiffToggle(e.target)) return;

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

  // ── Logs view controls ── live in app-logs.js; wired here, at the spot the
  // block occupied, so its document-level key handler keeps its place in line.
  logs.initControls();

  document.addEventListener('keydown', function (e) {
    if (e.key === 'Escape') clog.escape();
  });
})();
