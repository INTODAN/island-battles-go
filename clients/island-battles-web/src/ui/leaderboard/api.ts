import type { createApiClients } from "../../api/client";
import { ApiError } from "../../api/errors";
import { type LeaderboardApi, ownCompetitorIds } from "./model";

type GameApi = ReturnType<typeof createApiClients>["gameApi"];

function answer<T>(data: T | undefined): T {
  if (data === undefined) {
    throw new ApiError({ kind: "http", status: 0, detail: "empty response" });
  }
  return data;
}

/** The leaderboard's routes on the game's client. They are public: no session, no token. */
export function leaderboardApi(gameApi: GameApi): LeaderboardApi {
  return {
    async list(query) {
      const { data } = await gameApi.GET("/leaderboard", {
        params: { query },
      });
      return answer(data);
    },
    async competitor(competitorId) {
      const { data } = await gameApi.GET("/leaderboard/{competitor_id}", {
        params: { path: { competitor_id: competitorId } },
      });
      return answer(data);
    },
  };
}

/**
 * The signed-in person's own competitors, from the game's `GET /account`, to
 * highlight on the leaderboard. A guest's is refused (403), and any failure
 * leaves nothing highlighted: the leaderboard shows without it.
 */
export async function ownCompetitors(
  gameApi: GameApi,
): Promise<ReadonlySet<string>> {
  try {
    const { data } = await gameApi.GET("/account");
    return data ? ownCompetitorIds(data) : new Set();
  } catch {
    return new Set();
  }
}
