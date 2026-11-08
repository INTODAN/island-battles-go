import { expect, test } from "bun:test";
import { createApiClients } from "./api/client";
import {
  apiBaseUrl,
  isDesktop,
  platformFetch,
  resolveAgainstBase,
  setDesktopFetch,
  tabStorage,
  webClientUrl,
} from "./platform";

// bun test never defines __DESKTOP_API_BASE__, so these are the browser build's answers.
test("the browser build is not the desktop build", () => {
  expect(isDesktop).toBe(false);
  expect(apiBaseUrl()).toBe(globalThis.location?.origin ?? "");
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
