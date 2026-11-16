import { expect, test } from "bun:test";
import {
  type DesktopServers,
  devServers,
  productionServers,
} from "./desktopServers";

interface TauriConfig {
  app: { security: { csp: Record<string, string> } };
}

interface Capability {
  permissions: { identifier: string; allow?: { url: string }[] }[];
}

const read = async <T>(path: string): Promise<T> =>
  (await Bun.file(new URL(path, import.meta.url)).json()) as T;

/** The URLs a capability's HTTP plugin scope allows. */
const httpScope = (capability: Capability): string[] =>
  capability.permissions
    .filter((permission) => permission.identifier === "http:default")
    .flatMap((permission) => permission.allow ?? [])
    .map((allowed) => allowed.url);

/** The sources the CSP lets the window connect to. */
const connectSources = (config: TauriConfig): string[] =>
  (config.app.security.csp["connect-src"] ?? "").split(" ");

const expectAllowed = (
  servers: DesktopServers,
  config: TauriConfig,
  capability: Capability,
): void => {
  for (const server of [servers.game, servers.lobby]) {
    expect(connectSources(config)).toContain(server);
    expect(httpScope(capability)).toContain(`${server}/*`);
  }
};

test("the desktop build talks to the official game and to its lobby on its own subdomain", () => {
  expect(productionServers).toEqual({
    game: "https://island-battles.elidan.org",
    lobby: "https://lobby.elidan.org",
  });
});

test("the desktop app's CSP and HTTP scope allow the game and the lobby it is built with", async () => {
  expectAllowed(
    productionServers,
    await read<TauriConfig>("src-tauri/tauri.conf.json"),
    await read<Capability>("src-tauri/capabilities/main.json"),
  );
});

test("a desktop dev build's CSP and HTTP scope allow the dev server and the local lobby", async () => {
  expectAllowed(
    devServers,
    await read<TauriConfig>("src-tauri/tauri.dev.conf.json"),
    await read<Capability>("src-tauri/capabilities/dev.json"),
  );
});
