import { describe, expect, test } from "bun:test";
import { createApiClients } from "../../api/client";
import { ApiError } from "../../api/errors";
import { leaderboardApi, ownCompetitors } from "./api";

const competitorId = "7c9e6679-7425-40de-944b-e07fc1f90ae7";

/** A fake game and lobby: records each request, and answers by path. */
function serverFor(session: string | undefined) {
  const seen: { url: URL; authorization: string | null }[] = [];
  const fetchStub = (async (
    input: Request | string | URL,
    init?: RequestInit,
  ) => {
    const request = new Request(input, init);
    const url = new URL(request.url);
    seen.push({ url, authorization: request.headers.get("Authorization") });
    if (url.pathname === "/lobby/v1/player-tokens") {
      return Response.json({
        token: "player-token",
        expires_at: "2099-01-01T00:00:00Z",
      });
    }
    if (url.pathname === "/leaderboard") {
      return Response.json({ total: 0, entries: [] });
    }
    if (url.pathname === `/leaderboard/${competitorId}`) {
      return Response.json(
        { status: 404, title: "Not Found", detail: "competitor not found" },
        { status: 404 },
      );
    }
    if (url.pathname === "/account") {
      if (session === "guest") {
        return Response.json(
          {
            status: 403,
            type: "urn:island-battles:problem:sign-in-required",
          },
          { status: 403 },
        );
      }
      const competitor = (id: string, kind: string) => ({
        id,
        kind,
        name: id,
        rating: 1500,
        games: 1,
        wins: 1,
        draws: 0,
        losses: 0,
        last_played_at: null,
      });
      return Response.json({
        account_id: "a1",
        display_name: "Dana",
        max_bot_names: 5,
        hand: competitor("hand", "manual"),
        bot_names: [competitor("kraken", "bot")],
      });
    }
    return new Response(null, { status: 500 });
  }) as typeof fetch;
  const { gameApi } = createApiClients({
    baseUrl: "http://game.test",
    getAccessToken: () => session,
    fetch: fetchStub,
  });
  return { gameApi, seen };
}

describe("the leaderboard's routes", () => {
  test("send a tab's query, with no credential and no call to the lobby", async () => {
    const server = serverFor(undefined);
    const api = leaderboardApi(server.gameApi);
    expect(await api.list({ kind: "bot", limit: 50, offset: 50 })).toEqual({
      total: 0,
      entries: [],
    });
    const [request] = server.seen;
    expect(request?.url.pathname).toBe("/leaderboard");
    expect(Object.fromEntries(request?.url.searchParams ?? [])).toEqual({
      kind: "bot",
      limit: "50",
      offset: "50",
    });
    expect(request?.authorization).toBeNull();
    expect(server.seen).toHaveLength(1);
  });

  test("a competitor that isn't listed rejects with its 404", async () => {
    const server = serverFor(undefined);
    const error = await leaderboardApi(server.gameApi)
      .competitor(competitorId)
      .catch((cause: unknown) => cause);
    expect(error).toBeInstanceOf(ApiError);
    expect((error as ApiError).status).toBe(404);
  });

  test("with a session, they still send no token", async () => {
    const server = serverFor("guest");
    await leaderboardApi(server.gameApi).list({ limit: 50, offset: 0 });
    expect(server.seen.map((request) => request.authorization)).toEqual([null]);
  });
});

describe("your own competitors", () => {
  test("are your hand play and bot names, read with a player token", async () => {
    const server = serverFor("signed-in");
    expect(await ownCompetitors(server.gameApi)).toEqual(
      new Set(["hand", "kraken"]),
    );
    const account = server.seen.find(
      (request) => request.url.pathname === "/account",
    );
    expect(account?.authorization).toBe("Bearer player-token");
  });

  test("are none for a guest, whose account route is refused", async () => {
    const server = serverFor("guest");
    expect(await ownCompetitors(server.gameApi)).toEqual(new Set());
  });
});
