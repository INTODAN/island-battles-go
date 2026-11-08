import { describe, expect, test } from "bun:test";
import type {
  CentrifugeLike,
  CentrifugeOptions,
  SubscriptionLike,
} from "./centrifuge";
import { UnauthorizedError } from "./centrifuge";
import type { GenerationState, Snapshot } from "./install";
import { createRealtime, type RealtimeOptions } from "./realtime";
import { type StorageLike, sessionStorageKey } from "./session-storage";

// --- Fakes ---

type Listener = (context: never) => void;

class FakeSubscription implements SubscriptionLike {
  listeners = new Map<string, Listener[]>();
  state: "new" | "subscribing" | "unsubscribed" = "new";
  constructor(
    readonly channel: string,
    readonly options: { recoverable: boolean; positioned: boolean },
  ) {}
  on(event: string, listener: Listener): this {
    this.listeners.set(event, [...(this.listeners.get(event) ?? []), listener]);
    return this;
  }
  subscribe() {
    this.state = "subscribing";
  }
  unsubscribe() {
    this.state = "unsubscribed";
  }
  emit(event: string, context: unknown = {}) {
    for (const listener of this.listeners.get(event) ?? []) {
      listener(context as never);
    }
  }
  publish(data: unknown) {
    this.emit("publication", { data });
  }
}

class FakeCentrifuge implements CentrifugeLike {
  listeners = new Map<string, Listener[]>();
  subscriptions: FakeSubscription[] = [];
  removed: SubscriptionLike[] = [];
  connects = 0;
  disconnects = 0;
  constructor(
    readonly url: string,
    readonly options: CentrifugeOptions,
  ) {}
  on(event: string, listener: Listener): this {
    this.listeners.set(event, [...(this.listeners.get(event) ?? []), listener]);
    return this;
  }
  newSubscription(
    channel: string,
    options: { recoverable: boolean; positioned: boolean },
  ) {
    const subscription = new FakeSubscription(channel, options);
    this.subscriptions.push(subscription);
    return subscription;
  }
  removeSubscription(subscription: SubscriptionLike) {
    this.removed.push(subscription);
  }
  connect() {
    this.connects++;
  }
  disconnect() {
    this.disconnects++;
  }
  emit(event: string, context: unknown = {}) {
    for (const listener of this.listeners.get(event) ?? []) {
      listener(context as never);
    }
  }
  sub(channel: string): FakeSubscription {
    const found = this.subscriptions.find(
      (subscription) =>
        subscription.channel === channel &&
        subscription.state !== "unsubscribed",
    );
    if (!found) throw new Error(`no subscription to ${channel}`);
    return found;
  }
}

class FakeStorage implements StorageLike {
  items = new Map<string, string>();
  getItem(key: string) {
    return this.items.get(key) ?? null;
  }
  setItem(key: string, value: string) {
    this.items.set(key, value);
  }
  removeItem(key: string) {
    this.items.delete(key);
  }
}

const json = (body: unknown, status = 200) =>
  new Response(JSON.stringify(body), {
    status,
    headers: {
      "Content-Type":
        status >= 400 ? "application/problem+json" : "application/json",
    },
  });

const LOBBY_ID = "11111111-1111-4111-8111-111111111111";
const PLAYER_ID = "22222222-2222-4222-8222-222222222222";
const CHANNEL = `player:${PLAYER_ID}`;
const LOBBY_CHANNEL = `lobby:island-battles:${LOBBY_ID}`;

const snapshot = (version: number, overrides: object = {}): Snapshot =>
  ({
    channel: LOBBY_CHANNEL,
    lobby: {
      id: LOBBY_ID,
      game_key: "island-battles",
      version,
      phase: "waiting",
    },
    members: [],
    membership_generation: 1,
    start_roster: null,
    start_rules: {},
    teams: [],
    ...overrides,
  }) as unknown as Snapshot;

const state = (generationId: number, overrides: object = {}): GenerationState =>
  ({
    bot: { kind: "localhost" },
    channel: CHANNEL,
    game_id: 7,
    generation: {
      id: generationId,
      castles: [],
      clashes: [],
      soldier_groups: [],
    },
    generation_id: generationId,
    lobby_id: LOBBY_ID,
    log_lines_stored: 0,
    memory: null,
    player_id: PLAYER_ID,
    submitted: false,
    turn_deadline_at: null,
    ...overrides,
  }) as unknown as GenerationState;

