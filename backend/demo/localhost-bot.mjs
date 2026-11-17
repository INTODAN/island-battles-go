// The player's own bot, which `island-battles-bot serve` runs on their machine.
// The page delivers each turn to it (the example player does, for a player
// whose bot is the localhost bot) and the bot choice shows whether it is
// running. This is the same protocol as the 3D web client's relay: the page
// reads the player's TurnDelivery from the server and POSTs it to the bot,
// which answers 202 and submits its commands itself with the delivery's
// one-turn submit token. The page never submits the bot's commands.
//
// A localhost bot listens on a port of 127.0.0.1: the Island Battles bot
// port, 41414, unless the player chose another. The host can also seat
// several localhost bots, each on its own port, and the host's page relays
// them all.

// The Island Battles bot port, where `island-battles-bot serve` listens
// without --port.
export const defaultBotPort = 41414;

// The ports a localhost bot may use: not a privileged one.
export const minBotPort = 1024;
export const maxBotPort = 65535;

// Where `island-battles-bot serve` listens on port.
export function localhostBotOrigin(port = defaultBotPort) {
  return `http://127.0.0.1:${port}`;
}

// The port that text names, or null when it is not a whole number from
// minBotPort to maxBotPort.
export function parseBotPort(text) {
  const trimmed = String(text ?? "").trim();
  if (!/^[0-9]{1,5}$/.test(trimmed)) return null;
  const port = Number(trimmed);
  return port >= minBotPort && port <= maxBotPort ? port : null;
}

// Why a port can't be used, for a field that a player typed it in, or null
// when it can: it must be a number in range, and not one of taken, the
// ports of the other localhost bots that the same page relays.
export function portProblem(text, taken = []) {
  const port = parseBotPort(text);
  if (port === null) return `Type a port from ${minBotPort} to ${maxBotPort}, such as ${defaultBotPort}.`;
  if (taken.includes(port)) return `Port ${port} is taken by another of your localhost bots: give each its own port.`;
  return null;
}

// The first port from the default one up that taken leaves free.
export function nextFreePort(taken = []) {
  let port = defaultBotPort;
  while (taken.includes(port) && port < maxBotPort) port++;
  return port;
}

// The command a player runs to serve their bot on port; mybot:play names
// their module and function.
export function serveCommandFor(port = defaultBotPort) {
  return port === defaultBotPort ? "uv run island-battles-bot serve mybot:play" : `uv run island-battles-bot serve --port ${port} mybot:play`;
}

// The command for the default port.
export const serveCommand = serveCommandFor();

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

// checkBotHealth is true when `island-battles-bot serve` answers GET /health
// on port.
export async function checkBotHealth({ port = defaultBotPort, fetch = globalThis.fetch, timeoutMs = 2000 } = {}) {
  try {
    const response = await fetch(`${localhostBotOrigin(port)}/health`, { signal: AbortSignal.timeout(timeoutMs) });
    return response.ok;
  } catch {
    return false;
  }
}

// deliverTurn posts a TurnDelivery, as GET /game/from-lobby/{lobby_id}/turn-delivery
// returned it, to the localhost bot on port, and resolves once the bot has
// accepted it with 202. The bot then plays and submits the turn itself. It
// rejects with a BotError.
export async function deliverTurn(delivery, { port = defaultBotPort, fetch = globalThis.fetch, timeoutMs = 5000 } = {}) {
  let response;
  try {
    response = await fetch(`${localhostBotOrigin(port)}/turn`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(delivery),
      signal: AbortSignal.timeout(timeoutMs),
    });
  } catch (error) {
    if (error?.name === "TimeoutError") throw new BotError("too-slow", `The bot on port ${port} did not accept the turn in time.`);
    const serve = port === defaultBotPort ? "island-battles-bot serve" : `island-battles-bot serve --port ${port}`;
    throw new BotError("unreachable", `Your bot is not reachable on 127.0.0.1:${port}. Start it with ${serve}.`);
  }
  if (response.status === 202) return;
  const body = (await response.text().catch(() => "")).trim();
  const said = body === "" ? "" : `\n${body.split("\n").slice(-maxErrorLines).join("\n")}`;
  if (response.status === 422) {
    throw new BotError("refused", `Your bot did not understand the turn. Upgrade its SDK (island-battles-python, or the Go SDK) to one that takes turn deliveries.${said}`);
  }
  throw new BotError("refused", `Your bot refused the turn (HTTP ${response.status}).${said}`);
}
