import { describe, expect, test } from "bun:test";
import { createRealtime } from "../../realtime";
import type { StorageLike } from "../../realtime/session-storage";
import { partlyDeletedProblem } from "./accountModel";
import { createAccountService } from "./accountService";

class FakeStorage implements StorageLike {
  items = new Map<string, string>();
  getItem(key: string) {
    return this.items.get(key) ?? null;
  }
  setItem(key: string, value: string) {
    this.items.set(key, value);
  }
  removeItem(key: string) {
    this.items.delete(key);
  }
}

const ACCOUNT_ID = "33333333-3333-4333-8333-333333333333";

const competitor = (name: string, id = `id-${name}`) => ({
  id,
  kind: "bot",
  name,
  rating: 1500,
  games: 0,
  wins: 0,
  draws: 0,
  losses: 0,
  last_played_at: null,
});

/**
 * A fake lobby and game for a signed-in account: the lobby issues numbered player tokens that
 * carry the display name, and the game keeps the bot names, at most 5, unique.
 */
async function signedIn() {
  const calls: string[] = [];
  let displayName = "Dana";
  /** What the fake refuses: the session's player tokens, or the lobby's account deletion. */
  const fail = { playerTokens: false, lobbyDelete: false };
  let tokens = 0;
  const tokenNames = new Map<string, string>();
  let bots: ReturnType<typeof competitor>[] = [];
  const fetchStub = (async (
    input: Request | string | URL,
    init?: RequestInit,
  ) => {
    const request = new Request(input, init);
    const path = new URL(request.url).pathname;
    const route = `${request.method} ${path}`;
    calls.push(route);
    const authorization = request.headers.get("Authorization") ?? "";
    switch (route) {
      case "POST /lobby/v1/sessions/google":
        return Response.json(
          {
            player_id: "22222222-2222-4222-8222-222222222222",
            access_token: "access-g",
            expires_at: new Date(Date.now() + 86_400_000).toISOString(),
            account: { id: ACCOUNT_ID, display_name: displayName },
          },
          { status: 201 },
        );
      case "POST /lobby/v1/player-tokens": {
        if (fail.playerTokens) {
          return Response.json(
            { status: 401, code: "unauthenticated" },
            { status: 401 },
          );
        }
        const token = `token-${++tokens}`;
        tokenNames.set(token, displayName);
        return Response.json({
          token,
          expires_at: new Date(Date.now() + 900_000).toISOString(),
        });
      }
      case "PUT /lobby/v1/account": {
        const body = (await request.json()) as { display_name: string };
        if (body.display_name === "Taken") {
          return Response.json(
            { status: 409, code: "display_name_taken" },
            { status: 409 },
          );
        }
        displayName = body.display_name;
        return Response.json({ id: ACCOUNT_ID, display_name: displayName });
      }
      case "DELETE /lobby/v1/account":
        if (fail.lobbyDelete) {
          return Response.json(
            { status: 503, code: "unavailable", detail: "try again later" },
            { status: 503 },
          );
        }
        return new Response(null, { status: 204 });
    }
    // The game's routes: the display name is the token's.
    const name = tokenNames.get(authorization.replace("Bearer ", ""));
    if (name === undefined) {
      return Response.json({ status: 401 }, { status: 401 });
    }
    if (route === "GET /account") {
      return Response.json({
        account_id: ACCOUNT_ID,
        display_name: name,
        max_bot_names: 5,
        hand: { ...competitor(name, "hand"), kind: "manual" },
        bot_names: bots,
      });
    }
    if (route === "POST /account/bot-names") {
      const body = (await request.json()) as { name: string };
      if (bots.length >= 5) {
        return Response.json(
          {
            status: 409,
            type: "urn:island-battles:problem:bot-name-limit",
            detail:
              "an account has at most 5 bot names: delete one to register another",
          },
          { status: 409 },
        );
      }
      if (
        bots.some((bot) => bot.name.toLowerCase() === body.name.toLowerCase())
      ) {
        return Response.json(
          {
            status: 409,
            type: "urn:island-battles:problem:bot-name-taken",
            detail: `the bot name "${body.name}" is taken`,
          },
          { status: 409 },
        );
      }
      const added = competitor(body.name);
      bots = [...bots, added];
      return Response.json(added, { status: 201 });
    }
    if (request.method === "DELETE" && path.startsWith("/account/bot-names/")) {
      const id = path.split("/").at(-1);
      bots = bots.filter((bot) => bot.id !== id);
      return new Response(null, { status: 204 });
    }
    if (route === "DELETE /account") {
      bots = [];
      return new Response(null, { status: 204 });
    }
    return Response.json({ status: 404 }, { status: 404 });
  }) as typeof fetch;
  const realtime = createRealtime({
    baseUrl: "http://api.test",
    fetch: fetchStub,
    storage: new FakeStorage(),
  });
  await realtime.signInWithGoogle("id-token");
  const service = createAccountService(realtime, {
    baseUrl: "http://api.test",
    fetch: fetchStub,
  });
  calls.length = 0;
  return { realtime, service, calls, fail };
}

