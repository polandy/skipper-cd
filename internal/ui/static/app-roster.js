// app-roster.js — the Stacks view (docs/dev/stack-roster-spec.md): the roster
// of every declared stack with its last outcome and live health, the peer
// roster rows, the disabled-stacks strip, the row-click detail panel, and the stack
// search with its updates-only preset. Cut out of the app script by view (ADR-0035
// amendment) and attached as App.roster.
//
// Loads after app-panels.js and app-hosts.js (imported at load) and before
// the stream, so what it needs from the chrome, the deploys table and the stream
// (App.chrome, App.deploys, App.stream) is read at call time. The bootstrap calls
// init() at the spot the view's listeners used to register, so the stacks
// type-to-search keeps its place behind the deploys one.
App.roster = (function () {
  const S = App.state;
  const { repoWebURLFor, stackUpdatesFor, stackHealthFor, appLinksMapFor } = App.resolve;
  const {
    toggleAppLinkPopover,
    closeAppLinkPopover,
    populateIcon,
    createAuditPanel,
    createWatchedPanel,
    createHealthPanel,
    createHooksPanel,
  } = App.panels;
  const { hostChip, hostChipKeydown, applyHostFilter, toggleHostFilterTo } = App.hosts;
  const detail = App.detail;
  const VIEW = DETAIL_VIEW.stacks;

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
    list.textContent = '';
    closeAppLinkPopover(); // rebuilt rows drop any open app-link popover too
    // Attention-ranked, stable (rosterOrdered, app-helpers.js): unhealthy first,
    // backend order kept within each group. Applied only here, at full render —
    // never on a live health poll — so rows never jump under an open panel.
    rosterOrdered(S.rosterSnap, S.healthSnap).forEach(function (entry) {
      const deploying = App.deploys.isDeploying(entry.name);
      const last = rosterLastDeploy(entry, deploying);
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
      row.innerHTML = tableRowHTML(ROW_KIND.roster, {
        host: hostChip(S.selfHost),
        stack: entry.name,
        health: rosterHealthHTML(rowHealth),
        version: rosterVersionCellHTML(
          entry.name,
          rowHealth,
          entry.disabled,
          stackUpdatesFor(entry.name, ''),
        ),
        state: rosterStatusHTML(entry, deploying),
        lastAt: last.at,
        absolute: S.absoluteTime,
        commit: last.commit,
        repoBase: S.repoWebURL,
      });
      populateIcon(row.querySelector('.stack-icon'), entry.name);
      list.appendChild(row);
    });
    renderPeerRosterRows(list); // append each peer's stacks, host-tagged + read-only
    // A `stacks` snapshot lands after every run — exactly when the open panel
    // has something new to say (a history record, a commit) — so the selection
    // re-opens on its rebuilt row, from the new snapshot.
    detail.reapply(VIEW, list.querySelectorAll('.roster-row'));
    applyHostFilter(); // dots + host filtering on the rebuilt roster
    refilterRoster(); // a re-render replaces the rows; re-apply any active filter
    App.stream.applyHookRun(); // re-paint a running hook's phase on the rebuilt roster rows
    App.chrome.renderUpdateBadge(); // the badge counts these rows, so it follows them
  }

  // renderPeerRosterRows appends every peer's stacks to the roster, one row each
  // in the local rows' shape. A click opens the same detail panel, read-only
  // (no retry, no jump), from the peer's fanned-in state. Rows are grouped per
  // host after the local set.
  function renderPeerRosterRows(list) {
    if (!S.peersSnap) return;
    (S.peersSnap.peers || []).forEach(function (p) {
      const roster = (p.state && p.state.stacks && p.state.stacks.roster) || [];
      const peerRepo = repoWebURLFor(p.name); // a peer's commits live on its own forge
      roster.forEach(function (entry) {
        // deploying=false: deployingRows is the primary's own in-flight set,
        // never a peer's.
        const last = rosterLastDeploy(entry, false);
        const health = stackHealthFor(entry.name, p.name);
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
        row.innerHTML = tableRowHTML(ROW_KIND.roster, {
          host: hostChip(p.name),
          stack: entry.name,
          health: entry.disabled ? '' : rosterHealthHTML(health),
          version: rosterVersionCellHTML(
            entry.name,
            health,
            entry.disabled,
            stackUpdatesFor(entry.name, p.name),
          ),
          state: rosterStatusHTML(entry, false),
          lastAt: last.at,
          absolute: S.absoluteTime,
          commit: last.commit,
          repoBase: peerRepo,
        });
        populateIcon(row.querySelector('.stack-icon'), entry.name);
        list.appendChild(row);
      });
    });
  }

  // refreshPeerRosterRows rebuilds only the peer roster rows, leaving local rows
  // and an open local panel intact — the roster's equivalent of renderPeerRows
  // for the deploy feed. An open peer panel is rebuilt from the new snapshot.
  function refreshPeerRosterRows() {
    const list = document.getElementById('roster-list');
    const peerOpen = !!list.querySelector('.detail-panel.peer-panel');
    list.querySelectorAll('.roster-row.peer-row, .detail-panel.peer-panel').forEach(function (r) {
      r.remove();
    });
    renderPeerRosterRows(list);
    if (peerOpen) detail.reapply(VIEW, list.querySelectorAll('.roster-row.peer-row'));
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

  // updateAppLinks re-renders the open local panel's head when the stack's
  // app links changed — app_links updates on the health-poll cadence, far more
  // often than the 'stacks' snapshot that rebuilds the panel.
  function updateAppLinks() {
    const panel = rosterList.querySelector('.detail-panel:not(.peer-panel)');
    if (!panel || panel.dataset.links === linksKey(panel.dataset.stack, '')) return;
    const c = detailContext(panel.dataset.stack, '');
    if (c) renderHead(panel, c, panel.dataset.tab);
  }

  // updateRosterHealth patches each local roster row's Health and Version cells
  // in place from the health snapshot (peer rows refresh via
  // refreshPeerRosterRows), so a health change — or the first poll after the
  // roster rendered — shows without a full re-render, which at poll cadence
  // would tear down and refetch an open panel (see renderRoster).
  function updateRosterHealth() {
    rosterList.querySelectorAll('.roster-row[data-stack]:not(.peer-row)').forEach(function (row) {
      if (row.classList.contains('disabled')) return;
      const h = S.healthSnap[row.dataset.stack];
      const verCell = row.querySelector('.col-version');
      // The versions ride the health snapshot, so the cell is refreshed here
      // rather than only at render — on connect the roster arrives first.
      if (verCell)
        verCell.innerHTML = rosterVersionInnerHTML(
          row.dataset.stack,
          h,
          stackUpdatesFor(row.dataset.stack, ''),
        );
      const healthCell = row.querySelector('.roster-health');
      if (healthCell) healthCell.innerHTML = rosterHealthHTML(h);
      // Keep the row's health marker (drives the severity bar + tint) in sync
      // in place — no re-sort, so a row never jumps out from under an open panel.
      if (h && h.status) row.dataset.health = h.status;
      else row.removeAttribute('data-health');
    });
  }

  // ── Detail panel (docs/dev/ui-redesign-plan.md §3.2) ──

  // detailContext resolves what a stack's detail panel shows on a host ('' for
  // local): its roster entry, health and updates, and for a peer its fanned-in
  // history and forge. null once the stack left the snapshot.
  function detailContext(stack, host) {
    const byName = function (r) {
      return r.name === stack;
    };
    let entry,
      peer = null;
    if (host) {
      const p = ((S.peersSnap && S.peersSnap.peers) || []).find(function (x) {
        return x.name === host;
      });
      entry = ((p && p.state && p.state.stacks && p.state.stacks.roster) || []).find(byName);
      if (entry) {
        peer = {
          entry: entry,
          base: repoWebURLFor(host),
          records: (p.deploys || []).filter(function (rec) {
            return rec.stack === stack;
          }),
        };
      }
    } else {
      entry = S.rosterSnap.find(byName);
    }
    if (!entry) return null;
    return {
      stack: stack,
      host: host,
      entry: entry,
      peer: peer,
      health: entry.disabled ? null : stackHealthFor(stack, host),
      updates: stackUpdatesFor(stack, host),
    };
  }

  function linksKey(stack, host) {
    return JSON.stringify(appLinksMapFor(host)[stack] || []);
  }

  // resolveTab keeps the wanted tab while the panel still offers it (the
  // Containers tab goes when the host stops reporting health), else falls back
  // to the stack's default, else to Triggers, which is always there.
  function resolveTab(c, tabs, want) {
    const offered = function (t) {
      return (
        (t === DETAIL_TAB.hooks && hookCount(c.entry.hooks || {}) > 0) ||
        tabs.some(function (x) {
          return x.id === t;
        })
      );
    };
    if (want && offered(want)) return want;
    const def = stackDefaultTab(c.entry);
    return offered(def) ? def : STACK_TAB.triggers;
  }

  function renderHead(panel, c, tab) {
    panel.querySelector('.dp-head').outerHTML = detailHeadHTML({
      stack: c.stack,
      host: hostChip(c.host || S.selfHost),
      links: c.entry.disabled ? [] : appLinksMapFor(c.host)[c.stack],
      logs: !!(c.health && c.health.services && c.health.services.length),
      logHost: c.host,
      hooks: c.entry.hooks,
      hooksOpen: tab === DETAIL_TAB.hooks,
      jump: !c.peer,
    });
    panel.dataset.links = linksKey(c.stack, c.host);
    populateIcon(panel.querySelector('.dp-head .stack-icon'), c.stack);
  }

  // paneBody builds the open tab's content from the shared panel builders.
  function paneBody(c, tab) {
    switch (tab) {
      case STACK_TAB.containers:
        return createHealthPanel(c.stack, c.host || undefined);
      case STACK_TAB.history:
        return createAuditPanel(c.stack, c.peer);
      case DETAIL_TAB.hooks:
        return createHooksPanel(c.stack, c.peer ? c.entry.hooks : undefined);
      default:
        return createWatchedPanel(c.stack, c.peer);
    }
  }

  // showTab points the panel's head, tab strip and pane at tab. Hooks is a
  // pseudo-tab the head's Hooks action toggles; it selects no strip tab.
  function showTab(panel, c, tabs, tab) {
    panel.dataset.tab = tab;
    renderHead(panel, c, tab);
    panel.querySelector('.dp-tabs').outerHTML = tabsHTML(tabs, tab);
    const pane = panel.querySelector('.dp-pane');
    pane.textContent = '';
    pane.appendChild(paneBody(c, tab));
  }

  // The Stacks detail: a row's head, verdict, facts and one tab — local rows
  // and peer rows alike, a peer's read-only. opts.tab survives the rebuild a
  // new snapshot triggers, so the reader stays on the tab they picked.
  detail.registerPane(DETAIL_TAB.overview, {
    openClass: 'audit-open',
    build: function (row, slot, opts) {
      const host = row.classList.contains('peer-row') ? row.dataset.host : '';
      const c = detailContext(row.dataset.stack, host);
      if (!c) return;
      const tabs = stackTabs(c.entry, c.health);
      const panel = document.createElement('div');
      panel.className = 'detail-panel' + (host ? ' peer-panel' : '');
      panel.dataset.testid = 'detail-panel';
      panel.dataset.stack = c.stack;
      panel.dataset.host = host || S.selfHost;
      panel.innerHTML =
        '<div class="dp-head"></div>' +
        verdictHTML(stackVerdict(c.entry, c.health, c.updates), c.stack, {
          canRetry: !c.peer,
          logHost: host,
        }) +
        stackFactsHTML(c.entry, c.health, c.updates, {
          repoBase: c.peer ? c.peer.base : S.repoWebURL,
          nowMs: Date.now(),
        }) +
        '<div class="dp-tabs"></div><div class="dp-pane" role="tabpanel"></div>';
      opts.tab = resolveTab(c, tabs, opts.tab);
      showTab(panel, c, tabs, opts.tab);
      panel.addEventListener('click', function (e) {
        const tabBtn = e.target.closest('.dp-tab');
        const hooksBtn = e.target.closest('[data-action="hooks"]');
        if (!tabBtn && !hooksBtn) return;
        let next;
        if (tabBtn) next = tabBtn.dataset.tab;
        else if (opts.tab === DETAIL_TAB.hooks) next = resolveTab(c, tabs, opts.lastTab);
        else next = DETAIL_TAB.hooks;
        if (opts.tab !== DETAIL_TAB.hooks) opts.lastTab = opts.tab;
        opts.tab = next;
        showTab(panel, c, tabs, next);
        App.stream.applyHookRun(); // the head was re-rendered — re-mark a running hook
      });
      slot.set(panel);
      App.stream.applyHookRun(); // a panel opened mid-hook carries the pulse at once
    },
  });

  // toggleRosterDetail toggles a row's tab pane; a freshly opened panel obeys
  // an active filter.
  function toggleRosterDetail(row, tab) {
    detail.toggle(VIEW, row, tab);
    refilterRoster();
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

  // retryHeld asks the server to deploy a held stack's change once more. The
  // button disables itself until the next roster snapshot rebuilds the panel: the run
  // it starts either clears the hold or records it again. A refusal is
  // announced as a UI note and the button re-enabled, so a dead click is never
  // mistaken for a pending one.
  const RETRY_LABEL = 'Retry once';
  function retryHeld(btn) {
    const stack = btn.dataset.retryStack;
    App.chrome.uiNote('debug', 'retry: requesting', stack);
    btn.disabled = true;
    btn.textContent = 'retrying…';
    fetch('/api/stacks/' + encodeURIComponent(stack) + '/retry', { method: 'POST' })
      .then(function (r) {
        if (r.ok) return;
        App.chrome.uiNote('warn', 'retry: refused for', stack, '— HTTP', r.status);
        btn.disabled = false;
        btn.textContent = RETRY_LABEL;
      })
      .catch(function (err) {
        App.chrome.uiNote('warn', 'retry: request for', stack, 'did not reach the server —', err);
        btn.disabled = false;
        btn.textContent = RETRY_LABEL;
      });
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
    App.stream.applyHookRun(); // the cell was replaced — re-paint a running hook's phase
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

      // Host chip: quick-filter the merged view to this row's host (toggle). Before
      // the panel logic so it works on every row's chip and on the panel head's.
      const hostChip = e.target.closest('.host-mono');
      if (hostChip) {
        const r = hostChip.closest('[data-host]');
        if (r) toggleHostFilterTo(r.dataset.host);
        return;
      }

      // Panel head and verdict actions. Tabs and Hooks are the panel's own listener.
      const act = e.target.closest('[data-action]');
      if (act) {
        switch (act.dataset.action) {
          case 'jump':
            App.chrome.jumpToStack(act.dataset.jumpView, act.dataset.jumpStack);
            break;
          case 'close':
            detail.close(VIEW);
            break;
          case 'logs':
            App.clog.toggle(act);
            break;
          case 'app':
            // A single link is an anchor that navigates on its own.
            if (act.tagName === 'BUTTON') toggleAppLinkPopover(act.closest('.link-wrap'));
            break;
        }
        return;
      }
      if (e.target.closest('.link-pop a')) {
        closeAppLinkPopover();
        return;
      }
      // Hook-log icon: a .clog-btn, so match it before the container-logs handler.
      const hookLog = e.target.closest('.hook-log-btn');
      if (hookLog) {
        App.clog.openHookLog(hookLog, hookLog.dataset.hookLog);
        return;
      }
      // Retry a held change (ADR-0062), the held verdict's action.
      const retryB = e.target.closest('.retry-btn');
      if (retryB) {
        retryHeld(retryB);
        return;
      }
      // Container logs of one service, on a Containers tab line (ADR-0037).
      const clogB = e.target.closest('.clog-btn');
      if (clogB) {
        App.clog.toggle(clogB);
        return;
      }
      const row = e.target.closest('.roster-row');
      if (!row || !rosterList.contains(row)) return;
      toggleRosterDetail(row, DETAIL_TAB.overview);
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
