import { createApiClients } from "../api/client";
import type { Realtime } from "../realtime";
import { playTurn } from "./localhost";
import { type BotRelay, createBotRelay } from "./relay";

export {
  BotError,
  type BotTurn,
  type BotTurnResult,
  buildBotTurn,
  checkBotHealth,
  localhostBotOrigin,
  playTurn,
  serveCommand,
} from "./localhost";
export {
  type BotRelay,
  createBotRelay,
  type RelayDeps,
  type RelayStatus,
  type TurnSubmission,
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
    async loadUpgrades() {
      const { data } = await gameApi.GET("/game/upgrades");
      return data?.upgrades ?? [];
    },
    playBot: (turn) => playTurn(turn),
    async submit(submission) {
      await gameApi.POST("/generation/commands", { body: submission });
    },
    async applyDeadline() {
      await gameApi.POST("/game/from-lobby/{lobby_id}/deadline", {
        params: { path: { lobby_id: lobbyId } },
      });
    },
    now: () => Date.now(),
    newRequestId: () => crypto.randomUUID(),
    schedule(run, ms) {
      const timer = setTimeout(run, ms);
      return () => clearTimeout(timer);
    },
  });
}
