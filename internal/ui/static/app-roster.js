// app-roster.js — the Stacks view (dev-docs/stack-roster-spec.md): the roster
// of every declared stack with its last outcome and live health, the peer
// roster rows, the disabled-stacks strip, the row-click card, and the stack
// search with its updates-only preset. Cut out of app.js by view (ADR-0035
// amendment) and attached as App.roster.
//
// Loads after app-panels.js and app-hosts.js (imported at load) and before
// app.js, so what it needs from the chrome, the deploys table and the stream
// (App.chrome, App.deploys, App.stream) is read at call time. app.js calls
// init() at the spot the view's listeners used to register, so the stacks
// type-to-search keeps its place behind the deploys one.
App.roster = (function () {
  const S = App.state;
  const { repoWebURLFor, stackUpdatesFor, stackHealthFor } = App.resolve;
  const {
    linkCell,
    toggleAppLinkPopover,
    closeAppLinkPopover,
    toggleMoreMenu,
    closeMoreMenu,
    createHooksPanel,
    closeHooksPanel,
    populateIcon,
    createAuditPanel,
    createWatchedPanel,
    createHealthPanel,
  } = App.panels;
  const {
    hostChip,
    hostChipKeydown,
    togglePeerDetail,
    openPeerDetail,
    applyHostFilter,
    toggleHostFilterTo,
  } = App.hosts;

  // ── Disabled-stacks strip (ADR-0034) ──

  function renderDisabledStacks() {
    const wrap = document.getElementById('disabled-stacks');
    const list = document.getElementById('disabled-list');
    list.textContent = '';
    if (!S.disabledSnap.length) {
      wrap.classList.remove('shown');
      return;
    }
    S.disabledSnap.forEach(function (name) {
      const chip = document.createElement('span');
      chip.className = 'dis-chip';
      chip.textContent = name;
      list.appendChild(chip);
    });
    wrap.classList.add('shown');
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
    closeAppLinkPopover(); // rebuilt rows drop any open app-link popover too
    // Attention-ranked, stable (rosterOrdered, app-helpers.js): unhealthy first,
    // backend order kept within each group. Applied only here, at full render —
    // never on a live health poll — so rows never jump under an open panel.
    rosterOrdered(S.rosterSnap, S.healthSnap).forEach(function (entry) {
      const deploying = App.deploys.isDeploying(entry.name);
      // Time + commit only apply to a real past deploy.
      const showMeta = !entry.disabled && !deploying && !!entry.last_status;
      // Shared time mode; title carries the opposite (relative <-> absolute).
      const when =
        showMeta && entry.last_at
          ? S.absoluteTime
            ? fullTime(entry.last_at)
            : formatTime(entry.last_at)
          : '';
      const whenTitle =
        showMeta && entry.last_at
          ? S.absoluteTime
            ? formatTime(entry.last_at)
            : fullTime(entry.last_at)
          : '';
      const commit = showMeta ? entry.last_commit || '' : '';
      const row = document.createElement('div');
      row.className = entry.disabled ? 'roster-row disabled' : 'roster-row';
      row.dataset.testid = 'roster-row';
      row.dataset.stack = entry.name;
      row.dataset.host = S.selfHost; // local stacks belong to the primary host
      // The updates-only filter's flag (UI_SPEC.md "Updates filter") — the
      // check rides the same 'stacks' snapshot that rebuilds these rows.
      if (stackHasUpdate(stackUpdatesFor(entry.name, ''))) row.dataset.updates = '1';
      // Mark the row with its live health so CSS can give an unhealthy row the
      // same severity bar + tint as a failed deploy row (kept in sync live by
      // updateRosterHealth). Only enabled locals — disabled stacks aren't polled.
      const rowHealth = entry.disabled ? null : S.healthSnap[entry.name];
      if (rowHealth && rowHealth.status) row.dataset.health = rowHealth.status;
      row.innerHTML =
        `<span class="roster-stack"><span class="roster-ident">${hostChip(S.selfHost)}<span class="stack-icon" data-testid="stack-icon"></span><span class="roster-name" title="${escapeAttr(entry.name)}">${escapeHtml(entry.name)}</span></span>${rowActionClusterHTML(
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
        `<span class="roster-status">${rosterStatusHTML(entry, App.deploys.isDeploying(entry.name))}${entry.disabled ? '' : rosterHealthPillHTML(entry.name, rowHealth)}${entry.disabled ? '' : outcomeStripHTML(entry.recent, Date.now()) + lastIncidentHTML(entry.last_incident, Date.now())}</span>` +
        `<span class="roster-when"${whenTitle ? ` title="${escapeAttr(whenTitle)}"` : ''}>${escapeHtml(when)}</span>` +
        commitLinkHTML(commit, { cls: 'roster-sha', base: S.repoWebURL, title: commit });
      populateIcon(row.querySelector('.stack-icon'), entry.name);
      list.appendChild(row);
    });
    renderPeerRosterRows(list); // append each peer's stacks, host-tagged + read-only
    reopenRosterPanel(list, reopen); // the open panel returns, carrying the new snapshot
    applyHostFilter(); // dots + host filtering on the rebuilt roster
    refilterRoster(); // a re-render replaces the rows; re-apply any active filter
    App.stream.applyHookRun(); // re-paint a running hook's phase on the rebuilt roster rows
    App.chrome.renderUpdateBadge(); // the badge counts these rows, so it follows them
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
    if (!S.peersSnap) return;
    (S.peersSnap.peers || []).forEach(function (p) {
      const roster = (p.state && p.state.stacks && p.state.stacks.roster) || [];
      const peerRepo = repoWebURLFor(p.name); // a peer's commits live on its own forge
      roster.forEach(function (entry) {
        const showMeta = !entry.disabled && !!entry.last_status;
        const when =
          showMeta && entry.last_at
            ? S.absoluteTime
              ? fullTime(entry.last_at)
              : formatTime(entry.last_at)
            : '';
        const whenTitle =
          showMeta && entry.last_at
            ? S.absoluteTime
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
        if (stackHasUpdate(stackUpdatesFor(entry.name, p.name))) row.dataset.updates = '1';
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

  // ── Stacks view: click-a-row-for-history + search (mirrors the deploys filter) ──
  const rosterList = document.getElementById('roster-list');
  const rosterFilterWrap = document.getElementById('roster-filter-wrap');
  const rosterFilter = document.getElementById('roster-filter');
  const rosterFilterClear = document.getElementById('roster-filter-clear');
  const rosterFilterCount = document.getElementById('roster-filter-count');
  const rosterFilterEmpty = document.getElementById('roster-filter-empty');
  const rosterUpdateFilterEl = document.getElementById('roster-update-filter');

  // Updates-only narrowing (UI_SPEC.md "Updates filter"). One toggle, not a
  // chip per status: an update is a single yes/no fact about a stack. Like the
  // Deploys status chips it is deliberately NOT persisted — a sticky filter
  // hiding rows across sessions is the failure mode the Logs view's persisted
  // severity chips demonstrated.
  let rosterUpdatesOnly = false;

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
  // the current S.appLinksSnap, without a full renderRoster() rebuild — app_links
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
      const h = S.healthSnap[row.dataset.stack];
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

  // openRosterDetail opens a local row's bound panels as one card. Shared with
  // the re-open after a roster rebuild, so a restored panel is built exactly
  // like a clicked one.
  function openRosterDetail(row) {
    row.classList.add('audit-open');
    const stack = row.dataset.stack;
    // Containers (health, if known) above deploy history, bound as one card.
    let anchor = row;
    const h = S.healthSnap[stack];
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

  // countRosterUpdates counts the roster rows a registry update is waiting on,
  // skipping rows a host filter has taken out of view — the badge and the chip
  // must both promise exactly the rows the filter can hand back.
  function countRosterUpdates() {
    return rosterList.querySelectorAll('.roster-row[data-updates="1"]:not(.host-hidden)').length;
  }

  function renderRosterUpdateChip() {
    rosterUpdateFilterEl.innerHTML = rosterUpdateChipHTML(countRosterUpdates(), rosterUpdatesOnly);
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
        // The name query and the updates toggle narrow independently; a row
        // shows only when it passes both.
        visible =
          el.dataset.stack.toLowerCase().indexOf(q) !== -1 &&
          (!rosterUpdatesOnly || el.dataset.updates === '1');
        if (visible) shown++;
      }
      // A trailing history panel shares its row's visibility.
      el.classList.toggle('filtered-out', !visible);
    }
    const narrowing = q.length > 0 || rosterUpdatesOnly;
    rosterFilterCount.textContent = narrowing ? shown + '/' + total : '';
    rosterFilterEmpty.classList.toggle('show', narrowing && total > 0 && shown === 0);
    // With the updates toggle on, "no stack matches <query>" would blame the
    // name for rows the toggle hid.
    if (rosterUpdatesOnly) {
      rosterFilterEmpty.textContent = q
        ? 'Nothing matches the active filters.'
        : 'No stack has an update available.';
    } else {
      rosterFilterEmpty.innerHTML = 'No stack matches “<span id="roster-filter-empty-q"></span>”.';
      document.getElementById('roster-filter-empty-q').textContent = q;
    }
    renderRosterUpdateChip();
    App.chrome.renderUpdateBadge();
  }
  // Re-run after the roster re-renders (a new snapshot) so new stacks obey it too.
  function refilterRoster() {
    if (rosterFilterWrap.classList.contains('has-value') || rosterUpdatesOnly) {
      applyRosterFilter();
    } else if (rosterFilterWrap.classList.contains('revealed')) {
      renderRosterUpdateChip();
    }
  }
  function revealRosterFilter(on) {
    rosterFilterWrap.classList.toggle('revealed', on);
    if (on) renderRosterUpdateChip();
    App.chrome.syncStackSearchBtn();
  }
  function clearRosterFilter(hide) {
    rosterFilter.value = '';
    rosterUpdatesOnly = false; // Esc/clear drops the toggle with the query
    applyRosterFilter();
    if (hide) {
      revealRosterFilter(false);
      rosterFilter.blur();
    }
  }

  // presetRosterUpdateFilter is the update badge's landing (UI_SPEC.md
  // "Updates filter"): reveal the bar with the toggle on and the name query
  // cleared, so the click lands on exactly the stacks the count promised.
  function presetRosterUpdateFilter() {
    rosterFilter.value = '';
    rosterUpdatesOnly = true;
    revealRosterFilter(true);
    applyRosterFilter();
  }

  // Live update of a single roster row's status (in-flight → settled) without a
  // full re-render, so an open history panel survives a deploy event.
  function refreshRosterRow(name) {
    const row = rosterList.querySelector(
      `.roster-row[data-stack="${window.CSS && CSS.escape ? CSS.escape(name) : name}"]:not(.peer-row)`,
    );
    if (!row) return;
    const entry = S.rosterSnap.find(function (x) {
      return x.name === name;
    });
    const cell = row.querySelector('.roster-status');
    if (entry && cell) cell.innerHTML = rosterStatusHTML(entry, App.deploys.isDeploying(name));
    App.stream.applyHookRun(); // rosterStatusHTML replaces the cell — re-paint a running hook's phase
  }

  // The three questions the header chrome asks the filter — kept as calls so
  // the chrome never reads this view's elements or state directly.
  function filterRevealed() {
    return rosterFilterWrap.classList.contains('revealed');
  }
  function openFilter() {
    revealRosterFilter(true);
    rosterFilter.focus();
  }
  function updateFilterPresetActive() {
    return updatePresetActive(rosterUpdatesOnly, rosterFilter.value);
  }

  // init wires the roster list, the search bar and the stacks type-to-search —
  // everything the view ran at load.
  function init() {
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
        App.chrome.jumpToStack(jumpBtn.dataset.jumpView, jumpBtn.dataset.jumpStack);
        return;
      }
      // Hook-log icon: a .clog-btn, so match it before the container-logs handler.
      const hookLog = e.target.closest('.hook-log-btn');
      if (hookLog) {
        App.clog.openHookLog(hookLog, hookLog.dataset.hookLog);
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
        App.clog.toggle(clogB);
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

    // Chip click: the single updates-only toggle.
    rosterUpdateFilterEl.addEventListener('click', function (e) {
      if (!e.target.closest('.status-chip')) return;
      rosterUpdatesOnly = !rosterUpdatesOnly;
      applyRosterFilter();
    });

    rosterFilter.addEventListener('input', applyRosterFilter);

    rosterFilter.addEventListener('keydown', function (e) {
      if (e.key === 'Escape') {
        e.stopPropagation();
        if (rosterFilter.value || rosterUpdatesOnly)
          clearRosterFilter(false); // first Esc clears query and toggle together
        else {
          revealRosterFilter(false);
          rosterFilter.blur();
        } // second Esc folds away
      }
    });

    rosterFilter.addEventListener('blur', function (e) {
      // A click on the chip blurs the input BEFORE the click lands; folding here
      // would collapse the bar mid-click and take the chip with it.
      if (e.relatedTarget && rosterUpdateFilterEl.contains(e.relatedTarget)) return;
      // Fold away only when nothing narrows — folding with the toggle on would
      // leave rows hidden by a filter that is no longer on screen.
      if (!rosterFilter.value && !rosterUpdatesOnly) revealRosterFilter(false);
    });

    rosterFilterClear.addEventListener('click', function () {
      clearRosterFilter(false);
      rosterFilter.focus();
    });

    // Mobile entry point: the "Search stacks" row in the stacks view-options popover.
    document.getElementById('roster-search-open').addEventListener('click', function () {
      App.chrome.setViewOptions(false);
      revealRosterFilter(true);
      rosterFilter.focus();
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
      if (App.chrome.activeView() !== 'stacks' || !S.rosterSnap.length) return;
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
  }

  return {
    renderDisabledStacks,
    renderRoster,
    refreshPeerRosterRows,
    refilterRoster,
    updateAppLinks,
    updateRosterHealth,
    refreshRosterRow,
    clearRosterFilter,
    presetRosterUpdateFilter,
    countRosterUpdates,
    filterRevealed,
    openFilter,
    updateFilterPresetActive,
    init,
  };
})();
