import { test } from "node:test";
import assert from "node:assert/strict";
import { existsSync, writeFileSync, mkdirSync } from "node:fs";
import { join } from "node:path";
import { TempDirs } from "./tempdirs.ts";

void test("with removes the dir after a successful run", async () => {
  const dirs = new TempDirs();
  await dirs.with("skipper-tempdirs-test-", (d) => {
    mkdirSync(join(d, "nested"));
    writeFileSync(join(d, "nested", "f"), "x");
    assert.ok(existsSync(d));
  });
  assert.equal(dirs.created().length, 1);
  assert.deepEqual(dirs.leaked(), []);
});

void test("with removes the dir after a forced failure", async () => {
  const dirs = new TempDirs();
  await assert.rejects(
    dirs.with("skipper-tempdirs-test-", (d) => {
      writeFileSync(join(d, "f"), "x");
      throw new Error("boom");
    }),
    /boom/,
  );
  assert.equal(dirs.created().length, 1);
  assert.deepEqual(dirs.leaked(), []);
});

void test("removeAll clears every recorded dir and is idempotent", () => {
  const dirs = new TempDirs();
  const a = dirs.create("skipper-tempdirs-test-");
  const b = dirs.create("skipper-tempdirs-test-");
  assert.deepEqual(dirs.leaked(), [a, b]);
  dirs.removeAll();
  dirs.removeAll();
  assert.deepEqual(dirs.leaked(), []);
});
