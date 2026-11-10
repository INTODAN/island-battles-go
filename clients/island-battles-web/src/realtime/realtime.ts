import { createApiClients } from "../api/client";
import { ApiError } from "../api/errors";
import {
  type CentrifugeLike,
  type CreateCentrifuge,
  createCentrifuge,
  type SubscriptionLike,
  UnauthorizedError,
} from "./centrifuge";
import {
  type GenerationState,
  nextGeneration,
  nextSnapshot,
  nextWatch,
  type Snapshot,
  type WatchState,
} from "./install";
import { Reconciler } from "./reconciler";
import {
  defaultStorage,
  loadSession,
  type Session,
  type StorageLike,
  saveSession,
} from "./session-storage";
import { createStore, type ReadableStore } from "./store";

export type ConnectionState = "connecting" | "connected" | "disconnected";

/** Why a store was emptied or stopped following, for the UI to explain. */
export interface RealtimeProblem {
  scope: "session" | "lobby" | "game" | "watch";
  error: ApiError;
}

export interface RealtimeOptions {
  /** Server origin for the HTTP API. Defaults to the page's own origin. */
  baseUrl?: string;
  /** Replaces `fetch`; tests use it. */
  fetch?: typeof fetch;
  /** Where the guest session is kept. Defaults to `sessionStorage`; null keeps the session in memory only. */
  storage?: StorageLike | null;
  /** Builds the Centrifugo client; tests pass a fake. */
  createClient?: CreateCentrifuge;
  /** Epoch milliseconds. */
  now?: () => number;
  /** Reconcile requests this close together share one read. Default 50. */
  settleMs?: number;
  /** First retry delay of a failed read; doubles up to `retryMaxMs`. Default 500. */
  retryBaseMs?: number;
  /** Default 10000. */
  retryMaxMs?: number;
}

export interface Realtime {
  /** The guest session, or null before sign-in and after it ended. */
  readonly session: ReadableStore<Session | null>;
  /** The latest snapshot of the followed lobby. */
  readonly lobby: ReadableStore<Snapshot | null>;
  /** The latest generation state of the followed game. */
  readonly game: ReadableStore<GenerationState | null>;
  /** The latest state of the watched game, for a watcher outside its roster. */
  readonly watch: ReadableStore<WatchState | null>;
  readonly connection: ReadableStore<ConnectionState>;
  /** Set when the session ended or a followed lobby or game became unavailable. */
  readonly problem: ReadableStore<RealtimeProblem | null>;
  /** Reuses the saved session while it lasts, else creates a guest session. */
  signIn(displayName: string): Promise<Session>;
  /** Follows a lobby: reads its snapshot, then keeps it current. */
  followLobby(target: { gameKey: string; lobbyId: string }): void;
  /** Installs a snapshot an HTTP call returned (create, join, start...) and follows its lobby. */
  adoptSnapshot(snapshot: Snapshot): void;
  leaveLobby(): void;
  /**
   * Follows the signed-in player's game of a lobby round. A lobby plays one game per round,
   * and states of another round's game are dropped.
   */
  followGame(lobbyId: string, round: number): void;
  /** Reads the followed game again now, for a caller that learned the game exists. */
  refreshGame(): void;
  leaveGame(): void;
  /**
   * Watches a lobby's game on its watch channel (from `POST /lobby/v1/watches`), reading
   * `GET /game/from-lobby/{lobby_id}/watch` on every subscribe. There is no lease and no
   * heartbeat. A watcher the host kicked is unsubscribed by the server; the read that follows
   * fails, and `problem` gets scope `watch`.
   */
  followWatch(target: { lobbyId: string; channel: string }): void;
  leaveWatch(): void;
  /** Ends the session and closes the connection. */
  signOut(): void;
}

const lobbyPath = (gameKey: string, lobbyId: string) => ({
  params: { path: { game_key: gameKey, lobby_id: lobbyId } },
});

function required<T>(data: T | undefined): T {
  if (data === undefined) {
    throw new ApiError({ kind: "http", status: 0, detail: "empty response" });
  }
  return data;
}

const isUnauthorized = (error: unknown) =>
  error instanceof ApiError && error.status === 401;

/** The lobby no longer lists this player, or does not exist. */
const isLobbyGone = (error: unknown) =>
  error instanceof ApiError &&
  (error.code === "not_found" ||
    error.code === "stale_membership" ||
    error.status === 404);

