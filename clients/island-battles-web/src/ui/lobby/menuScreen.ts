import { ApiError } from "../../api/errors";
import { h } from "../dom";
import type { LobbyActions } from "./actions";
import {
  CreateRequests,
  maxNameLength,
  normalizeJoinCode,
  validateCreateForm,
  validateJoinCode,
} from "./form";
import { mountMapFields } from "./mapFields";
import type { Screen } from "./nameScreen";
import { describeProblem } from "./problems";

export interface MenuScreenOptions {
  playerName: string;
  actions: LobbyActions;
  /** A join code to prefill and try at once (the page's `?join=` link). */
  joinCode: string | undefined;
  /** A code to watch at once (the page's `?watch=` link). */
  watchCode?: string | undefined;
  /** Starts watching the game with this code; rejects with the refusal. */
  onWatch(joinCode: string): Promise<void>;
  /** A sentence to show first, such as why the previous lobby ended. */
  notice: string;
  /** The session ended while a call ran. */
  onSignedOut(): void;
  onChangeName(): void;
  newId(): string;
}

/** Screen 2: create a lobby or join one by its code. */
export function mountMenuScreen(options: MenuScreenOptions): Screen {
  const { actions } = options;
  let disposed = false;
  const requests = new CreateRequests(options.newId);

  // --- Join ---
  const codeInput = h("input", {
    id: "join-code",
    type: "text",
    maxLength: 12,
    autocomplete: "off",
    spellcheck: false,
    placeholder: "ABCD1234",
    value: options.joinCode ?? "",
  });
  const joinError = h("p", { class: "field-error", attrs: { role: "alert" } });
  joinError.textContent = options.notice;
  const joinButton = h(
    "button",
    { type: "submit", class: "primary" },
    "Join lobby",
  );
  const joinForm = h(
    "form",
    { class: "card", noValidate: true },
    h("h2", {}, "Join with a code"),
    h("label", { htmlFor: "join-code" }, "Join code"),
    codeInput,
    joinError,
    h("div", { class: "actions" }, joinButton),
  );

  async function join(rawCode: string): Promise<void> {
    const problem = validateJoinCode(rawCode);
    if (problem) {
      joinError.textContent = problem;
      return;
    }
    joinError.textContent = "";
    joinButton.disabled = true;
    try {
      await actions.join(normalizeJoinCode(rawCode));
    } catch (cause) {
      const shown = describeProblem(cause, "join");
      joinError.textContent = shown.message;
      if (shown.signedOut) options.onSignedOut();
    } finally {
      if (!disposed) joinButton.disabled = false;
    }
  }
  joinForm.addEventListener("submit", (event) => {
    event.preventDefault();
    void join(codeInput.value);
  });

  // --- Watch ---
  const watchInput = h("input", {
    id: "watch-code",
    type: "text",
    maxLength: 12,
    autocomplete: "off",
    spellcheck: false,
    placeholder: "ABCD1234",
    value: options.watchCode ?? "",
  });
  const watchError = h("p", {
    class: "field-error",
    attrs: { role: "alert" },
  });
  const watchButton = h(
    "button",
    { type: "submit", class: "secondary" },
    "Watch game",
  );
  const watchForm = h(
    "form",
    { class: "card", noValidate: true },
    h("h2", {}, "Watch a game"),
    h(
      "p",
      { class: "hint" },
      "Follow a game live without taking a seat. Ask a player for its code.",
    ),
    h("label", { htmlFor: "watch-code" }, "Join code"),
    watchInput,
    watchError,
    h("div", { class: "actions" }, watchButton),
  );

  async function watch(rawCode: string): Promise<void> {
    const problem = validateJoinCode(rawCode);
    if (problem) {
      watchError.textContent = problem;
      return;
    }
    watchError.textContent = "";
    watchButton.disabled = true;
    try {
      await options.onWatch(normalizeJoinCode(rawCode));
    } catch (cause) {
      const shown = describeProblem(cause, "watch");
      watchError.textContent = shown.message;
      if (shown.signedOut) options.onSignedOut();
    } finally {
      if (!disposed) watchButton.disabled = false;
    }
  }
  watchForm.addEventListener("submit", (event) => {
    event.preventDefault();
    void watch(watchInput.value);
  });

  // --- Create ---
  const lobbyName = h("input", {
    id: "lobby-name",
    type: "text",
    maxLength: maxNameLength,
    value: `${options.playerName}'s lobby`,
  });
  const mapFields = mountMapFields({ actions });
  const lobbyNameError = h("p", {
    class: "field-error",
    attrs: { role: "alert", "data-field": "lobbyName" },
  });
  const createError = h("p", {
    class: "field-error",
    attrs: { role: "alert" },
  });
  const createButton = h(
    "button",
    { type: "submit", class: "primary" },
    "Create lobby",
  );
  const createForm = h(
    "form",
    { class: "card", noValidate: true },
    h("h2", {}, "Create a lobby"),
    h("label", { htmlFor: "lobby-name" }, "Lobby name"),
    lobbyName,
    lobbyNameError,
    ...mapFields.nodes,
    createError,
    h("div", { class: "actions" }, createButton),
  );

  createForm.addEventListener("submit", async (event) => {
    event.preventDefault();
    createError.textContent = "";
    await mapFields.settled();
    const result = validateCreateForm(
      { lobbyName: lobbyName.value, ...mapFields.values() },
      mapFields.templates(),
    );
    const found = result.ok ? {} : result.errors;
    lobbyNameError.textContent = found.lobbyName ?? "";
    mapFields.showErrors(found);
    if (!result.ok) {
      const [first] = Object.keys(result.errors);
      createForm
        .querySelector<HTMLElement>(`[data-field="${first}"]`)
        ?.scrollIntoView?.({ block: "nearest" });
      return;
    }
    createButton.disabled = true;
    const body = {
      name: lobbyName.value.trim(),
      teams: result.teams,
      game_settings: result.gameSettings,
    };
    try {
      await actions.create({ ...body, request_id: requests.idFor(body) });
      requests.settled();
    } catch (cause) {
      const shown = describeProblem(cause, "create");
      // A lost connection leaves the request id in place, so pressing Create
      // again resends the same create instead of making a second lobby.
      if (cause instanceof ApiError && cause.kind === "transport") {
        createError.textContent = `${shown.message} Press Create again to retry.`;
      } else {
        requests.settled();
        createError.textContent = shown.message;
        if (shown.signedOut) options.onSignedOut();
      }
    } finally {
      if (!disposed) createButton.disabled = false;
    }
  });

  const heading = h(
    "div",
    { class: "greeting" },
    h("h1", {}, "Island Battles"),
    h(
      "p",
      { class: "lead" },
      `Playing as ${options.playerName}. `,
      h(
        "button",
        {
          type: "button",
          class: "link",
          onclick: () => options.onChangeName(),
        },
        "Change name",
      ),
    ),
  );
  const el = h(
    "div",
    { class: "menu" },
    heading,
    h(
      "div",
      { class: "cards" },
      createForm,
      h("div", { class: "card-stack" }, joinForm, watchForm),
    ),
  );
  if (options.joinCode) queueMicrotask(() => void join(options.joinCode ?? ""));
  else if (options.watchCode) {
    queueMicrotask(() => void watch(options.watchCode ?? ""));
  }
  return {
    el,
    dispose() {
      disposed = true;
      mapFields.dispose();
    },
  };
}
