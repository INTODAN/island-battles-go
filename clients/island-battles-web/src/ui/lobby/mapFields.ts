import { h } from "../dom";
import type { LobbyActions, MapTemplateSummary } from "./actions";
import {
  defaultTurnTimeout,
  type FormErrors,
  fixedTeamCount,
  type MapChoice,
  type MapSettingsValues,
  type MapTemplate,
  maxTeams,
  minTeams,
  readMapFile,
  teamName,
  turnTimeouts,
} from "./form";
import { createMapBuilderLink, mapBuilderAvailable } from "./mapBuilder";

interface FileState {
  name: string;
  map: MapTemplate | null;
  error: string;
  checking: Promise<void> | null;
}

export interface MapFieldsOptions {
  actions: Pick<LobbyActions, "mapTemplates" | "validateMap">;
  /** Prefixes the fields' element IDs, so two sets can share a page. */
  idPrefix?: string;
  /** The values to start from: a lobby's current settings for Change map. */
  initial?: MapSettingsValues;
}

export interface MapFields {
  /** The fields' nodes in order, for the form that holds them. */
  nodes: HTMLElement[];
  values(): MapSettingsValues;
  /** The default maps, once they have loaded. */
  templates(): MapTemplateSummary[];
  /** Resolves once a map file being checked has its verdict. */
  settled(): Promise<void>;
  /** Shows each field's problem, and clears the others. */
  showErrors(found: FormErrors): void;
  dispose(): void;
}

/**
 * The map, team count and turn limit fields, which the create form and the
 * room's Change map form share. A map decides the teams, so the team count
 * field follows the map choice.
 */
