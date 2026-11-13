import { expect, test } from "bun:test";
import { createApiClients } from "./api/client";
import {
  gameBaseUrl,
  isDesktop,
  loadServerUrls,
  lobbyBaseUrl,
  platformFetch,
  resolveAgainstBase,
  setDesktopFetch,
  setServerUrls,
  tabStorage,
  webClientUrl,
} from "./platform";

// bun test never defines __DESKTOP_API_BASE__, so these are the browser build's answers.
test("the browser build is not the desktop build", () => {
  expect(isDesktop).toBe(false);
  expect(gameBaseUrl()).toBe(globalThis.location?.origin ?? "");
  expect(lobbyBaseUrl()).toBe(globalThis.location?.origin ?? "");
});

test("the runtime config names the lobby's and the game's URLs", () => {
  try {
    setServerUrls({
      lobby_url: "https://lobby.example.test/",
      game_url: "https://game.example.test",
    });
    expect(lobbyBaseUrl()).toBe("https://lobby.example.test");
    expect(gameBaseUrl()).toBe("https://game.example.test");
    // A missing or invalid URL keeps the page's own origin.
    setServerUrls({ lobby_url: "javascript:alert(1)" });
    expect(lobbyBaseUrl()).toBe(globalThis.location?.origin ?? "");
    expect(gameBaseUrl()).toBe(globalThis.location?.origin ?? "");
  } finally {
    setServerUrls({});
  }
});

test("the browser reads config.json beside the page, and keeps its own origin without one", async () => {
  const saved = (globalThis as { document?: unknown }).document;
  (globalThis as { document?: unknown }).document = {
    baseURI: "https://game.example.test/play/",
  };
  const asked: string[] = [];
  const answer =
    (response: Response) =>
    async (input: URL | RequestInfo): Promise<Response> => {
      asked.push(String(input));
      return response;
    };
  try {
    await loadServerUrls(
      answer(
        Response.json({ lobby_url: "https://lobby.example.test" }),
      ) as typeof fetch,
    );
    expect(asked).toEqual(["https://game.example.test/play/config.json"]);
    expect(lobbyBaseUrl()).toBe("https://lobby.example.test");
    expect(gameBaseUrl()).toBe(globalThis.location?.origin ?? "");
    setServerUrls({});
    await loadServerUrls(
      answer(new Response("<html>", { status: 200 })) as typeof fetch,
    );
    await loadServerUrls(
      answer(new Response("", { status: 404 })) as typeof fetch,
    );
    expect(lobbyBaseUrl()).toBe(globalThis.location?.origin ?? "");
  } finally {
    setServerUrls({});
    (globalThis as { document?: unknown }).document = saved;
  }
});

test("the browser keeps its tab's state in sessionStorage", () => {
  const stored = new Map<string, string>();
  const fake = {
    getItem: (k: string) => stored.get(k) ?? null,
    setItem: (k: string, v: string) => void stored.set(k, v),
    removeItem: (k: string) => void stored.delete(k),
  };
  const saved = (globalThis as { sessionStorage?: unknown }).sessionStorage;
  (globalThis as { sessionStorage?: unknown }).sessionStorage = fake;
  try {
    expect(tabStorage()).toBe(fake as unknown as Storage);
  } finally {
    (globalThis as { sessionStorage?: unknown }).sessionStorage = saved;
  }
});

test("a share link points at the page in the browser", () => {
  const saved = globalThis.location;
  (globalThis as { location?: unknown }).location = {
    origin: "https://example.test",
    pathname: "/play/",
  };
  try {
    expect(webClientUrl()).toBe("https://example.test/play/");
  } finally {
    (globalThis as { location?: unknown }).location = saved;
  }
});

test("paths resolve against the desktop game server and other inputs are left alone", () => {
  const base = "https://island-battles.elidan.org";
  expect(resolveAgainstBase("/lobby/v1/sessions?x=1", base)).toBe(
    "https://island-battles.elidan.org/lobby/v1/sessions?x=1",
  );
  expect(resolveAgainstBase("http://127.0.0.1:41414/turn", base)).toBe(
    "http://127.0.0.1:41414/turn",
  );
  const request = new Request("https://island-battles.elidan.org/healthz");
  expect(resolveAgainstBase(request, base)).toBe(request);
});

test("API clients send the lobby's routes to the lobby and the game's to the game", async () => {
  const seen: string[] = [];
  const { lobbyApi, gameApi } = createApiClients({
    lobbyUrl: "https://lobby.example.test",
    gameUrl: "https://game.example.test",
    getAccessToken: () => "guest",
    fetch: (async (input: Request) => {
      seen.push(input.url);
      const body = input.url.endsWith("/player-tokens")
        ? { token: "t", expires_at: "2999-01-01T00:00:00Z" }
        : {};
      return new Response(JSON.stringify(body), {
        headers: { "content-type": "application/json" },
      });
    }) as unknown as typeof fetch,
  });
  await lobbyApi.POST("/lobby/v1/connection-token", { body: {} });
  await gameApi.GET("/bots/defaults");
  expect(seen).toEqual([
    "https://lobby.example.test/lobby/v1/connection-token",
    "https://game.example.test/bots/defaults",
  ]);
});

test("API clients use an explicit base URL over the default", async () => {
  let seen = "";
  const { gameApi } = createApiClients({
    baseUrl: "https://island-battles.elidan.org",
    fetch: (async (input: Request) => {
      seen = input.url;
      return new Response("{}", {
        headers: { "content-type": "application/json" },
      });
    }) as unknown as typeof fetch,
  });
  await gameApi.GET("/healthz" as never);
  expect(seen).toBe("https://island-battles.elidan.org/healthz");
});

test("platformFetch calls the page's fetch as it is at call time, until the desktop one is set", async () => {
  const saved = globalThis.fetch;
  const calls: string[] = [];
  globalThis.fetch = (async (input: string) => {
    calls.push(`page ${input}`);
    return new Response("");
  }) as unknown as typeof fetch;
  try {
    await platformFetch("/a");
    setDesktopFetch((async (input: string) => {
      calls.push(`desktop ${input}`);
      return new Response("");
    }) as unknown as typeof fetch);
    await platformFetch("/b");
    expect(calls).toEqual(["page /a", "desktop /b"]);
  } finally {
    setDesktopFetch(undefined);
    globalThis.fetch = saved;
  }
});
