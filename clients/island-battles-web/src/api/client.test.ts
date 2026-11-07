import { describe, expect, test } from "bun:test";
import { createApiClients } from "./client";
import { ApiError } from "./errors";

const problem = (status: number, body: object) =>
  new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/problem+json" },
  });

function clientsFor(
  handler: (request: Request) => Response | Promise<Response>,
) {
  const requests: Request[] = [];
  const fetchStub = (async (
    input: Request | string | URL,
    init?: RequestInit,
  ) => {
    const request = new Request(input, init);
    requests.push(request);
    return handler(request);
  }) as typeof fetch;
  const clients = createApiClients({
    baseUrl: "http://api.test",
    getAccessToken: () => "tok",
    fetch: fetchStub,
  });
  return { ...clients, requests };
}

const caught = async (promise: Promise<unknown>): Promise<ApiError> => {
  try {
    await promise;
  } catch (error) {
    expect(error).toBeInstanceOf(ApiError);
    return error as ApiError;
  }
  throw new Error("expected the call to reject");
};

describe("error mapping", () => {
  test("a lobby problem carries its code", async () => {
    const { lobbyApi } = clientsFor(() =>
      problem(409, {
        $schema: "http://api.test/schemas/ErrorModel.json",
        title: "Conflict",
        status: 409,
        detail: "the lobby is full",
        code: "lobby_full",
        added_later: true,
      }),
    );
    const error = await caught(
      lobbyApi.GET("/lobby/v1/games/{game_key}/lobbies/{lobby_id}", {
        params: { path: { game_key: "g", lobby_id: "l" } },
      }),
    );
    expect(error).toMatchObject({
      kind: "problem",
      status: 409,
      code: "lobby_full",
      title: "Conflict",
      detail: "the lobby is full",
    });
    expect(error.problemType).toBeUndefined();
  });

  test("a game problem carries its type", async () => {
    const { gameApi } = clientsFor(() =>
      problem(409, {
        type: "urn:island-battles:problem:game-ended",
        title: "Game ended",
        status: 409,
      }),
    );
    const error = await caught(gameApi.GET("/game/upgrades"));
    expect(error).toMatchObject({
      kind: "problem",
      status: 409,
      problemType: "urn:island-battles:problem:game-ended",
      title: "Game ended",
    });
    expect(error.code).toBeUndefined();
  });

  test("a non-JSON error body still maps to an error with the status", async () => {
    const { gameApi } = clientsFor(
      () =>
        new Response("<h1>Bad Gateway</h1>", {
          status: 502,
          statusText: "Bad Gateway",
        }),
    );
    const error = await caught(gameApi.GET("/game/upgrades"));
    expect(error).toMatchObject({
      kind: "http",
      status: 502,
      title: "Bad Gateway",
    });
    expect(error.code).toBeUndefined();
    expect(error.problemType).toBeUndefined();
  });

  test("a network failure is a transport error", async () => {
    const { gameApi } = clientsFor(() => {
      throw new TypeError("fetch failed");
    });
    const error = await caught(gameApi.GET("/game/upgrades"));
    expect(error).toMatchObject({
      kind: "transport",
      status: 0,
      detail: "fetch failed",
    });
    expect(error.cause).toBeInstanceOf(TypeError);
  });
});

describe("requests", () => {
  test("send the bearer token, except when creating a session", async () => {
    const { lobbyApi, requests } = clientsFor(() =>
      Response.json({ unknown_field: 1 }),
    );
    await lobbyApi.POST("/lobby/v1/sessions", {
      body: { display_name: "Ada" },
    });
    await lobbyApi.POST("/lobby/v1/connection-token", { body: {} });
    expect(requests[0]?.url).toBe("http://api.test/lobby/v1/sessions");
    expect(requests[0]?.headers.get("Authorization")).toBeNull();
    expect(requests[1]?.headers.get("Authorization")).toBe("Bearer tok");
  });

  test("tolerate unknown fields in successful responses", async () => {
    const { gameApi } = clientsFor(() =>
      Response.json({ $schema: "x", surprise: [1, 2, 3] }),
    );
    const { data } = await gameApi.GET("/game/upgrades");
    expect(data).toMatchObject({ surprise: [1, 2, 3] });
  });
});
