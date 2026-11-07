/** The part of `Storage` the module uses; `sessionStorage` fits. */
export interface StorageLike {
  getItem(key: string): string | null;
  setItem(key: string, value: string): void;
  removeItem(key: string): void;
}

export interface Session {
  playerId: string;
  accessToken: string;
  /** When the server ends the session, as epoch milliseconds. */
  expiresAt: number;
  displayName: string;
}

export const sessionStorageKey = "island-battles-web.session";

/** Reads the saved session; null when missing, malformed or expired. */
export function loadSession(
  storage: StorageLike | undefined,
  now: number,
): Session | null {
  try {
    const raw = storage?.getItem(sessionStorageKey);
    if (!raw) return null;
    const value = JSON.parse(raw) as Partial<Session> | null;
    if (
      value &&
      typeof value.playerId === "string" &&
      typeof value.accessToken === "string" &&
      typeof value.displayName === "string" &&
      typeof value.expiresAt === "number" &&
      value.expiresAt > now
    ) {
      return value as Session;
    }
    storage?.removeItem(sessionStorageKey);
  } catch {
    // Storage may be blocked; the page then works without it.
  }
  return null;
}

export function saveSession(
  storage: StorageLike | undefined,
  session: Session | null,
): void {
  try {
    if (session) storage?.setItem(sessionStorageKey, JSON.stringify(session));
    else storage?.removeItem(sessionStorageKey);
  } catch {
    // Storage may be blocked or full; the session then lives for this page only.
  }
}

/** `sessionStorage`, or undefined where the browser blocks it. */
export function defaultStorage(): StorageLike | undefined {
  try {
    return globalThis.sessionStorage;
  } catch {
    return undefined;
  }
}
