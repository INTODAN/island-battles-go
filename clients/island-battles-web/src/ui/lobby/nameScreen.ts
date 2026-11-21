import { nameRule, validateAccountName } from "../account/accountModel";
import { h } from "../dom";
import { leaderboardLink } from "../leaderboard/links";
import { maxNameLength, validateDisplayName } from "./form";
import type { GoogleSignIn } from "./googleSignIn";
import { googleSignInStep } from "./signIn";

export interface Screen {
  el: HTMLElement;
  dispose(): void;
}

export interface GoogleSignInOptions {
  /** Google's sign-in, once its script loaded; undefined when the lobby doesn't offer it. */
  ready: Promise<GoogleSignIn | undefined>;
  /**
   * Signs in with Google's ID token, and the display name of a first sign-in; rejects with the
   * lobby's `ApiError` (422 `display_name_required`, 409 `display_name_taken`).
   */
  submit(credential: string, displayName?: string): Promise<void>;
}

export interface NameScreenOptions {
  /** A join code from the page's link, shown so the person knows where they are going. */
  joinCode: string | undefined;
  /** A code from the page's `?watch=` link, for a person who will watch the game. */
  watchCode?: string | undefined;
  initialName: string;
  /** Signs in; rejects with the sentence to show. */
  onSubmit(name: string): Promise<void>;
  /** Shown when the person arrives here because the session ended. */
  notice: string;
  /**
   * Sign in with Google (#369). Without it, or while the lobby doesn't offer it, the screen is
   * the guest name step it always was.
   */
  google?: GoogleSignInOptions;
  /** Opens the leaderboard, from a line at the foot of the panel. */
  onLeaderboard?: () => void;
}

/** The lead sentence once Google's button shows above the name field. */
function signInLead(options: NameScreenOptions): string {
  if (options.joinCode) {
    return `You were invited to lobby ${options.joinCode}. Sign in, or choose a name to join it.`;
  }
  if (options.watchCode) {
    return `You were invited to watch game ${options.watchCode}. Sign in, or choose a name to watch it.`;
  }
  return "Command your castles across the sea.";
}

