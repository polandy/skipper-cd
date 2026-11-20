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
  const { createLoadError, fetchDiffs, populateIcon } = App.panels;
  const { hostChip, hostChipKeydown, toggleHostFilterTo, peerRecord } = App.hosts;
  const { healthMapFor, repoWebURLFor } = App.resolve;
  const detail = App.detail;
  const VIEW = DETAIL_VIEW.deploys;

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
  // rowEvents keeps each local row's latest event for its detail panel.
  const rowEvents = new WeakMap();
  // isDeploying answers the roster's question about the deploys table's state.
  function isDeploying(stack) {
    return !!deployingRows[stack];
  }
  // Queued (paused) rows, keyed by stack, so a stack's pending row is replaced
  // rather than duplicated, and removed once it deploys or drains.
  const queuedRows = {};
  let hasRows = false;

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
  // for the row's cause line (deployRowCause).
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

  // The entrance plays once. Left on, `new-row` replays it whenever the row is
  // shown again — a view switch or a filter un-hiding it — and a row that is
  // mid-animation has no stable geometry. Cancelled counts too: a row hidden
  // before its entrance finished has nothing left to announce.
  function dropEntranceOnceSettled(row) {
    const settle = (e) => {
      if (e.target !== row) return;
      row.classList.remove('new-row');
      row.removeEventListener('animationend', settle);
      row.removeEventListener('animationcancel', settle);
    };
    row.addEventListener('animationend', settle);
    row.addEventListener('animationcancel', settle);
  }

  function createRow(evt, isHistory) {
    const row = document.createElement('div');
    row.className = rowClass(isHistory);
    if (!isHistory) dropEntranceOnceSettled(row);
    row.dataset.testid = 'deploy-row';
    row.dataset.stack = evt.stack;
    row.dataset.status = evt.status;
    row.dataset.eventId = evt.id;
    row.dataset.host = S.selfHost; // local rows belong to the primary host
    if (evt.has_diffs) row.dataset.hasDiffs = '1';

    // A healed row applied no change; others stash theirs for the live view-options toggle.
    if (evt.status !== 'healed') {
      stashChanges(row, evt);
    }
    // Rendered from the stash, like the settle path below: one source for the
    // cell, so a healed row's empty stash is the only thing that empties it.
    const delta = S.imageDeltaOn ? stashedChangeCellHTML(row) : '';
    row.innerHTML = tableRowHTML(ROW_KIND.deploy, {
      host: hostChip(S.selfHost),
      stack: evt.stack,
      cause: deployRowCause(evt.status, evt.error, pendingReason(evt.stack)),
      ts: evt.timestamp,
      absolute: S.absoluteTime,
      version: delta,
      state: badgeHTML(evt.status) + repeatNoteHTML(evt),
      durationMs: evt.duration_ms,
      commit: '',
      repoBase: S.repoWebURL,
    });

    populateIcon(row.querySelector('.stack-icon'), evt.stack);
    rowEvents.set(row, evt);
    return row;
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

  // refreshPendingCauses re-renders pending rows' causes once the (unordered) queue snapshot lands.
  function refreshPendingCauses() {
    Object.keys(queuedRows).forEach(function (stack) {
      const row = queuedRows[stack];
      const evt = rowEvents.get(row);
      setCauseLine(row, deployRowCause(row.dataset.status, evt && evt.error, pendingReason(stack)));
    });
  }

  // setRowCommit writes the commit, known once the diff loaded, into the When cell.
  function setRowCommit(row, sha) {
    if (row.dataset.commit === sha) return;
    row.dataset.commit = sha;
    const when = row.querySelector('.cell-when');
    const old = when.querySelector('.cell-sha');
    if (old) old.remove();
    when.insertAdjacentHTML('beforeend', deployShaHTML(sha, S.repoWebURL));
  }

  // setCauseLine replaces (or clears) a row's cause line in place.
  function setCauseLine(row, text) {
    const who = row.querySelector('.cell-who');
    const old = who.querySelector('.cell-cause');
    if (old) old.remove();
    if (!text) return;
    const line = document.createElement('span');
    line.className = 'cell-cause';
    line.dataset.testid = 'cause-line';
    line.title = text;
    line.textContent = text;
    who.appendChild(line);
  }

  // removeQueuedRow drops a stack's pending (queued) row and its open detail,
  // so a drained row never strands a panel in the table. No-op when the stack
  // has no queued row.
  function removeQueuedRow(stack) {
    const row = queuedRows[stack];
    if (!row) return;
    detail.closeIf(VIEW, row);
    row.remove();
    delete queuedRows[stack];
  }

  // A repeat collapses into the event it absorbed (ADR-0056), so the incoming
  // event replaces the row already on screen rather than adding to it; the
  // caller drops it once the new row stands, so an open detail moves along.
  function supersededRow(evt) {
    if (!evt.supersedes_id) return null;
    return tbody.querySelector(
      '.event-row:not(.peer-row)[data-event-id="' + evt.supersedes_id + '"]',
    );
  }

  // replaceRow moves prev's open detail onto row, then drops prev.
  function replaceRow(prev, row) {
    if (!prev) return;
    detail.rebind(VIEW, prev, row);
    prev.remove();
  }

  function handleEvent(evt, isHistory) {
    handleEventRow(evt, isHistory);
    scheduleRefold();
  }

  function handleEventRow(evt, isHistory) {
    // A skipped deploy means an unchanged stack — no signal worth showing, so
    // it is never rendered (skipped events are live-only, never in history).
    // A held one repeats the failure row already shown every tick; the roster
    // carries the standing hold (ADR-0062).
    if (evt.status === 'skipped' || evt.status === 'held') return;

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
    const prev = supersededRow(evt);

    if (evt.status === 'deploying') {
      const row = createRow(evt, isHistory);
      tbody.insertBefore(row, tbody.firstChild);
      replaceRow(prev, row);
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
      // Never re-add `new-row` here: the row already made its entrance, and
      // restoring the class would replay it as the deploy settles.
      if (isHistory) existing.classList.remove('new-row');
      existing.dataset.status = evt.status;
      existing.dataset.eventId = evt.id;
      if (evt.has_diffs) existing.dataset.hasDiffs = '1';
      // The terminal event alone knows whether this success retries a rollback,
      // so the note lands as the row settles (like the Version column below).
      existing.querySelector('.status-cell').innerHTML =
        badgeHTML(evt.status) + repeatNoteHTML(evt);
      existing.querySelector('.cell-when').outerHTML = deployWhenHTML({
        ts: evt.timestamp,
        absolute: S.absoluteTime,
        durationMs: evt.duration_ms,
        commit: existing.dataset.commit,
        repoBase: S.repoWebURL,
      });
      setCauseLine(existing, deployRowCause(evt.status, evt.error));
      // The terminal event carries image_changes (the preceding deploying event
      // does not), so fill the Changes column as the row settles — otherwise a
      // live deploy would show it only after a reload.
      stashChanges(existing, evt);
      const versionCell = existing.querySelector('.col-version');
      if (versionCell)
        versionCell.innerHTML = S.imageDeltaOn ? stashedChangeCellHTML(existing) : '';
      rowEvents.set(existing, evt);
      detail.rebind(VIEW, existing, existing); // an open detail shows the outcome
      replaceRow(prev, existing);
      delete deployingRows[evt.stack];
      updateDeployingIndicator();
      return;
    }

    const row = createRow(evt, isHistory);
    tbody.insertBefore(row, tbody.firstChild);
    replaceRow(prev, row);
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

  // ── Fold + day separators (computed per render, never stored) ──

  // expandedFolds holds the keys of folds the user opened; a fold's key is its
  // oldest member's event id, which new rows landing on top leave alone.
  const expandedFolds = new Set();

  function deployRowChangeKind(row) {
    if (row.dataset.imageChanges) return 'image';
    return row.dataset.fileChanges ? 'files' : 'none';
  }

  function rowTimestamp(row) {
    const cell = row.querySelector('.cell-time');
    return cell ? cell.dataset.ts : '';
  }

  function foldLineInfo(run, rows, nowMs) {
    const lead = rows[run.lead];
    const oldest = rows[run.members[run.members.length - 1]];
    const label = deployFoldLabel(
      run.members.length,
      lead.status,
      lead.stack,
      rowTimestamp(oldest.el),
      rowTimestamp(lead.el),
      nowMs,
    );
    return { label: label, status: lead.status };
  }

  // refoldDeploys rebuilds the day separators and fold lines over the rows the
  // filters left visible, so a filter acts before the fold.
  function refoldDeploys() {
    tbody.querySelectorAll('.day-sep, .fold-row').forEach(function (el) {
      el.remove();
    });
    const nowMs = Date.now();
    const all = tbody.querySelectorAll(':scope > [data-testid="deploy-row"]');
    all.forEach(function (el) {
      el.classList.remove('fold-hidden');
    });
    const rows = [];
    all.forEach(function (el) {
      if (el.classList.contains('filtered-out') || el.classList.contains('host-hidden')) return;
      rows.push({
        el: el,
        stack: el.dataset.stack,
        host: el.dataset.host,
        status: el.dataset.status,
        kind: deployRowChangeKind(el),
        key: el.dataset.eventId || '',
        open: detail.isOpen(VIEW, el),
      });
    });
    const runs = foldDeployRows(rows, expandedFolds);
    runs.forEach(function (run) {
      const info = foldLineInfo(run, rows, nowMs);
      const line = document.createElement('button');
      line.type = 'button';
      line.className = 'fold-row';
      line.dataset.testid = 'deploy-fold';
      line.dataset.status = info.status;
      line.dataset.foldKey = run.key;
      line.setAttribute('aria-expanded', run.expanded ? 'true' : 'false');
      line.title = run.expanded ? 'Fold these deploys again' : 'Show these deploys';
      line.textContent = info.label;
      rows[run.members[0]].el.before(line);
      if (!run.expanded) {
        run.members.forEach(function (m) {
          rows[m].el.classList.add('fold-hidden');
        });
      }
    });
    let day = '';
    rows.forEach(function (r) {
      if (r.el.classList.contains('fold-hidden')) return;
      const ts = rowTimestamp(r.el);
      if (!ts || deployDayKey(ts) === day) return;
      day = deployDayKey(ts);
      const sep = document.createElement('div');
      sep.className = 'day-sep';
      sep.dataset.testid = 'deploy-day';
      sep.textContent = deployDayLabel(ts, nowMs);
      r.el.before(sep);
    });
  }

  // scheduleRefold coalesces the burst of rows a history replay adds into one pass.
  let refoldQueued = false;
  function scheduleRefold() {
    if (refoldQueued) return;
    refoldQueued = true;
    queueMicrotask(function () {
      refoldQueued = false;
      refoldDeploys();
    });
  }

  function toggleFold(line) {
    const key = line.dataset.foldKey;
    if (expandedFolds.has(key)) expandedFolds.delete(key);
    else expandedFolds.add(key);
    refoldDeploys();
  }

  // ── Deploys view controls: time mode, version column, stack filter ──

  // Time mode (default: relative). One shared mode; a toggle in both the Deploys
  // and Stacks popovers flips it and updates both views.
  const timeModeBtns = [
    document.getElementById('time-mode'),
    document.getElementById('roster-time-mode'),
  ];

  function setTimeCell(cell, ts) {
    const t = timeLabelPair(ts, S.absoluteTime);
    cell.textContent = t.text;
    cell.title = t.title;
  }

  function applyTimeMode() {
    timeModeBtns.forEach(function (btn) {
      if (!btn) return;
      btn.classList.toggle('active', S.absoluteTime);
      btn.title = S.absoluteTime ? 'Switch to relative time' : 'Switch to absolute time';
    });
    tbody.querySelectorAll('.cell-time').forEach(function (cell) {
      const ts = cell.dataset.ts;
      if (!ts) return;
      setTimeCell(cell, ts);
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
    detail.relayout(); // a side panel follows its row out of view
    scheduleRefold();
  }

  // Re-run the filter after live/replayed rows land so new stacks obey it too
  // — and keep the chip counts current while the bar is open.
  function refilterDeploys() {
    if (deployFilterWrap.classList.contains('has-value') || deployStatusFilter.length > 0) {
      applyDeployFilter();
    } else {
      if (deployFilterWrap.classList.contains('revealed')) renderDeployStatusChips();
      scheduleRefold();
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

  // openRollbackFor is the Open rollback verdict action (UI_SPEC.md "Rollback
  // linkage"): flash the superseded rollback's own row while its event is
  // still rendered, else open the stack's history in Stacks — the durable
  // record the rollback always keeps.
  function openRollbackFor(stack, host, rollbackId) {
    if (rollbackId) {
      const escaped = window.CSS && CSS.escape ? CSS.escape(rollbackId) : rollbackId;
      const target = tbody.querySelector(`.event-row:not(.peer-row)[data-event-id="${escaped}"]`);
      if (target) {
        App.chrome.flashRow(target);
        return;
      }
    }
    showInStacks(stack, host, STACK_TAB.history);
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
    refreshPendingCauses();
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
    // The side column holds the open detail on a wide screen; it takes the
    // list's delegation so the panel works the same in either place.
    [tbody, document.getElementById('deploy-side')].forEach(function (host) {
      host.addEventListener('keydown', hostChipKeydown); // T2.10 keyboard host chip
      host.addEventListener('keydown', function (e) {
        detail.rowKeydown(VIEW, e);
      });
      host.addEventListener('click', onListClick);
    });
  }

  // onListClick is the click delegation of the deploy list and its side column.
  function onListClick(e) {
    const fold = e.target.closest('.fold-row');
    if (fold) {
      toggleFold(fold);
      return;
    }
    if (e.target.closest('.sha-link')) return; // the commit link opens the forge; no panel toggle
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

    // The deploy detail's head and verdict actions; its tabs are its own.
    const act = e.target.closest('.detail-panel [data-action]');
    if (act) {
      deployPanelAction(act);
      return;
    }
    if (e.target.closest('.detail-panel')) return;

    // Hook-log icon: a .clog-btn, so match it before the container-logs handler.
    const hookLog = e.target.closest('.hook-log-btn');
    if (hookLog) {
      App.clog.openHookLog(hookLog, hookLog.dataset.hookLog);
      return;
    }

    // Anywhere else on the row: the deploy detail.
    const row = e.target.closest('.event-row');
    if (row) detail.toggle(VIEW, row, DETAIL_TAB.deploy, { tab: null });
  }
  // deployPanelAction runs a deploy detail's head or verdict action.
  function deployPanelAction(act) {
    const panel = act.closest('.detail-panel');
    switch (act.dataset.action) {
      case 'jump':
        showInStacks(act.dataset.jumpStack, panel.dataset.host, null);
        break;
      case 'close':
        detail.close(VIEW);
        break;
      case 'logs':
        App.clog.toggle(act);
        break;
      case 'rollback':
        openRollbackFor(panel.dataset.stack, panel.dataset.host, act.dataset.rollbackId);
        break;
    }
  }

  // showInStacks switches to the Stacks view with stack's detail open.
  function showInStacks(stack, host, tab) {
    App.chrome.jumpToStack(DETAIL_VIEW.stacks, stack);
    App.roster.showDetail(stack, host, tab);
  }

  // eventFor is the event a row's detail shows: a local row's latest, or a
  // peer row's audit record.
  function eventFor(row) {
    return row.classList.contains('peer-row') ? peerRecord(row) : rowEvents.get(row);
  }

  // loadDeployDiff fetches a deploy's diff — a peer's through the primary's
  // proxy — and calls back with (diffs, commits, err). A peer that evicted the
  // event answers 404: no diff, not an error.
  function loadDeployDiff(row, evt, cb) {
    if (!row.classList.contains('peer-row')) {
      fetchDiffs(evt.id, cb);
      return;
    }
    const host = row.dataset.host;
    fetch(
      '/api/peers/' + encodeURIComponent(host) + '/events/' + encodeURIComponent(evt.id) + '/diffs',
    )
      .then(function (r) {
        if (r.ok) return r.json();
        if (r.status >= 500) throw new Error('unreachable');
        return null;
      })
      .then(function (data) {
        cb((data && data.diffs) || null, null, null);
      })
      .catch(function (err) {
        cb(null, null, err);
      });
  }

  // The deploy detail: one panel per deploy row, in the Stacks detail's
  // anatomy — head, verdict, facts, then the Changes/Drift · Output · Commits
  // tabs. The diff loads lazily; until it lands the commit reads —, and a load
  // error stays inside the Changes tab with a Retry.
  detail.registerPane(DETAIL_TAB.deploy, {
    openClass: 'audit-open',
    build: function (row, slot, opts) {
      const evt = eventFor(row);
      if (!evt) return;
      const peer = row.classList.contains('peer-row');
      const host = row.dataset.host;
      const logHost = peer ? host : '';
      const repoBase = peer ? repoWebURLFor(host) : S.repoWebURL;
      let health = healthMapFor(logHost)[evt.stack];
      const d = {
        diffs: null,
        commits: [],
        loading: false,
        error: null,
        expanded: false,
        repoBase: repoBase,
      };
      const panel = document.createElement('div');
      panel.className = 'detail-panel' + (peer ? ' peer-panel' : '');
      panel.dataset.testid = 'detail-panel';
      panel.dataset.kind = 'deploy';
      panel.dataset.stack = evt.stack;
      panel.dataset.host = peer ? host : S.selfHost;
      panel.dataset.status = evt.status;

      // shown is the markup last rendered, so a refresh that changes nothing
      // leaves the panel (and its focus and scroll) alone.
      let shown = '';
      const render = function () {
        const tabs = deployTabs(evt, { commits: d.commits.length });
        const offered = function (t) {
          return tabs.some(function (x) {
            return x.id === t;
          });
        };
        if (!offered(opts.tab)) opts.tab = tabs.length ? tabs[0].id : null;
        const html = deployPanelHTML(evt, {
          host: hostChip(host),
          logs: !!(health && health.services && health.services.length),
          logHost: logHost,
          jump: !isPseudoStack(evt.stack),
          nowMs: Date.now(),
          repoBase: repoBase,
          commits: d.commits,
          queueReason: pendingReason(evt.stack),
          tab: opts.tab,
        });
        const loadError = opts.tab === DEPLOY_TAB.changes && d.error;
        const paneHTML = loadError ? '' : deployPaneHTML(opts.tab, evt, d);
        const next = html + '\0' + (loadError ? d.error : paneHTML);
        if (next === shown) return;
        shown = next;
        panel.dataset.tab = opts.tab || '';
        panel.innerHTML = html;
        populateIcon(panel.querySelector('.dp-head .stack-icon'), evt.stack);
        const pane = panel.querySelector('.dp-pane');
        if (loadError) pane.appendChild(createLoadError(d.error, loadDiff));
        else pane.innerHTML = paneHTML;
      };

      // loadDiff fetches the diff once; Retry re-runs it.
      const loadDiff = function () {
        d.loading = true;
        d.error = null;
        render();
        loadDeployDiff(row, evt, function (diffs, commits, err) {
          if (!slot.live()) return; // closed or replaced while loading
          d.loading = false;
          if (err)
            d.error = peer
              ? 'Couldn’t reach ' + host + ' for the diff.'
              : "Couldn't load the diff.";
          d.diffs = diffs;
          d.commits = commits || [];
          if (!peer && d.commits.length) setRowCommit(row, d.commits[0].sha);
          render();
        });
      };

      panel.addEventListener('click', function (e) {
        const tabBtn = e.target.closest('.dp-tab');
        if (tabBtn) {
          opts.tab = tabBtn.dataset.tab;
          d.expanded = false;
          render();
          return;
        }
        if (e.target.closest('[data-action="output-more"]')) {
          d.expanded = !d.expanded;
          render();
          panel.querySelector('[data-action="output-more"]').focus();
        }
      });
      panel.addEventListener('keydown', function (e) {
        detail.tabKeydown(panel, e);
      });

      slot.onRefresh(function () {
        health = healthMapFor(logHost)[evt.stack];
        render();
      });
      slot.set(panel);
      const hasDiff = peer ? !!evt.id && changedFileCount(evt) > 0 : !!evt.has_diffs;
      if (hasDiff) loadDiff();
      else render();
    },
  });

  return {
    isDeploying,
    showTable,
    settleInitialState,
    initialStateIsSettled,
    updateDeployingIndicator,
    setRunDrawer,
    applyQueueSnapshot,
    applyOrphansSnapshot,
    handleEvent,
    insertRowByTime,
    refilterDeploys,
    scheduleRefold,
    clearDeployFilter,
    presetDeployStatusFilter,
    filterRevealed,
    openFilter,
    incidentPresetIsActive,
    init,
  };
})();
