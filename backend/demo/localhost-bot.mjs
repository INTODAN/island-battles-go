// The player's own bot, which `island-battles-bot serve` runs on their machine.
// The page delivers each turn to it (the example player does, for a player
// whose bot is the localhost bot) and the bot choice shows whether it is
// running. This is the same protocol as the 3D web client's relay: the page
// reads the player's TurnDelivery from the server and POSTs it to the bot,
// which answers 202 and submits its commands itself with the delivery's
// one-turn submit token. The page never submits the bot's commands.

// Where `island-battles-bot serve` listens: the Island Battles bot port.
export const localhostBotOrigin = "http://127.0.0.1:41414";

// The command a player runs to serve their bot; mybot:play names their module
// and function.
export const serveCommand = "uv run island-battles-bot serve mybot:play";

// How many lines of a bot's refusal the page shows, the last ones.
const maxErrorLines = 8;

// How a delivery to the localhost bot failed: "unreachable" (nothing answered:
// the bot is not running, or the browser blocked the request), "too-slow" (the
// bot did not accept the turn in time) or "refused" (the bot answered with
// something other than 202, such as a submit host it doesn't allow, or an old
// SDK that doesn't know turn deliveries).
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

// deliverTurn posts a TurnDelivery, as GET /game/from-lobby/{lobby_id}/turn-delivery
// returned it, to the player's localhost bot, and resolves once the bot has
// accepted it with 202. The bot then plays and submits the turn itself. It
// rejects with a BotError.
export async function deliverTurn(delivery, { fetch = globalThis.fetch, timeoutMs = 5000 } = {}) {
  let response;
  try {
    response = await fetch(`${localhostBotOrigin}/turn`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(delivery),
      signal: AbortSignal.timeout(timeoutMs),
    });
  } catch (error) {
    if (error?.name === "TimeoutError") throw new BotError("too-slow", "Your bot did not accept the turn in time.");
    throw new BotError("unreachable", "Your bot is not reachable on 127.0.0.1:41414. Start it with island-battles-bot serve.");
  }
  if (response.status === 202) return;
  const body = (await response.text().catch(() => "")).trim();
  const said = body === "" ? "" : `\n${body.split("\n").slice(-maxErrorLines).join("\n")}`;
  if (response.status === 422) {
    throw new BotError("refused", `Your bot did not understand the turn. Upgrade its SDK (island-battles-python, or the Go SDK) to one that takes turn deliveries.${said}`);
  }
  throw new BotError("refused", `Your bot refused the turn (HTTP ${response.status}).${said}`);
}
