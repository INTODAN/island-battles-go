// The player's own bot, which `island-battles-bot serve` runs on their machine.
// The page relays each turn to it (the example player does, for a player whose
// bot is the localhost bot) and the bot choice shows whether it is running.
// This is the same protocol as the 3D web client's relay: BotTurn in,
// BotTurnResult out.

// Where `island-battles-bot serve` listens: the Island Battles bot port.
export const localhostBotOrigin = "http://127.0.0.1:41414";

// The command a player runs to serve their bot; mybot:play names their module
// and function.
export const serveCommand = "uv run island-battles-bot serve mybot:play";

// How many lines of a failed bot's answer the page shows, the last ones, where
// a Python traceback names the error.
const maxErrorLines = 8;

// How a call to the localhost bot failed: "unreachable" (nothing answered: the
// bot is not running, or the browser blocked the request), "crashed" (the bot
// raised: 500 with its traceback), "too-slow" (the bot took longer than its
// own limit: 504, or the page gave up) or "invalid" (the answer is not a
// BotTurnResult).
export class BotError extends Error {
  constructor(kind, message) {
    super(message);
    this.name = "BotError";
    this.kind = kind;
  }
}

// checkBotHealth is true when `island-battles-bot serve` answers GET /health.
export async function checkBotHealth({ fetch = globalThis.fetch, timeoutMs = 2000 } = {}) {
  try {
    const response = await fetch(`${localhostBotOrigin}/health`, { signal: AbortSignal.timeout(timeoutMs) });
    return response.ok;
  } catch {
    return false;
  }
}

// buildBotTurn is the BotTurn for a player's state: the board, their team,
// their memory, the deadline and the upgrade cost table.
export function buildBotTurn(state, upgrades) {
  return {
    game_id: state.game_id,
    generation: state.generation,
    memory: state.memory ?? null,
    team_color: state.team_color,
    turn_deadline_at: state.turn_deadline_at ?? null,
    upgrades,
  };
}

const isRecord = (value) => typeof value === "object" && value !== null && !Array.isArray(value);

// parseBotResult checks that a bot's answer is a BotTurnResult and fills its
// optional parts.
export function parseBotResult(value) {
  if (!isRecord(value) || !Array.isArray(value.commands)) throw new BotError("invalid", "The bot's answer has no commands list.");
  const memory = value.memory ?? null;
  if (memory !== null && !isRecord(memory)) throw new BotError("invalid", "The bot's memory must be a JSON object.");
  const logs = value.logs ?? [];
  if (!Array.isArray(logs)) throw new BotError("invalid", "The bot's logs must be a list.");
  return { commands: value.commands, logs, memory };
}

// playTurn posts one turn to the player's localhost bot and resolves to its
// BotTurnResult. It rejects with a BotError.
export async function playTurn(turn, { fetch = globalThis.fetch, timeoutMs = 20000 } = {}) {
  let response;
  try {
    response = await fetch(`${localhostBotOrigin}/turn`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(turn),
      signal: AbortSignal.timeout(timeoutMs),
    });
  } catch (error) {
    if (error?.name === "TimeoutError") throw new BotError("too-slow", "Your bot did not answer in time.");
    throw new BotError("unreachable", "Your bot is not reachable on 127.0.0.1:41414. Start it with island-battles-bot serve.");
  }
  if (response.status === 504) throw new BotError("too-slow", "Your bot took too long to answer.");
  if (!response.ok) {
    const body = (await response.text().catch(() => "")).trim();
    throw new BotError("crashed", body === "" ? `Your bot answered HTTP ${response.status}.` : `Your bot failed:\n${body.split("\n").slice(-maxErrorLines).join("\n")}`);
  }
  let body;
  try {
    body = await response.json();
  } catch {
    throw new BotError("invalid", "The bot's answer is not JSON.");
  }
  return parseBotResult(body);
}
