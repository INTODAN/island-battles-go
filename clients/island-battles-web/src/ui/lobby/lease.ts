import { ApiError } from "../../api/errors";

export const heartbeatIntervalMs = 15_000;

export interface LeaseDeps {
  /** Sends one heartbeat with the current `membership_generation`; null when there is no membership to keep. */
  heartbeat(): Promise<void> | null;
  /** The membership or the session is gone; the screen leaves the room. */
  onLost(error: ApiError): void;
  schedule(run: () => void, ms: number): () => void;
  intervalMs?: number;
}

/** Keeps a membership's lease alive: one heartbeat every 15 s until stopped. */
export function startHeartbeat(deps: LeaseDeps): { stop(): void } {
  let stopped = false;
  let cancel: (() => void) | undefined;
  const interval = deps.intervalMs ?? heartbeatIntervalMs;
  const arm = () => {
    cancel = deps.schedule(() => void beat(), interval);
  };
  const beat = async () => {
    try {
      await deps.heartbeat();
    } catch (error) {
      if (stopped) return;
      if (
        error instanceof ApiError &&
        (error.status === 401 ||
          error.code === "stale_membership" ||
          error.code === "not_found")
      ) {
        stopped = true;
        deps.onLost(error);
        return;
      }
      // A failed beat is retried at the next interval; the lease has a grace period.
    }
    if (!stopped) arm();
  };
  arm();
  return {
    stop() {
      stopped = true;
      cancel?.();
    },
  };
}

export interface ReleaseTarget {
  gameKey: string;
  lobbyId: string;
  clientInstanceId: string;
  membershipGeneration: number;
  accessToken: string;
}

/**
 * Releases this tab's lease when the page goes away. `keepalive` lets the
 * request outlive the page; the answer is never read.
 */
export function releaseLease(
  target: ReleaseTarget,
  doFetch: typeof fetch = ((...args: Parameters<typeof fetch>) =>
    globalThis.fetch(...args)) as typeof fetch,
): void {
  const path = `/lobby/v1/games/${encodeURIComponent(target.gameKey)}/lobbies/${encodeURIComponent(target.lobbyId)}/leases/${encodeURIComponent(target.clientInstanceId)}`;
  try {
    void doFetch(`${path}?generation=${target.membershipGeneration}`, {
      method: "DELETE",
      keepalive: true,
      headers: { Authorization: `Bearer ${target.accessToken}` },
    }).catch(() => undefined);
  } catch {
    // The page is going away; there is nothing more to do.
  }
}
