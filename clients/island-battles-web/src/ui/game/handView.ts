// What the game view shows a player who plays by hand (#345): no DOM and no Babylon, so every
// rule is tested. `screen.ts` draws what `handView` decides.
//
// Everything here keys on hand play's `status`, never on `turn` or `castles` alone: a player whose
// bot plays has a hand play whose status is `idle` with its turn and castles filled in.

import type { PlayState, PlayTurn } from "../../play";
import {
  type LiveTiming,
  PLAYING,
  WATCHING,
} from "../../scene/animation/player";
import type { ViewState } from "./controller";

export const RETURN_TO_LIVE_TEXT = "Return to live to give orders";
export const TEAM_OUT_TEXT =
  "Your team is out. You are watching the rest of the game.";

/** What the hand player's status row says, or null when it is hidden. */
export type HandStatusRow = "return-to-live" | "team-out" | null;

export interface HandView {
  /** A player who plays by hand: their team is marked and the Controls button shows. */
  readonly hand: boolean;
  /** The team to mark as yours, or null. */
  readonly yourTeam: number | null;
  /** The open turn, for the pace bar, while the player gives orders in a real-time game. */
  readonly pace: PlayTurn | null;
  /** The status row above the timeline. */
  readonly status: HandStatusRow;
  /** The speed select does nothing while a hand player follows live: hidden then. */
  readonly hideSpeed: boolean;
  /** How the board times live turns. */
  readonly timing: LiveTiming;
  /** The game's turn pace for the board's timing (`TurnPlayer.setLivePace`), or null. */
  readonly livePaceMs: number | null;
  /**
   * Returning to live jumps to the newest turn instead of replaying the turns missed, so orders
   * are given against the open generation at once.
   */
  readonly jumpToLive: boolean;
}

/** A watcher's, a demo's and a bot player's view: nothing of hand play. */
export const NO_HAND: HandView = Object.freeze({
  hand: false,
  yourTeam: null,
  pace: null,
  status: null,
  hideSpeed: false,
  timing: WATCHING,
  livePaceMs: null,
  jumpToLive: false,
});

export function handView(play: PlayState | null, view: ViewState): HandView {
  if (play === null || play.status === "idle") return NO_HAND;
  const playing = play.status === "playing";
  // A running live game that is loaded, shown on an older turn: the button can return to live.
  const behind =
    view.live &&
    view.outcome === null &&
    view.index >= 0 &&
    view.index < view.frames.length - 1;
  const realTime =
    play.turn !== null &&
    play.turn.paceMs !== null &&
    play.turn.endsAt !== null &&
    play.turn.startsAt !== null;
  let status: HandStatusRow = null;
  if (playing && behind) status = "return-to-live";
  else if (play.status === "eliminated") status = "team-out";
  return {
    hand: true,
    yourTeam: play.teamColor,
    pace: playing && realTime ? play.turn : null,
    status,
    hideSpeed: playing && view.follow,
    timing: playing ? PLAYING : WATCHING,
    livePaceMs: playing ? (play.turn?.paceMs ?? null) : null,
    jumpToLive: playing,
  };
}

/** The status row's sentence. */
export function statusText(status: Exclude<HandStatusRow, null>): string {
  return status === "return-to-live" ? RETURN_TO_LIVE_TEXT : TEAM_OUT_TEXT;
}

/**
 * Where the pace bar is at `nowMs` (epoch ms, as `turn.startsAt` and `turn.endsAt`), from 0 to 1,
 * and how long until it is full. A turn whose time is over is full at once.
 */
export function paceSegment(
  turn: PlayTurn,
  nowMs: number,
): { from: number; durationMs: number } | null {
  const { paceMs, startsAt, endsAt } = turn;
  if (paceMs === null || startsAt === null || endsAt === null || paceMs <= 0) {
    return null;
  }
  const from = Math.min(1, Math.max(0, (nowMs - startsAt) / paceMs));
  return { from, durationMs: Math.max(0, endsAt - nowMs) };
}

/**
 * Whether a tab that comes back to the front should jump to the newest turn: a hand player
 * following live with turns piled up while no frame ran.
 */
export function jumpsOnReturn(
  view: HandView,
  following: boolean,
  waiting: number,
): boolean {
  return view.jumpToLive && following && waiting >= 2;
}
