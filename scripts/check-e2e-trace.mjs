#!/usr/bin/env node
// Fail when the e2e suite and docs/dev/e2e-tests.md disagree on test IDs.
//
// Every Playwright test or describe titled `ID: ...` and every Go test named
// `TestID_...` must be specified as a `- **ID — ...` case, and no ID may
// be claimed by two tests, so a case cannot silently lose or share its spec.
import { readdirSync, readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const root = join(dirname(fileURLToPath(import.meta.url)), '..');
const doc = readFileSync(join(root, 'docs/dev/e2e-tests.md'), 'utf8');

const specified = new Set([...doc.matchAll(/^- \*\*([A-Z]+\d+[a-z]?) —/gm)].map((m) => m[1]));

const found = new Map(); // id -> [file, ...]
const record = (id, file) => found.set(id, [...(found.get(id) ?? []), file]);

const uiDir = join(root, 'e2e/ui/tests');
for (const f of readdirSync(uiDir).filter((n) => n.endsWith('.spec.ts'))) {
  const src = readFileSync(join(uiDir, f), 'utf8');
  for (const m of src.matchAll(/^\s*test(?:\.describe)?\(\s*['"`]([A-Z]+\d+[a-z]?):/gm)) record(m[1], `e2e/ui/tests/${f}`);
}
const goDir = join(root, 'e2e');
for (const f of readdirSync(goDir).filter((n) => n.endsWith('_test.go'))) {
  const src = readFileSync(join(goDir, f), 'utf8');
  for (const m of src.matchAll(/^func Test([A-Z]+\d+[a-z]?)_/gm)) record(m[1], `e2e/${f}`);
}

const problems = [];
for (const [id, files] of [...found].sort()) {
  if (!specified.has(id)) problems.push(`${id} (${files.join(', ')}) has no "- **${id} —" case in docs/dev/e2e-tests.md`);
  if (files.length > 1) problems.push(`${id} is claimed ${files.length} times: ${files.join(', ')}`);
}

if (problems.length > 0) {
  console.error('ERROR: e2e test IDs and docs/dev/e2e-tests.md disagree:');
  for (const p of problems) console.error(`  ${p}`);
  process.exit(1);
}
console.log(`OK: ${found.size} e2e test IDs, each specified in the doc and used once.`);