/** Screen 1: the player's name, or Sign in with Google. */
export function mountNameScreen(options: NameScreenOptions): Screen {
  let disposed = false;
  const input = h("input", {
    id: "player-name",
    type: "text",
    maxLength: maxNameLength,
    value: options.initialName,
    autocomplete: "name",
    placeholder: "Captain Nemo",
  });
  const error = h("p", { class: "field-error", attrs: { role: "alert" } });
  error.textContent = options.notice;
  const button = h(
    "button",
    { type: "submit", class: "primary" },
    options.joinCode
      ? `Continue and join ${options.joinCode}`
      : options.watchCode
        ? `Continue and watch ${options.watchCode}`
        : "Continue",
  );
  const lead = h(
    "p",
    { class: "lead" },
    options.joinCode
      ? `You were invited to lobby ${options.joinCode}. Choose a name to join it.`
      : options.watchCode
        ? `You were invited to watch game ${options.watchCode}. Choose a name to watch it.`
        : "Command your castles across the sea. Choose a name to begin.",
  );
  const label = h("label", { htmlFor: "player-name" }, "Your name");
  const form = h(
    "form",
    { class: "panel narrow", noValidate: true },
    h("h1", {}, "Island Battles"),
    lead,
    label,
    input,
    error,
    h("div", { class: "actions" }, button),
    options.onLeaderboard
      ? h(
          "p",
          { class: "panel-footer" },
          "Who's on top? ",
          leaderboardLink("See the leaderboard", options.onLeaderboard),
        )
      : null,
  );
  form.addEventListener("submit", async (event) => {
    event.preventDefault();
    const problem = validateDisplayName(input.value);
    if (problem) {
      error.textContent = problem;
      input.focus();
      return;
    }
    error.textContent = "";
    button.disabled = true;
    try {
      await options.onSubmit(input.value.trim());
    } catch (cause) {
      error.textContent =
        cause instanceof Error ? cause.message : "Could not sign in.";
      button.disabled = false;
    }
  });
  queueMicrotask(() => input.focus());

  /** Option B of the mockups: Google's button, what signing in gives, then the guest's name. */
  function addGoogle(signIn: GoogleSignIn, google: GoogleSignInOptions): void {
    const buttonHost = h("div", { class: "google-button" });
    lead.textContent = signInLead(options);
    for (const node of [
      buttonHost,
      h(
        "p",
        { class: "signin-why" },
        "Signed in, you can play rated games, register your bots' names and climb the leaderboard. Nothing from your Google account is shown.",
      ),
      h("div", { class: "or-divider" }, "or play as a guest"),
      h("p", { class: "hint" }, "Guests play unrated, as today."),
    ]) {
      form.insertBefore(node, label);
    }
    let busy = false;
    const onCredential = async (credential: string) => {
      if (busy || disposed) return;
      busy = true;
      error.textContent = "";
      try {
        await google.submit(credential);
      } catch (cause) {
        if (disposed) return;
        const next = googleSignInStep(cause, false);
        if (next.kind === "ask-name") {
          step.open(credential);
          form.replaceWith(step.el);
          step.focus();
        } else {
          error.textContent = next.message;
        }
      } finally {
        busy = false;
      }
    };
    const render = () => signIn.renderButton(buttonHost, onCredential);
    const step = mountNameStep(google, (message) => {
      step.el.replaceWith(form);
      error.textContent = message;
      render();
    });
    render();
  }

  const google = options.google;
  if (google) {
    void google.ready.then((signIn) => {
      if (signIn && !disposed) addGoogle(signIn, google);
    });
  }

  return {
    el: form,
    dispose() {
      disposed = true;
    },
  };
}

/**
 * The first sign-in's step: the display name others see, posted with the same Google credential.
 * `onBack` goes back to the name screen, with a sentence when the credential is no use any more.
 * No account exists until Continue succeeds.
 */
function mountNameStep(
  google: GoogleSignInOptions,
  onBack: (message: string) => void,
) {
  let credential = "";
  const input = h("input", {
    id: "account-name",
    type: "text",
    maxLength: 24,
    autocomplete: "off",
    spellcheck: false,
  });
  const error = h("p", { class: "field-error", attrs: { role: "alert" } });
  const button = h("button", { type: "submit", class: "primary" }, "Continue");
  const cancel = h("button", { type: "button", class: "secondary" }, "Cancel");
  const el = h(
    "form",
    { class: "panel narrow", noValidate: true },
    h("h1", {}, "Choose a display name"),
    h(
      "p",
      { class: "lead" },
      "This is the name others see. It isn't your Google name.",
    ),
    h("label", { htmlFor: "account-name" }, "Display name"),
    input,
    h("p", { class: "hint" }, nameRule),
    error,
    h("div", { class: "actions" }, button, cancel),
  );
  cancel.addEventListener("click", () => onBack(""));
  el.addEventListener("submit", async (event) => {
    event.preventDefault();
    const problem = validateAccountName(input.value);
    if (problem) {
      error.textContent = problem;
      input.focus();
      return;
    }
    error.textContent = "";
    button.disabled = true;
    cancel.disabled = true;
    try {
      await google.submit(credential, input.value.trim());
    } catch (cause) {
      const next = googleSignInStep(cause, true);
      if (next.kind === "button-problem") {
        onBack(next.message);
      } else if (next.kind === "name-problem") {
        error.textContent = next.message;
        input.focus();
      }
    } finally {
      button.disabled = false;
      cancel.disabled = false;
    }
  });
  return {
    el,
    open(next: string) {
      credential = next;
      error.textContent = "";
    },
    focus() {
      queueMicrotask(() => input.focus());
    },
  };
}
