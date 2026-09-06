// ESLint flat config for the embedded UI's plain-script JS. Dev-only tooling
// (see package.json) — not part of the shipped app, no bundler involved.
'use strict';

const js = require('@eslint/js');
const globals = require('globals');

module.exports = [
  js.configs.recommended,
  {
    // app-stream.js is the SSE stream dispatcher and the bootstrap, a plain
    // browser <script> loaded last that attaches itself as App.stream and calls
    // the pure layers by bare name — the globals below are that contract.
    files: ['app-stream.js'],
    languageOptions: {
      sourceType: 'script',
      globals: {
        ...globals.browser,
        App: 'readonly',
        makeReconnector: 'readonly',
      },
    },
    rules: {
      'no-unused-vars': ['error', { caughtErrorsIgnorePattern: '^_' }],
    },
  },
  {
    // app-state.js defines the App namespace and the shared snapshot store; a
    // plain browser <script> loaded after app-helpers.js, whose resolve*
    // helpers its per-host resolvers bind by bare name.
    files: ['app-state.js'],
    languageOptions: {
      sourceType: 'script',
      globals: {
        ...globals.browser,
        // defined here (window.App) and read back by bare name below
        App: 'writable',
        resolveAppLinksMap: 'readonly',
        resolveHealthMap: 'readonly',
        resolveHealthwatchMap: 'readonly',
        resolveRepoWebURL: 'readonly',
        resolveUpdates: 'readonly',
      },
    },
  },
  {
    // app-chrome.js is the app chrome: a plain browser <script> loaded right
    // after app-state.js that attaches itself as App.chrome and calls the pure
    // layers by bare name — the globals below are that contract.
    files: ['app-chrome.js'],
    languageOptions: {
      sourceType: 'script',
      globals: {
        ...globals.browser,
        App: 'readonly',
        makeSurfaceRegistry: 'readonly',
        attentionLabel: 'readonly',
        attentionStacks: 'readonly',
        formatTime: 'readonly',
        incidentBadgeLabel: 'readonly',
        recentIncidentCount: 'readonly',
        updateBadgeLabel: 'readonly',
        WARN_ICON: 'readonly',
        attentionBandHTML: 'readonly',
        beaconPopHTML: 'readonly',
      },
    },
    rules: {
      'no-unused-vars': ['error', { caughtErrorsIgnorePattern: '^_' }],
    },
  },
  {
    // app-panels.js holds the shared row panels and affordances: a plain
    // browser <script> loaded after app-state.js that attaches itself as
    // App.panels and calls the pure layers by bare name — the globals below
    // are that contract.
    files: ['app-panels.js'],
    languageOptions: {
      sourceType: 'script',
      globals: {
        ...globals.browser,
        App: 'readonly',
        serviceVersionHTML: 'readonly',
        phaseDuration: 'readonly',
        auditCountText: 'readonly',
        auditRowsHTML: 'readonly',
        clogBtnHTML: 'readonly',
        diffPanelHTML: 'readonly',
        escapeAttr: 'readonly',
        escapeHtml: 'readonly',
        filesPanelHTML: 'readonly',
        healPanelHTML: 'readonly',
        healthClass: 'readonly',
        healthHistoryHTML: 'readonly',
        hookPhaseHTML: 'readonly',
        hooksBadgeHTML: 'readonly',
        linkCellHTML: 'readonly',
        updateCheckMetaHTML: 'readonly',
        watchedPanelHTML: 'readonly',
      },
    },
    rules: {
      'no-unused-vars': ['error', { caughtErrorsIgnorePattern: '^_' }],
    },
  },
  {
    // app-hosts.js is the multi-host view file: a plain browser <script>
    // loaded after app-panels.js that attaches itself as App.hosts and calls
    // the pure layers by bare name — the globals below are that contract.
    files: ['app-hosts.js'],
    languageOptions: {
      sourceType: 'script',
      globals: {
        ...globals.browser,
        App: 'readonly',
        assignHostColors: 'readonly',
        buildHostList: 'readonly',
        formatDuration: 'readonly',
        formatTime: 'readonly',
        fullTime: 'readonly',
        hostFilterActive: 'readonly',
        reconcileHostFilter: 'readonly',
        rowClass: 'readonly',
        LINK_ICON: 'readonly',
        badgeHTML: 'readonly',
        commitLinkHTML: 'readonly',
        escapeAttr: 'readonly',
        escapeHtml: 'readonly',
        healthPillHTML: 'readonly',
        hostChipHTML: 'readonly',
        repeatNoteHTML: 'readonly',
      },
    },
    rules: {
      'no-unused-vars': ['error', { caughtErrorsIgnorePattern: '^_' }],
    },
  },
  {
    // app-roster.js is the Stacks view file: a plain browser <script> loaded
    // after app-hosts.js that attaches itself as App.roster and calls the pure
    // layers by bare name — the globals below are that contract.
    files: ['app-roster.js'],
    languageOptions: {
      sourceType: 'script',
      globals: {
        ...globals.browser,
        App: 'readonly',
        escapeAttr: 'readonly',
        escapeHtml: 'readonly',
        formatTime: 'readonly',
        fullTime: 'readonly',
        rosterOrdered: 'readonly',
        stackHasUpdate: 'readonly',
        updatePresetActive: 'readonly',
        commitLinkHTML: 'readonly',
        healthPillHTML: 'readonly',
        jumpBtnHTML: 'readonly',
        rowActionClusterHTML: 'readonly',
        lastIncidentHTML: 'readonly',
        outcomeStripHTML: 'readonly',
        rosterHealthPillHTML: 'readonly',
        rosterRowActionsHTML: 'readonly',
        rosterStatusHTML: 'readonly',
        rosterUpdateChipHTML: 'readonly',
        rosterVersionCellHTML: 'readonly',
        rosterVersionInnerHTML: 'readonly',
      },
    },
    rules: {
      'no-unused-vars': ['error', { caughtErrorsIgnorePattern: '^_' }],
    },
  },
  {
    // app-deploys.js is the Deploys view file: a plain browser <script> loaded
    // after app-roster.js that attaches itself as App.deploys and calls the pure
    // layers by bare name — the globals below are that contract.
    files: ['app-deploys.js'],
    languageOptions: {
      sourceType: 'script',
      globals: {
        ...globals.browser,
        App: 'readonly',
        escapeAttr: 'readonly',
        escapeHtml: 'readonly',
        containerMatchesQuery: 'readonly',
        deployAnnouncement: 'readonly',
        deployStatusLabel: 'readonly',
        formatDuration: 'readonly',
        formatTime: 'readonly',
        fullTime: 'readonly',
        incidentPresetActive: 'readonly',
        orphanMatchesQuery: 'readonly',
        orphanMeta: 'readonly',
        orphanStateClass: 'readonly',
        rowClass: 'readonly',
        badgeHTML: 'readonly',
        filesHTML: 'readonly',
        healPillHTML: 'readonly',
        healthPillHTML: 'readonly',
        hookCount: 'readonly',
        changeCellHTML: 'readonly',
        changeChipsHTML: 'readonly',
        changeAttributionHTML: 'readonly',
        imageDeltaChipsHTML: 'readonly',
        jumpBtnHTML: 'readonly',
        deployStatusChipsHTML: 'readonly',
        nextTrailHTML: 'readonly',
        pendingTagHTML: 'readonly',
        repeatNoteHTML: 'readonly',
        retryNoteHTML: 'readonly',
        runListHTML: 'readonly',
        runSummaryHTML: 'readonly',
      },
    },
    rules: {
      'no-unused-vars': ['error', { caughtErrorsIgnorePattern: '^_' }],
    },
  },
  {
    // app-autosync.js is the autosync view file: a plain browser <script>
    // loaded after app-state.js that attaches itself as App.autosync and calls
    // the pure layers by bare name — the globals below are that contract.
    files: ['app-autosync.js'],
    languageOptions: {
      sourceType: 'script',
      globals: {
        ...globals.browser,
        App: 'readonly',
        // app-helpers.js functions
        snapshotIsFresh: 'readonly',
        // app-render.js functions
        autosyncDetailHTML: 'readonly',
        autosyncPosText: 'readonly',
        autosyncReasonChipHTML: 'readonly',
        autosyncRowHTML: 'readonly',
        autosyncSwitchTitle: 'readonly',
        escapeHtml: 'readonly',
      },
    },
    rules: {
      'no-unused-vars': ['error', { caughtErrorsIgnorePattern: '^_' }],
    },
  },
  {
    // app-logs.js is the Logs view file: a plain browser <script> loaded after
    // app-state.js that attaches itself as App.logs and calls the pure layers
    // by bare name — the globals below are that contract.
    files: ['app-logs.js'],
    languageOptions: {
      sourceType: 'script',
      globals: {
        ...globals.browser,
        App: 'readonly',
        isLogOutcome: 'readonly',
        logDiffBlockHTML: 'readonly',
        logFacets: 'readonly',
        logFiltersActive: 'readonly',
        logLineHTML: 'readonly',
        logLineLevel: 'readonly',
        logLineVisible: 'readonly',
        logMatchesFilters: 'readonly',
        makeReconnector: 'readonly',
        mergeLogView: 'readonly',
        parseLogFilters: 'readonly',
      },
    },
    rules: {
      'no-unused-vars': ['error', { caughtErrorsIgnorePattern: '^_' }],
    },
  },
  {
    // app-clog.js is the container-logs view file: a plain browser <script>
    // loaded after app-state.js that attaches itself as App.clog and calls the
    // pure layers by bare name — the globals below are that contract.
    files: ['app-clog.js'],
    languageOptions: {
      sourceType: 'script',
      globals: {
        ...globals.browser,
        App: 'readonly',
        // app-helpers.js functions
        clogStreamStatus: 'readonly',
        logLineVisible: 'readonly',
        // app-render.js constants and functions
        CLOG_ICON: 'readonly',
        clogSvcsHTML: 'readonly',
        escapeHtml: 'readonly',
      },
    },
    rules: {
      'no-unused-vars': ['error', { caughtErrorsIgnorePattern: '^_' }],
    },
  },
  {
    // app-helpers.js runs as a plain <script> in the browser (its functions
    // become globals) and is also require()'d under `node --test`; it only
    // touches the `module` global itself, guarded by a typeof check.
    files: ['app-helpers.js'],
    languageOptions: {
      sourceType: 'script',
      globals: { module: 'readonly' },
    },
  },
  {
    // app-render.js is dual-use like app-helpers.js, and additionally calls
    // app-helpers functions by bare name — the globals below are that contract
    // (browser: installed by the earlier <script>; node: via the guarded
    // globalThis assign at its top).
    files: ['app-render.js'],
    languageOptions: {
      sourceType: 'script',
      globals: {
        module: 'readonly',
        require: 'readonly',
        // app-helpers.js constants it uses
        AUDIT_BAD_STATUSES: 'readonly',
        AUDIT_FOLD_NOUN: 'readonly',
        FOLD_START_MAX_MS: 'readonly',
        UNCHANGED_SINCE: 'readonly',
        // app-helpers.js functions it calls
        attentionLabel: 'readonly',
        foldPhases: 'readonly',
        foldAuditRecords: 'readonly',
        auditStatusLabel: 'readonly',
        classifyDiffLine: 'readonly',
        commitURL: 'readonly',
        formatDuration: 'readonly',
        formatTime: 'readonly',
        fullTime: 'readonly',
        hostMonogram: 'readonly',
        imageDelta: 'readonly',
        levelClass: 'readonly',
        logLineLevel: 'readonly',
        logNarrative: 'readonly',
        logTime: 'readonly',
        phaseDuration: 'readonly',
        phaseSince: 'readonly',
        repeatNote: 'readonly',
        reasonFromSnap: 'readonly',
        rosterVersion: 'readonly',
        shortImageTag: 'readonly',
        shortSHA: 'readonly',
        statusIcon: 'readonly',
        statusText: 'readonly',
        waitedSince: 'readonly',
        watchedSummary: 'readonly',
      },
    },
  },
  {
    // The service worker runs in its own worker global scope.
    files: ['sw.js'],
    languageOptions: {
      sourceType: 'script',
      globals: globals.serviceworker,
    },
  },
  {
    // node --test unit layer: CommonJS, node built-ins.
    files: ['app-helpers.test.js', 'app-render.test.js'],
    languageOptions: {
      sourceType: 'commonjs',
      globals: globals.node,
    },
  },
];
