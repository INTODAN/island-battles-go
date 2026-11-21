// The account page's requests (#369): the game's account and bot names, with a player token, and
// the lobby's account through the realtime session, which keeps the session's name in step.

import { type ApiOptions, createApiClients } from "../../api/client";
import { ApiError } from "../../api/errors";
import type { Realtime } from "../../realtime";
import type { Competitor, IslandAccount } from "./accountModel";

export interface AccountService {
  /** The game's `GET /account`: the hand play and the bot names. */
  read(): Promise<IslandAccount>;
  /** `POST /account/bot-names`, with the URL of the hosted bot that plays the name. */
  addBotName(name: string, url: string): Promise<Competitor>;
  /**
   * `PUT /account/bot-names/{competitor_id}`: the name's new hosted bot URL. It keeps its rating;
   * a game that started before plays the URL it started with.
   */
  setBotNameUrl(competitorId: string, url: string): Promise<Competitor>;
  /** `DELETE /account/bot-names/{competitor_id}`, with its rating. */
  deleteBotName(competitorId: string): Promise<void>;
  /**
   * `DELETE /account/bot-names/{competitor_id}/away-pause`: resumes a bot name paused after it
   * missed a turn while it played away, so it plays away in rated games again (#412).
   */
  resumeBotName(competitorId: string): Promise<Competitor>;
  /**
   * The lobby's `PUT /lobby/v1/account`, then the game's `GET /account` with a new player token,
   * which carries the new name, so the game learns it at once.
   */
  rename(displayName: string): Promise<IslandAccount>;
  /**
   * Deletes the account: the game's part first (`DELETE /account`), then `steps.gameDeleted()`,
   * then the lobby's (`DELETE /lobby/v1/account`), then `steps.signOut()`. A failure stops there,
   * and the account page says so; deleting again is safe, since the game's part answers 204 when
   * it is gone.
   */
  deleteAccount(steps: DeleteAccountSteps): Promise<void>;
}

export interface DeleteAccountSteps {
  /**
   * The game's part is gone: the bot names and every rating. Runs before the lobby's call, so the
   * page can turn Google's silent sign-in off before a refusal there could start one.
   */
  gameDeleted(): void;
  /** The account is gone in the lobby too. */
  signOut(): void;
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
  /**
   * Runs a call of these clients. Their player tokens come from the lobby with the session's
   * credential, so a 401 means the lobby refused the session: it ends, as a refusal of the
   * session's own calls ends it, and the silent sign-in or the name screen follows.
   */
  async function call<T>(run: () => Promise<T>): Promise<T> {
    try {
      return await run();
    } catch (error) {
      if (error instanceof ApiError && error.status === 401) {
        realtime.sessionRefused(error);
      }
      throw error;
    }
  }
  const read = () =>
    call(async () => required((await gameApi.GET("/account")).data));
  return {
    read,
    addBotName: (name, url) =>
      call(async () => {
        const { data } = await gameApi.POST("/account/bot-names", {
          body: { name, url },
        });
        return required(data);
      }),
    setBotNameUrl: (competitorId, url) =>
      call(async () => {
        const { data } = await gameApi.PUT(
          "/account/bot-names/{competitor_id}",
          {
            params: { path: { competitor_id: competitorId } },
            body: { url },
          },
        );
        return required(data);
      }),
    deleteBotName: (competitorId) =>
      call(async () => {
        await gameApi.DELETE("/account/bot-names/{competitor_id}", {
          params: { path: { competitor_id: competitorId } },
        });
      }),
    resumeBotName: (competitorId) =>
      call(async () => {
        const { data } = await gameApi.DELETE(
          "/account/bot-names/{competitor_id}/away-pause",
          { params: { path: { competitor_id: competitorId } } },
        );
        return required(data);
      }),
    async rename(displayName) {
      await realtime.renameAccount(displayName);
      playerTokens.forget();
      return read();
    },
    async deleteAccount(steps) {
      await call(() => gameApi.DELETE("/account"));
      steps.gameDeleted();
      await realtime.deleteAccount();
      steps.signOut();
    },
  };
}