const outcome = {
  reason: "generation_cap" as const,
  summary: "Blue won",
  winner_team_color: 1,
};

interface Harness {
  realtime: ReturnType<typeof createRealtime>;
  storage: FakeStorage;
  clients: FakeCentrifuge[];
  calls: { method: string; path: string }[];
  /** Answers by "METHOD path"; later assignments replace earlier ones. */
  routes: Map<string, (request: Request) => Response | Promise<Response>>;
  count(method: string, path: string): number;
  client(): FakeCentrifuge;
}

function harness(extra: Partial<RealtimeOptions> = {}): Harness {
  const storage = new FakeStorage();
  const clients: FakeCentrifuge[] = [];
  const calls: { method: string; path: string }[] = [];
  const routes: Harness["routes"] = new Map();
  let tokens = 0;
  routes.set("POST /lobby/v1/sessions", () =>
    json({
      player_id: PLAYER_ID,
      access_token: "access-1",
      expires_at: new Date(Date.now() + 86_400_000).toISOString(),
    }),
  );
  routes.set("POST /lobby/v1/connection-token", () =>
    json({
      token: `jwt-${++tokens}`,
      expires_at: new Date(Date.now() + 300_000).toISOString(),
      websocket_url: "ws://centrifugo.test/connection/websocket",
    }),
  );
  const fetchStub = (async (
    input: Request | string | URL,
    init?: RequestInit,
  ) => {
    const request = new Request(input, init);
    const path = new URL(request.url).pathname;
    calls.push({ method: request.method, path });
    const route = routes.get(`${request.method} ${path}`);
    if (!route) return json({ title: "Not Found", status: 404 }, 404);
    return route(request);
  }) as typeof fetch;
  const realtime = createRealtime({
    baseUrl: "http://api.test",
    fetch: fetchStub,
    storage,
    createClient: (url, options) => {
      const client = new FakeCentrifuge(url, options);
      clients.push(client);
      return client;
    },
    settleMs: 1,
    retryBaseMs: 2,
    retryMaxMs: 8,
    ...extra,
  });
  return {
    realtime,
    storage,
    clients,
    calls,
    routes,
    count: (method, path) =>
      calls.filter((call) => call.method === method && call.path === path)
        .length,
    client: () => {
      const client = clients.at(-1);
      if (!client) throw new Error("no client yet");
      return client;
    },
  };
}

const lobbyPath = `/lobby/v1/games/island-battles/lobbies/${LOBBY_ID}`;
const generationPath = `/game/from-lobby/${LOBBY_ID}/generation`;

async function until(condition: () => boolean, what = "condition") {
  for (let i = 0; i < 400; i++) {
    if (condition()) return;
    await new Promise((resolve) => setTimeout(resolve, 2));
  }
  throw new Error(`timed out waiting for ${what}`);
}
/** Lets pending settle timers and reads finish when no event is expected. */
const settle = () => new Promise((resolve) => setTimeout(resolve, 40));

async function followedLobby(version = 1) {
  const h = harness();
  h.routes.set(`GET ${lobbyPath}`, () => json(snapshot(version)));
  await h.realtime.signIn("Ada");
  h.realtime.followLobby({ gameKey: "island-battles", lobbyId: LOBBY_ID });
  await until(
    () => h.clients.length > 0 && h.realtime.lobby.get() !== null,
    "lobby",
  );
  await until(() => h.client().subscriptions.length > 0, "subscription");
  return h;
}

async function followedGame() {
  const h = harness();
  h.routes.set(`GET ${generationPath}`, () => json(state(5)));
  await h.realtime.signIn("Ada");
  h.realtime.followGame(LOBBY_ID);
  await until(
    () => h.clients.length > 0 && (h.clients[0]?.subscriptions.length ?? 0) > 0,
  );
  h.client().sub(CHANNEL).emit("subscribed");
  await until(() => h.realtime.game.get() !== null, "game");
  return h;
}

// --- Session ---

