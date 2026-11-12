import { describe, expect, test } from "bun:test";
import {
  BotError,
  checkBotHealth,
  deliverTurn,
  type TurnDelivery,
} from "./localhost";

const delivery = {
  turn: {
    game_id: 5,
    generation: { id: 2, castles: [], soldier_groups: [], clashes: [] },
    memory: { a: 1 },
    team_color: 1,
    turn_deadline_at: null,
    upgrades: [],
  },
  submit: {
    url: "http://localhost:8888/generation/commands",
    token: "ibst1.x",
    deadline_at: null,
  },
} as unknown as TurnDelivery;

const reply = (body: BodyInit, status = 200) =>
  (async () => new Response(body, { status })) as unknown as typeof fetch;

async function failure(doFetch: typeof fetch): Promise<BotError> {
  try {
    await deliverTurn(delivery, { fetch: doFetch });
  } catch (error) {
    if (error instanceof BotError) return error;
    throw error;
  }
  throw new Error("the delivery did not fail");
}

describe("the localhost bot", () => {
  test("posts the delivery as JSON to 127.0.0.1:41414/turn and takes 202", async () => {
    let seen: { url: string; init: RequestInit | undefined } | undefined;
    await deliverTurn(delivery, {
      fetch: (async (url: string, init?: RequestInit) => {
        seen = { url, init };
        return new Response('{"accepted": true}', { status: 202 });
      }) as unknown as typeof fetch,
    });
    expect(seen?.url).toBe("http://127.0.0.1:41414/turn");
    expect(seen?.init?.method).toBe("POST");
    expect(JSON.parse(String(seen?.init?.body))).toEqual(delivery);
    expect(seen?.init?.headers).toEqual({ "Content-Type": "application/json" });
  });

  test("a bot that is not running is unreachable", async () => {
    const error = await failure((async () => {
      throw new TypeError("Failed to fetch");
    }) as unknown as typeof fetch);
    expect(error.kind).toBe("unreachable");
    expect(error.message).toContain("127.0.0.1:41414");
  });

  test("a bot that doesn't accept in time is too slow", async () => {
    const error = await failure((async () => {
      throw new DOMException("timed out", "TimeoutError");
    }) as unknown as typeof fetch);
    expect(error.kind).toBe("too-slow");
  });

  test("a refusal shows the bot's reason, its last lines only", async () => {
    const lines = `${Array.from({ length: 20 }, (_, index) => `line ${index}`).join("\n")}\nsubmit.url is not an allowed host`;
    const error = await failure(reply(lines, 400));
    expect(error.kind).toBe("refused");
    expect(error.message).toStartWith("Your bot refused the turn (HTTP 400).");
    expect(error.message).toContain("not an allowed host");
    expect(error.message).not.toContain("line 3\n");
    expect((await failure(reply("", 500))).message).toBe(
      "Your bot refused the turn (HTTP 500).",
    );
  });

  test("an old SDK's answer asks for an upgrade", async () => {
    expect((await failure(reply("{}", 200))).kind).toBe("refused");
    const old = await failure(reply("the body is not a BotTurn", 422));
    expect(old.message).toContain("Upgrade its SDK");
  });

  test("health is true only for an OK answer", async () => {
    expect(await checkBotHealth({ fetch: reply("{}") })).toBe(true);
    expect(await checkBotHealth({ fetch: reply("", 500) })).toBe(false);
    expect(
      await checkBotHealth({
        fetch: (async () => {
          throw new TypeError("Failed to fetch");
        }) as unknown as typeof fetch,
      }),
    ).toBe(false);
  });
});
