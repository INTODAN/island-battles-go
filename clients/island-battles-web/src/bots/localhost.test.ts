import { describe, expect, test } from "bun:test";
import {
  BotError,
  buildBotTurn,
  checkBotHealth,
  type GenerationState,
  parseBotResult,
  playTurn,
  type Upgrade,
} from "./localhost";

const turn = buildBotTurn(
  {
    game_id: 5,
    generation: { id: 2, castles: [], soldier_groups: [], clashes: [] },
    memory: { a: 1 },
    team_color: 1,
    turn_deadline_at: null,
    bot: { kind: "localhost" },
  } as unknown as GenerationState,
  [] as Upgrade[],
);

const reply = (body: BodyInit, status = 200) =>
  (async () => new Response(body, { status })) as unknown as typeof fetch;

describe("the localhost bot", () => {
  test("builds the turn from the player's state", () => {
    expect(turn).toEqual({
      game_id: 5,
      generation: { id: 2, castles: [], soldier_groups: [], clashes: [] },
      memory: { a: 1 },
      team_color: 1,
      turn_deadline_at: null,
      upgrades: [],
    });
  });

  test("posts JSON to 127.0.0.1:41414/turn", async () => {
    let seen: { url: string; init: RequestInit | undefined } | undefined;
    const result = await playTurn(turn, {
      fetch: (async (url: string, init?: RequestInit) => {
        seen = { url, init };
        return new Response(
          JSON.stringify({ commands: [], logs: [], memory: { b: 2 } }),
        );
      }) as unknown as typeof fetch,
    });
    expect(seen?.url).toBe("http://127.0.0.1:41414/turn");
    expect(seen?.init?.method).toBe("POST");
    expect(seen?.init?.headers).toEqual({ "Content-Type": "application/json" });
    expect(JSON.parse(String(seen?.init?.body))).toEqual(turn);
    expect(result).toEqual({ commands: [], logs: [], memory: { b: 2 } });
  });

  test("tells apart a bot that is not running, one that crashed and one that is too slow", async () => {
    const down = (async () => {
      throw new TypeError("Failed to fetch");
    }) as unknown as typeof fetch;
    await expect(playTurn(turn, { fetch: down })).rejects.toMatchObject({
      kind: "unreachable",
    });
    await expect(
      playTurn(turn, {
        fetch: reply("Traceback...\nZeroDivisionError: boom", 500),
      }),
    ).rejects.toMatchObject({
      kind: "crashed",
      message: expect.stringContaining("ZeroDivisionError"),
    });
    await expect(
      playTurn(turn, { fetch: reply("slow", 504) }),
    ).rejects.toMatchObject({
      kind: "too-slow",
    });
    await expect(
      playTurn(turn, { fetch: reply("not json") }),
    ).rejects.toBeInstanceOf(BotError);
  });

  test("checks a bot's answer", () => {
    expect(parseBotResult({ commands: [] })).toEqual({
      commands: [],
      logs: [],
      memory: null,
    });
    expect(() => parseBotResult({})).toThrow(BotError);
    expect(() => parseBotResult({ commands: [], memory: [] })).toThrow(
      BotError,
    );
  });

  test("health is true only when the bot answers", async () => {
    expect(await checkBotHealth({ fetch: reply("ok") })).toBe(true);
    expect(await checkBotHealth({ fetch: reply("no", 500) })).toBe(false);
    expect(
      await checkBotHealth({
        fetch: (async () => {
          throw new TypeError("refused");
        }) as unknown as typeof fetch,
      }),
    ).toBe(false);
  });
});