describe("session", () => {
  test("creates a guest session, saves it and reuses it", async () => {
    const h = harness();
    const session = await h.realtime.signIn("Ada");
    expect(session).toMatchObject({
      playerId: PLAYER_ID,
      accessToken: "access-1",
      displayName: "Ada",
    });
    expect(h.storage.getItem(sessionStorageKey)).toContain("access-1");
    expect(await h.realtime.signIn("Other")).toBe(session);
    expect(h.count("POST", "/lobby/v1/sessions")).toBe(1);
  });

  test("a new page reuses the saved session", async () => {
    const h = harness();
    await h.realtime.signIn("Ada");
    const again = createRealtime({
      storage: h.storage,
      fetch: (() => {
        throw new Error("no HTTP expected");
      }) as unknown as typeof fetch,
    });
    expect(again.session.get()?.accessToken).toBe("access-1");
  });

  test("a session past expires_at is dropped", async () => {
    const h = harness();
    await h.realtime.signIn("Ada");
    const later = createRealtime({
      storage: h.storage,
      now: () => Date.now() + 90_000_000,
    });
    expect(later.session.get()).toBeNull();
    expect(h.storage.getItem(sessionStorageKey)).toBeNull();
  });

  test("the session expires while the page is open", async () => {
    const h = harness({ now: () => Date.now() });
    h.routes.set("POST /lobby/v1/sessions", () =>
      json({
        player_id: PLAYER_ID,
        access_token: "a",
        expires_at: new Date(Date.now() + 20).toISOString(),
      }),
    );
    await h.realtime.signIn("Ada");
    await until(() => h.realtime.session.get() === null, "expiry");
    expect(h.storage.getItem(sessionStorageKey)).toBeNull();
  });

  test("sends the access token on API calls except session creation", async () => {
    const h = harness();
    const seen: (string | null)[] = [];
    h.routes.set("POST /lobby/v1/connection-token", (request) => {
      seen.push(request.headers.get("Authorization"));
      return json({
        token: "t",
        expires_at: "2030-01-01T00:00:00Z",
        websocket_url: "ws://x",
      });
    });
    await h.realtime.signIn("Ada");
    h.routes.set(`GET ${lobbyPath}`, () => json(snapshot(1)));
    h.realtime.followLobby({ gameKey: "island-battles", lobbyId: LOBBY_ID });
    await until(() => seen.length > 0);
    expect(seen[0]).toBe("Bearer access-1");
  });
});

// --- Connection ---

describe("connection", () => {
  test("connects with the token and websocket url, and follows the client's state", async () => {
    const h = await followedLobby();
    const client = h.client();
    expect(client.url).toBe("ws://centrifugo.test/connection/websocket");
    expect(client.options.token).toBe("jwt-1");
    expect(client.connects).toBe(1);
    expect(h.realtime.connection.get()).toBe("connecting");
    client.emit("connected");
    expect(h.realtime.connection.get()).toBe("connected");
    client.emit("disconnected");
    expect(h.realtime.connection.get()).toBe("disconnected");
  });

  test("getToken asks for a new connection token", async () => {
    const h = await followedLobby();
    expect(await h.client().options.getToken()).toBe("jwt-2");
    expect(h.count("POST", "/lobby/v1/connection-token")).toBe(2);
  });

  test("a 401 from getToken ends the session and stops the client", async () => {
    const h = await followedLobby();
    h.routes.set("POST /lobby/v1/connection-token", () =>
      json({ title: "Unauthorized", status: 401, code: "unauthorized" }, 401),
    );
    await expect(h.client().options.getToken()).rejects.toBeInstanceOf(
      UnauthorizedError,
    );
    expect(h.realtime.session.get()).toBeNull();
    expect(h.realtime.lobby.get()).toBeNull();
    expect(h.realtime.connection.get()).toBe("disconnected");
    expect(h.client().disconnects).toBe(1);
    expect(h.realtime.problem.get()?.scope).toBe("session");
    expect(h.storage.getItem(sessionStorageKey)).toBeNull();
  });

  test("another getToken failure is left to the client to retry", async () => {
    const h = await followedLobby();
    h.routes.set("POST /lobby/v1/connection-token", () =>
      json({ title: "Unavailable", status: 503 }, 503),
    );
    const error = await h
      .client()
      .options.getToken()
      .catch((e: unknown) => e);
    expect(error).not.toBeInstanceOf(UnauthorizedError);
    expect(h.realtime.session.get()).not.toBeNull();
  });

  test("retries the first connection token with backoff", async () => {
    const h = harness();
    let attempts = 0;
    h.routes.set("POST /lobby/v1/connection-token", () =>
      ++attempts < 3
        ? json({ title: "Unavailable", status: 503 }, 503)
        : json({
            token: "t",
            expires_at: "2030-01-01T00:00:00Z",
            websocket_url: "ws://x",
          }),
    );
    h.routes.set(`GET ${lobbyPath}`, () => json(snapshot(1)));
    await h.realtime.signIn("Ada");
    h.realtime.followLobby({ gameKey: "island-battles", lobbyId: LOBBY_ID });
    await until(() => h.clients.length === 1, "client");
    expect(attempts).toBe(3);
  });
});

