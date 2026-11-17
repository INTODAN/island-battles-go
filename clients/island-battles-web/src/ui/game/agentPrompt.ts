// "Ask your AI agent": a ready-made prompt that a player pastes into their own AI agent, so it
// investigates the game with them through the island-battles MCP server
// (clients/island-battles-go/cmd/island-battles-mcp, #282). Pure, so `bun test` covers it.

/**
 * The game the agent should load: a lobby's game by its join code, a lobby's game whose code
 * this page doesn't know, or a demo file.
 */
export type AgentGame =
  | { kind: "lobby"; code: string }
  | { kind: "unknown-lobby"; gameId: number }
  | { kind: "demo"; gameId: number; fileName: string | null };

export interface AgentPromptOptions {
  game: AgentGame;
  /** The turn the replay shows, from 1, and how many turns there are. */
  turn: number;
  turns: number;
  /** The player's team, as the screen names it, when the viewer plays in the game. */
  team?: { name: string; color: number } | null;
  /** The game and lobby the client talks to; the MCP server is told only when they differ from production's. */
  gameUrl: string;
  lobbyUrl: string;
}

export const PRODUCTION_GAME_URL = "https://island-battles.elidan.org";
export const PRODUCTION_LOBBY_URL = "https://lobby.elidan.org";
export const MCP_INSTALL =
  "go install github.com/INTODAN/island-battles-go/clients/island-battles-go/cmd/island-battles-mcp@latest";
export const MCP_README =
  "https://github.com/INTODAN/island-battles-go/blob/main/clients/island-battles-go/cmd/island-battles-mcp/README.md";

const sameOrigin = (url: string, expected: string) =>
  url.replace(/\/+$/, "") === expected;

/** The command that registers the MCP server with Claude Code, with the servers it reads. */
export function mcpAddCommand(gameUrl: string, lobbyUrl: string): string {
  const flags: string[] = [];
  if (!sameOrigin(gameUrl, PRODUCTION_GAME_URL)) {
    flags.push(`--game-url ${gameUrl}`);
  }
  if (!sameOrigin(lobbyUrl, PRODUCTION_LOBBY_URL)) {
    flags.push(`--lobby-url ${lobbyUrl}`);
  }
  return ["claude mcp add island-battles -- island-battles-mcp", ...flags].join(
    " ",
  );
}

/** The prompt's line that tells the agent how to load the game. */
function loadLine(game: AgentGame): string {
  switch (game.kind) {
    case "lobby":
      return `The game: call load_lobby with the lobby code ${game.code}. It reads the lobby's newest game as a watcher, also while it is still running; a watcher sees no bot logs until the game has ended, and then every team's.`;
    case "unknown-lobby":
      return `The game: game ${game.gameId} of a lobby whose join code this page doesn't know. Ask me for the code, as the lobby page shows it, then call load_lobby with it; it loads the lobby's newest game.`;
    case "demo":
      return `The game: I saved it as a demo file${game.fileName ? ` named "${game.fileName}"` : ` of game ${game.gameId}`}. Ask me for its path on this computer, then call load_demo with it.`;
  }
}

/** The prompt that "Ask your AI agent" copies. */
export function agentPrompt(options: AgentPromptOptions): string {
  const { game, turn, turns } = options;
  const lines = [
    "Let's investigate a game of Island Battles together, with the island-battles MCP server.",
    "",
    "If its tools (load_demo, load_lobby, game_overview, explain_castle, diff_turns, get_turn, get_logs) aren't available, set it up first. It needs Go 1.25 or later:",
    `  ${MCP_INSTALL}`,
    `  ${mcpAddCommand(options.gameUrl, options.lobbyUrl)}`,
    `Then restart this session. For another MCP client, run island-battles-mcp as a stdio server. Setup: ${MCP_README}`,
    "",
    loadLine(game),
  ];
  if (options.team) {
    lines.push(
      `I play for team ${options.team.name} (colour ${options.team.color}).`,
    );
  }
  lines.push(
    `The replay is stopped on turn ${turn} of ${turns}. "The current turn" means turn ${turn}.`,
    "",
    `Load the game, read game_overview, look at turn ${turn}, and then wait for my question, such as "In the current turn, how did my castle get captured?"`,
  );
  return lines.join("\n");
}

export interface ClipboardLike {
  writeText(text: string): Promise<void>;
}

/** Copies the prompt; resolves false when the clipboard refuses, so the screen shows it to copy by hand. */
export async function copyText(
  text: string,
  clipboard: ClipboardLike | null | undefined = globalThis.navigator?.clipboard,
): Promise<boolean> {
  if (!clipboard) return false;
  try {
    await clipboard.writeText(text);
    return true;
  } catch {
    return false;
  }
}