const rejection = async (promise: Promise<unknown>) => {
  try {
    await promise;
  } catch (error) {
    return error as { status: number; problemType?: string; code?: string };
  }
  throw new Error("expected a rejection");
};

describe("the account service", () => {
  test("reads the game's account with a player token", async () => {
    const { service, calls } = await signedIn();
    const account = await service.read();
    expect(account.display_name).toBe("Dana");
    expect(calls).toEqual(["POST /lobby/v1/player-tokens", "GET /account"]);
  });

  test("adds bot names up to 5, refuses a sixth and a taken one, and deletes one", async () => {
    const { service } = await signedIn();
    for (const name of ["Kraken", "Squid", "Narwhal", "Orca", "Eel"]) {
      expect((await service.addBotName(name)).name).toBe(name);
    }
    expect(await rejection(service.addBotName("Sixth"))).toMatchObject({
      status: 409,
      problemType: "urn:island-battles:problem:bot-name-limit",
    });
    await service.deleteBotName("id-Squid");
    expect(await rejection(service.addBotName("kraken"))).toMatchObject({
      status: 409,
      problemType: "urn:island-battles:problem:bot-name-taken",
    });
    expect((await service.read()).bot_names.map((bot) => bot.name)).toEqual([
      "Kraken",
      "Narwhal",
      "Orca",
      "Eel",
    ]);
  });

  test("a rename goes to the lobby, then the game learns it with a new player token", async () => {
    const { realtime, service, calls } = await signedIn();
    await service.read();
    calls.length = 0;
    const account = await service.rename("Dana B");
    expect(calls).toEqual([
      "PUT /lobby/v1/account",
      "POST /lobby/v1/player-tokens",
      "GET /account",
    ]);
    expect(account.display_name).toBe("Dana B");
    expect(realtime.session.get()?.displayName).toBe("Dana B");
    expect(realtime.session.get()?.account?.displayName).toBe("Dana B");
  });

  test("a taken display name changes nothing", async () => {
    const { realtime, service, calls } = await signedIn();
    expect(await rejection(service.rename("Taken"))).toMatchObject({
      status: 409,
      code: "display_name_taken",
    });
    expect(calls).toEqual(["PUT /lobby/v1/account"]);
    expect(realtime.session.get()?.displayName).toBe("Dana");
  });

  test("deleting the account deletes the game's part, then the lobby's, then signs out", async () => {
    const { realtime, service, calls } = await signedIn();
    const order: string[] = [];
    await service.deleteAccount({
      gameDeleted: () => order.push(...calls, "game deleted"),
      signOut: () => {
        order.push(calls.at(-1) ?? "", "sign out");
        realtime.signOut();
      },
    });
    expect(order).toEqual([
      "POST /lobby/v1/player-tokens",
      "DELETE /account",
      "game deleted",
      "DELETE /lobby/v1/account",
      "sign out",
    ]);
    expect(realtime.session.get()).toBeNull();
  });

  test("when the lobby's part fails after the game's, it says so, and deleting again finishes it", async () => {
    const { realtime, service, calls, fail } = await signedIn();
    await service.addBotName("Kraken");
    fail.lobbyDelete = true;
    const steps: string[] = [];
    const deleteSteps = {
      gameDeleted: () => steps.push("game deleted"),
      signOut: () => {
        steps.push("sign out");
        realtime.signOut();
      },
    };
    const error = await rejection(service.deleteAccount(deleteSteps));
    expect(error.status).toBe(503);
    // The game's part is gone already, so the page shows no bot names and says what is left.
    expect(steps).toEqual(["game deleted"]);
    expect(realtime.session.get()).not.toBeNull();
    expect(partlyDeletedProblem(error)).toBe(
      "Your bot names and every rating are deleted, but your account isn't yet. Try again later. Press Delete account again to finish.",
    );
    fail.lobbyDelete = false;
    calls.length = 0;
    await service.deleteAccount(deleteSteps);
    expect(calls).toEqual(["DELETE /account", "DELETE /lobby/v1/account"]);
    expect(steps).toEqual(["game deleted", "game deleted", "sign out"]);
  });

  test("when the game's part can't be deleted, the lobby's isn't either", async () => {
    const { realtime, service, calls } = await signedIn();
    // Without a session the game refuses the call, so nothing else is sent.
    realtime.signOut();
    const steps: string[] = [];
    await rejection(
      service.deleteAccount({
        gameDeleted: () => steps.push("game deleted"),
        signOut: () => steps.push("sign out"),
      }),
    );
    expect(calls).not.toContain("DELETE /lobby/v1/account");
    expect(steps).toEqual([]);
  });

  test("a session the lobby refuses ends, with its problem, so the silent sign-in can follow", async () => {
    const { realtime, service, fail } = await signedIn();
    fail.playerTokens = true;
    const error = await rejection(service.read());
    expect(error.status).toBe(401);
    expect(realtime.session.get()).toBeNull();
    expect(realtime.problem.get()?.scope).toBe("session");
  });

  test("a refused bot name or delete ends a refused session too", async () => {
    const { realtime, service, fail } = await signedIn();
    fail.playerTokens = true;
    await rejection(service.addBotName("Kraken"));
    expect(realtime.session.get()).toBeNull();
  });
});
