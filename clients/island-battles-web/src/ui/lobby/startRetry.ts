import { ApiError } from "../../api/errors";

/** Delays before the first, second and third retry; after that the delay is `steadyDelayMs`. */
export const startRetryDelaysMs = [1000, 2000, 4000];
export const steadyDelayMs = 10_000;

export interface StartRetryDecision {
  retry: boolean;
  delayMs: number;
}

/**
 * Decides what to do after a read of the game failed right after the lobby
 * became `started`. The lobby is `started` before the game exists (#92), so a
 * read can get 404 (no game yet) or 409 (the game is being made); both, and
 * a failure to reach the server, are retried with growing delays: 1 s, 2 s,
 * 4 s, then every 10 s. Anything else, such as 403 (not on the roster) or
 * 401, will not change by waiting. When the game could not start, the lobby
 * stays started without a game until the host starts it again, which
 * `watchForGame`'s `restart` does.
 * @param attempt how many reads have failed so far, counting this one, from 1
 */
export function decideStartRetry(
  error: unknown,
  attempt: number,
): StartRetryDecision {
  const waiting =
    error instanceof ApiError &&
    (error.status === 404 ||
      error.status === 409 ||
      error.kind === "transport" ||
      error.status >= 500);
  if (!waiting) return { retry: false, delayMs: 0 };
  return {
    retry: true,
    delayMs: startRetryDelaysMs[attempt - 1] ?? steadyDelayMs,
  };
}

export interface GameWatcherDeps {
  /** Reads the game once, rejecting with `ApiError` while it is not there. */
  probe(): Promise<void>;
  /** The read worked; ask the session to take the state in. */
  onReady(): void;
  /**
   * Asks the lobby to start the game again, after a read found no game (404):
   * the lobby's start (`POST …/start`) on a started lobby asks the game again
   * until it has made the round's game, and changes nothing once it has. Only
   * the host may, so only the host's screen passes it. Its failures are
   * ignored: the next read tries again.
   */
  restart?(): Promise<void>;
  /** The read failed for a reason waiting will not fix. */
  onFatal(error: unknown): void;
  schedule(run: () => void, ms: number): () => void;
}

/**
 * Reads the game until it exists, with the delays of `decideStartRetry`,
 * restarting it after each read that finds none, when `restart` is given. It
 * stops when `stop()` is called, which the screen does when the lobby changes
 * (the player left, or the lobby is gone).
 */
export function watchForGame(deps: GameWatcherDeps): { stop(): void } {
  let stopped = false;
  let cancel: (() => void) | undefined;
  let failures = 0;
  const attempt = async () => {
    try {
      await deps.probe();
    } catch (error) {
      if (stopped) return;
      failures++;
      const decision = decideStartRetry(error, failures);
      if (!decision.retry) {
        deps.onFatal(error);
        return;
      }
      if (deps.restart && error instanceof ApiError && error.status === 404) {
        await deps.restart().catch(() => undefined);
        if (stopped) return;
      }
      cancel = deps.schedule(() => void attempt(), decision.delayMs);
      return;
    }
    if (!stopped) deps.onReady();
  };
  void attempt();
  return {
    stop() {
      stopped = true;
      cancel?.();
    },
  };
}
