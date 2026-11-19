// Hand play for the app shell: `startHandPlay` gives `createHandPlay` the game API, the realtime
// store, the clock and the hidden-tab timers.

import { createApiClients } from "../api/client";
import { ApiError } from "../api/errors";
import type { Realtime } from "../realtime";
import { createHandPlay, type HandPlay } from "./handPlay";
import { type Schedule, workerSchedule } from "./ticker";

export {
  type BurstRow,
  burstAmount,
  type CastleMenuModel,
  castleMenuModel,
  doubleClickFocuses,
  isRightClick,
  type MenuGate,
  MenuGestures,
  type MenuOpener,
  type MenuPlay,
  type MenuPointer,
  menuAllowed,
  menuReason,
  menuUpgradeOrder,
  placeMenu,
  type UpgradeRow,
} from "./castleMenu";
export {
  type CheckResult,
  createHandPlay,
  type HandPlay,
  type HandPlayDeps,
  type OrderResult,
  type PlayOrder,
  type PlayState,
  type PlayStatus,
  type PlayTurn,
} from "./handPlay";
export {
  type Amount,
  type CastleLedger,
  type Ledger,
  type OrderSpec,
  playsByHand,
  type Refusal,
  type ResolvedOrder,
  refusalSentence,
  resolveAmount,
  teamOut,
  type UpgradeTable,
  type UpgradeType,
  upgradeCost,
  upgradeTypes,
  upgradeValue,
} from "./rules";
export { type Schedule, workerSchedule } from "./ticker";

function required<T>(data: T | undefined): T {
  if (data === undefined) {
    throw new ApiError({ kind: "http", status: 0, detail: "empty response" });
  }
  return data;
}

/** A v4 UUID. `crypto.randomUUID` exists only on secure origins, so a LAN dev URL gets one built here. */
export function newRequestId(): string {
  if (typeof crypto.randomUUID === "function") return crypto.randomUUID();
  const bytes = crypto.getRandomValues(new Uint8Array(16));
  bytes[6] = ((bytes[6] ?? 0) & 0x0f) | 0x40;
  bytes[8] = ((bytes[8] ?? 0) & 0x3f) | 0x80;
  const hex = [...bytes].map((byte) => byte.toString(16).padStart(2, "0"));
  return `${hex.slice(0, 4).join("")}-${hex.slice(4, 6).join("")}-${hex.slice(6, 8).join("")}-${hex.slice(8, 10).join("")}-${hex.slice(10).join("")}`;
}

/** One schedule for the page, so every hand play shares one timer Worker. */
let pageSchedule: Schedule | undefined;

/**
 * Starts hand play for the signed-in player's game in `lobbyId`, with the browser's `fetch`, the
 * realtime session's token and timers in a Worker. The game must be followed
 * (`realtime.followGame`). It stays idle unless the player plays by hand.
 */
export function startHandPlay(options: {
  realtime: Realtime;
  lobbyId: string;
  /** Replaces `fetch`; tests use it. */
  fetch?: typeof fetch;
  /** One server for the game and the lobby, such as a test's. */
  baseUrl?: string;
  /** Replaces the Worker's timers; tests use a fake clock. */
  schedule?: Schedule;
  /** Replaces `Date.now`; tests use a fake clock. */
  now?: () => number;
}): HandPlay {
  const { realtime, lobbyId } = options;
  const { gameApi } = createApiClients({
    getAccessToken: () => realtime.session.get()?.accessToken,
    ...(options.fetch === undefined ? {} : { fetch: options.fetch }),
    ...(options.baseUrl === undefined ? {} : { baseUrl: options.baseUrl }),
  });
  const path = { params: { path: { lobby_id: lobbyId } } };
  pageSchedule ??= workerSchedule();
  const schedule = options.schedule ?? pageSchedule;
  return createHandPlay({
    game: realtime.game,
    async postCommands(body, signal) {
      const { data } = await gameApi.POST("/generation/commands", {
        body,
        signal,
      });
      return required(data);
    },
    async applyDeadline(signal) {
      const { data } = await gameApi.POST(
        "/game/from-lobby/{lobby_id}/deadline",
        { ...path, signal },
      );
      return required(data);
    },
    async readState(signal) {
      const { data } = await gameApi.GET(
        "/game/from-lobby/{lobby_id}/generation",
        { ...path, signal },
      );
      return required(data);
    },
    async loadUpgrades(signal) {
      const { data } = await gameApi.GET("/game/upgrades", { signal });
      return required(data).upgrades;
    },
    adopt: (state) => realtime.adoptGame(state),
    refresh: () => realtime.refreshGame(),
    now: options.now ?? (() => Date.now()),
    schedule,
    newRequestId,
  });
}
