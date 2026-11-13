import { expect, test } from "bun:test";
import { backendOrigin, proxyOrigin } from "./proxy";

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

test("keeps the client's own paths local, and forwards nothing to the lobby, which is on its own origin", () => {
  for (const pathname of [
    "/lobby/v1/sessions",
    "/lobby/ui/",
    "/",
    "/index.html",
    "/gamepad",
    "/healthz/x",
    "/lobbyist",
  ]) {
    expect(proxyOrigin(pathname)).toBeUndefined();
  }
});