const isRecord = (value: unknown): value is Record<string, unknown> =>
  typeof value === "object" && value !== null;

interface LobbyFollow {
  epoch: number;
  gameKey: string;
  lobbyId: string;
  /** The highest `lobby.changed` version announced, to avoid reading twice for one change. */
  announced: number;
  subscription: SubscriptionLike | undefined;
  reconciler: Reconciler;
}

interface GameFollow {
  epoch: number;
  lobbyId: string;
  round: number;
  playerId: string;
  channel: string;
  subscription: SubscriptionLike | undefined;
  reconciler: Reconciler;
}

interface WatchFollow {
  epoch: number;
  lobbyId: string;
  channel: string;
  subscription: SubscriptionLike | undefined;
  reconciler: Reconciler;
}

export function createRealtime(options: RealtimeOptions = {}): Realtime {
  const now = options.now ?? (() => Date.now());
  const storage =
    options.storage === undefined
      ? defaultStorage()
      : (options.storage ?? undefined);
  const createClient = options.createClient ?? createCentrifuge;
  const settleMs = options.settleMs ?? 50;
  const retryBaseMs = options.retryBaseMs ?? 500;
  const retryMaxMs = options.retryMaxMs ?? 10_000;

  const session = createStore<Session | null>(loadSession(storage, now()));
  const lobby = createStore<Snapshot | null>(null);
  const game = createStore<GenerationState | null>(null);
  const watch = createStore<WatchState | null>(null);
  const connection = createStore<ConnectionState>("disconnected");
  const problem = createStore<RealtimeProblem | null>(null);

  const { lobbyApi, gameApi } = createApiClients({
    ...(options.baseUrl === undefined ? {} : { baseUrl: options.baseUrl }),
    ...(options.fetch === undefined ? {} : { fetch: options.fetch }),
    getAccessToken: () => session.get()?.accessToken,
  });

  // Each epoch counts the sessions, lobbies or games the client has left; an
  // async result that finds its epoch changed belongs to something left.
  let sessionEpoch = 0;
  let lobbyEpoch = 0;
  let gameEpoch = 0;
  let watchEpoch = 0;
  let lobbyFollow: LobbyFollow | undefined;
  let gameFollow: GameFollow | undefined;
  let watchFollow: WatchFollow | undefined;
  let client: CentrifugeLike | undefined;
  let clientStarting = false;
  let clientRetry: ReturnType<typeof setTimeout> | undefined;
  let clientFailures = 0;
  let expiryTimer: ReturnType<typeof setTimeout> | undefined;

  async function fetchConnectionToken() {
    const { data } = await lobbyApi.POST("/lobby/v1/connection-token", {
      body: {},
    });
    return required(data);
  }

  function endSession(error?: ApiError): void {
    sessionEpoch++;
    lobbyEpoch++;
    gameEpoch++;
    watchEpoch++;
    lobbyFollow?.reconciler.cancel();
    gameFollow?.reconciler.cancel();
    watchFollow?.reconciler.cancel();
    lobbyFollow = undefined;
    gameFollow = undefined;
    watchFollow = undefined;
    clearTimeout(clientRetry);
    clearTimeout(expiryTimer);
    clientRetry = undefined;
    clientStarting = false;
    clientFailures = 0;
    const closing = client;
    client = undefined;
    closing?.disconnect();
    saveSession(storage, null);
    session.set(null);
    lobby.set(null);
    game.set(null);
    watch.set(null);
    connection.set("disconnected");
    problem.set(error ? { scope: "session", error } : null);
  }

  function watchExpiry(current: Session | null): void {
    clearTimeout(expiryTimer);
    expiryTimer = undefined;
    if (!current) return;
    const expiredAt = current.expiresAt;
    const wait = Math.min(Math.max(expiredAt - now(), 0), 2 ** 31 - 1);
    expiryTimer = setTimeout(() => {
      if (session.get()?.expiresAt === expiredAt) {
        if (now() >= expiredAt) {
          endSession(
            new ApiError({
              kind: "http",
              status: 401,
              detail: "The session expired.",
            }),
          );
        } else {
          watchExpiry(session.get());
        }
      }
    }, wait);
  }
  watchExpiry(session.get());

  function subscribeChannel(
    channel: string,
    onPublication: (data: unknown) => void,
    reconcile: () => void,
  ): SubscriptionLike | undefined {
    if (!client) return undefined;
    const subscription = client.newSubscription(channel, {
      recoverable: true,
      positioned: true,
    });
    subscription.on("subscribed", reconcile);
    subscription.on("unsubscribed", reconcile);
    subscription.on("publication", ({ data }) => onPublication(data));
    subscription.subscribe();
    return subscription;
  }

  function dropSubscription(subscription: SubscriptionLike | undefined): void {
    if (!subscription || !client) return;
    subscription.unsubscribe();
    client.removeSubscription(subscription);
  }

  /** Starts the Centrifugo client once a session exists, retrying with backoff. */
  function ensureClient(): void {
    if (client || clientStarting || !session.get()) return;
    const epoch = sessionEpoch;
    clientStarting = true;
    connection.set("connecting");
    void (async () => {
      try {
        const first = await fetchConnectionToken();
        if (epoch !== sessionEpoch) return;
        const created = createClient(first.websocket_url, {
          token: first.token,
          getToken: async () => {
            try {
              return (await fetchConnectionToken()).token;
            } catch (error) {
              if (isUnauthorized(error)) {
                // The session ended; stop for good instead of reconnecting.
                if (epoch === sessionEpoch) endSession(error as ApiError);
                throw new UnauthorizedError("the session ended");
              }
              throw error;
            }
          },
        });
        client = created;
        clientFailures = 0;
        created.on("connecting", () => {
          if (epoch === sessionEpoch) connection.set("connecting");
        });
        created.on("disconnected", () => {
          if (epoch === sessionEpoch) connection.set("disconnected");
        });
        created.on("connected", () => {
          if (epoch !== sessionEpoch) return;
          connection.set("connected");
          lobbyFollow?.reconciler.request();
          gameFollow?.reconciler.request();
          watchFollow?.reconciler.request();
        });
        attachSubscriptions();
        created.connect();
      } catch (error) {
        if (epoch !== sessionEpoch) return;
        if (isUnauthorized(error)) {
          endSession(error as ApiError);
          return;
        }
        connection.set("disconnected");
        clientFailures++;
        clientRetry = setTimeout(
          () => {
            clientRetry = undefined;
            ensureClient();
          },
          Math.min(retryBaseMs * 2 ** (clientFailures - 1), retryMaxMs),
        );
      } finally {
        if (epoch === sessionEpoch) clientStarting = false;
      }
    })();
  }

  function attachSubscriptions(): void {
    if (lobbyFollow && !lobbyFollow.subscription) {
      const snapshot = lobby.get();
      if (snapshot && snapshot.lobby.id === lobbyFollow.lobbyId) {
        attachLobby(lobbyFollow, snapshot.channel);
      }
    }
    if (gameFollow && !gameFollow.subscription) attachGame(gameFollow);
    if (watchFollow && !watchFollow.subscription) attachWatch(watchFollow);
  }

  // --- Lobby ---

  function attachLobby(follow: LobbyFollow, channel: string): void {
    follow.subscription = subscribeChannel(
      channel,
      (data) => {
        if (follow.epoch !== lobbyEpoch) return;
        // Ignore events of an unknown schema version, and events for another lobby.
        if (
          !isRecord(data) ||
          data.type !== "lobby.changed" ||
          data.schema_version !== 1 ||
          data.lobby_id !== follow.lobbyId ||
          data.game_key !== follow.gameKey ||
          typeof data.version !== "number"
        ) {
          return;
        }
        const known = Math.max(
          lobby.get()?.lobby.version ?? -1,
          follow.announced,
        );
        if (data.version <= known) return;
        follow.announced = data.version;
        follow.reconciler.request();
      },
      () => {
        if (follow.epoch === lobbyEpoch) follow.reconciler.request();
      },
    );
  }

  async function readLobby(follow: LobbyFollow): Promise<void> {
    try {
      const { data } = await lobbyApi.GET(
        "/lobby/v1/games/{game_key}/lobbies/{lobby_id}",
        lobbyPath(follow.gameKey, follow.lobbyId),
      );
      if (follow.epoch !== lobbyEpoch) return;
      const next = nextSnapshot(lobby.get(), required(data), follow.lobbyId);
      if (next) lobby.set(next);
      if (!follow.subscription) {
        if (client) attachLobby(follow, required(data).channel);
        else ensureClient();
      }
      // A read behind a change we were told about: read again.
      if ((lobby.get()?.lobby.version ?? -1) < follow.announced) {
        throw new Error("the lobby read is older than the announced version");
      }
    } catch (error) {
      if (follow.epoch !== lobbyEpoch) return;
      if (isUnauthorized(error)) {
        endSession(error as ApiError);
      } else if (isLobbyGone(error)) {
        leaveLobby();
        problem.set({ scope: "lobby", error: error as ApiError });
      } else {
        throw error;
      }
    }
  }

  function leaveLobby(): void {
    lobbyEpoch++;
    const follow = lobbyFollow;
    lobbyFollow = undefined;
    follow?.reconciler.cancel();
    dropSubscription(follow?.subscription);
    lobby.set(null);
  }

  function startLobbyFollow(gameKey: string, lobbyId: string): LobbyFollow {
    if (
      lobbyFollow &&
      lobbyFollow.lobbyId === lobbyId &&
      lobbyFollow.gameKey === gameKey
    ) {
      return lobbyFollow;
    }
    leaveLobby();
    problem.set(null);
    const epoch = lobbyEpoch;
    const follow: LobbyFollow = {
      epoch,
      gameKey,
      lobbyId,
      announced: -1,
      subscription: undefined,
      reconciler: new Reconciler({
        run: () => readLobby(follow),
        retryable: () => true,
        settleMs,
        retryBaseMs,
        retryMaxMs,
      }),
    };
    lobbyFollow = follow;
    ensureClient();
    return follow;
  }

  // --- Game ---

  function attachGame(follow: GameFollow): void {
    follow.subscription = subscribeChannel(
      follow.channel,
      (data) => {
        if (follow.epoch !== gameEpoch) return;
        if (
          !isRecord(data) ||
          data.schema_version !== 1 ||
          (data.type !== "game.next_generation" &&
            data.type !== "game.ended") ||
          !isRecord(data.state)
        ) {
          return;
        }
        installGame(follow, data.state as unknown as GenerationState);
      },
      () => {
        if (follow.epoch === gameEpoch) follow.reconciler.request();
      },
    );
  }

  /** Installs a state only for this player's game of this lobby round on its channel. */
  function installGame(follow: GameFollow, state: GenerationState): void {
    if (
      state.channel !== follow.channel ||
      state.lobby_id !== follow.lobbyId ||
      state.lobby_round !== follow.round ||
      state.player_id !== follow.playerId
    ) {
      return;
    }
    const next = nextGeneration(game.get(), state);
    if (next) game.set(next);
  }

  async function readGame(follow: GameFollow): Promise<void> {
    try {
      const { data } = await gameApi.GET(
        "/game/from-lobby/{lobby_id}/generation",
        { params: { path: { lobby_id: follow.lobbyId } } },
      );
      if (follow.epoch !== gameEpoch) return;
      installGame(follow, required(data));
    } catch (error) {
      if (follow.epoch !== gameEpoch) return;
      if (isUnauthorized(error)) {
        endSession(error as ApiError);
      } else if (error instanceof ApiError && error.status === 404) {
        // No game yet (or it was deleted): its first event arrives on the channel.
      } else if (error instanceof ApiError && error.status === 403) {
        leaveGame();
        problem.set({ scope: "game", error });
      } else {
        throw error;
      }
    }
  }

  function leaveGame(): void {
    gameEpoch++;
    const follow = gameFollow;
    gameFollow = undefined;
    follow?.reconciler.cancel();
    dropSubscription(follow?.subscription);
    game.set(null);
  }

  // --- Watching ---

  function attachWatch(follow: WatchFollow): void {
    follow.subscription = subscribeChannel(
      follow.channel,
      (data) => {
        if (follow.epoch !== watchEpoch) return;
        if (
          !isRecord(data) ||
          data.schema_version !== 1 ||
          (data.type !== "game.next_generation" &&
            data.type !== "game.ended") ||
          !isRecord(data.state)
        ) {
          return;
        }
        installWatch(follow, data.state as unknown as WatchState);
      },
      () => {
        if (follow.epoch === watchEpoch) follow.reconciler.request();
      },
    );
  }

  /** Installs a state only for this lobby's game on its watch channel. */
  function installWatch(follow: WatchFollow, state: WatchState): void {
    if (state.channel !== follow.channel || state.lobby_id !== follow.lobbyId) {
      return;
    }
    const next = nextWatch(watch.get(), state);
    if (next) watch.set(next);
  }

  async function readWatch(follow: WatchFollow): Promise<void> {
    try {
      const { data } = await gameApi.GET("/game/from-lobby/{lobby_id}/watch", {
        params: { path: { lobby_id: follow.lobbyId } },
      });
      if (follow.epoch !== watchEpoch) return;
      installWatch(follow, required(data));
    } catch (error) {
      if (follow.epoch !== watchEpoch) return;
      if (isUnauthorized(error)) {
        endSession(error as ApiError);
      } else if (error instanceof ApiError && error.status === 404) {
        // No game yet: its first turn arrives on the channel.
      } else if (error instanceof ApiError && error.status === 403) {
        // The host kicked this watcher.
        leaveWatch();
        problem.set({ scope: "watch", error });
      } else {
        throw error;
      }
    }
  }

  function leaveWatch(): void {
    watchEpoch++;
    const follow = watchFollow;
    watchFollow = undefined;
    follow?.reconciler.cancel();
    dropSubscription(follow?.subscription);
    watch.set(null);
  }

  // --- Public API ---

  function leaveLobbyPublic(): void {
    leaveLobby();
    problem.set(null);
  }

  return {
    session,
    lobby,
    game,
    watch,
    connection,
    problem,

    async signIn(displayName) {
      const held = session.get();
      if (held && held.expiresAt > now()) return held;
      const { data } = await lobbyApi.POST("/lobby/v1/sessions", {
        body: { display_name: displayName },
      });
      const created = required(data);
      const next: Session = {
        playerId: created.player_id,
        accessToken: created.access_token,
        expiresAt: Date.parse(created.expires_at),
        displayName,
      };
      saveSession(storage, next);
      problem.set(null);
      session.set(next);
      watchExpiry(next);
      return next;
    },

    followLobby({ gameKey, lobbyId }) {
      if (!session.get()) throw new Error("sign in before following a lobby");
      const follow = startLobbyFollow(gameKey, lobbyId);
      follow.reconciler.request();
    },

    adoptSnapshot(snapshot) {
      if (!session.get()) throw new Error("sign in before following a lobby");
      const follow = startLobbyFollow(
        snapshot.lobby.game_key,
        snapshot.lobby.id,
      );
      const next = nextSnapshot(lobby.get(), snapshot, follow.lobbyId);
      if (next) lobby.set(next);
      if (!follow.subscription) {
        if (client) attachLobby(follow, snapshot.channel);
        else ensureClient();
      }
    },

    leaveLobby: leaveLobbyPublic,

    followGame(lobbyId, round) {
      const current = session.get();
      if (!current) throw new Error("sign in before following a game");
      if (
        gameFollow &&
        gameFollow.lobbyId === lobbyId &&
        gameFollow.round === round
      ) {
        return;
      }
      leaveGame();
      const epoch = gameEpoch;
      const follow: GameFollow = {
        epoch,
        lobbyId,
        round,
        playerId: current.playerId,
        channel: `player:${current.playerId}`,
        subscription: undefined,
        reconciler: new Reconciler({
          run: () => readGame(follow),
          retryable: () => true,
          settleMs,
          retryBaseMs,
          retryMaxMs,
        }),
      };
      gameFollow = follow;
      if (client) attachGame(follow);
      else ensureClient();
    },

    refreshGame() {
      gameFollow?.reconciler.request();
    },

    leaveGame,

    followWatch({ lobbyId, channel }) {
      if (!session.get()) throw new Error("sign in before watching a game");
      if (watchFollow && watchFollow.lobbyId === lobbyId) return;
      leaveWatch();
      problem.set(null);
      const epoch = watchEpoch;
      const follow: WatchFollow = {
        epoch,
        lobbyId,
        channel,
        subscription: undefined,
        reconciler: new Reconciler({
          run: () => readWatch(follow),
          retryable: () => true,
          settleMs,
          retryBaseMs,
          retryMaxMs,
        }),
      };
      watchFollow = follow;
      if (client) attachWatch(follow);
      else ensureClient();
    },

    leaveWatch,

    signOut() {
      endSession();
    },
  };
}
