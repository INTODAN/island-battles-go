import type { components } from "../api/generated/game";

export type BotTurn = components["schemas"]["BotTurn"];
export type BotTurnResult = components["schemas"]["BotTurnResult"];
export type Upgrade = components["schemas"]["Upgrade"];
export type GenerationState = components["schemas"]["GenerationState"];

/** Where `island-battles-bot serve` listens: the Island Battles bot port. */
export const localhostBotOrigin = "http://127.0.0.1:41414";

/** The command a player runs to serve their bot; `mybot:play` names their module and function. */
export const serveCommand = "uv run island-battles-bot serve mybot:play";

/** How a call to the localhost bot failed. */
export type BotFailureKind =
  /** Nothing answered: the bot is not running, or the browser blocked the request. */
  | "unreachable"
  /** The bot raised: `500` with the traceback. */
  | "crashed"
  /** The bot took longer than its own limit: `504`. */
  | "too-slow"
  /** The bot answered with something that is not a `BotTurnResult`. */
  | "invalid";

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
  /** Gives up on the bot after this long. Default 20 s. */
  timeoutMs?: number;
}

const defaultFetch = ((...args: Parameters<typeof fetch>) =>
  globalThis.fetch(...args)) as typeof fetch;

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

/** The `BotTurn` for a player's state: the board, their team, their memory, the deadline and the cost table. */
export function buildBotTurn(
  state: GenerationState,
  upgrades: Upgrade[],
): BotTurn {
  return {
    game_id: state.game_id,
    generation: state.generation,
    memory: state.memory,
    team_color: state.team_color,
    turn_deadline_at: state.turn_deadline_at,
    upgrades,
  };
}

const isRecord = (value: unknown): value is Record<string, unknown> =>
  typeof value === "object" && value !== null && !Array.isArray(value);

/** Checks a bot's answer is a `BotTurnResult` and fills the optional parts. */
export function parseBotResult(value: unknown): BotTurnResult {
  if (!isRecord(value) || !Array.isArray(value.commands)) {
    throw new BotError("invalid", "The bot's answer has no commands list.");
  }
  const memory = value.memory ?? null;
  if (memory !== null && !isRecord(memory)) {
    throw new BotError("invalid", "The bot's memory must be a JSON object.");
  }
  const logs = value.logs ?? [];
  if (!Array.isArray(logs)) {
    throw new BotError("invalid", "The bot's logs must be a list.");
  }
  return {
    commands: value.commands as BotTurnResult["commands"],
    logs: logs as BotTurnResult["logs"],
    memory,
  };
}

/** Posts one turn to the player's localhost bot and returns its answer. */
export async function playTurn(
  turn: BotTurn,
  options: BotCallOptions = {},
): Promise<BotTurnResult> {
  const doFetch = options.fetch ?? defaultFetch;
  let response: Response;
  try {
    response = await doFetch(`${localhostBotOrigin}/turn`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(turn),
      signal: AbortSignal.timeout(options.timeoutMs ?? 20_000),
    });
  } catch (error) {
    if (error instanceof DOMException && error.name === "TimeoutError") {
      throw new BotError("too-slow", "Your bot did not answer in time.");
    }
    throw new BotError(
      "unreachable",
      "Your bot is not reachable on 127.0.0.1:41414. Start it with island-battles-bot serve.",
    );
  }
  if (response.status === 504) {
    throw new BotError("too-slow", "Your bot took too long to answer.");
  }
  if (!response.ok) {
    const body = (await response.text().catch(() => "")).trim();
    throw new BotError(
      "crashed",
      body === ""
        ? `Your bot answered HTTP ${response.status}.`
        : `Your bot failed: ${body.split("\n").slice(-6).join("\n")}`,
    );
  }
  let body: unknown;
  try {
    body = await response.json();
  } catch {
    throw new BotError("invalid", "The bot's answer is not JSON.");
  }
  return parseBotResult(body);
}
