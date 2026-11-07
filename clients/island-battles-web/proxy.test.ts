import { expect, test } from "bun:test";
import { isProxiedPath } from "./proxy";

test("forwards API paths to the backend", () => {
  for (const pathname of [
    "/lobby/v1/sessions",
    "/game/from-lobby/abc/generation",
    "/generation/1",
    "/map/create",
    "/bots/defaults",
    "/healthz",
    "/openapi.json",
  ]) {
    expect(isProxiedPath(pathname)).toBe(true);
  }
});

test("keeps the client's own paths local", () => {
  for (const pathname of [
    "/",
    "/index.html",
    "/lobby/ui/",
    "/gamepad",
    "/healthz/x",
  ]) {
    expect(isProxiedPath(pathname)).toBe(false);
  }
});
