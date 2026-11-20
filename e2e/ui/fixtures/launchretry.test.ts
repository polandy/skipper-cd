import { test } from "node:test";
import assert from "node:assert/strict";
import { retryOnFreshWorkspace } from "./launchretry.ts";

const stolen = new Error("listen EADDRINUSE");
const isStolen = (err: unknown) => err === stolen;

void test("a retried launch runs on a fresh workspace and discards the failed one", async () => {
  let next = 0;
  const launched: number[] = [];
  const discarded: number[] = [];
  const result = await retryOnFreshWorkspace(
    () => Promise.resolve(++next),
    (ws) => {
      launched.push(ws);
      return ws === 1 ? Promise.reject(stolen) : Promise.resolve(`ok on ${ws}`);
    },
    (ws) => discarded.push(ws),
    3,
    isStolen,
  );
  assert.equal(result, "ok on 2");
  assert.deepEqual(launched, [1, 2]);
  assert.deepEqual(discarded, [1]);
});

void test("a non-retryable failure discards its workspace and propagates at once", async () => {
  const boom = new Error("boom");
  const discarded: string[] = [];
  let launches = 0;
  await assert.rejects(
    retryOnFreshWorkspace(
      () => Promise.resolve("ws"),
      () => {
        launches++;
        return Promise.reject(boom);
      },
      (ws) => discarded.push(ws),
      3,
      isStolen,
    ),
    /boom/,
  );
  assert.equal(launches, 1);
  assert.deepEqual(discarded, ["ws"]);
});

void test("retries stop at maxAttempts with every workspace discarded", async () => {
  let next = 0;
  const discarded: number[] = [];
  await assert.rejects(
    retryOnFreshWorkspace(
      () => Promise.resolve(++next),
      () => Promise.reject(stolen),
      (ws) => discarded.push(ws),
      3,
      isStolen,
    ),
    /EADDRINUSE/,
  );
  assert.deepEqual(discarded, [1, 2, 3]);
});
