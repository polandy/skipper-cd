// ESLint flat config for the embedded UI's plain-script JS. Dev-only tooling
// (see package.json) — not part of the shipped app, no bundler involved.
'use strict';

const js = require('@eslint/js');
const globals = require('globals');

module.exports = [
  js.configs.recommended,
  {
    // app.js is the main app script, a plain browser <script> loaded after
    // app-helpers.js and app-render.js. The globals list below is the contract
    // it calls by bare name — a new use fails no-undef until it is added here.
    // The reverse has no linter behind it: when the last call moves out of
    // app.js, drop the entry by hand or the list drifts.
    files: ['app.js'],
    languageOptions: {
      sourceType: 'script',
      globals: {
        ...globals.browser,
        // app-helpers.js functions
        assignHostColors: 'readonly',
        auditCountText: 'readonly',
        attentionLabel: 'readonly',
        attentionStacks: 'readonly',
        buildHostList: 'readonly',
        clogStreamStatus: 'readonly',
        containerMatchesQuery: 'readonly',
        deployAnnouncement: 'readonly',
        deployStatusLabel: 'readonly',
        formatDuration: 'readonly',
        formatTime: 'readonly',
        fullTime: 'readonly',
        healthClass: 'readonly',
        hostFilterActive: 'readonly',
        imageDelta: 'readonly',
        logLineLevel: 'readonly',
        logLineVisible: 'readonly',
        makeReconnector: 'readonly',
        orphanMatchesQuery: 'readonly',
        orphanMeta: 'readonly',
        orphanStateClass: 'readonly',
        phaseDuration: 'readonly',
        reconcileHostFilter: 'readonly',
        resolveAppLinksMap: 'readonly',
        resolveHealthMap: 'readonly',
        resolveHealthwatchMap: 'readonly',
        resolveRepoWebURL: 'readonly',
        resolveUpdates: 'readonly',
        rosterOrdered: 'readonly',
        rowClass: 'readonly',
        snapshotIsFresh: 'readonly',
        // app-render.js constants
        CLOG_ICON: 'readonly',
        LINK_ICON: 'readonly',
        WARN_ICON: 'readonly',
        // app-render.js functions
        attentionBandHTML: 'readonly',
        autosyncDetailHTML: 'readonly',
        autosyncPosText: 'readonly',
        autosyncReasonChipHTML: 'readonly',
        autosyncRowHTML: 'readonly',
        autosyncSwitchTitle: 'readonly',
        auditRowsHTML: 'readonly',
        badgeHTML: 'readonly',
        beaconPopHTML: 'readonly',
        clogBtnHTML: 'readonly',
        clogSvcsHTML: 'readonly',
        commitLinkHTML: 'readonly',
        diffPanelHTML: 'readonly',
        escapeAttr: 'readonly',
        escapeHtml: 'readonly',
        filesHTML: 'readonly',
        filesPanelHTML: 'readonly',
        healPanelHTML: 'readonly',
        healPillHTML: 'readonly',
        healthHistoryHTML: 'readonly',
        healthPillHTML: 'readonly',
        hookCount: 'readonly',
        hookPhaseHTML: 'readonly',
        hooksBadgeHTML: 'readonly',
        hostChipHTML: 'readonly',
        imageDeltaHTML: 'readonly',
        jumpBtnHTML: 'readonly',
        linkCellHTML: 'readonly',
        logLineHTML: 'readonly',
        nextTrailHTML: 'readonly',
        pendingTagHTML: 'readonly',
        rosterHealthPillHTML: 'readonly',
        rosterRowActionsHTML: 'readonly',
        rosterStatusHTML: 'readonly',
        rosterVersionCellHTML: 'readonly',
        rosterVersionInnerHTML: 'readonly',
        runListHTML: 'readonly',
        runSummaryHTML: 'readonly',
        serviceVersionHTML: 'readonly',
        updateCheckMetaHTML: 'readonly',
        watchedPanelHTML: 'readonly',
      },
    },
    rules: {
      // catch (_) is this file's established ignore idiom.
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
        UNCHANGED_SINCE: 'readonly',
        // app-helpers.js functions it calls
        attentionLabel: 'readonly',
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
        logTime: 'readonly',
        phaseDuration: 'readonly',
        phaseSince: 'readonly',
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
