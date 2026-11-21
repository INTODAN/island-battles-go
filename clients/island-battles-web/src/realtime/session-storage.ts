import { isDesktop, tabStorage } from "../platform";

/** The part of `Storage` the module uses; `sessionStorage` fits. */
export interface StorageLike {
  getItem(key: string): string | null;
  setItem(key: string, value: string): void;
  removeItem(key: string): void;
}

/** The lobby account of a session signed in with Google. A guest's session has none. */
export interface SessionAccount {
  id: string;
  /** The name the person chose; never anything of Google's. */
  displayName: string;
}

export interface Session {
  playerId: string;
  accessToken: string;
  /** When the server ends the session, as epoch milliseconds. */
  expiresAt: number;
  displayName: string;
  /** Set for a session signed in with Google; absent for a guest. */
  account?: SessionAccount;
}

const isAccount = (value: unknown): value is SessionAccount =>
  typeof value === "object" &&
  value !== null &&
  typeof (value as Partial<SessionAccount>).id === "string" &&
  typeof (value as Partial<SessionAccount>).displayName === "string";

export const sessionStorageKey = "island-battles-web.session";

/**
 * The browser's latest session signed in with Google (#409), in `localStorage`: a tab without a
 * session of its own takes it, so a new tab or a restarted browser stays signed in.
 */
export const rememberedSessionKey = "island-battles-web.google-session";

/** Reads the saved session; null when missing, malformed or expired. */
export function loadSession(
  storage: StorageLike | undefined,
  now: number,
  key = sessionStorageKey,
): Session | null {
  try {
    const raw = storage?.getItem(key);
    if (!raw) return null;
    const value = JSON.parse(raw) as Partial<Session> | null;
    if (
      value &&
      typeof value.playerId === "string" &&
      typeof value.accessToken === "string" &&
      typeof value.displayName === "string" &&
      typeof value.expiresAt === "number" &&
      value.expiresAt > now &&
      (value.account === undefined || isAccount(value.account))
    ) {
      return value as Session;
    }
    storage?.removeItem(key);
  } catch {
    // Storage may be blocked; the page then works without it.
  }
  return null;
}

export function saveSession(
  storage: StorageLike | undefined,
  session: Session | null,
  key = sessionStorageKey,
): void {
  try {
    if (session) storage?.setItem(key, JSON.stringify(session));
    else storage?.removeItem(key);
  } catch {
    // Storage may be blocked or full; the session then lives for this page only.
  }
}

/** The tab's storage (`localStorage` in the desktop build), or undefined where the browser blocks it. */
export function defaultStorage(): StorageLike | undefined {
  try {
    return tabStorage();
  } catch {
    return undefined;
  }
}

/**
 * Where a session signed in with Google is remembered across tabs: `localStorage`, or undefined
 * where the browser blocks it, and in the desktop app, whose own session already outlives the
 * window and which doesn't offer Google.
 */
export function defaultRememberStorage(): StorageLike | undefined {
  if (isDesktop) return undefined;
  try {
    return globalThis.localStorage ?? undefined;
  } catch {
    return undefined;
  }
}
