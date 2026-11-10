import { describe, expect, test } from "bun:test";
import { createApiClients } from "./client";
import type { ApiError } from "./errors";
import { maxRetryWaitMs, serverBusyMessage } from "./retry";

const throttled = (headers: Record<string, string> = {}) =>
  new Response('{"Reason":"X","Message":"Rate Exceeded."}', {
    status: 429,
    statusText: "Too Many Requests",
    headers,
  });
const ok = () =>
  new Response("{}", {
    status: 200,
    headers: { "Content-Type": "application/json" },
  });

function setup(responses: Array<() => Response>) {
  const requests: Request[] = [];
  const waits: number[] = [];
  const fetchStub = (async (input: Request | string | URL) => {
    requests.push(input as Request);
    const next = responses[Math.min(requests.length, responses.length) - 1];
    return (next as () => Response)();
  }) as typeof fetch;
  const clients = createApiClients({
    baseUrl: "http://api.test",
    getAccessToken: () => "tok",
    fetch: fetchStub,
    sleep: async (ms) => {
      waits.push(ms);
    },
    random: () => 0,
  });
  return { ...clients, requests, waits };
}

const lobbyPath = { game_key: "g", lobby_id: "l" };
const caught = async (promise: Promise<unknown>) => {
  try {
    await promise;
  } catch (error) {
    return error as ApiError;
  }
  throw new Error("expected the call to reject");
};

describe("throttled requests", () => {
  test("a 429 followed by a success returns the success", async () => {
    const { gameApi, requests, waits } = setup([throttled, ok]);
    const result = await gameApi.GET("/map/templates");
    expect(result.data).toBeDefined();
    expect(requests).toHaveLength(2);
    expect(waits).toEqual([500]);
  });

  test("stops after 3 retries with the plain message", async () => {
    const { gameApi, requests, waits } = setup([throttled]);
    const error = await caught(gameApi.GET("/map/templates"));
    expect(requests).toHaveLength(4);
    expect(waits).toEqual([500, 1000, 2000]);
    expect(error.status).toBe(429);
    expect(error.message).toBe(serverBusyMessage);
    expect(error.message).not.toContain("Too Many");
  });

  test("jitter adds up to 25 percent", async () => {
    const waits: number[] = [];
    const { gameApi } = createApiClients({
      baseUrl: "http://api.test",
      fetch: (async () => throttled()) as unknown as typeof fetch,
      sleep: async (ms) => {
        waits.push(ms);
      },
      random: () => 0.999,
    });
    await caught(gameApi.GET("/map/templates"));
    expect(waits[0]).toBeCloseTo(625, -1);
    expect(waits[2]).toBeCloseTo(2500, -1);
  });

  test("Retry-After lengthens the wait but never past 5 seconds", async () => {
    const { gameApi, waits } = setup([
      () => throttled({ "Retry-After": "3" }),
      () => throttled({ "Retry-After": "60" }),
      ok,
    ]);
    await gameApi.GET("/map/templates");
    expect(waits).toEqual([3000, maxRetryWaitMs]);
  });

  test("a repeatable PUT keeps its body across retries", async () => {
    const { gameApi, requests } = setup([throttled, ok]);
    await gameApi.PUT("/game/lobbies/{lobby_id}/bot", {
      params: { path: { lobby_id: "l" } },
      body: { bot_id: "b" } as never,
    });
    expect(requests).toHaveLength(2);
    expect(await requests[1]?.text()).toBe('{"bot_id":"b"}');
  });

  test("team, ready, heartbeat and reopen are retried", async () => {
    const a = setup([throttled, ok]);
    await a.lobbyApi.PUT("/lobby/v1/games/{game_key}/lobbies/{lobby_id}/team", {
      params: { path: lobbyPath },
      body: { team_id: null },
    });
    expect(a.requests).toHaveLength(2);
    const b = setup([throttled, ok]);
    await b.lobbyApi.PUT(
      "/lobby/v1/games/{game_key}/lobbies/{lobby_id}/ready",
      { params: { path: lobbyPath }, body: { ready: true } },
    );
    expect(b.requests).toHaveLength(2);
    const c = setup([throttled, ok]);
    await c.lobbyApi.POST(
      "/lobby/v1/games/{game_key}/lobbies/{lobby_id}/heartbeat",
      {
        params: { path: lobbyPath },
        body: { client_instance_id: "c", membership_generation: 1 },
      },
    );
    expect(c.requests).toHaveLength(2);
    const d = setup([throttled, ok]);
    await d.lobbyApi.POST(
      "/lobby/v1/games/{game_key}/lobbies/{lobby_id}/reopen",
      { params: { path: lobbyPath } },
    );
    expect(d.requests).toHaveLength(2);
  });

  test("create, join, start, seats and watcher kicks are not retried", async () => {
    const calls: Array<(s: ReturnType<typeof setup>) => Promise<unknown>> = [
      (s) =>
        s.lobbyApi.POST("/lobby/v1/lobbies", {
          body: { game_key: "g", client_instance_id: "c" } as never,
        }),
      (s) =>
        s.lobbyApi.POST("/lobby/v1/memberships", {
          body: { game_key: "g", join_code: "x", client_instance_id: "c" },
        }),
      (s) =>
        s.lobbyApi.POST("/lobby/v1/games/{game_key}/lobbies/{lobby_id}/start", {
          params: { path: lobbyPath },
          body: {},
        }),
      (s) =>
        s.lobbyApi.POST("/lobby/v1/games/{game_key}/lobbies/{lobby_id}/seats", {
          params: { path: lobbyPath },
          body: {} as never,
        }),
      (s) =>
        s.lobbyApi.DELETE(
          "/lobby/v1/games/{game_key}/lobbies/{lobby_id}/seats/{seat_id}",
          { params: { path: { ...lobbyPath, seat_id: "s" } } },
        ),
      (s) =>
        s.lobbyApi.DELETE(
          "/lobby/v1/games/{game_key}/lobbies/{lobby_id}/watchers/{player_id}",
          { params: { path: { ...lobbyPath, player_id: "p" } } },
        ),
    ];
    for (const call of calls) {
      const s = setup([throttled, ok]);
      const error = await caught(call(s));
      expect(s.requests).toHaveLength(1);
      expect(s.waits).toEqual([]);
      expect(error.message).toBe(serverBusyMessage);
    }
  });

  test.each([400, 409, 500])("%d is not retried", async (status) => {
    const { gameApi, requests } = setup([
      () => new Response("{}", { status }),
      ok,
    ]);
    const error = await caught(gameApi.GET("/map/templates"));
    expect(error.status).toBe(status);
    expect(requests).toHaveLength(1);
  });
});
