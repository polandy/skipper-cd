// app-hosts.js — the multi-host surface (ADR-0048, dev-docs/multi-host-spec.md):
// peer rows merged into the deploy feed, host chips and colours, the Hosts
// drawer and filter, the peer detail panel. Cut out of the app script by view
// (ADR-0035 amendment) and attached as App.hosts.
//
// A peer instance's read data, merged into the local views and tagged by host.
// Everything here is inert on a single-host instance (S.peersSnap null): the
// Hosts control, Host column and peer rows never appear.
//
// Loads after app-panels.js (imported at load) and before the stream, so what it
// needs from the chrome, the deploys table and the roster (App.chrome,
// App.deploys, App.roster) is read at call time. The bootstrap calls
// init() at the spot the drawer wiring used to run, and applyPeers() after it
// has stored a peers snapshot.
App.hosts = (function () {
  const S = App.state;
  const { healthMapFor, repoWebURLFor } = App.resolve;
  const { CHECK_ICON, renderDiffPanel, createLoadError, populateIcon, createHealthPanel } =
    App.panels;
  // Peer rows live in the deploys table, so the feed is addressed directly.
  const tbody = document.getElementById('tbody');

  // hostColors maps each host name to its palette slot (assignHostColors,
  // collision-avoiding). hostSelected is the set of in-view host names (the
  // Hosts filter); null means all hosts. Persisted per browser (localStorage
  // key hostFilter) and restored once, the first time the peers snapshot
  // arrives (see applyPeers) — the host set isn't known any earlier —
  // reconciled against it via reconcileHostFilter so a saved host that no
  // longer exists can't strand the view.
  let hostColors = {};
  let hostSelected = null;
  let hostFilterRestored = false;

  // hostList is the effective host set: the primary (self) first, then each
  // peer — buildHostList (app-helpers.js) with the live peers snapshot bound.
  function hostList() {
    return buildHostList(S.peersSnap, S.selfHost);
  }

  // isHostSelected reports whether a host is in view. hostSelected null means the
  // filter is off (all hosts shown); otherwise it is the selected-name set.
  function isHostSelected(name) {
    return hostSelected === null || hostSelected.has(name);
  }

  // recomputeHostColors reassigns palette slots whenever the host set changes,
  // keeping the no-two-hosts-share-a-colour guarantee (assignHostColors).
  function recomputeHostColors() {
    if (!S.peersSnap) {
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
    row.className = rowClass(true) + ' peer-row' + (stale ? ' peer-stale' : '');
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
      escapeAttr(S.absoluteTime ? relTs : absTs) +
      '">' +
      (S.absoluteTime ? absTs : relTs) +
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
      // A peer's record collapses repeats the same way ours does (ADR-0056), and
      // without the count its one row would read as a one-off incident.
      repeatNoteHTML(rec) +
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

  // renderPeerRows rebuilds every peer row from the current snapshot, interleaved
  // into the local rows by timestamp. Local rows and their panels are never
  // touched; only peer rows (which have no panels) are removed and rebuilt.
  function renderPeerRows() {
    tbody.querySelectorAll('.peer-row, .peer-detail').forEach(function (r) {
      r.remove();
    });
    if (!S.peersSnap) return;
    const items = [];
    (S.peersSnap.peers || []).forEach(function (p) {
      (p.deploys || []).forEach(function (rec) {
        if (rec.status === 'skipped') return; // an unchanged stack is not shown, as locally
        items.push({ rec: rec, host: p.name, stale: !!p.stale });
      });
    });
    // Newest first, so each insert-above-first-older builds a correct merge.
    items.sort(function (a, b) {
      return a.rec.timestamp < b.rec.timestamp ? 1 : a.rec.timestamp > b.rec.timestamp ? -1 : 0;
    });
    if (items.length) App.deploys.showTable();
    // The first row seen per (host, stack) is the newest — it carries the live
    // health pill (a current per-stack value), like the local newest row.
    const seenNewest = {};
    items.forEach(function (it) {
      const perHost = seenNewest[it.host] || (seenNewest[it.host] = {});
      const newest = !perHost[it.rec.stack];
      perHost[it.rec.stack] = true;
      App.deploys.insertRowByTime(
        createPeerRow(it.rec, it.host, it.stale, newest),
        it.rec.timestamp,
      );
    });
  }

  // setLeadingChip replaces (or clears) a stack cell's leading host chip in place.
  function setLeadingChip(cell, hostName) {
    const existing = cell.querySelector('.host-mono');
    if (existing) existing.remove();
    if (hostName) cell.insertAdjacentHTML('afterbegin', hostChip(hostName));
  }

  // retagLocalRows fills in the host dot + data-host on local rows created before
  // the first peers snapshot set S.selfHost (deploy history and the roster are both
  // painted just before it), across both merged views.
  function retagLocalRows() {
    tbody.querySelectorAll('.event-row[data-stack]:not(.peer-row)').forEach(function (row) {
      row.dataset.host = S.selfHost;
      const cell = row.querySelector('.cell-stack');
      if (cell) setLeadingChip(cell, S.selfHost);
    });
    document
      .getElementById('roster-list')
      .querySelectorAll('.roster-row[data-stack]:not(.peer-row)')
      .forEach(function (row) {
        row.dataset.host = S.selfHost;
        // The chip leads the identity group, not the cell — the cell's first
        // child is that group.
        const cell = row.querySelector('.roster-ident');
        if (cell) setLeadingChip(cell, S.selfHost);
      });
  }

  // renderHosts paints the Hosts control (header badge + drawer list). Hidden
  // entirely when no peers are configured.
  function renderHosts() {
    const btn = document.getElementById('hosts-btn');
    const hosts = hostList();
    const hasPeers = !!(S.peersSnap && (S.peersSnap.peers || []).length);
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
    if (!S.peersSnap || !(S.peersSnap.peers || []).length) {
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
    App.chrome.renderUpdateBadge(); // a host taken out of view takes its updates with it

    const staleSel = (S.peersSnap.peers || []).filter(function (p) {
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
    App.roster.refreshPeerRosterRows();
    applyHostFilter();
    App.deploys.refilterDeploys();
    App.roster.refilterRoster();
  }

  // schedulePeerReflow re-interleaves peer rows after local deploy rows land.
  // Local rows are inserted at the top (newest first); a burst of replayed
  // history — or a fresh live deploy — therefore lands above the peer rows that
  // were already merged in, so the peer rows must be re-slotted by timestamp
  // afterwards. Coalesced to one pass per frame so a history replay reflows once.
  let peerReflowScheduled = false;
  function schedulePeerReflow() {
    if (!S.peersSnap || peerReflowScheduled) return;
    peerReflowScheduled = true;
    setTimeout(function () {
      peerReflowScheduled = false;
      renderPeerRows();
      applyHostFilter();
      App.deploys.refilterDeploys();
    }, 0);
  }

  // applyPeers applies the 'peers' snapshot the dispatcher (app-stream.js) has just
  // stored in S.peersSnap / S.selfHost: restores the saved host filter once,
  // recolours, retags and re-renders, then re-filters both views.
  function applyPeers() {
    if (!hostFilterRestored && S.peersSnap) {
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
    App.roster.refreshPeerRosterRows(); // roster peer rows (local roster rows + panels untouched)
    applyHostFilter(); // dots + host filtering across both views; also renderHosts
    App.deploys.refilterDeploys(); // re-apply any active stack search to the new peer rows
    App.roster.refilterRoster(); // and to the new peer roster rows
  }

  const hostsBtn = document.getElementById('hosts-btn');
  const hostsDrawer = document.getElementById('hosts-drawer');

  function setHostsDrawer(open) {
    if (open) App.chrome.closeOtherSurfaces(hostsDrawer);
    hostsDrawer.classList.toggle('open', open);
    hostsBtn.classList.toggle('open', open);
    hostsBtn.setAttribute('aria-expanded', String(open));
    App.chrome.manageSurfaceFocus(hostsDrawer, hostsBtn, open);
  }

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
    App.deploys.refilterDeploys();
  }

  // init wires the Hosts drawer — everything the block ran at load.
  function init() {
    App.chrome.trapFocus(hostsDrawer);

    hostsBtn.addEventListener('click', function () {
      setHostsDrawer(!hostsDrawer.classList.contains('open'));
    });

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
      App.deploys.refilterDeploys();
    });

    App.chrome.registerSurface({
      surface: hostsDrawer,
      isOpen: function () {
        return hostsDrawer.classList.contains('open');
      },
      close: function () {
        setHostsDrawer(false);
      },
      within: [hostsDrawer, hostsBtn],
    });
  }

  return {
    hostChip,
    hostChipKeydown,
    togglePeerDetail,
    openPeerDetail,
    applyHostFilter,
    toggleHostFilterTo,
    schedulePeerReflow,
    applyPeers,
    init,
  };
})();
