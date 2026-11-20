/** retryOnFreshWorkspace runs launch on a newly scaffolded workspace, retrying
 *  up to maxAttempts while isRetryable(err) holds. Every failed attempt's
 *  workspace is discarded and never reused: the failed instance may already
 *  have written state into it. */
export async function retryOnFreshWorkspace<W, R>(
  scaffold: () => Promise<W>,
  launch: (ws: W) => Promise<R>,
  discard: (ws: W) => void,
  maxAttempts: number,
  isRetryable: (err: unknown) => boolean,
): Promise<R> {
  for (let attempt = 1; ; attempt++) {
    const ws = await scaffold();
    try {
      return await launch(ws);
    } catch (err) {
      discard(ws);
      if (attempt >= maxAttempts || !isRetryable(err)) throw err;
    }
  }
}
