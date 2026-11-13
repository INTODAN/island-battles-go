import { describe, expect, test } from "bun:test";
import { createApiClients } from "../../api/client";
import { ApiError } from "../../api/errors";
import { readDemo } from "./demoFile";
import {
  downloadDemo,
  fallbackFileName,
  fileNameFromDisposition,
} from "./saveDemo";
import { sampleDemo } from "./testing";

function clientFor(respond: (request: Request) => Response) {
  const requests: Request[] = [];
  const { gameApi } = createApiClients({
    baseUrl: "http://api.test",
    getAccessToken: () => "tok",
    fetch: (async (input: Request | string | URL, init?: RequestInit) => {
      const request = new Request(input, init);
      // The demo routes take a player token, which the lobby issues first.
      if (new URL(request.url).pathname === "/lobby/v1/player-tokens") {
        return Response.json({
          token: "player-token",
          expires_at: new Date(Date.now() + 900_000).toISOString(),
        });
      }
      requests.push(request);
      return respond(request);
    }) as typeof fetch,
  });
  return { gameApi, requests };
}

describe("downloadDemo", () => {
  test("the saved bytes open again as a demo, under the server's file name", async () => {
    const body = JSON.stringify(sampleDemo());
    const { gameApi, requests } = clientFor(
      () =>
        new Response(body, {
          headers: {
            "Content-Type": "application/json",
            "Content-Disposition":
              'attachment; filename="island-battles-game-12-generation-2.json"',
          },
        }),
    );
    const saved = await downloadDemo(gameApi, "lobby 1", false);
    expect(new URL(requests[0]?.url ?? "").pathname).toBe(
      "/game/from-lobby/lobby%201/demo",
    );
    expect(saved.fileName).toBe("island-battles-game-12-generation-2.json");
    expect(saved.text).toBe(body);
    expect(readDemo(saved.text)).toEqual(sampleDemo());
  });

  test("a watcher asks for the watch demo and the name falls back to the pattern", async () => {
    const { gameApi, requests } = clientFor(
      () =>
        new Response(JSON.stringify(sampleDemo()), {
          headers: { "Content-Type": "application/json" },
        }),
    );
    const saved = await downloadDemo(gameApi, "abc", true);
    expect(new URL(requests[0]?.url ?? "").pathname).toBe(
      "/game/from-lobby/abc/watch/demo",
    );
    expect(saved.fileName).toBe("island-battles-game-12-generation-2.json");
    expect(readDemo(saved.text)).toEqual(sampleDemo());
  });

  test("a refusal rejects with an ApiError", async () => {
    const { gameApi } = clientFor(
      () =>
        new Response(JSON.stringify({ detail: "no such game" }), {
          status: 404,
          headers: { "Content-Type": "application/problem+json" },
        }),
    );
    const error = await downloadDemo(gameApi, "abc", false).catch(
      (cause) => cause,
    );
    expect(error).toBeInstanceOf(ApiError);
    expect(error.status).toBe(404);
  });
});

describe("file names", () => {
  test("Content-Disposition forms", () => {
    expect(fileNameFromDisposition(null)).toBeUndefined();
    expect(fileNameFromDisposition("attachment")).toBeUndefined();
    expect(fileNameFromDisposition("attachment; filename=a.json")).toBe(
      "a.json",
    );
    expect(
      fileNameFromDisposition("attachment; filename*=UTF-8''g%201.json"),
    ).toBe("g 1.json");
    expect(
      fileNameFromDisposition('attachment; filename="../etc/passwd"'),
    ).toBeUndefined();
  });

  test("the fallback follows the server's pattern", () => {
    expect(fallbackFileName(JSON.stringify(sampleDemo()))).toBe(
      "island-battles-game-12-generation-2.json",
    );
    expect(fallbackFileName("nonsense")).toBe("island-battles-demo.json");
  });
});
