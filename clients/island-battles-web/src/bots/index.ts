import { createApiClients } from "../api/client";
import type { Realtime } from "../realtime";
import { createStore, type ReadableStore } from "../realtime/store";
import {
  defaultBotPort,
  deliverTurn,
  type GenerationState,
  type RelayedBot,
} from "./localhost";
import { type BotRelay, createBotRelay, type RelayDeps } from "./relay";

export {
  BotError,
  checkBotHealth,
  defaultBotPort,
  deliverTurn,
  localhostBotOrigin,
  nextFreePort,
  parseBotPort,
  portProblem,
  type RelayedBot,
  serveCommand,
  serveCommandFor,
  type TurnDelivery,
} from "./localhost";
export {
  type BotRelay,
  createBotRelay,
  type RelayDeps,
  type RelayStatus,
} from "./relay";

/** A localhost bot seat that this tab relays, and its relay. */
export interface SeatRelay {
  bot: RelayedBot;
  relay: BotRelay;
}

/** The relays of the tab: the player's own bot's, and one per localhost bot seat they relay. */
export interface BotRelays {
  own: BotRelay;
  /** The seats' relays, in team order; they follow `GenerationState.relayed_bots`. */
  seats: ReadableStore<SeatRelay[]>;
  dispose(): void;
}

/** The game API calls that a relay makes, for the player or for a seat they relay. */
function relayCalls(options: {
  realtime: Realtime;
  lobbyId: string;
  fetch?: typeof fetch;
}): (
  seatId?: string,
) => Pick<RelayDeps, "loadDelivery" | "applyDeadline" | "now" | "schedule"> {
  const { realtime, lobbyId } = options;
  const { gameApi } = createApiClients({
    getAccessToken: () => realtime.session.get()?.accessToken,
    ...(options.fetch === undefined ? {} : { fetch: options.fetch }),
  });
  return (seatId) => ({
    async loadDelivery() {
      const { data } = await gameApi.GET(
        "/game/from-lobby/{lobby_id}/turn-delivery",
        {
          params: {
            path: { lobby_id: lobbyId },
            ...(seatId === undefined ? {} : { query: { player_id: seatId } }),
          },
        },
      );
      if (!data) throw new Error("The server sent no turn delivery.");
      return data;
    },
    async applyDeadline() {
      await gameApi.POST("/game/from-lobby/{lobby_id}/deadline", {
        params: { path: { lobby_id: lobbyId } },
      });
    },
    now: () => Date.now(),
    schedule(run, ms) {
      const timer = setTimeout(run, ms);
      return () => clearTimeout(timer);
    },
  });
}

/**
 * Starts the relay for the signed-in player's game in `lobbyId`, with the
 * browser's `fetch` and the realtime session's token. The game store must be
 * followed (`realtime.followGame`) for the relay to see turns. It delivers to
 * the port of the player's localhost bot, `GenerationState.bot.port`.
 */
export function startBotRelay(options: {
  realtime: Realtime;
  lobbyId: string;
  fetch?: typeof fetch;
}): BotRelay {
  const calls = relayCalls(options);
  const game = options.realtime.game;
  return createBotRelay({
    game,
    ...calls(),
    deliver: (delivery) =>
      deliverTurn(delivery, { port: game.get()?.bot.port ?? defaultBotPort }),
  });
}

/**
 * Starts the player's own relay and one relay per localhost bot seat that they added as the
 * host, `GenerationState.relayed_bots`: each reads its seat's turn with
 * `player_id` and delivers it to the seat's own port. Only the player's own relay asks for
 * turn deadlines.
 */
export function startBotRelays(options: {
  realtime: Realtime;
  lobbyId: string;
  fetch?: typeof fetch;
}): BotRelays {
  const own = startBotRelay(options);
  const calls = relayCalls(options);
  const game = options.realtime.game;
  const seats = createStore<SeatRelay[]>([]);
  const key = (bot: RelayedBot) => `${bot.player_id}:${bot.port}`;

  function follow(state: GenerationState | null): void {
    const wanted = state?.relayed_bots ?? [];
    const current = seats.get();
    if (
      wanted.length === current.length &&
      wanted.every((bot, index) => {
        const other = current[index];
        return other !== undefined && key(bot) === key(other.bot);
      })
    ) {
      return;
    }
    const kept = new Map(current.map((seat) => [key(seat.bot), seat]));
    const next = wanted.map((bot) => {
      const found = kept.get(key(bot));
      kept.delete(key(bot));
      return (
        found ?? {
          bot,
          relay: createBotRelay({
            game,
            ...calls(bot.player_id),
            deliver: (delivery) => deliverTurn(delivery, { port: bot.port }),
            seat: true,
          }),
        }
      );
    });
    for (const gone of kept.values()) gone.relay.dispose();
    seats.set(next);
  }
  follow(game.get());
  const unsubscribe = game.subscribe(follow);

  return {
    own,
    seats,
    dispose() {
      unsubscribe();
      own.dispose();
      for (const seat of seats.get()) seat.relay.dispose();
      seats.set([]);
    },
  };
}
