import { expect, test } from "bun:test";
import { backendOrigin, lobbyOrigin, proxyOrigin } from "./proxy";

test("forwards the game's paths to the backend", () => {
  for (const pathname of [
    "/game/from-lobby/abc/generation",
    "/game/demo/bot-choice.mjs",
    "/generation/1",
    "/map/create",
    "/bots/defaults",
    "/healthz",
    "/openapi.json",
  ]) {
    expect(proxyOrigin(pathname)).toBe(backendOrigin);
  }
});

test("forwards the lobby's API and page to the lobby", () => {
  for (const pathname of [
    "/lobby/v1/sessions",
    "/lobby/v1/jwks.json",
    "/lobby/ui/",
    "/lobby/ui/app.js",
  ]) {
    expect(proxyOrigin(pathname)).toBe(lobbyOrigin);
  }
});

test("keeps the client's own paths local", () => {
  for (const pathname of [
    "/",
    "/index.html",
    "/gamepad",
    "/healthz/x",
    "/lobbyist",
  ]) {
    expect(proxyOrigin(pathname)).toBeUndefined();
  }
});
