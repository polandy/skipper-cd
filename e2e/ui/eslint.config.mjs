// ESLint for the Playwright suite's TypeScript. Dev-only, like the config in
// internal/ui/static - nothing here ships in the binary. .mjs so it is ESM
// without putting "type": "module" in package.json, which would change how
// Playwright transpiles the specs (they rely on __dirname).
//
// The rule that earns this config its keep is no-floating-promises: a forgotten
// `await` on an assertion makes a test pass without ever checking anything, and
// a vacuously green test is worse than no test. It needs type information, so
// the type-checked preset is used rather than the plain one.
import eslint from '@eslint/js';
import tseslint from 'typescript-eslint';

export default tseslint.config(
  {
    ignores: [
      'node_modules/',
      'playwright-report/',
      'test-results/',
      '__screenshots__/',
      'eslint.config.mjs', // this file: outside the tsconfig the typed rules use
    ],
  },
  eslint.configs.recommended,
  ...tseslint.configs.recommendedTypeChecked,
  {
    languageOptions: {
      parserOptions: {
        projectService: true,
        tsconfigRootDir: import.meta.dirname,
      },
    },
    rules: {
      // The harness spawns processes and shells out; `any` from those edges is
      // handled explicitly rather than typed through, so the unsafe-* family
      // would fire on deliberate boundaries rather than on mistakes.
      '@typescript-eslint/no-unsafe-assignment': 'off',
      '@typescript-eslint/no-unsafe-member-access': 'off',
      '@typescript-eslint/no-unsafe-argument': 'off',
      '@typescript-eslint/no-unsafe-call': 'off',
      '@typescript-eslint/no-unsafe-return': 'off',
      // catch (_) is this suite's ignore idiom, matching internal/ui/static.
      '@typescript-eslint/no-unused-vars': ['error', { caughtErrorsIgnorePattern: '^_' }],
      // Playwright reads a fixture's destructuring pattern to build its
      // dependency graph, so `async ({}, use)` is how a fixture declares it
      // needs none. The rule reads that as a pointless empty pattern.
      'no-empty-pattern': 'off',
    },
  },
);