export function mountMapFields(options: MapFieldsOptions): MapFields {
  const { actions } = options;
  const id = (name: string) => `${options.idPrefix ?? ""}${name}`;
  const initial = options.initial;
  let disposed = false;
  let templates: MapTemplateSummary[] = [];
  let file: FileState | null =
    initial?.map.kind === "file" && initial.map.map
      ? {
          name: "The lobby's map file",
          map: initial.map.map,
          error: "",
          checking: null,
        }
      : null;
  // The lobby's default map, until the default maps load and the select can show it.
  let pendingTemplate =
    initial?.map.kind === "template" ? initial.map.templateId : undefined;

  const teamCount = h("input", {
    id: id("team-count"),
    type: "number",
    min: String(minTeams),
    max: String(maxTeams),
    value: String(initial?.teamCount ?? 2),
  });
  const mapSelect = h(
    "select",
    { id: id("map-choice") },
    new Option("Generated map", "generated"),
    new Option("Load a map file…", "file"),
    ...(mapBuilderAvailable ? [new Option("Build a map…", "builder")] : []),
  );
  if (file) mapSelect.value = "file";
  const mapFile = h("input", {
    id: id("map-file"),
    type: "file",
    accept: ".json,application/json",
    hidden: true,
  });
  mapFile.setAttribute("aria-label", "Map file");
  const mapStatus = h("p", { class: "hint", attrs: { role: "status" } });
  const timeout =
    initial === undefined ? defaultTurnTimeout : initial.turnTimeoutSeconds;
  const timeoutOptions = turnTimeouts.some(
    (option) => option.seconds === timeout,
  )
    ? turnTimeouts
    : [...turnTimeouts, { seconds: timeout, label: `${timeout} s` }];
  const timeoutSelect = h(
    "select",
    { id: id("turn-timeout") },
    ...timeoutOptions.map(
      (option) =>
        new Option(
          option.label,
          option.seconds === null ? "none" : String(option.seconds),
          false,
          option.seconds === timeout,
        ),
    ),
  );
  const errorOf = (field: string) =>
    h("p", {
      class: "field-error",
      attrs: { role: "alert", "data-field": field },
    });
  const errors = {
    teamCount: errorOf("teamCount"),
    map: errorOf("map"),
    turnTimeout: errorOf("turnTimeout"),
  };

  function mapChoice(): MapChoice {
    if (mapSelect.value === "file" || mapSelect.value === "builder") {
      return {
        kind: "file",
        map: file?.map ?? null,
        ...(file?.error ? { error: file.error } : {}),
      };
    }
    if (mapSelect.value.startsWith("template:")) {
      return { kind: "template", templateId: Number(mapSelect.value.slice(9)) };
    }
    if (pendingTemplate !== undefined) {
      return { kind: "template", templateId: pendingTemplate };
    }
    return { kind: "generated" };
  }

  /** Keeps the team count field and the map's note in step with the map choice. */
  function refreshMap(): void {
    const choice = mapChoice();
    mapFile.hidden = mapSelect.value !== "file";
    const fixed = fixedTeamCount(choice, templates);
    teamCount.disabled = fixed !== null || pendingTemplate !== undefined;
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
          : mapSelect.value === "builder"
            ? "Build a map in the new tab, then press Use in lobby there."
            : "Choose a map file made with the map builder.";
    } else if (pendingTemplate !== undefined) {
      mapStatus.textContent = "Loading the lobby's map…";
    } else {
      mapStatus.textContent = names ? `Teams: ${names}.` : "";
    }
  }

  // The map builder: its "Use in lobby" map is taken as if it had been loaded from a file.
  const mapBuilder = createMapBuilderLink({
    onMapText(text) {
      pendingTemplate = undefined;
      mapSelect.value = "file";
      mapFile.value = "";
      errors.map.textContent = "";
      checkMapFile("From the map builder", async () => text);
    },
    onProblem(message) {
      pendingTemplate = undefined;
      file = { name: "", map: null, error: message, checking: null };
      mapSelect.value = "file";
      refreshMap();
    },
  });

  mapSelect.addEventListener("change", () => {
    pendingTemplate = undefined;
    errors.map.textContent = "";
    if (mapSelect.value === "builder") mapBuilder.open();
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
    checkMapFile(chosen.name, () => chosen.text());
  });

  /** Has the server check a map's text; the checked map is what the form sends. */
  function checkMapFile(name: string, text: () => Promise<string>): void {
    const state: FileState = { name, map: null, error: "", checking: null };
    file = state;
    state.checking = (async () => {
      try {
        const map = readMapFile(await text());
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
  }

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
      if (pendingTemplate !== undefined) {
        const wanted = `template:${pendingTemplate}`;
        // A map that is no longer listed stays chosen, and the check names it.
        if (maps.some((map) => `template:${map.map_template_id}` === wanted)) {
          mapSelect.value = wanted;
          pendingTemplate = undefined;
        }
      }
      refreshMap();
    })
    .catch(() => {
      if (!disposed) {
        errors.map.textContent =
          "Could not load the default maps. You can still use the generated map or a map file.";
      }
    });

  refreshMap();
  return {
    nodes: [
      h("label", { htmlFor: id("team-count") }, "Teams"),
      teamCount,
      h("p", { class: "hint" }, "One player or bot per team."),
      errors.teamCount,
      h("label", { htmlFor: id("map-choice") }, "Map"),
      mapSelect,
      mapFile,
      mapStatus,
      errors.map,
      h("label", { htmlFor: id("turn-timeout") }, "Turn limit"),
      timeoutSelect,
      errors.turnTimeout,
    ],
    values: () => ({
      teamCount: Number(teamCount.value),
      map: mapChoice(),
      turnTimeoutSeconds:
        timeoutSelect.value === "none" ? null : Number(timeoutSelect.value),
      ...(initial
        ? { currentTurnTimeoutSeconds: initial.turnTimeoutSeconds }
        : {}),
    }),
    templates: () => templates,
    async settled() {
      if (file?.checking) await file.checking;
    },
    showErrors(found) {
      for (const [field, node] of Object.entries(errors)) {
        node.textContent = found[field as keyof FormErrors] ?? "";
      }
    },
    dispose() {
      disposed = true;
      mapBuilder.dispose();
    },
  };
}
