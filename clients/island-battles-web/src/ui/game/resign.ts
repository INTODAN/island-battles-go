// The Resign button of a hand player (#348): when it shows, what its confirmation says and does
// with the keyboard, and the request that resigns the team. Pure (no DOM, no realtime), so
// `bun test` covers it; `screen.ts` only wires it.

import { ApiError } from "../../api/errors";
import type { components } from "../../api/generated/game";
import type { PlayStatus } from "../../play";
import { isTransientError } from "../../play/rules";

type GenerationState = components["schemas"]["GenerationState"];

/** What the in-page confirmation asks. */
export const resignQuestion = "Resign? Your team loses this game.";

const problemPrefix = "urn:island-battles:problem:";

export interface ResignControlInput {
  /**
   * The hand play's status. `playing` is exactly "a `manual` player in a real-time game, whose
   * team is in it, with no outcome"; a watcher, a demo and a player with a bot never have it.
   */
  status: PlayStatus;
  /** The resign request is running. */
  sending: boolean;
  /** The request succeeded; the turn that takes the team out hasn't played yet. */
  resigned: boolean;
}

export type ResignControl =
  /** No button and no text: not a hand player's game, or the team is out or the game over. */
  | { kind: "hidden" }
  | { kind: "button"; label: string; disabled: boolean }
  /** The resignation is recorded and waits for the open turn to play. */
  | { kind: "resigned"; text: string };

/** What the top bar shows in place of the Resign button. */
export function resignControl(input: ResignControlInput): ResignControl {
  if (input.status !== "playing") return { kind: "hidden" };
  if (input.resigned) return { kind: "resigned", text: "You resigned" };
  return input.sending
    ? { kind: "button", label: "Resigning…", disabled: true }
    : { kind: "button", label: "Resign", disabled: false };
}

/**
 * What a key does while the confirmation is open: only `Esc` does anything, and it keeps playing.
 * Another feature may have used the key already (`defaultPrevented`); then this does nothing.
 * Not open: nothing, so `Esc` stays free for the rest of the page.
 */
export function confirmationKey(
  open: boolean,
  event: { key: string; defaultPrevented: boolean },
): "keep-playing" | null {
  if (!open || event.defaultPrevented) return null;
  return event.key === "Escape" ? "keep-playing" : null;
}

export type ResignResult =
  /** The server recorded the resignation, or played it already: its answer. */
  | { kind: "resigned"; state: GenerationState }
  /** The game has ended or the team is out already (`game-ended`, `team-out`): nothing to do. */
  | { kind: "already" }
  /** The server refused; `sentence` is for the player. */
  | { kind: "refused"; sentence: string }
  /** No answer after the attempts. */
  | { kind: "unreachable"; sentence: string };

export interface ResignDeps {
  /** One `POST /game/from-lobby/{lobby_id}/resign`; rejects with `ApiError`. */
  call(): Promise<GenerationState>;
  sleep(milliseconds: number): Promise<void>;
}

/** The waits before each new attempt: a transport failure is tried again twice. */
export const resignRetryDelaysMs = [1000, 2000];

/** True for the two 409s that mean the team's resignation needs no more calls. */
export function isAlreadyOut(error: unknown): boolean {
  return (
    error instanceof ApiError &&
    error.status === 409 &&
    (error.problemType === `${problemPrefix}game-ended` ||
      error.problemType === `${problemPrefix}team-out`)
  );
}

/** The player's words for a refusal: the server's detail when it has one. */
export function resignRefusalSentence(error: unknown): string {
  if (error instanceof ApiError) {
    if (error.problemType === `${problemPrefix}not-real-time`) {
      return "This game doesn't take resignations: only a game with a player who plays by hand does.";
    }
    if (error.status === 401) return "Your session has ended. Sign in again.";
    return error.detail ?? error.message;
  }
  return "The resignation could not be sent.";
}

/**
 * Resigns the team. A repeat is safe on the server (a pending resignation answers 200 again), so
 * a failure with no answer, a server fault or throttling is tried again, at most twice. A refusal
 * is not: `game-ended` and `team-out` mean the team is out already, any other is shown.
 */
export async function requestResign(deps: ResignDeps): Promise<ResignResult> {
  for (let attempt = 0; ; attempt++) {
    try {
      return { kind: "resigned", state: await deps.call() };
    } catch (error) {
      if (isAlreadyOut(error)) return { kind: "already" };
      const delay = resignRetryDelaysMs[attempt];
      if (isTransientError(error) && delay !== undefined) {
        await deps.sleep(delay);
        continue;
      }
      if (isTransientError(error)) {
        return {
          kind: "unreachable",
          sentence:
            "The resignation could not be sent: the game didn't answer. Try again.",
        };
      }
      return { kind: "refused", sentence: resignRefusalSentence(error) };
    }
  }
}
