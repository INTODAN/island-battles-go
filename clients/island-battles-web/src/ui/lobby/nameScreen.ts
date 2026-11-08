import { h } from "../dom";
import { maxNameLength, validateDisplayName } from "./form";

export interface Screen {
  el: HTMLElement;
  dispose(): void;
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
}

/** Screen 1: the player's name. */
export function mountNameScreen(options: NameScreenOptions): Screen {
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
  const form = h(
    "form",
    { class: "panel narrow", noValidate: true },
    h("h1", {}, "Island Battles"),
    h(
      "p",
      { class: "lead" },
      options.joinCode
        ? `You were invited to lobby ${options.joinCode}. Choose a name to join it.`
        : options.watchCode
          ? `You were invited to watch game ${options.watchCode}. Choose a name to watch it.`
          : "Command your castles across the sea. Choose a name to begin.",
    ),
    h("label", { htmlFor: "player-name" }, "Your name"),
    input,
    error,
    h("div", { class: "actions" }, button),
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
  return { el: form, dispose() {} };
}
