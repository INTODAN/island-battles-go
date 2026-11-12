import type { components } from "../api/generated/game";
import { platformFetch } from "../platform";

export type TurnDelivery = components["schemas"]["TurnDelivery"];
export type GenerationState = components["schemas"]["GenerationState"];

/** Where `island-battles-bot serve` listens: the Island Battles bot port. */
export const localhostBotOrigin = "http://127.0.0.1:41414";

/** The command a player runs to serve their bot; `mybot:play` names their module and function. */
export const serveCommand = "uv run island-battles-bot serve mybot:play";

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
  /** Gives up on the bot after this long. Default 2 s for health, 5 s for a delivery. */
  timeoutMs?: number;
}

const defaultFetch = platformFetch;

/** How many lines of a bot's refusal are shown, the last ones. */
const maxErrorLines = 6;

/** True when `island-battles-bot serve` answers `GET /health`. */
export async function checkBotHealth(
  options: BotCallOptions = {},
): Promise<boolean> {
  const doFetch = options.fetch ?? defaultFetch;
  try {
    const response = await doFetch(`${localhostBotOrigin}/health`, {
      signal: AbortSignal.timeout(options.timeoutMs ?? 2000),
    });
    return response.ok;
  } catch {
    return false;
  }
}

/**
 * Posts a `TurnDelivery`, as `GET /game/from-lobby/{lobby_id}/turn-delivery`
 * returned it, to the player's localhost bot, and resolves once the bot has
 * accepted it with `202`. The bot then plays the turn and submits it itself,
 * with the delivery's one-turn submit token. Rejects with `BotError`.
 */
export async function deliverTurn(
  delivery: TurnDelivery,
  options: BotCallOptions = {},
): Promise<void> {
  const doFetch = options.fetch ?? defaultFetch;
  let response: Response;
  try {
    response = await doFetch(`${localhostBotOrigin}/turn`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(delivery),
      signal: AbortSignal.timeout(options.timeoutMs ?? 5000),
    });
  } catch (error) {
    if (error instanceof DOMException && error.name === "TimeoutError") {
      throw new BotError(
        "too-slow",
        "Your bot did not accept the turn in time.",
      );
    }
    throw new BotError(
      "unreachable",
      "Your bot is not reachable on 127.0.0.1:41414. Start it with island-battles-bot serve.",
    );
  }
  if (response.status === 202) return;
  const body = (await response.text().catch(() => "")).trim();
  const said =
    body === "" ? "" : ` ${body.split("\n").slice(-maxErrorLines).join("\n")}`;
  if (response.status === 422) {
    throw new BotError(
      "refused",
      `Your bot did not understand the turn. Upgrade its SDK (island-battles-python, or the Go SDK) to one that takes turn deliveries.${said}`,
    );
  }
  throw new BotError(
    "refused",
    `Your bot refused the turn (HTTP ${response.status}).${said}`,
  );
}
