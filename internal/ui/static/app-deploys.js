// app-deploys.js — the Deploys view: the deploy table and its rows, the
// skeleton/empty/loaded states, the deploying indicator and run drawer, the
// deploy-event ingest (queued rows, handleEvent), the time-mode / version /
// stack-filter controls with the status chips, the orphan projects section
// below the table, and the row-click delegation that opens the panels. Cut out
// of the app script by view (ADR-0035 amendment) and attached as App.deploys.
//
// Loads after app-panels.js and app-hosts.js (imported at load) and before
// the stream, so what it needs from the chrome, the stream and the other views
// (App.chrome, App.stream, App.roster, App.logs) is read at call
// time. The bootstrap calls init() after the surface registry exists, at the spot the
// run drawer used to register — every load-time statement of the view runs
// there, in its original order, so the deploys type-to-search keeps its place
// between the panels' Escape handler and the stacks one.
App.deploys = (function () {
  const S = App.state;
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
    populateIcon,
    closeHealthPanel,
    closeDiffPanel,
    closeAuditPanel,
    createAuditPanel,
    createHealthPanel,
  } = App.panels;
  const { hostChip, hostChipKeydown, togglePeerDetail, toggleHostFilterTo } = App.hosts;

  const tbody = document.getElementById('tbody');
  const table = document.getElementById('deploy-table');
  const emptyState = document.getElementById('empty-state');
  const loadingState = document.getElementById('loading-state');

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
      App.stream.clearOfflineNotice(); // after the flag, so it does not restore the skeleton
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
    App.stream.clearOfflineNotice();
    if (!hasRows) emptyState.style.display = '';
  }

  // ── Deploy rows: stack icons, row build, per-stack affordances ──

  // pendingReason draws a stack's pause/block reason from the queue snapshot
  // for pendingTagHTML (app-render.js).
  function pendingReason(stack) {
    const item = S.queueByStack[stack];
    return item && item.reason;
  }

  // stashChanges keeps the row's own copy of what the event said changed, so the
  // Changes column can be rebuilt (view-option toggle, a terminal event settling
  // over a deploying row) without the event. Written from every event that
  // reaches it, deletes included, so a re-emitted one stays idempotent and the
  // toggle can never restore a chip the cell no longer shows.
  function stashChanges(row, evt) {
    if (evt.image_changes && evt.image_changes.length) {
      row.dataset.imageChanges = JSON.stringify(evt.image_changes);
    } else {
      delete row.dataset.imageChanges;
    }
    if (evt.file_changes && evt.file_changes.length) {
      row.dataset.fileChanges = JSON.stringify(evt.file_changes);
    } else {
      delete row.dataset.fileChanges;
    }
  }

  // stashedChangeCellHTML rebuilds a row's Changes column from what it stashed.
  function stashedChangeCellHTML(row) {
    const images = row.dataset.imageChanges ? JSON.parse(row.dataset.imageChanges) : null;
    const files = row.dataset.fileChanges ? JSON.parse(row.dataset.fileChanges) : null;
    return changeCellHTML(images, files);
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
    // A healed row applied no change at all (self-heal re-applies the same
    // version); every other row names which service(s) moved — to a new image,
    // or through a file that carries none — in the Changes column. Stash both
    // lists on the row so the view-options toggle can fill/empty the column live
    // without a reload.
    if (evt.status !== 'healed') {
      stashChanges(row, evt);
    }
    // Rendered from the stash, like the settle path below: one source for the
    // cell, so a healed row's empty stash is the only thing that empties it.
    const delta = S.imageDeltaOn ? stashedChangeCellHTML(row) : '';
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
        // compose project, so it has no container App.logs.
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
    App.stream.applyHookRun(); // re-paint a running hook's phase after rows/affordances changed
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

  // setRunDrawer opens/closes the run panel.
  function setRunDrawer(open) {
    if (open) App.chrome.closeOtherSurfaces(runDrawer);
    runDrawer.classList.toggle('open', open);
    deployStatus.setAttribute('aria-expanded', String(open));
    App.chrome.manageSurfaceFocus(runDrawer, deployStatus, open);
  }

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
    App.chrome.announceOutcome(deployAnnouncement(evt.status, evt.stack));

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
      // does not), so fill the Changes column as the row settles — otherwise a
      // live deploy would show it only after a reload.
      stashChanges(existing, evt);
      const versionCell = existing.querySelector('.col-version');
      if (versionCell)
        versionCell.innerHTML = S.imageDeltaOn ? stashedChangeCellHTML(existing) : '';
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

  // Changes-column toggle (Deploys popover). Reflects the toggle state,
  // collapses the whole column when off (.no-version — an empty column
  // would keep taking width, and its header would lie), and fills/empties every
  // already-rendered row's cell from its stashed image_changes, so flipping it
  // takes effect live without a reload. New rows honour S.imageDeltaOn in
  // createRow / the settle path.
  const imageDeltaBtn = document.getElementById('image-delta-toggle');
  const deployTable = document.getElementById('deploy-table');

  function applyImageDelta() {
    if (imageDeltaBtn) {
      imageDeltaBtn.classList.toggle('active', S.imageDeltaOn);
      imageDeltaBtn.title = S.imageDeltaOn ? 'Hide the Changes column' : 'Show the Changes column';
    }
    if (deployTable) deployTable.classList.toggle('no-version', !S.imageDeltaOn);
    tbody.querySelectorAll('.event-row[data-stack]').forEach(function (row) {
      const cell = row.querySelector('.col-version');
      if (!cell) return;
      cell.innerHTML = S.imageDeltaOn ? stashedChangeCellHTML(row) : '';
    });
  }

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
    App.chrome.syncStackSearchBtn();
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

  // presetDeployStatusFilter is the incident badge's landing (UI_SPEC.md
  // "Incident badge"): reveal the bar with the given chips pre-selected and
  // the name query cleared, so the click lands on exactly the promised rows.
  function presetDeployStatusFilter(statuses) {
    deployFilter.value = '';
    deployStatusFilter = statuses.slice();
    revealDeployFilter(true);
    applyDeployFilter();
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
        App.chrome.flashRow(target);
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

  // ── Deploys table click delegation (row panels) ──

  // What the dispatcher and the chrome ask of this view — kept as calls so
  // neither reads its elements or state directly.
  function initialStateIsSettled() {
    return initialStateSettled;
  }
  // applyQueueSnapshot drops the queued row of every stack that left the
  // pending set without a deploy event (e.g. resumed then found unchanged),
  // then re-renders the pending tags from the snapshot's authoritative reasons.
  function applyQueueSnapshot() {
    Object.keys(queuedRows).forEach(function (stack) {
      if (!S.queueByStack[stack]) removeQueuedRow(stack);
    });
    refreshPendingTags();
  }
  // applyOrphansSnapshot re-runs the filter under an active search so new data
  // obeys the query, otherwise renders plainly.
  function applyOrphansSnapshot() {
    if (deployFilterWrap.classList.contains('has-value')) applyDeployFilter();
    else renderOrphans();
  }
  function filterRevealed() {
    return deployFilterWrap.classList.contains('revealed');
  }
  function openFilter() {
    revealDeployFilter(true);
    deployFilter.focus();
  }
  function incidentPresetIsActive(statuses) {
    return incidentPresetActive(deployStatusFilter, deployFilter.value, statuses);
  }

  // init runs everything the view ran at load: the orphans header, the
  // deploying indicator, the run drawer's surface, the time-mode and version
  // toggles with their initial paint, the filter bar, the deploys
  // type-to-search and the table's click delegation.
  function init() {
    (function () {
      document.getElementById('orphans-head').addEventListener('click', function () {
        orphansSectionOpen = !orphansSectionOpen;
        applyOrphansSectionOpen();
      });
    })();

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

    App.chrome.trapFocus(runDrawer); // asDrawer/hostsDrawer are trapped in their own sections (declared later)

    App.chrome.registerSurface({
      surface: runDrawer,
      isOpen: function () {
        return runDrawer.classList.contains('open');
      },
      close: function () {
        setRunDrawer(false);
      },
      within: [runDrawer, deployStatus],
    });

    timeModeBtns.forEach(function (btn) {
      if (!btn) return;
      btn.addEventListener('click', function () {
        S.absoluteTime = !S.absoluteTime;
        localStorage.setItem('timeMode', S.absoluteTime ? 'absolute' : 'relative');
        applyTimeMode();
        // Kept out of applyTimeMode: its setup-time call runs before the roster
        // block is initialized (would TDZ). Reached only on a click here.
        if (App.chrome.activeView() === 'stacks') App.roster.renderRoster();
      });
    });

    applyTimeMode();

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
      App.chrome.setViewOptions(false);
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
      if (App.chrome.activeView() !== 'deploys' || !hasRows) return;
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
      if (App.logs.handleDiffToggle(e.target)) return;

      // Cross-view jump button: hand off to Stacks. Handled before the files
      // logic so a tap never opens the row's diff panel.
      const jumpBtn = e.target.closest('.jump-btn');
      if (jumpBtn) {
        App.chrome.jumpToStack(jumpBtn.dataset.jumpView, jumpBtn.dataset.jumpStack);
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
        App.clog.openHookLog(hookLog, hookLog.dataset.hookLog);
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
        App.clog.toggle(clogB);
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
            createHealPanel(drift, {
              stack: healRow.dataset.stack,
              status: healRow.dataset.status,
            }),
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
      const meta = {
        stack: row.dataset.stack,
        status: row.dataset.status,
        // Lets both panels name the services each changed file reached.
        fileChanges: row.dataset.fileChanges ? JSON.parse(row.dataset.fileChanges) : null,
      };

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
  }

  return {
    isDeploying,
    showTable,
    settleInitialState,
    initialStateIsSettled,
    updateStackAffordances,
    updateDeployingIndicator,
    setRunDrawer,
    applyQueueSnapshot,
    applyOrphansSnapshot,
    handleEvent,
    insertRowByTime,
    refilterDeploys,
    clearDeployFilter,
    presetDeployStatusFilter,
    filterRevealed,
    openFilter,
    incidentPresetIsActive,
    init,
  };
})();
