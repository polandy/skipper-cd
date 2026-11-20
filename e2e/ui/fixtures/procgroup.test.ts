import { test } from "node:test";
import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { mkdtempSync, mkdirSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { groupAlive, killGroup, procStat } from "./procgroup.ts";

void test("procStat reads state and group past a command name with spaces and parens", () => {
  assert.deepEqual(procStat("42 (sh (x) y) S 1 4242 4242 0 -1"), {
    state: "S",
    pgrp: 4242,
  });
});

/** startGroup spawns a group leader with a background child and resolves once both run. */
async function startGroup(): Promise<number> {
  const proc = spawn("sh", ["-c", "sleep 30 & echo up; wait"], {
    detached: true,
    stdio: ["ignore", "pipe", "ignore"],
  });
  await new Promise<void>((res) => proc.stdout.once("data", () => res()));
  proc.stdout.destroy();
  proc.unref();
  return proc.pid!;
}

void test("killGroup returns only once the leader's children are gone too", async () => {
  const pgid = await startGroup();
  assert.equal(groupAlive(pgid), true);
  await killGroup(pgid);
  assert.equal(groupAlive(pgid), false);
});

void test("killGroup on a group already gone returns at once", async () => {
  const pgid = await startGroup();
  await killGroup(pgid);
  await killGroup(pgid);
});

void test("killGroup without a /proc falls back to signal 0", async () => {
  const pgid = await startGroup();
  assert.equal(groupAlive(pgid, "/nonexistent-proc"), true);
  await killGroup(pgid, undefined, "/nonexistent-proc");
  assert.equal(groupAlive(pgid, "/nonexistent-proc"), false);
});

void test("groupAlive ignores zombies, other groups and entries that vanish mid-scan", () => {
  const root = mkdtempSync(join(tmpdir(), "skipper-procgroup-test-"));
  try {
    const entry = (pid: string, stat: string) => {
      mkdirSync(join(root, pid));
      writeFileSync(join(root, pid, "stat"), stat);
    };
    entry("10", "10 (zombie) Z 1 77 77");
    entry("11", "11 (other) S 1 78 78");
    mkdirSync(join(root, "12")); // exited: no stat left to read
    writeFileSync(join(root, "self"), "");
    assert.equal(groupAlive(77, root), false);
    entry("13", "13 (live) R 1 77 77");
    assert.equal(groupAlive(77, root), true);
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});

void test("killGroup fails loudly when the group outlives its deadline", async () => {
  const root = mkdtempSync(join(tmpdir(), "skipper-procgroup-test-"));
  try {
    // A killed group (the signal hits nothing real) that a fake /proc keeps listing.
    const pgid = await startGroup();
    await killGroup(pgid);
    mkdirSync(join(root, "1"));
    writeFileSync(join(root, "1", "stat"), `1 (stuck) S 0 ${pgid} ${pgid}`);
    await assert.rejects(killGroup(pgid, 0, root), /still running 0ms after SIGKILL/);
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});