// --- Lobby ---

describe("lobby subscription", () => {
  test("reads the snapshot, then subscribes recoverable and positioned, and reads again on subscribed", async () => {
    const h = await followedLobby(3);
    expect(h.realtime.lobby.get()?.lobby.version).toBe(3);
    const sub = h.client().sub(LOBBY_CHANNEL);
    expect(sub.options).toEqual({ recoverable: true, positioned: true });
    expect(sub.state).toBe("subscribing");
    expect(h.count("GET", lobbyPath)).toBe(1);
    h.routes.set(`GET ${lobbyPath}`, () => json(snapshot(4)));
    sub.emit("subscribed");
    await until(() => h.realtime.lobby.get()?.lobby.version === 4);
    expect(h.count("GET", lobbyPath)).toBe(2);
  });

  test("a lobby.changed that is not newer causes no read", async () => {
    const h = await followedLobby(3);
    const sub = h.client().sub(LOBBY_CHANNEL);
    for (const version of [3, 2, 1]) {
      sub.publish({
        schema_version: 1,
        type: "lobby.changed",
        lobby_id: LOBBY_ID,
        game_key: "island-battles",
        version,
      });
    }
    await settle();
    expect(h.count("GET", lobbyPath)).toBe(1);
  });

  test("a newer lobby.changed reads the snapshot once, also when repeated", async () => {
    const h = await followedLobby(3);
    const sub = h.client().sub(LOBBY_CHANNEL);
    h.routes.set(`GET ${lobbyPath}`, () => json(snapshot(5)));
    const event = {
      schema_version: 1,
      type: "lobby.changed",
      lobby_id: LOBBY_ID,
      game_key: "island-battles",
      version: 5,
    };
    sub.publish(event);
    sub.publish(event);
    sub.publish({ ...event, version: 4 });
    await until(() => h.realtime.lobby.get()?.lobby.version === 5);
    await settle();
    expect(h.count("GET", lobbyPath)).toBe(2);
  });

  test("ignores unknown schema versions and other lobbies", async () => {
    const h = await followedLobby(3);
    const sub = h.client().sub(LOBBY_CHANNEL);
    const event = {
      schema_version: 1,
      type: "lobby.changed",
      lobby_id: LOBBY_ID,
      game_key: "island-battles",
      version: 9,
    };
    sub.publish({ ...event, schema_version: 2 });
    sub.publish({ ...event, lobby_id: "someone-else" });
    sub.publish({ ...event, type: "something.else" });
    sub.publish("not an object");
    await settle();
    expect(h.count("GET", lobbyPath)).toBe(1);
  });

  test("never installs an older snapshot", async () => {
    const h = await followedLobby(5);
    h.routes.set(`GET ${lobbyPath}`, () => json(snapshot(4)));
    h.client().sub(LOBBY_CHANNEL).emit("subscribed");
    await until(() => h.count("GET", lobbyPath) === 2);
    await settle();
    expect(h.realtime.lobby.get()?.lobby.version).toBe(5);
  });

  test("a result arriving after the lobby was left is dropped", async () => {
    const h = harness();
    let release: (response: Response) => void = () => {};
    h.routes.set(
      `GET ${lobbyPath}`,
      () =>
        new Promise<Response>((resolve) => {
          release = resolve;
        }),
    );
    await h.realtime.signIn("Ada");
    h.realtime.followLobby({ gameKey: "island-battles", lobbyId: LOBBY_ID });
    await until(() => h.count("GET", lobbyPath) === 1);
    h.realtime.leaveLobby();
    release(json(snapshot(9)));
    await settle();
    expect(h.realtime.lobby.get()).toBeNull();
    expect(h.clients.every((client) => client.subscriptions.length === 0)).toBe(
      true,
    );
  });

  test("a result for the previous lobby does not overwrite the next one", async () => {
    const h = harness();
    const other = "33333333-3333-4333-8333-333333333333";
    let release: (response: Response) => void = () => {};
    h.routes.set(
      `GET ${lobbyPath}`,
      () =>
        new Promise<Response>((resolve) => {
          release = resolve;
        }),
    );
    h.routes.set(`GET /lobby/v1/games/island-battles/lobbies/${other}`, () =>
      json(
        snapshot(2, {
          channel: `lobby:island-battles:${other}`,
          lobby: { id: other, game_key: "island-battles", version: 2 },
        }),
      ),
    );
    await h.realtime.signIn("Ada");
    h.realtime.followLobby({ gameKey: "island-battles", lobbyId: LOBBY_ID });
    await until(() => h.count("GET", lobbyPath) === 1);
    h.realtime.followLobby({ gameKey: "island-battles", lobbyId: other });
    await until(() => h.realtime.lobby.get()?.lobby.id === other);
    release(json(snapshot(9)));
    await settle();
    expect(h.realtime.lobby.get()?.lobby.id).toBe(other);
  });

  test("leaving unsubscribes and removes the subscription", async () => {
    const h = await followedLobby();
    const sub = h.client().sub(LOBBY_CHANNEL);
    h.realtime.leaveLobby();
    expect(sub.state).toBe("unsubscribed");
    expect(h.client().removed).toContain(sub);
    sub.emit("unsubscribed");
    await settle();
    expect(h.count("GET", lobbyPath)).toBe(1);
  });

  test("when the server drops the player from the lobby, the lobby is released", async () => {
    const h = await followedLobby();
    h.routes.set(`GET ${lobbyPath}`, () =>
      json({ title: "Conflict", status: 409, code: "stale_membership" }, 409),
    );
    h.client().sub(LOBBY_CHANNEL).emit("unsubscribed", { code: 2500 });
    await until(
      () =>
        h.realtime.lobby.get() === null && h.realtime.problem.get() !== null,
    );
    expect(h.realtime.problem.get()?.scope).toBe("lobby");
    await settle();
    expect(h.count("GET", lobbyPath)).toBe(2);
  });

  test("a 401 on the snapshot read ends the session", async () => {
    const h = harness();
    h.routes.set(`GET ${lobbyPath}`, () =>
      json({ title: "Unauthorized", status: 401 }, 401),
    );
    await h.realtime.signIn("Ada");
    h.realtime.followLobby({ gameKey: "island-battles", lobbyId: LOBBY_ID });
    await until(() => h.realtime.session.get() === null);
  });

  test("a failed read is retried with backoff and then installed", async () => {
    const h = harness();
    let attempts = 0;
    h.routes.set(`GET ${lobbyPath}`, () =>
      ++attempts < 3
        ? json({ title: "Unavailable", status: 503 }, 503)
        : json(snapshot(1)),
    );
    await h.realtime.signIn("Ada");
    h.realtime.followLobby({ gameKey: "island-battles", lobbyId: LOBBY_ID });
    await until(() => h.realtime.lobby.get() !== null, "snapshot");
    expect(attempts).toBe(3);
  });

  test("adoptSnapshot installs a snapshot and subscribes without a read", async () => {
    const h = harness();
    await h.realtime.signIn("Ada");
    h.realtime.adoptSnapshot(snapshot(2));
    expect(h.realtime.lobby.get()?.lobby.version).toBe(2);
    await until(
      () => h.clients.length === 1 && h.client().subscriptions.length === 1,
    );
    expect(h.count("GET", lobbyPath)).toBe(0);
    h.realtime.adoptSnapshot(snapshot(1));
    expect(h.realtime.lobby.get()?.lobby.version).toBe(2);
  });

  test("stores notify subscribers", async () => {
    const h = harness();
    h.routes.set(`GET ${lobbyPath}`, () => json(snapshot(1)));
    const seen: (number | undefined)[] = [];
    h.realtime.lobby.subscribe((value) => seen.push(value?.lobby.version));
    await h.realtime.signIn("Ada");
    h.realtime.followLobby({ gameKey: "island-battles", lobbyId: LOBBY_ID });
    await until(() => seen.length > 0);
    expect(seen).toEqual([1]);
  });
});

