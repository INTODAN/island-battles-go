import type { components } from "../api/generated/game";
import { platformFetch } from "../platform";

export type TurnDelivery = components["schemas"]["TurnDelivery"];
export type GenerationState = components["schemas"]["GenerationState"];
export type RelayedBot = components["schemas"]["RelayedBot"];

/** The Island Battles bot port, where `island-battles-bot serve` listens without `--port`. */
export const defaultBotPort = 41414;

/** The ports a localhost bot may use: not a privileged one. */
export const minBotPort = 1024;
export const maxBotPort = 65535;

/** Where `island-battles-bot serve` listens on `port`. */
export function localhostBotOrigin(port: number = defaultBotPort): string {
  return `http://127.0.0.1:${port}`;
}

/** The port `text` names, or null when it is not a whole number from 1024 to 65535. */
export function parseBotPort(
  text: string | number | null | undefined,
): number | null {
  const trimmed = String(text ?? "").trim();
  if (!/^[0-9]{1,5}$/.test(trimmed)) return null;
  const port = Number(trimmed);
  return port >= minBotPort && port <= maxBotPort ? port : null;
}

/**
 * Why a typed port can't be used, or null when it can: it must be a number in range, and not
 * one of `taken`, the ports of the other localhost bots that this client relays.
 */
export function portProblem(
  text: string,
  taken: readonly number[] = [],
): string | null {
  const port = parseBotPort(text);
  if (port === null) {
    return `Type a port from ${minBotPort} to ${maxBotPort}, such as ${defaultBotPort}.`;
  }
  if (taken.includes(port)) {
    return `Port ${port} is taken by another of your localhost bots: give each its own port.`;
  }
  return null;
}

/** The first port from the default one up that `taken` leaves free. */
export function nextFreePort(taken: readonly number[] = []): number {
  let port = defaultBotPort;
  while (taken.includes(port) && port < maxBotPort) port++;
  return port;
}

/** The command a player runs to serve their bot on `port`: the one way to run a localhost bot, which the tutorial and the template's README show too. `bot:play` is the template's module and function. */
export function serveCommandFor(port: number = defaultBotPort): string {
  return port === defaultBotPort
    ? "uv run island-battles-bot serve bot:play"
    : `uv run island-battles-bot serve --port ${port} bot:play`;
}

/** The command for the default port. */
export const serveCommand = serveCommandFor();

/** How a delivery to the localhost bot failed. */
export type BotFailureKind =
  /** Nothing answered: the bot is not running, or the browser blocked the request. */
  | "unreachable"
  /** The bot did not accept the turn in time. */
  | "too-slow"
  /**
   * The bot answered with something other than `202`: a submit host it doesn't
   * allow, or an SDK too old for turn deliveries.
   */
  | "refused";

export class BotError extends Error {
  readonly kind: BotFailureKind;
  constructor(kind: BotFailureKind, message: string) {
    super(message);
    this.name = "BotError";
    this.kind = kind;
  }
}

export interface BotCallOptions {
  fetch?: typeof fetch;
  /** The bot's port on 127.0.0.1. Default 41414. */
  port?: number;
  /** Gives up on the bot after this long. Default 2 s for health, 5 s for a delivery. */
  timeoutMs?: number;
}

const defaultFetch = platformFetch;

/** How many lines of a bot's refusal are shown, the last ones. */
const maxErrorLines = 6;

/** True when `island-battles-bot serve` answers `GET /health` on the port. */
export async function checkBotHealth(
  options: BotCallOptions = {},
): Promise<boolean> {
  const doFetch = options.fetch ?? defaultFetch;
  try {
    const response = await doFetch(
      `${localhostBotOrigin(options.port)}/health`,
      { signal: AbortSignal.timeout(options.timeoutMs ?? 2000) },
    );
    return response.ok;
  } catch {
    return false;
  }
}

/**
 * Posts a `TurnDelivery`, as `GET /game/from-lobby/{lobby_id}/turn-delivery`
 * returned it, to the localhost bot on the port, and resolves once the bot has
 * accepted it with `202`. The bot then plays the turn and submits it itself,
 * with the delivery's one-turn submit token. Rejects with `BotError`.
 */
export async function deliverTurn(
  delivery: TurnDelivery,
  options: BotCallOptions = {},
): Promise<void> {
  const doFetch = options.fetch ?? defaultFetch;
  const port = options.port ?? defaultBotPort;
  let response: Response;
  try {
    response = await doFetch(`${localhostBotOrigin(port)}/turn`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(delivery),
      signal: AbortSignal.timeout(options.timeoutMs ?? 5000),
    });
  } catch (error) {
    if (error instanceof DOMException && error.name === "TimeoutError") {
      throw new BotError(
        "too-slow",
        `The bot on port ${port} did not accept the turn in time.`,
      );
    }
    const serve =
      port === defaultBotPort
        ? "island-battles-bot serve"
        : `island-battles-bot serve --port ${port}`;
    throw new BotError(
      "unreachable",
      `Your bot is not reachable on 127.0.0.1:${port}. Start it with ${serve}.`,
    );
  }
  if (response.status === 202) return;
  const body = (await response.text().catch(() => "")).trim();
  const said =
    body === "" ? "" : ` ${body.split("\n").slice(-maxErrorLines).join("\n")}`;
  if (response.status === 422) {
    throw new BotError(
      "refused",
      `Your bot did not understand the turn: its SDK is older than the game. Upgrade it and restart the bot: uv sync --upgrade-package island-battles-python in the bot's folder (or go get github.com/INTODAN/island-battles-go/clients/island-battles-go@latest for the Go SDK).${said}`,
    );
  }
  throw new BotError(
    "refused",
    `Your bot refused the turn (HTTP ${response.status}).${said}`,
  );
}
