import { existsSync, readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { setTimeout as sleep } from "node:timers/promises";

/** How long a SIGKILLed process group may take to be gone before teardown fails. */
export const GROUP_EXIT_DEADLINE_MS = 10_000;

/** procStat reads state and pgrp from a /proc/<pid>/stat line, counting past the last ')' of the comm. */
export function procStat(line: string): { state: string; pgrp: number } {
  const fields = line.slice(line.lastIndexOf(")") + 2).split(" ");
  return { state: fields[0], pgrp: Number(fields[2]) };
}

/** groupAlive reports a non-zombie member of pgid (a non-reaping container PID 1 keeps zombies); no /proc falls back to signal 0. */
export function groupAlive(pgid: number, procRoot = "/proc"): boolean {
  if (!existsSync(procRoot)) return signalReaches(pgid);
  for (const pid of readdirSync(procRoot)) {
    if (!/^\d+$/.test(pid)) continue;
    let line: string;
    try {
      line = readFileSync(join(procRoot, pid, "stat"), "utf8");
    } catch {
      continue; // exited between the listing and the read
    }
    const { state, pgrp } = procStat(line);
    if (pgrp === pgid && state !== "Z" && state !== "X") return true;
  }
  return false;
}

function signalReaches(pgid: number): boolean {
  try {
    process.kill(-pgid, 0);
    return true;
  } catch {
    return false;
  }
}

/** killGroup SIGKILLs group pgid and resolves once no member runs, so no orphan still writes into the temp tree. */
export async function killGroup(
  pgid: number,
  deadlineMs = GROUP_EXIT_DEADLINE_MS,
  procRoot = "/proc",
): Promise<void> {
  try {
    process.kill(-pgid, "SIGKILL");
  } catch (e) {
    if ((e as NodeJS.ErrnoException).code !== "ESRCH") throw e;
  }
  const deadline = Date.now() + deadlineMs;
  while (groupAlive(pgid, procRoot)) {
    if (Date.now() > deadline) {
      throw new Error(`process group ${pgid} still running ${deadlineMs}ms after SIGKILL`);
    }
    // No kernel wait exists for a whole group; the SIGKILL is certain to land.
    await sleep(10);
  }
}