// --- Game ---

describe("game subscription", () => {
  test("subscribes to the player channel and reads the generation on subscribed", async () => {
    const h = await followedGame();
    const sub = h.client().sub(CHANNEL);
    expect(sub.options).toEqual({ recoverable: true, positioned: true });
    expect(h.realtime.game.get()?.generation_id).toBe(5);
    expect(h.count("GET", generationPath)).toBe(1);
  });

  test("installs newer generations, ignoring duplicates and older ones", async () => {
    const h = await followedGame();
    const sub = h.client().sub(CHANNEL);
    const seen: number[] = [];
    h.realtime.game.subscribe(
      (value) => value && seen.push(value.generation_id),
    );
    const next = (s: GenerationState) => ({
      schema_version: 1,
      type: "game.next_generation",
      state: s,
    });
    sub.publish(next(state(7)));
    sub.publish(next(state(7)));
    sub.publish(next(state(6)));
    sub.publish(next(state(5)));
    sub.publish(next(state(9)));
    sub.publish(next(state(8)));
    expect(seen).toEqual([7, 9]);
    expect(h.realtime.game.get()?.generation_id).toBe(9);
    expect(h.count("GET", generationPath)).toBe(1);
  });

  test("a late read cannot rewind the game", async () => {
    const h = await followedGame();
    h.client()
      .sub(CHANNEL)
      .publish({
        schema_version: 1,
        type: "game.next_generation",
        state: state(8),
      });
    h.routes.set(`GET ${generationPath}`, () => json(state(6)));
    h.client().sub(CHANNEL).emit("subscribed");
    await until(() => h.count("GET", generationPath) === 2);
    await settle();
    expect(h.realtime.game.get()?.generation_id).toBe(8);
  });

  test("game.ended is final: nothing changes after it", async () => {
    const h = await followedGame();
    const sub = h.client().sub(CHANNEL);
    sub.publish({
      schema_version: 1,
      type: "game.ended",
      state: state(8, { outcome }),
    });
    const ended = h.realtime.game.get();
    expect(ended?.outcome).toEqual(outcome);
    sub.publish({
      schema_version: 1,
      type: "game.next_generation",
      state: state(9),
    });
    sub.publish({
      schema_version: 1,
      type: "game.next_generation",
      state: state(8),
    });
    sub.publish({
      schema_version: 1,
      type: "game.ended",
      state: state(8, { outcome: { ...outcome, summary: "Other" } }),
    });
    expect(h.realtime.game.get()).toBe(ended);
  });

  test("keeps a known outcome when a later read for the same generation lacks it", async () => {
    const h = await followedGame();
    h.client()
      .sub(CHANNEL)
      .publish({
        schema_version: 1,
        type: "game.ended",
        state: state(8, { outcome }),
      });
    h.routes.set(`GET ${generationPath}`, () =>
      json(state(8, { submitted: true })),
    );
    h.client().sub(CHANNEL).emit("subscribed");
    await until(() => h.count("GET", generationPath) === 2);
    await settle();
    expect(h.realtime.game.get()?.outcome).toEqual(outcome);
  });

  test("a later state for the same generation does not reopen a submitted turn", async () => {
    const h = await followedGame();
    const sub = h.client().sub(CHANNEL);
    sub.publish({
      schema_version: 1,
      type: "game.next_generation",
      state: state(6, { submitted: true }),
    });
    sub.publish({
      schema_version: 1,
      type: "game.next_generation",
      state: state(6, { submitted: false }),
    });
    expect(h.realtime.game.get()?.submitted).toBe(true);
  });

  test("accepts events only for the expected game, lobby, player and channel", async () => {
    const h = await followedGame();
    const sub = h.client().sub(CHANNEL);
    const publish = (overrides: object) =>
      sub.publish({
        schema_version: 1,
        type: "game.next_generation",
        state: state(20, overrides),
      });
    publish({ game_id: 8 });
    publish({ lobby_id: "44444444-4444-4444-8444-444444444444" });
    publish({ channel: "player:someone-else" });
    publish({ player_id: "someone-else" });
    expect(h.realtime.game.get()?.generation_id).toBe(5);
    publish({});
    expect(h.realtime.game.get()?.generation_id).toBe(20);
  });

  test("ignores unknown schema versions and event types", async () => {
    const h = await followedGame();
    const sub = h.client().sub(CHANNEL);
    sub.publish({
      schema_version: 2,
      type: "game.next_generation",
      state: state(20),
    });
    sub.publish({ schema_version: 1, type: "game.unknown", state: state(20) });
    sub.publish({ schema_version: 1, type: "game.ended" });
    expect(h.realtime.game.get()?.generation_id).toBe(5);
  });

  test("a game not started yet (404) is waited for on the channel", async () => {
    const h = harness();
    await h.realtime.signIn("Ada");
    h.realtime.followGame(LOBBY_ID);
    await until(
      () =>
        h.clients.length > 0 && (h.clients[0]?.subscriptions.length ?? 0) > 0,
    );
    h.client().sub(CHANNEL).emit("subscribed");
    await until(() => h.count("GET", generationPath) === 1);
    await settle();
    expect(h.count("GET", generationPath)).toBe(1);
    h.client()
      .sub(CHANNEL)
      .publish({
        schema_version: 1,
        type: "game.next_generation",
        state: state(1),
      });
    expect(h.realtime.game.get()?.generation_id).toBe(1);
  });

  test("a result arriving after the game was left is dropped", async () => {
    const h = harness();
    let release: (response: Response) => void = () => {};
    h.routes.set(
      `GET ${generationPath}`,
      () =>
        new Promise<Response>((resolve) => {
          release = resolve;
        }),
    );
    await h.realtime.signIn("Ada");
    h.realtime.followGame(LOBBY_ID);
    await until(
      () =>
        h.clients.length > 0 && (h.clients[0]?.subscriptions.length ?? 0) > 0,
    );
    h.client().sub(CHANNEL).emit("subscribed");
    await until(() => h.count("GET", generationPath) === 1);
    h.realtime.leaveGame();
    release(json(state(3)));
    await settle();
    expect(h.realtime.game.get()).toBeNull();
  });
});

