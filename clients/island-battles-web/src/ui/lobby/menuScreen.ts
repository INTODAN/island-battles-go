import { ApiError } from "../../api/errors";
import { h } from "../dom";
import type { LobbyActions, MapTemplateSummary } from "./actions";
import {
  type CreateFormValues,
  CreateRequests,
  defaultTurnTimeout,
  type FormErrors,
  fixedTeamCount,
  type MapChoice,
  type MapTemplate,
  maxNameLength,
  maxTeamCapacity,
  maxTeams,
  minTeams,
  normalizeJoinCode,
  readMapFile,
  teamName,
  turnTimeouts,
  validateCreateForm,
  validateJoinCode,
} from "./form";
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

interface FileState {
  name: string;
  map: MapTemplate | null;
  error: string;
  checking: Promise<void> | null;
}

/** Screen 2: create a lobby or join one by its code. */
export function mountMenuScreen(options: MenuScreenOptions): Screen {
  const { actions } = options;
  let disposed = false;
  let templates: MapTemplateSummary[] = [];
  let file: FileState | null = null;
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
  const teamCount = h("input", {
    id: "team-count",
    type: "number",
    min: String(minTeams),
    max: String(maxTeams),
    value: "2",
  });
  const teamCapacity = h("input", {
    id: "team-capacity",
    type: "number",
    min: "1",
    max: String(maxTeamCapacity),
    value: "1",
  });
  const mapSelect = h(
    "select",
    { id: "map-choice" },
    new Option("Generated map", "generated"),
    new Option("Load a map file…", "file"),
  );
  const mapFile = h("input", {
    id: "map-file",
    type: "file",
    accept: ".json,application/json",
    hidden: true,
  });
  mapFile.setAttribute("aria-label", "Map file");
  const mapStatus = h("p", { class: "hint", attrs: { role: "status" } });
  const timeoutSelect = h(
    "select",
    { id: "turn-timeout" },
    ...turnTimeouts.map(
      (option) =>
        new Option(
          option.label,
          option.seconds === null ? "none" : String(option.seconds),
          false,
          option.seconds === defaultTurnTimeout,
        ),
    ),
  );
  const errorOf = (field: string) =>
    h("p", {
      class: "field-error",
      attrs: { role: "alert", "data-field": field },
    });
  const errors = {
    lobbyName: errorOf("lobbyName"),
    teamCount: errorOf("teamCount"),
    teamCapacity: errorOf("teamCapacity"),
    map: errorOf("map"),
    turnTimeout: errorOf("turnTimeout"),
  };
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
    errors.lobbyName,
    h(
      "div",
      { class: "row" },
      h(
        "div",
        {},
        h("label", { htmlFor: "team-count" }, "Teams"),
        teamCount,
        errors.teamCount,
      ),
      h(
        "div",
        {},
        h("label", { htmlFor: "team-capacity" }, "Players per team"),
        teamCapacity,
        errors.teamCapacity,
      ),
    ),
    h("label", { htmlFor: "map-choice" }, "Map"),
    mapSelect,
    mapFile,
    mapStatus,
    errors.map,
    h("label", { htmlFor: "turn-timeout" }, "Turn limit"),
    timeoutSelect,
    errors.turnTimeout,
    createError,
    h("div", { class: "actions" }, createButton),
  );

  function mapChoice(): MapChoice {
    if (mapSelect.value === "file") {
      return {
        kind: "file",
        map: file?.map ?? null,
        ...(file?.error ? { error: file.error } : {}),
      };
    }
    if (mapSelect.value.startsWith("template:")) {
      return { kind: "template", templateId: Number(mapSelect.value.slice(9)) };
    }
    return { kind: "generated" };
  }

  function values(): CreateFormValues {
    return {
      lobbyName: lobbyName.value,
      teamCount: Number(teamCount.value),
      teamCapacity: Number(teamCapacity.value),
      map: mapChoice(),
      turnTimeoutSeconds:
        timeoutSelect.value === "none" ? null : Number(timeoutSelect.value),
    };
  }

  function showErrors(found: FormErrors): void {
    for (const [field, node] of Object.entries(errors)) {
      node.textContent = found[field as keyof FormErrors] ?? "";
    }
  }

  /** Keeps the team count field and the map's note in step with the map choice. */
  function refreshMap(): void {
    const choice = mapChoice();
    mapFile.hidden = choice.kind !== "file";
    const fixed = fixedTeamCount(choice, templates);
    teamCount.disabled = fixed !== null;
    if (fixed !== null) teamCount.value = String(fixed);
    const count = fixed ?? (Number(teamCount.value) || 0);
    const names =
      count >= minTeams && count <= maxTeams
        ? Array.from({ length: count }, (_, index) => teamName(index + 1)).join(
            ", ",
          )
        : "";
    if (choice.kind === "file") {
      mapStatus.textContent = file?.checking
        ? "Checking the map file…"
        : file
          ? file.error ||
            `${file.name}: ${file.map?.map_name}. Teams: ${names}.`
          : "Choose a map file made with the map builder.";
    } else {
      mapStatus.textContent = names ? `Teams: ${names}.` : "";
    }
  }

  mapSelect.addEventListener("change", () => {
    errors.map.textContent = "";
    refreshMap();
  });
  teamCount.addEventListener("input", refreshMap);
  mapFile.addEventListener("change", () => {
    const [chosen] = Array.from(mapFile.files ?? []);
    file = null;
    errors.map.textContent = "";
    if (!chosen) {
      refreshMap();
      return;
    }
    const state: FileState = {
      name: chosen.name,
      map: null,
      error: "",
      checking: null,
    };
    file = state;
    state.checking = (async () => {
      try {
        const map = readMapFile(await chosen.text());
        const verdict = await actions.validateMap(map);
        if (file !== state) return;
        if (verdict.valid) state.map = map;
        else {
          const first = verdict.errors
            .slice(0, 3)
            .map((item) =>
              item.location
                ? `${item.location}: ${item.message}`
                : item.message,
            )
            .join(" ");
          state.error =
            `The server did not accept this map file. ${first}`.trim();
        }
      } catch (cause) {
        if (file === state) {
          state.error =
            cause instanceof Error
              ? cause.message
              : "Could not check the map file.";
        }
      } finally {
        state.checking = null;
        if (!disposed && file === state) refreshMap();
      }
    })();
    refreshMap();
  });

  void actions
    .mapTemplates()
    .then((maps) => {
      if (disposed) return;
      templates = maps;
      const group = h(
        "optgroup",
        { label: "Default maps" },
        ...maps.map(
          (map) =>
            new Option(
              `${map.map_name} · ${map.team_count} teams · ${map.plane_size_x}×${map.plane_size_y}`,
              `template:${map.map_template_id}`,
            ),
        ),
      );
      mapSelect.insertBefore(group, mapSelect.options[1] ?? null);
      refreshMap();
    })
    .catch(() => {
      if (!disposed) {
        errors.map.textContent =
          "Could not load the default maps. You can still use the generated map or a map file.";
      }
    });

  createForm.addEventListener("submit", async (event) => {
    event.preventDefault();
    createError.textContent = "";
    if (file?.checking) await file.checking;
    const result = validateCreateForm(values(), templates);
    showErrors(result.ok ? {} : result.errors);
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
      capacity: result.capacity,
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
  refreshMap();
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
    },
  };
}
