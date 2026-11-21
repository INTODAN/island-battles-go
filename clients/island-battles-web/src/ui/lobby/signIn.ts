// What the name screen does when signing in with Google fails (#369). Pure, so `bun test` covers
// it; `nameScreen.ts` only follows it.

import { ApiError } from "../../api/errors";
import { displayNameProblem } from "../account/accountModel";

export type GoogleSignInStep =
  /** A first sign-in: ask for the display name, then post the same credential with it. */
  | { kind: "ask-name" }
  /** Stay on the display name step, with this sentence under the field. */
  | { kind: "name-problem"; message: string }
  /** Back to the name screen and Google's button, with this sentence. */
  | { kind: "button-problem"; message: string };

const unreachable =
  "Cannot reach the server. Check your connection and try again.";

/**
 * The next step after the lobby refused or failed `POST /lobby/v1/sessions/google`. `onNameStep`
 * is true when the request carried the display name the person typed.
 */
export function googleSignInStep(
  error: unknown,
  onNameStep: boolean,
): GoogleSignInStep {
  if (error instanceof ApiError) {
    if (error.code === "display_name_required") {
      return onNameStep
        ? { kind: "name-problem", message: displayNameProblem(error) }
        : { kind: "ask-name" };
    }
    if (error.status === 401) {
      // Google's token was refused, or expired while the person chose a name.
      return {
        kind: "button-problem",
        message: onNameStep
          ? "Google's sign-in expired. Sign in with Google again."
          : "Google's sign-in couldn't be checked. Try again.",
      };
    }
    if (error.status === 404) {
      return {
        kind: "button-problem",
        message: "Sign-in with Google is off for now. Play as a guest.",
      };
    }
    if (error.kind === "transport") {
      return onNameStep
        ? { kind: "name-problem", message: unreachable }
        : { kind: "button-problem", message: unreachable };
    }
    if (
      onNameStep &&
      (error.code === "display_name_taken" || error.code === "invalid_input")
    ) {
      return { kind: "name-problem", message: displayNameProblem(error) };
    }
  }
  const message =
    error instanceof ApiError && error.detail
      ? displayNameProblem(error)
      : "Could not sign in. Please try again.";
  return onNameStep
    ? { kind: "name-problem", message }
    : { kind: "button-problem", message };
}

/**
 * Whether the lobby refused Google's credential of a silent sign-in for good (#409): the token
 * was refused, sign-in is off, or the account is gone. The browser then stops signing in by
 * itself. A failure to reach the lobby, or its own, keeps it for the next tab.
 */
export function refusedGoogleSignIn(error: unknown): boolean {
  return (
    error instanceof ApiError &&
    (error.status === 401 ||
      error.status === 404 ||
      error.code === "display_name_required")
  );
}

/** The browser remembers, in `localStorage`, that its person signed in with Google (#409). */
export const googleSignInKey = "island-battles-web.google-sign-in";