// --- Recovery ---

const WATCH_CHANNEL = `watch:island-battles:${LOBBY_ID}`;
const watchPath = `/game/from-lobby/${LOBBY_ID}/watch`;
const watchState = (generationId: number, overrides: object = {}) => ({
  channel: WATCH_CHANNEL,
  game_id: 7,
  generation: {
    id: generationId,
    castles: [],
    clashes: [],
    soldier_groups: [],
  },
  generation_id: generationId,
  lobby_id: LOBBY_ID,
  teams: [],
  turn_deadline_at: null,
  ...overrides,
});

async function watchedGame() {
  const h = harness();
  h.routes.set(`GET ${watchPath}`, () => json(watchState(5)));
  await h.realtime.signIn("Robin");
  h.realtime.followWatch({ lobbyId: LOBBY_ID, channel: WATCH_CHANNEL });
  await until(
    () => h.clients.length > 0 && (h.clients[0]?.subscriptions.length ?? 0) > 0,
  );
  h.client().sub(WATCH_CHANNEL).emit("subscribed");
  await until(() => h.realtime.watch.get() !== null, "watch");
  return h;
}

describe("watching", () => {
  test("subscribes to the watch channel, reads the watch state and installs newer turns", async () => {
    const h = await watchedGame();
    const sub = h.client().sub(WATCH_CHANNEL);
    expect(sub.options).toEqual({ recoverable: true, positioned: true });
    expect(h.realtime.watch.get()?.generation_id).toBe(5);
    expect(h.realtime.game.get()).toBeNull();
    const next = (generationId: number, overrides: object = {}) => ({
      schema_version: 1,
      type: "game.next_generation",
      state: watchState(generationId, overrides),
    });
    sub.publish(next(4));
    sub.publish(next(6, { channel: CHANNEL }));
    sub.publish({ ...next(6), schema_version: 2 });
    expect(h.realtime.watch.get()?.generation_id).toBe(5);
    sub.publish(next(6));
    expect(h.realtime.watch.get()?.generation_id).toBe(6);
    sub.publish({
      schema_version: 1,
      type: "game.ended",
      state: watchState(7, { outcome }),
    });
    sub.publish(next(8));
    expect(h.realtime.watch.get()?.generation_id).toBe(7);
    // A watcher never heartbeats a lobby or reads the player's generation.
    expect(h.calls.some((call) => call.path.endsWith("/heartbeat"))).toBe(
      false,
    );
    expect(h.count("GET", generationPath)).toBe(0);
  });

  test("a kicked watcher is told so when the server unsubscribes them", async () => {
    const h = await watchedGame();
    h.routes.set(`GET ${watchPath}`, () =>
      json({ title: "Forbidden", status: 403, detail: "kicked" }, 403),
    );
    const sub = h.client().sub(WATCH_CHANNEL);
    sub.emit("unsubscribed", { code: 2000 });
    await until(() => h.realtime.problem.get() !== null, "problem");
    expect(h.realtime.problem.get()?.scope).toBe("watch");
    expect(h.realtime.watch.get()).toBeNull();
    expect(sub.state).toBe("unsubscribed");
  });

  test("leaving the watch drops the subscription", async () => {
    const h = await watchedGame();
    const sub = h.client().sub(WATCH_CHANNEL);
    h.realtime.leaveWatch();
    expect(sub.state).toBe("unsubscribed");
    expect(h.client().removed).toContain(sub);
    expect(h.realtime.watch.get()).toBeNull();
  });
});

