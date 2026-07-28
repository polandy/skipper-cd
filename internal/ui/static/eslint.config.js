// ESLint flat config for the embedded UI's plain-script JS. Dev-only tooling
// (see package.json) — not part of the shipped app, no bundler involved.
'use strict';

const js = require('@eslint/js');
const globals = require('globals');

module.exports = [
  js.configs.recommended,
  {
    // app.js is the main app script, a plain browser <script> loaded after
    // app-helpers.js. The globals list below is the helper contract it calls
    // by bare name — a new helper use fails no-undef until it is added here.
    files: ['app.js'],
    languageOptions: {
      sourceType: 'script',
      globals: {
        ...globals.browser,
        // app-helpers.js constants
        HEALTH: 'readonly',
        UNCHANGED_SINCE: 'readonly',
        // app-helpers.js functions
        assignHostColors: 'readonly',
        attentionStacks: 'readonly',
        auditStatusLabel: 'readonly',
        classifyDiffLine: 'readonly',
        clogStreamStatus: 'readonly',
        commitURL: 'readonly',
        containerMatchesQuery: 'readonly',
        deployAnnouncement: 'readonly',
        formatDuration: 'readonly',
        formatTime: 'readonly',
        fullTime: 'readonly',
        healthClass: 'readonly',
        hostFilterActive: 'readonly',
        hostMonogram: 'readonly',
        imageDelta: 'readonly',
        levelClass: 'readonly',
        logLineVisible: 'readonly',
        logTime: 'readonly',
        orphanMatchesQuery: 'readonly',
        orphanMeta: 'readonly',
        orphanStateClass: 'readonly',
        phaseDuration: 'readonly',
        phaseSince: 'readonly',
        reasonFromSnap: 'readonly',
        reconcileHostFilter: 'readonly',
        rosterVersion: 'readonly',
        shortImageTag: 'readonly',
        shortSHA: 'readonly',
        snapshotIsFresh: 'readonly',
        statusIcon: 'readonly',
        statusText: 'readonly',
        watchedSummary: 'readonly',
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
    // The service worker runs in its own worker global scope.
    files: ['sw.js'],
    languageOptions: {
      sourceType: 'script',
      globals: globals.serviceworker,
    },
  },
  {
    // node --test unit layer: CommonJS, node built-ins.
    files: ['app-helpers.test.js'],
    languageOptions: {
      sourceType: 'commonjs',
      globals: globals.node,
    },
  },
];
