/** The game's key in the lobby: the audience of its player tokens. */
export const playerTokenGameKey = "island-battles";

/** A player token is renewed once this share of its life has passed. */
export const playerTokenRenewShare = 0.8;

/** What the lobby's `POST /lobby/v1/player-tokens` answers. */
export interface IssuedPlayerToken {
  token: string;
  expires_at: string;
}

/**
 * The game's routes that take a player token instead of the guest credential:
 * a player's game, its watcher reads, the command batches and a lobby's bot
 * packages.
 */
export const needsPlayerToken = (pathname: string): boolean =>
  pathname.includes("/game/from-lobby/") ||
  pathname.includes("/game/lobbies/") ||
  pathname.endsWith("/generation/commands");

/**
 * The player token the game's routes take, for the current guest session. The
 * lobby issues one when there is none for the session, once
 * `playerTokenRenewShare` of the current one's life has passed, or when the
 * game refused the current one. Callers waiting at the same time share one
 * request.
 */
export class PlayerTokens {
  private current:
    | { token: string; credential: string; renewAt: number }
    | undefined;
  private pending: { credential: string; token: Promise<string> } | undefined;

  constructor(
    private readonly issue: () => Promise<IssuedPlayerToken>,
    private readonly now: () => number = Date.now,
  ) {}

  /**
   * The token for `credential`, the guest session's. `stale` is a token the
   * game refused: a new one replaces it.
   */
  get(credential: string, stale?: string): Promise<string> {
    const current = this.current;
    if (
      current !== undefined &&
      current.credential === credential &&
      current.token !== stale &&
      this.now() < current.renewAt
    ) {
      return Promise.resolve(current.token);
    }
    if (this.pending?.credential === credential) return this.pending.token;
    const pending = { credential, token: this.issueFor(credential) };
    this.pending = pending;
    const done = () => {
      if (this.pending === pending) this.pending = undefined;
    };
    pending.token.then(done, done);
    return pending.token;
  }

  private async issueFor(credential: string): Promise<string> {
    const issuedAt = this.now();
    const { token, expires_at } = await this.issue();
    const life = Date.parse(expires_at) - issuedAt;
    this.current = {
      token,
      credential,
      renewAt: issuedAt + life * playerTokenRenewShare,
    };
    return token;
  }
}

/**
 * Wraps fetch so a request to a game route carries the session's player
 * token, and one answered 401 is sent once more with a new token. Other
 * requests, and any request without a session, go out unchanged.
 */
export function playerTokenFetch(
  inner: typeof fetch,
  tokens: PlayerTokens,
  getAccessToken: () => string | undefined,
): typeof fetch {
  return (async (input: Request | string | URL, init?: RequestInit) => {
    const request = new Request(input, init);
    const credential = getAccessToken();
    if (
      credential === undefined ||
      !needsPlayerToken(new URL(request.url).pathname)
    ) {
      return inner(request);
    }
    // A request body can be read once, so each attempt sends a copy.
    const send = (token: string) => {
      const attempt = request.clone();
      attempt.headers.set("Authorization", `Bearer ${token}`);
      return inner(attempt);
    };
    const token = await tokens.get(credential);
    const response = await send(token);
    if (response.status !== 401) return response;
    return send(await tokens.get(credential, token));
  }) as typeof fetch;
}