describe("recovery", () => {
  test("a disconnect and reconnect reconciles once per followed target", async () => {
    const h = await followedLobby(3);
    h.routes.set(`GET ${generationPath}`, () => json(state(5)));
    h.realtime.followGame(LOBBY_ID);
    await until(() => h.client().subscriptions.length === 2);
    h.client().sub(CHANNEL).emit("subscribed");
    await until(() => h.realtime.game.get() !== null);
    await settle();
    const lobbyReads = h.count("GET", lobbyPath);
    const gameReads = h.count("GET", generationPath);

    h.client().emit("disconnected");
    h.client().emit("connecting");
    await settle();
    expect(h.count("GET", lobbyPath)).toBe(lobbyReads);
    expect(h.count("GET", generationPath)).toBe(gameReads);

    h.client().emit("connected");
    h.client().sub(LOBBY_CHANNEL).emit("subscribed");
    h.client().sub(CHANNEL).emit("subscribed");
    await until(
      () =>
        h.count("GET", lobbyPath) > lobbyReads &&
        h.count("GET", generationPath) > gameReads,
    );
    await settle();
    expect(h.count("GET", lobbyPath)).toBe(lobbyReads + 1);
    expect(h.count("GET", generationPath)).toBe(gameReads + 1);
  });

  test("an unsubscribed event reconciles", async () => {
    const h = await followedGame();
    h.client().sub(CHANNEL).emit("unsubscribed", { code: 3 });
    await until(() => h.count("GET", generationPath) === 2);
  });

  test("applies what changed while disconnected", async () => {
    const h = await followedLobby(3);
    h.client().emit("disconnected");
    h.routes.set(`GET ${lobbyPath}`, () => json(snapshot(6)));
    h.client().emit("connected");
    await until(() => h.realtime.lobby.get()?.lobby.version === 6);
  });

  test("makes no requests while idle", async () => {
    const h = await followedLobby(3);
    const before = h.calls.length;
    await new Promise((resolve) => setTimeout(resolve, 100));
    expect(h.calls.length).toBe(before);
  });

  test("retries a failed reconcile with backoff until it succeeds", async () => {
    const h = await followedGame();
    let attempts = 0;
    h.routes.set(`GET ${generationPath}`, () =>
      ++attempts < 4
        ? json({ title: "Unavailable", status: 503 }, 503)
        : json(state(12)),
    );
    h.client().emit("connected");
    await until(() => h.realtime.game.get()?.generation_id === 12, "recovery");
    expect(attempts).toBe(4);
    await settle();
    expect(attempts).toBe(4);
  });

  test("signing out clears everything and closes the client", async () => {
    const h = await followedLobby();
    h.realtime.signOut();
    expect(h.realtime.session.get()).toBeNull();
    expect(h.realtime.lobby.get()).toBeNull();
    expect(h.realtime.connection.get()).toBe("disconnected");
    expect(h.client().disconnects).toBe(1);
    expect(h.realtime.problem.get()).toBeNull();
  });
});
