// app-state.js — the App namespace and the shared snapshot store.
//
// The app is plain scripts, not modules (ADR-0035: no bundler, no build step),
// so the files share state through one global namespace object instead of the
// global lexical scope: every cross-file access reads `App.<file>.<name>` or
// `App.state.<name>` and is greppable as such. This file loads first after the
// pure layers (app-helpers.js, app-render.js) and defines the namespace; each
// later file attaches its own API to it.
//
// App.state holds what the SSE streams publish and every view only reads —
// populated by the stream dispatcher in app.js (applyState / applyPeers /
// handleEvent), rendered by all views. Two display modes live here too
// (absoluteTime, imageDeltaOn): not snapshots, but read by five and three
// files respectively, so they belong at the same address. State a single view
// owns (filters, open panels, drawer positions) stays in that view's file.

window.App = {
  state: {
    // Whether the deploy table shows absolute or relative timestamps;
    // persisted per browser under the localStorage key timeMode.
    absoluteTime: localStorage.getItem('timeMode') === 'absolute',
    // Whether the deploy table's Version column is shown. On by default; the
    // Deploys view-options toggle persists an off choice per browser. Only the
    // explicit 'off' hides it, so a first-time visitor sees the column.
    imageDeltaOn: localStorage.getItem('imageDelta') !== 'off',

    // Autosync state, populated from the 'autosync' and 'queue' SSE events.
    autosyncSnap: null, // GET /api/autosync shape
    autosyncVersion: null, // version of the applied snapshot
    queueSnap: { count: 0, pending: [] }, // GET /api/queue shape
    queueByStack: {}, // stack name -> pending item

    // Run look-ahead, from the 'upcoming' SSE event: stacks that will deploy
    // after the one currently deploying, in deploy order.
    upcomingSnap: [],

    // Live stack health, from the 'health' SSE snapshot: stack name -> {
    // status, services }. Rendered as a pill on the newest row of each stack
    // (ADR-0027).
    healthSnap: {},

    // Health-watch status history, from the 'healthwatch' SSE snapshot: stack
    // -> service -> phases (newest first, <= 10). Empty when the watchdog is
    // off — the health panel then renders without age/timeline (ADR-0031).
    healthwatchSnap: {},

    // Names parked with disabled: true, from the 'stacks' SSE snapshot (stack
    // discovery, ADR-0034). Rendered as a quiet chip line below the deploy
    // table; empty (and the line hidden) in host-list mode.
    disabledSnap: [],

    // 'stacks' SSE snapshot. Inventory, not an event log — every declared stack
    // appears, including never-deployed and disabled ones. See
    // dev-docs/stack-roster-spec.md.
    rosterSnap: [],

    // The registry update-check snapshot ({stacks, checked_at}, ADR-0054) from
    // the same 'stacks' snapshot, or null while the check is disabled or has
    // not run. Drives the amber ⇡ markers on version chips and the containers
    // panel's update summary.
    updatesSnap: null,

    // The bad terminal audit records of the last 24h from the same 'stacks'
    // snapshot, driving the header incident badge. The badge re-filters the
    // list against the window on the relative-time tick, so the count ages out
    // between republishes.
    incidentsSnap: [],

    // The deploy repo's forge browse URL from the same 'stacks' snapshot, or ''
    // when the server could derive none from repo_url. Every commit SHA the UI
    // prints links to its commit page through it (commitLinkHTML); without it
    // the SHAs stay plain text. A peer's SHAs use that peer's own value — its
    // repo may live on a different forge — so this one is the local set's base
    // only.
    repoWebURL: '',

    // Traefik-routed hostnames per stack, from the 'app_links' SSE snapshot
    // (dev-docs/traefik-app-links-spec.md): stack name -> hostnames, absent
    // when none were discovered. Feeds the roster row's link icon.
    appLinksSnap: {},

    // Orphans (ADR-0036): compose projects the discovered stack set no longer
    // accounts for, from the 'orphans' SSE snapshot — a collapsed section
    // below the table, hidden when empty.
    orphansSnap: [],

    // The currently-executing deploy hook ({} when none) from the hookrun SSE
    // snapshot (ADR-0038). The hook commands themselves ride inline on the
    // stacks snapshot.
    hookRunSnap: {},

    // peersSnap is the 'peers' state payload
    // ({self, peers:[{name,url,reachable,stale,last_seen,state,deploys}]}) or
    // null on a single-host instance with no peers configured — in which case
    // the whole multi-host surface (Hosts control, Host column, peer rows)
    // stays hidden and the UI is exactly the single-host one (ADR-0048).
    // selfHost is the primary's own host label, the identity every local row
    // is tagged with.
    peersSnap: null,
    selfHost: '',
  },
};

// The per-host resolvers: the one piece of logic that knows a peer's snapshot
// lives somewhere else than the primary's own. Each resolves the per-stack map
// (or forge browse URL) for a host — the primary's live snapshot for self, else
// the peer's fanned-in state (ADR-0048) — so peer rows render the same
// container/health/app-link detail the primary shows for its own stacks. Thin
// wrappers binding the store to the pure resolve* helpers (app-helpers.js),
// which own — and unit-test — the self-vs-peer fallback and the tolerance for
// older peers missing a section.
(function () {
  const S = App.state;

  function healthMapFor(host) {
    return resolveHealthMap(S.peersSnap, S.selfHost, host, S.healthSnap);
  }
  function healthwatchMapFor(host) {
    return resolveHealthwatchMap(S.peersSnap, S.selfHost, host, S.healthwatchSnap);
  }
  function appLinksMapFor(host) {
    return resolveAppLinksMap(S.peersSnap, S.selfHost, host, S.appLinksSnap);
  }
  function repoWebURLFor(host) {
    return resolveRepoWebURL(S.peersSnap, S.selfHost, host, S.repoWebURL);
  }
  function updatesFor(host) {
    return resolveUpdates(S.peersSnap, S.selfHost, host, S.updatesSnap);
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

  App.resolve = {
    healthMapFor,
    healthwatchMapFor,
    appLinksMapFor,
    repoWebURLFor,
    updatesFor,
    stackUpdatesFor,
    stackHealthFor,
  };
})();
