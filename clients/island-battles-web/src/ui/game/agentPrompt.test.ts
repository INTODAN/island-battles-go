import { describe, expect, test } from "bun:test";
import {
  agentPrompt,
  copyText,
  MCP_INSTALL,
  MCP_README,
  mcpAddCommand,
  PRODUCTION_GAME_URL,
  PRODUCTION_LOBBY_URL,
} from "./agentPrompt";

const production = {
  gameUrl: PRODUCTION_GAME_URL,
  lobbyUrl: PRODUCTION_LOBBY_URL,
};

describe("Ask your AI agent", () => {
  test("a live game's prompt names its lobby code, the turn shown and the player's team", () => {
    const prompt = agentPrompt({
      game: { kind: "lobby", code: "ABCD2345" },
      turn: 40,
      turns: 57,
      team: { name: "Blue", color: 1 },
      ...production,
    });
    expect(prompt).toContain(MCP_INSTALL);
    expect(prompt).toContain(
      "claude mcp add island-battles -- island-battles-mcp\n",
    );
    expect(prompt).toContain(MCP_README);
    expect(prompt).toContain("call load_lobby with the lobby code ABCD2345");
    expect(prompt).toContain("I play for team Blue (colour 1).");
    expect(prompt).toContain(
      'The replay is stopped on turn 40 of 57. "The current turn" means turn 40.',
    );
    expect(prompt).toContain("how did my castle get captured?");
    expect(prompt).not.toContain("load_demo with");
  });

  test("a demo's prompt asks for the file's path and names no team for a watcher", () => {
    const prompt = agentPrompt({
      game: {
        kind: "demo",
        gameId: 12,
        fileName: "island-battles-game-12-generation-80.json",
      },
      turn: 1,
      turns: 80,
      ...production,
    });
    expect(prompt).toContain(
      'I saved it as a demo file named "island-battles-game-12-generation-80.json". Ask me for its path on this computer, then call load_demo with it.',
    );
    expect(prompt).toContain("turn 1 of 80");
    expect(prompt).not.toContain("I play for team");
    expect(
      agentPrompt({
        game: { kind: "demo", gameId: 12, fileName: null },
        turn: 3,
        turns: 80,
        team: null,
        ...production,
      }),
    ).toContain("I saved it as a demo file of game 12.");
  });

  test("a local or other server is passed to the MCP server", () => {
    expect(mcpAddCommand(`${PRODUCTION_GAME_URL}/`, PRODUCTION_LOBBY_URL)).toBe(
      "claude mcp add island-battles -- island-battles-mcp",
    );
    expect(
      mcpAddCommand("http://localhost:3000", "http://localhost:8890"),
    ).toBe(
      "claude mcp add island-battles -- island-battles-mcp --game-url http://localhost:3000 --lobby-url http://localhost:8890",
    );
  });

  test("copying falls back when the clipboard refuses", async () => {
    const copied: string[] = [];
    expect(
      await copyText("prompt", {
        writeText: async (text) => {
          copied.push(text);
        },
      }),
    ).toBe(true);
    expect(copied).toEqual(["prompt"]);
    expect(
      await copyText("prompt", {
        writeText: () => Promise.reject(new Error("denied")),
      }),
    ).toBe(false);
    expect(await copyText("prompt", null)).toBe(false);
  });
});
