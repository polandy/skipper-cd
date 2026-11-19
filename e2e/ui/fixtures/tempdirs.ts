import { mkdtempSync, rmSync, existsSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

/** TempDirs creates temp dirs and remembers them, so a run can remove every one
 *  on success, failure or interrupt — and a test can assert none is left. */
export class TempDirs {
  private readonly paths: string[] = [];

  /** create makes a fresh dir under the OS temp dir and records it. */
  create(prefix: string): string {
    const dir = mkdtempSync(join(tmpdir(), prefix));
    this.paths.push(dir);
    return dir;
  }

  /** created lists every path handed out so far. */
  created(): readonly string[] {
    return this.paths;
  }

  /** remove deletes one recorded dir; safe to call twice. */
  remove(dir: string): void {
    rmSync(dir, { recursive: true, force: true });
  }

  /** removeAll deletes every recorded dir that still exists. */
  removeAll(): void {
    for (const dir of this.paths) this.remove(dir);
  }

  /** leaked lists the recorded dirs that still exist. */
  leaked(): string[] {
    return this.paths.filter((p) => existsSync(p));
  }

  /** with runs fn on a fresh dir and removes it afterwards, even when fn throws. */
  async with<T>(prefix: string, fn: (dir: string) => Promise<T> | T): Promise<T> {
    const dir = this.create(prefix);
    try {
      return await fn(dir);
    } finally {
      this.remove(dir);
    }
  }
}

/** workspaces records every dir the harness scaffolds in this process. */
export const workspaces = new TempDirs();
