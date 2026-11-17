import { afterEach, describe, expect, test } from "bun:test";
import { setDesktopFetch, setServerUrls } from "../../platform";
import type { Realtime } from "../../realtime";
import { playerSource, watchSource } from "./sources";

// The sources read with the signed-in session; nothing else of Realtime is used to read logs.
const realtime = {
  session: { get: () => ({ accessToken: "tok" }) },
} as unknown as Realtime;

function recordRequests(): string[] {
  const paths: string[] = [];
  setServerUrls({ lobby_url: "http://api.test", game_url: "http://api.test" });
  setDesktopFetch((async (
    input: Request | string | URL,
    init?: RequestInit,
  ) => {
    const request = new Request(input, init);
    const url = new URL(request.url);
    // The game routes take a player token, which the lobby issues first.
    if (url.pathname === "/lobby/v1/player-tokens") {
      return Response.json({
        token: "player-token",
        expires_at: new Date(Date.now() + 900_000).toISOString(),
      });
    }
    paths.push(`${url.pathname}${url.search}`);
    return Response.json({
      generation_id: 7,
      players: [],
      hidden_team_colors: [1, 2],
    });
  }) as typeof fetch);
  return paths;
}

afterEach(() => {
  setDesktopFetch(undefined);
  setServerUrls({});
});

describe("log reads", () => {
  test("a player reads their own logs route", async () => {
    const paths = recordRequests();
    const source = playerSource(realtime, "lobby-1", 1);
    expect(source.watching).toBe(false);
    await source.readLogs(7);
    expect(paths).toEqual(["/game/from-lobby/lobby-1/logs?generation_id=7"]);
  });

  test("a watcher reads the watch route, the only one the lobby relays for watchers", async () => {
    const paths = recordRequests();
    const source = watchSource(realtime, {
      lobbyId: "lobby-1",
      channel: "watch:x",
    });
    expect(source.watching).toBe(true);
    const logs = await source.readLogs(7);
    expect(paths).toEqual([
      "/game/from-lobby/lobby-1/watch/logs?generation_id=7",
    ]);
    expect(logs.hidden_team_colors).toEqual([1, 2]);
  });
});
