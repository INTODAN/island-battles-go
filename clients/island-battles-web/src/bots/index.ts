import { createApiClients } from "../api/client";
import type { Realtime } from "../realtime";
import { deliverTurn } from "./localhost";
import { type BotRelay, createBotRelay } from "./relay";

export {
  BotError,
  checkBotHealth,
  deliverTurn,
  localhostBotOrigin,
  serveCommand,
  type TurnDelivery,
} from "./localhost";
export {
  type BotRelay,
  createBotRelay,
  type RelayDeps,
  type RelayStatus,
} from "./relay";

/**
 * Starts the relay for the signed-in player's game in `lobbyId`, with the
 * browser's `fetch` and the realtime session's token. The game store must be
 * followed (`realtime.followGame`) for the relay to see turns.
 */
export function startBotRelay(options: {
  realtime: Realtime;
  lobbyId: string;
  fetch?: typeof fetch;
}): BotRelay {
  const { realtime, lobbyId } = options;
  const { gameApi } = createApiClients({
    getAccessToken: () => realtime.session.get()?.accessToken,
    ...(options.fetch === undefined ? {} : { fetch: options.fetch }),
  });
  return createBotRelay({
    game: realtime.game,
    async loadDelivery() {
      const { data } = await gameApi.GET(
        "/game/from-lobby/{lobby_id}/turn-delivery",
        { params: { path: { lobby_id: lobbyId } } },
      );
      if (!data) throw new Error("The server sent no turn delivery.");
      return data;
    },
    deliver: (delivery) => deliverTurn(delivery),
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
