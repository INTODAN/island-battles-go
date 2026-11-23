import { describe, expect, test } from "bun:test";
import type { Realtime } from "../realtime";
import { type GenerationState, nextGeneration } from "../realtime/install";
import { createStore } from "../realtime/store";
import { newRequestId, startHandPlay } from "./index";
import { fakeClock, flush, handState, upgradeTable } from "./testing";

const json = (body: unknown, status = 200) =>
  new Response(JSON.stringify(body), {
    status,
    headers: {
      "Content-Type":
        status < 300 ? "application/json" : "application/problem+json",
    },
  });

/** The game API on a fake `fetch`, with a fake realtime session and game store. */
function setup(answer: (request: Request) => Response | Promise<Response>) {
  const clock = fakeClock();
  const game = createStore<GenerationState | null>(handState(10));
  const requests: Request[] = [];
  let refreshes = 0;
  const realtime = {
    session: createStore({ accessToken: "guest-token", playerId: "p1" }),
    game,
    adoptGame(state: GenerationState) {
      const next = nextGeneration(game.get(), state);
      if (next) game.set(next);
    },
    refreshGame() {
      refreshes++;
    },
  } as unknown as Realtime;
  const fakeFetch = (async (
    input: Request | string | URL,
    init?: RequestInit,
  ) => {
    const request = new Request(input, init);
    const path = new URL(request.url).pathname;
    if (path === "/lobby/v1/player-tokens") {
      return json({
        token: "player-token",
        expires_at: "2100-01-01T00:00:00Z",
      });
    }
    requests.push(request);
    if (path === "/game/upgrades") return json({ upgrades: upgradeTable });
    return answer(request);
  }) as typeof fetch;
  const play = startHandPlay({
    realtime,
    lobbyId: "lobby",
    fetch: fakeFetch,
    baseUrl: "http://game.test",
    schedule: clock.schedule,
    now: clock.now,
  });
  return { play, game, clock, requests, refreshes: () => refreshes };
}

describe("startHandPlay", () => {
  test("sends an order to /generation/commands with the player token", async () => {
    const env = setup(async () => json(handState(10)));
    await flush();
    expect(env.play.state.get().upgrades).toEqual(upgradeTable);
    const given = env.play.send(1, 4, 5);
    expect(given.ok && (await given.settled).state).toBe("accepted");
    const post = env.requests.find((request) =>
      request.url.endsWith("/generation/commands"),
    );
    expect(post?.method).toBe("POST");
    expect(post?.headers.get("Authorization")).toBe("Bearer player-token");
    const body = await post?.clone().json();
    expect(body).toMatchObject({
      game_id: 7,
      generation_id: 10,
      complete: false,
      commands: [{ command_type: "send_soldier_group" }],
    });
    expect(body.request_id).toMatch(
      /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/,
    );
  });

  test("calls the deadline route at the turn's end and installs its answer", async () => {
    const env = setup(async (request) =>
      new URL(request.url).pathname === "/game/from-lobby/lobby/deadline"
        ? json(handState(11, {}, 1_001_000))
        : json({}, 404),
    );
    await env.clock.advance(1000);
    await flush();
    const deadline = env.requests.find((request) =>
      request.url.endsWith("/game/from-lobby/lobby/deadline"),
    );
    expect(deadline?.method).toBe("POST");
    expect(deadline?.headers.get("Authorization")).toBe("Bearer player-token");
    expect(env.game.get()?.generation_id).toBe(11);
  });

  test("a late order that the server plays in the open turn is accepted there", async () => {
    let posts = 0;
    const env = setup(async (request) => {
      const path = new URL(request.url).pathname;
      if (path === "/generation/commands") {
        posts++;
        env.game.set(handState(11));
        return json(handState(11));
      }
      return json({}, 404);
    });
    await flush();
    const given = env.play.send(1, 4, 5);
    expect(given.ok && (await given.settled)).toMatchObject({
      state: "accepted",
      generationId: 11,
    });
    expect(posts).toBe(1);
  });

  test("a stale turn's 409, in both shapes, is read through the real error mapping", async () => {
    for (const type of [
      "urn:island-battles:problem:stale-generation",
      undefined,
    ]) {
      let posts = 0;
      const env = setup(async (request) => {
        const path = new URL(request.url).pathname;
        if (path === "/generation/commands") {
          posts++;
          if (posts === 1) {
            env.game.set(handState(11));
            return json(
              {
                status: 409,
                detail: "generation is no longer current",
                ...(type ? { type } : {}),
              },
              409,
            );
          }
          return json(handState(11));
        }
        return json({}, 404);
      });
      await flush();
      const given = env.play.send(1, 4, 5);
      // A late batch is played in the open turn, so a stale turn's 409 no longer comes for a
      // late order; if it does, the order is dropped, not sent again.
      expect(given.ok && (await given.settled)).toMatchObject({
        state: "rejected",
        sentence: "The turn ended before the order arrived.",
      });
      expect(posts).toBe(1);
    }
  });

  test("a request with no answer is aborted after its timeout", async () => {
    const env = setup(
      (request) =>
        new Promise<Response>((_, reject) => {
          request.signal.addEventListener("abort", () =>
            reject(request.signal.reason),
          );
        }),
    );
    await flush();
    env.play.send(1, 4, 5);
    await flush();
    const post = env.requests.find((request) =>
      request.url.endsWith("/generation/commands"),
    );
    expect(post?.signal.aborted).toBe(false);
    await env.clock.advance(2000);
    expect(post?.signal.aborted).toBe(true);
    // Sent again, the same request.
    await env.clock.advance(100);
    const posts = env.requests.filter((request) =>
      request.url.endsWith("/generation/commands"),
    );
    expect(posts).toHaveLength(2);
    env.play.dispose();
  });
});

describe("request IDs", () => {
  test("are v4 UUIDs, also without crypto.randomUUID", () => {
    const uuid =
      /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;
    expect(newRequestId()).toMatch(uuid);
    const original = crypto.randomUUID;
    try {
      Object.defineProperty(crypto, "randomUUID", {
        value: undefined,
        configurable: true,
      });
      const ids = new Set(Array.from({ length: 50 }, () => newRequestId()));
      expect(ids.size).toBe(50);
      for (const id of ids) expect(id).toMatch(uuid);
    } finally {
      Object.defineProperty(crypto, "randomUUID", {
        value: original,
        configurable: true,
      });
    }
  });
});
