// ESLint flat config for the embedded UI's plain-script JS. Dev-only tooling
// (see package.json) — not part of the shipped app, no bundler involved.
'use strict';

const js = require('@eslint/js');
const globals = require('globals');

module.exports = [
  js.configs.recommended,
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
