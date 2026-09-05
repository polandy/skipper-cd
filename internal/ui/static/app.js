(function () {
  // Shared state comes from app-state.js (loaded before this file); S is the
  // store every view reads and applyState / applyPeers / handleEvent below
  // write. The views that have moved out are imported from App.<view>.
  const S = App.state;
  const chrome = App.chrome;
  const { armAnnounceGate, applyView, renderIncidentBadge, renderHealthAttention } = chrome;
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
  } = roster;
  const deploys = App.deploys;
  const {
    isDeploying,
    settleInitialState,
    initialStateIsSettled,
    updateStackAffordances,
    updateDeployingIndicator,
    applyQueueSnapshot,
    applyOrphansSnapshot,
    handleEvent,
    refilterDeploys,
  } = deploys;
  const hosts = App.hosts;
  const { schedulePeerReflow } = hosts;
  const panels = App.panels;
  const { createLoadError, hookPhaseNode } = panels;
  App.stream = { streamIsOpen, applyHookRun, clearOfflineNotice };
  const loadingState = document.getElementById('loading-state');
  const connDot = document.getElementById('conn-dot');
  const connText = document.getElementById('conn-text');
  // ── Chrome ── lives in app-chrome.js (App.chrome); wired first, so every
  // view can register its surfaces with it.
  chrome.init();
  // ── Row panels, ⋯ menu, app-link popover, icons ── live in app-panels.js
  // (App.panels); their document-level listeners are wired here, at the spot
  // they used to register.
  panels.init();

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
    const drow = Array.from(
      document.getElementById('tbody').querySelectorAll('.event-row[data-stack]'),
    ).find(function (r) {
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
    if (rlist && isDeploying(hr.stack)) {
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

  // ── Deploys view ── lives in app-deploys.js (App.deploys); wired here, once
  // the surface registry above exists — the spot its run drawer registered.
  deploys.init();

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
        applyQueueSnapshot();
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
        applyOrphansSnapshot();
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
    if (initialStateIsSettled()) return; // the table is up — the indicator carries it
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
    if (!initialStateIsSettled()) loadingState.style.display = '';
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
      if (chrome.activeView() === 'stacks') refreshRosterRow(evt.stack); // reflect in-flight/settled state live
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

  // ── Stacks view ── lives in app-roster.js (App.roster); wired here, at the
  // spot the block occupied, so its type-to-search stays behind the deploys one.
  roster.init();

  // ── Autosync controls ── live in app-autosync.js (App.autosync); wired here,
  // at the spot the block occupied, once the surfaces it registers with exist.
  autosync.init();

  // ── Hosts drawer ── lives in app-hosts.js (App.hosts); wired here, at the
  // spot the block occupied.
  hosts.init();

  // ── Logs view ── lives in app-logs.js (App.logs); wired here, at the spot the block occupied.
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

  logs.renderLogQuickChips(); // paint the persisted filter before the first lines land
  logs.applyFollow();
  applyView();

  connect();

  // ── Logs view controls ── live in app-logs.js; wired here, at the spot the
  // block occupied, so its document-level key handler keeps its place in line.
  logs.initControls();

  document.addEventListener('keydown', function (e) {
    if (e.key === 'Escape') clog.escape();
  });
})();
