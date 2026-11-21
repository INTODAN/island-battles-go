// One tab per player (#409). A tab that has a session signed in with Google holds a Web Lock
// named after its player, so another tab takes the remembered session only once that tab is
// closed: the browser lets go of a closed tab's locks.

/** The part of the Web Locks API's `LockManager` the module uses. */
export interface LockManagerLike {
  request(
    name: string,
    options: { ifAvailable: boolean },
    callback: (lock: unknown) => Promise<void> | undefined,
  ): Promise<unknown>;
}

export interface SessionLock {
  /**
   * Holds the lock of `playerId` for this tab, letting go of any other it held. Resolves false
   * when another tab holds it, and true where the browser has no Web Locks.
   */
  claim(playerId: string): Promise<boolean>;
  /** Lets go of the held lock. */
  release(): void;
}

export const sessionLockName = (playerId: string): string =>
  `island-battles-web.player.${playerId}`;

function defaultLocks(): LockManagerLike | undefined {
  try {
    return (globalThis.navigator as { locks?: LockManagerLike } | undefined)
      ?.locks;
  } catch {
    return undefined;
  }
}

export function createSessionLock(
  locks: LockManagerLike | undefined = defaultLocks(),
): SessionLock {
  let held:
    | { playerId: string; claimed: Promise<boolean>; release?: () => void }
    | undefined;
  const release = () => {
    const entry = held;
    held = undefined;
    entry?.release?.();
  };
  return {
    claim(playerId) {
      if (held?.playerId === playerId) return held.claimed;
      release();
      if (!locks) return Promise.resolve(true);
      const entry: {
        playerId: string;
        claimed: Promise<boolean>;
        release?: () => void;
      } = { playerId, claimed: Promise.resolve(false) };
      held = entry;
      entry.claimed = new Promise<boolean>((resolve) => {
        locks
          .request(sessionLockName(playerId), { ifAvailable: true }, (lock) => {
            // Another tab holds it, or this tab let go before it was granted.
            if (!lock || held !== entry) {
              if (held === entry) held = undefined;
              resolve(false);
              return undefined;
            }
            resolve(true);
            return new Promise<void>((done) => {
              entry.release = done;
            });
          })
          // A browser that refuses the request (an opaque origin) can't tell either.
          .catch(() => resolve(true));
      });
      return entry.claimed;
    },
    release,
  };
}
