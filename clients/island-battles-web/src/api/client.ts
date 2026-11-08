import createClient, { type Middleware } from "openapi-fetch";
import { apiBaseUrl } from "../platform";
import { ApiError, errorFromResponse } from "./errors";
import type { paths as GamePaths } from "./generated/game";
import type { paths as LobbyPaths } from "./generated/lobby";

export interface ApiOptions {
  /** Server origin. Defaults to the page's own origin, or the game server in the desktop build. */
  baseUrl?: string;
  /** Returns the current bearer token, or undefined before a session exists. */
  getAccessToken?: () => string | undefined;
  /** Replaces `fetch`; tests use it. */
  fetch?: typeof fetch;
}

let accessToken: string | undefined;

/** Sets the token the default clients send. Pass undefined to sign out. */
export function setAccessToken(token: string | undefined): void {
  accessToken = token;
}

/** Creating a guest session is the only call made without credentials. */
const isSessionCreate = (method: string, schemaPath: string) =>
  method === "POST" && schemaPath === "/lobby/v1/sessions";

function authMiddleware(getAccessToken: () => string | undefined): Middleware {
  return {
    onRequest({ request, schemaPath }) {
      if (isSessionCreate(request.method, schemaPath)) return undefined;
      const token = getAccessToken();
      if (token !== undefined) {
        request.headers.set("Authorization", `Bearer ${token}`);
      }
      return request;
    },
  };
}

/** Turns every non-2xx response into a rejected ApiError. */
const errorMiddleware: Middleware = {
  async onResponse({ response }) {
    if (response.ok) return undefined;
    throw await errorFromResponse(response);
  },
};

/** Wraps fetch so a failure to get any response rejects with a transport ApiError. */
function transportFetch(inner: typeof fetch): typeof fetch {
  return (async (...args: Parameters<typeof fetch>) => {
    try {
      return await inner(...args);
    } catch (error) {
      if (error instanceof DOMException && error.name === "AbortError") {
        throw error;
      }
      throw new ApiError({
        kind: "transport",
        status: 0,
        detail: error instanceof Error ? error.message : String(error),
        cause: error,
      });
    }
  }) as typeof fetch;
}

/**
 * Creates the lobby and game clients. Calls resolve with `{ data }` on success
 * and reject with ApiError on any failure, so callers never see `error`.
 */
export function createApiClients(options: ApiOptions = {}) {
  const baseUrl = options.baseUrl ?? apiBaseUrl();
  const getAccessToken = options.getAccessToken ?? (() => accessToken);
  const inner = options.fetch;
  const make = <P extends {}>() => {
    // Look fetch up per call so a later global replacement (and tests) is honoured.
    const client = createClient<P>({
      baseUrl,
      fetch: transportFetch(
        inner ?? (((...args) => globalThis.fetch(...args)) as typeof fetch),
      ),
    });
    client.use(authMiddleware(getAccessToken), errorMiddleware);
    return client;
  };
  return { lobbyApi: make<LobbyPaths>(), gameApi: make<GamePaths>() };
}

export const { lobbyApi, gameApi } = createApiClients();
