import { describe, expect, test } from "bun:test";
import { createApiClients } from "./client";
import { ApiError } from "./errors";

const LOBBY_ID = "4f7c0a2e-58c4-4a51-9d2e-3c1f0f1d2a3b";
const generation = `/game/from-lobby/${LOBBY_ID}/generation`;

/**
 * A fake server: the lobby issues numbered tokens that last 15 minutes from
 * `now`, and the game accepts only the tokens in `valid`.
 */
function serverFor(options: { credential?: () => string } = {}) {
  let now = Date.parse("2026-11-12T10:00:00Z");
  let issued = 0;
  const valid = new Set<string>();
  // Whether the game refuses every token, as for a player off its roster.
  const refuse = { all: false };
  const seen: { path: string; authorization: string | null; body: string }[] =
    [];
  const credential = options.credential ?? (() => "guest");
  const fetchStub = (async (
    input: Request | string | URL,
    init?: RequestInit,
  ) => {
    const request = new Request(input, init);
    const path = new URL(request.url).pathname;
    const authorization = request.headers.get("Authorization");
    seen.push({ path, authorization, body: await request.text() });
    if (path === "/lobby/v1/player-tokens") {
      if (!authorization?.startsWith("Bearer guest")) {
        return Response.json(
          { status: 401, code: "unauthenticated" },
          { status: 401 },
        );
      }
      const token = `token-${++issued}`;
      valid.add(token);
      return Response.json({
        token,
        expires_at: new Date(now + 900_000).toISOString(),
      });
    }
    if (
      path.startsWith("/game/from-lobby/") ||
      path === "/generation/commands"
    ) {
      if (
        refuse.all ||
        !valid.has(authorization?.replace("Bearer ", "") ?? "")
      ) {
        return Response.json(
          { status: 401, detail: "authentication required" },
          { status: 401 },
        );
      }
    }
    return Response.json({ ok: true });
  }) as typeof fetch;
  const clients = createApiClients({
    baseUrl: "http://api.test",
    getAccessToken: credential,
    fetch: fetchStub,
    now: () => now,
  });
  return {
    ...clients,
    seen,
    valid,
    refuse,
    issued: () => issued,
    advance(milliseconds: number) {
      now += milliseconds;
    },
    gameAuthorizations: () =>
      seen
        .filter((request) => !request.path.startsWith("/lobby/"))
        .map((request) => request.authorization),
  };
}

const read = (server: ReturnType<typeof serverFor>) =>
  server.gameApi.GET("/game/from-lobby/{lobby_id}/generation", {
    params: { path: { lobby_id: LOBBY_ID } },
  });

describe("player tokens", () => {
  test("the game's routes get a player token from the lobby, never the guest credential", async () => {
    const server = serverFor();
    await read(server);
    await server.gameApi.POST("/game/from-lobby/{lobby_id}/deadline", {
      params: { path: { lobby_id: LOBBY_ID } },
    });
    expect(server.seen.map((request) => request.path)).toEqual([
      "/lobby/v1/player-tokens",
      generation,
      `/game/from-lobby/${LOBBY_ID}/deadline`,
    ]);
    expect(server.seen[0]?.authorization).toBe("Bearer guest");
    expect(JSON.parse(server.seen[0]?.body ?? "")).toEqual({
      game_key: "island-battles",
    });
    expect(server.gameAuthorizations()).toEqual([
      "Bearer token-1",
      "Bearer token-1",
    ]);
  });

  test("the game's public routes need no token", async () => {
    const server = serverFor();
    await server.gameApi.GET("/game/upgrades");
    await server.gameApi.GET("/map/templates");
    expect(server.issued()).toBe(0);
    expect(server.gameAuthorizations()).toEqual([null, null]);
  });

  test("a token is renewed once 80 % of its life has passed", async () => {
    const server = serverFor();
    await read(server);
    server.advance(11 * 60_000 + 59_000);
    await read(server);
    expect(server.issued()).toBe(1);
    server.advance(1_000);
    await read(server);
    await read(server);
    expect(server.issued()).toBe(2);
    expect(server.gameAuthorizations()).toEqual([
      "Bearer token-1",
      "Bearer token-1",
      "Bearer token-2",
      "Bearer token-2",
    ]);
  });

  test("requests at once share one new token", async () => {
    const server = serverFor();
    await Promise.all([read(server), read(server), read(server)]);
    expect(server.issued()).toBe(1);
  });

  test("a refused token is renewed once, and the request sent again with its body", async () => {
    const server = serverFor();
    await read(server);
    // The game stops accepting the token early, as after a key rotation.
    server.valid.clear();
    const batch = {
      game_id: 7,
      generation_id: 3,
      request_id: "8c1c7c55-3c8f-4c55-9a3e-7c6b1b2d3e4f",
      commands: [],
      complete: true,
    };
    await server.gameApi.POST("/generation/commands", { body: batch });
    const commands = server.seen.filter(
      (request) => request.path === "/generation/commands",
    );
    expect(commands.map((request) => request.authorization)).toEqual([
      "Bearer token-1",
      "Bearer token-2",
    ]);
    expect(commands.map((request) => JSON.parse(request.body))).toEqual([
      batch,
      batch,
    ]);
  });

  test("a token the game refuses twice is the caller's 401", async () => {
    const server = serverFor();
    await read(server);
    server.refuse.all = true;
    const error = await read(server).catch((caught: unknown) => caught);
    expect(error).toBeInstanceOf(ApiError);
    expect((error as ApiError).status).toBe(401);
    expect(server.issued()).toBe(2);
  });

  test("a new session gets its own token, and an ended one none", async () => {
    let credential = "guest-1";
    const server = serverFor({ credential: () => credential });
    await read(server);
    credential = "guest-2";
    await read(server);
    expect(server.issued()).toBe(2);
    expect(server.gameAuthorizations()).toEqual([
      "Bearer token-1",
      "Bearer token-2",
    ]);
    credential = "expired";
    const error = await read(server).catch((caught: unknown) => caught);
    expect((error as ApiError).status).toBe(401);
    expect(server.issued()).toBe(2);
  });
});
