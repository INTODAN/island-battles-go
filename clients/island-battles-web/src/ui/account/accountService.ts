// The account page's requests (#369): the game's account and bot names, with a player token, and
// the lobby's account through the realtime session, which keeps the session's name in step.

import { type ApiOptions, createApiClients } from "../../api/client";
import { ApiError } from "../../api/errors";
import type { Realtime } from "../../realtime";
import type { Competitor, IslandAccount } from "./accountModel";

export interface AccountService {
  /** The game's `GET /account`: the hand play and the bot names. */
  read(): Promise<IslandAccount>;
  /** `POST /account/bot-names`. */
  addBotName(name: string): Promise<Competitor>;
  /** `DELETE /account/bot-names/{competitor_id}`, with its rating. */
  deleteBotName(competitorId: string): Promise<void>;
  /**
   * The lobby's `PUT /lobby/v1/account`, then the game's `GET /account` with a new player token,
   * which carries the new name, so the game learns it at once.
   */
  rename(displayName: string): Promise<IslandAccount>;
  /**
   * Deletes the account: the game's part first (`DELETE /account`), then the lobby's
   * (`DELETE /lobby/v1/account`), then `signOut`. A failure stops there, and the account page
   * says so; deleting again is safe, since the game's part answers 204 when it is gone.
   */
  deleteAccount(signOut: () => void): Promise<void>;
}

function required<T>(data: T | undefined): T {
  if (data === undefined) {
    throw new ApiError({ kind: "http", status: 0, detail: "empty response" });
  }
  return data;
}

export function createAccountService(
  realtime: Realtime,
  options: Pick<ApiOptions, "baseUrl" | "gameUrl" | "lobbyUrl" | "fetch"> = {},
): AccountService {
  const { gameApi, playerTokens } = createApiClients({
    ...options,
    getAccessToken: () => realtime.session.get()?.accessToken,
  });
  const read = async () => required((await gameApi.GET("/account")).data);
  return {
    read,
    async addBotName(name) {
      const { data } = await gameApi.POST("/account/bot-names", {
        body: { name },
      });
      return required(data);
    },
    async deleteBotName(competitorId) {
      await gameApi.DELETE("/account/bot-names/{competitor_id}", {
        params: { path: { competitor_id: competitorId } },
      });
    },
    async rename(displayName) {
      await realtime.renameAccount(displayName);
      playerTokens.forget();
      return read();
    },
    async deleteAccount(signOut) {
      await gameApi.DELETE("/account");
      await realtime.deleteAccount();
      signOut();
    },
  };
}
