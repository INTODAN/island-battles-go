// Island Battles' map choice and turn timeout for the lobby frontend's create
// form. The host either keeps the generated map, picks a default map from
// GET /map/templates, or loads a map file made with the map builder. The
// chosen map sets the lobby's teams: one per team colour, named after the
// colour the board draws. The turn timeout sets how long each turn lasts.

// The board's team colours, in team colour order (see replay.html).
const colourNames = ["Blue", "Red", "Green", "Purple", "Amber", "Teal"];

export function teamName(colour) {
  return colourNames[colour - 1] || `Team ${colour}`;
}

// The lobby teams for a map made for teamCount teams, in team colour order.
export function teamNames(teamCount) {
  return Array.from({ length: teamCount }, (_, index) => teamName(index + 1));
}

// The generated map fits any number of teams, so the host chooses how many
// teams a lobby on it has, as in the 3D client's form.
export const generatedTeamCounts = [2, 3, 4, 5, 6];
export const defaultGeneratedTeams = 2;

// generatedTeamSelection is the team count the form shows for a lobby that has
// teamCount teams on the generated map: that count when the form offers it,
// and the default otherwise.
export function generatedTeamSelection(teamCount) {
  return generatedTeamCounts.includes(teamCount) ? teamCount : defaultGeneratedTeams;
}

// The turn timeouts the form offers, as game_settings.turn_timeout_seconds:
// seconds, or null for no limit. The server's default, when the settings
// leave it out, is 30 seconds.
export const turnTimeouts = [
  { seconds: 10, label: "10 s" },
  { seconds: 30, label: "30 s" },
  { seconds: 120, label: "2 min" },
  { seconds: 600, label: "10 min" },
  { seconds: null, label: "No limit" },
];
const defaultTurnTimeout = 30;

// A short line naming a lobby's turn timeout, from its game_settings.
export function describeTurnTimeout(settings) {
  const seconds = settings && "turn_timeout_seconds" in settings ? settings.turn_timeout_seconds : defaultTurnTimeout;
  if (seconds === null) return "No turn limit";
  return `Turn limit: ${seconds % 60 === 0 ? `${seconds / 60} min` : `${seconds} s`}`;
}

let defaultMaps = null;

// The default maps, loaded once per page and shared by the form and the room.
export function loadDefaultMaps() {
  defaultMaps ||= (async () => {
    const response = await fetch("/map/templates", { headers: { Accept: "application/json" } });
    if (!response.ok) throw new Error(`HTTP ${response.status}`);
    return (await response.json()).maps;
  })().catch((error) => {
    defaultMaps = null;
    throw error;
  });
  return defaultMaps;
}

// Read a map file's text. The server checks the whole file when the lobby is
// created; this only finds what the form needs: a name and the team count.
export function readMapFile(text) {
  let map;
  try { map = JSON.parse(text); } catch { throw new Error("The map file is not valid JSON."); }
  if (!map || typeof map !== "object" || Array.isArray(map)) throw new Error("The map file must hold one map object.");
  if (!Number.isInteger(map.player_amount) || map.player_amount < 1) throw new Error("The map file needs a whole player_amount of at least 1.");
  if (typeof map.map_name !== "string" || !map.map_name.trim()) throw new Error("The map file needs a map_name.");
  return map;
}

// The map builder's Use in lobby button posts this message type to the page
// that opened it. mapbuilder/builder.mjs holds the sending side.
const mapMessageType = "island-battles.map-file";
const builderURL = "/map/builder/";
const builderWindowName = "island-battles-map-builder";

// mapFromBuilderMessage reads a message event for the map it carries. It
// returns null for a message that isn't the builder's: from another origin or
// window, or of another type. A message that is the builder's but holds no
// usable map throws, like a bad file does. builder is the window the form
// opened.
export function mapFromBuilderMessage(event, { origin, builder }) {
  if (!builder || event.origin !== origin || event.source !== builder) return null;
  if (event.data?.type !== mapMessageType) return null;
  if (typeof event.data.text !== "string") throw new Error("The map builder sent no map file.");
  return readMapFile(event.data.text);
}

// The page tells a builder window that it is the page to send maps to. A
// builder that outlived a reload of the form still holds the old, closed
// page as its opener.
const lobbyPageMessageType = "island-battles.lobby-page";

// builderWindowAction decides what to do with the window that
// window.open("", name) returned. A window already on the builder is kept,
// because navigating it would reload the builder and lose its unsaved map.
// Anything else, such as a fresh blank window, is sent to the builder.
export function builderWindowAction(opened, origin) {
  try {
    if (opened.location.origin === origin && opened.location.pathname.startsWith(builderURL)) return "keep";
  } catch { /* A window of another origin can't be read; take it over. */ }
  return "navigate";
}

function describeMap(name, teamCount, width, height) {
  return `${name} · ${teamCount} teams · ${width}×${height}`;
}

// A short line for the room, naming the map that a lobby's settings chose and
// its turn timeout.
export async function describeSettings(settings) {
  return `${await describeMapChoice(settings)} · ${describeTurnTimeout(settings)}`;
}

async function describeMapChoice(settings) {
  if (settings?.map) return `Map: ${settings.map.map_name}`;
  if (settings?.map_template_id) {
    try {
      const chosen = (await loadDefaultMaps()).find((map) => map.map_template_id === settings.map_template_id);
      if (chosen) return `Map: ${chosen.map_name}`;
    } catch { /* Name the map by its ID instead. */ }
    return `Map: default map #${settings.map_template_id}`;
  }
  return "Map: generated for the players";
}

// The status line's start for a map file, by where it came from.
const fileSourceLabels = { file: "", builder: "From the map builder: ", lobby: "The lobby’s map file: " };

// The label of the room's control that opens this form for a waiting lobby.
export const changeSettingsLabel = "Change map";

// settingsSelection reads a lobby's game_settings as the form shows them: the
// map select's value ("" for the generated map, "template:ID" or "file"), the
// map file the settings carry, and the turn timeout select's value ("none" for
// no limit).
export function settingsSelection(settings) {
  const seconds = settings && "turn_timeout_seconds" in settings ? settings.turn_timeout_seconds : defaultTurnTimeout;
  let map = "";
  if (settings?.map) map = "file";
  else if (settings?.map_template_id) map = `template:${settings.map_template_id}`;
  return { map, file: settings?.map || null, timeout: seconds === null ? "none" : String(seconds) };
}

function node(tag, properties = {}, ...children) {
  const element = Object.assign(document.createElement(tag), properties);
  element.append(...children);
  return element;
}

// The create form's map and turn timeout controls. The lobby page constructs
// it with a container for its controls and an onChange callback, and calls
// choice() when the host creates a lobby. The room's form for changing a
// waiting lobby's settings is the same, constructed with current, the lobby's
// game_settings, and teamCount, how many teams the lobby has, which it shows as
// chosen. destroy() stops a form that the
// page removes from listening to the map builder.
export class SettingsForm {
  constructor({ container, onChange = () => {}, current = null, teamCount = defaultGeneratedTeams }) {
    this.onChange = onChange;
    this.maps = [];
    this.file = null;
    this.fileError = "";
    // Where the loaded map file came from: "file", "builder", or "lobby" for
    // the map file of the lobby's current settings.
    this.fileSource = "file";
    this.builder = null;
    this.mapsError = "";
    // The default map of the lobby's current settings, as a select value,
    // until the default maps load and the select can show it.
    this.pendingTemplate = "";
    this.listening = new AbortController();
    this.select = node("select", {},
      new Option("Generated map", ""), new Option("Load a map file…", "file"));
    this.fileInput = node("input", { type: "file", accept: ".json,application/json", hidden: true });
    this.fileInput.setAttribute("aria-label", "Map file");
    this.teams = node("select", {},
      ...generatedTeamCounts.map((count) => new Option(String(count), String(count), false, count === defaultGeneratedTeams)));
    this.teamsLabel = node("label", {}, "Teams", this.teams);
    this.status = node("p", { className: "hint" });
    this.status.setAttribute("role", "status");
    this.builderButton = node("button", { type: "button", className: "secondary" }, "Build a map…");
    this.timeout = node("select", {},
      ...turnTimeouts.map(({ seconds, label }) => new Option(label, seconds === null ? "none" : String(seconds), false, seconds === defaultTurnTimeout)));
    container.append(node("label", {}, "Map", this.select), this.teamsLabel, this.fileInput, this.builderButton, this.status,
      node("label", {}, "Turn timeout", this.timeout),
      node("p", { className: "hint" }, "A team whose player doesn’t finish a turn in time loses the game."));
    this.builderButton.addEventListener("click", () => this.openBuilder());
    window.addEventListener("message", (event) => this.receive(event), { signal: this.listening.signal });
    this.select.addEventListener("change", () => {
      this.pendingTemplate = "";
      this.update();
    });
    this.teams.addEventListener("change", () => this.update());
    this.fileInput.addEventListener("change", () => this.readFile());
    if (current) this.showCurrent(current, teamCount);
    this.update();
    this.loadMaps();
  }

  destroy() {
    this.listening.abort();
  }

  // showCurrent selects the map and turn timeout of a lobby's settings. A
  // default map is selected once the default maps load.
  showCurrent(settings, teamCount) {
    const selection = settingsSelection(settings);
    if (![...this.timeout.options].some((option) => option.value === selection.timeout)) {
      this.timeout.append(new Option(`${selection.timeout} s`, selection.timeout));
    }
    this.timeout.value = selection.timeout;
    if (selection.file) {
      this.file = selection.file;
      this.fileSource = "lobby";
      this.select.value = "file";
    } else {
      this.pendingTemplate = selection.map;
      // Only the generated map is made for the lobby's own number of teams.
      if (!selection.map) this.teams.value = String(generatedTeamSelection(teamCount));
    }
  }

  async loadMaps() {
    try {
      this.maps = await loadDefaultMaps();
    } catch {
      this.mapsError = "Could not load the default maps. You can still load a map file.";
      this.update();
      return;
    }
    const options = this.maps.map((map) => new Option(describeMap(map.map_name, map.team_count, map.plane_size_x, map.plane_size_y), `template:${map.map_template_id}`));
    this.select.insertBefore(node("optgroup", { label: "Default maps" }, ...options), this.select.options[1]);
    if (this.pendingTemplate) {
      if (options.some((option) => option.value === this.pendingTemplate)) this.select.value = this.pendingTemplate;
      else this.mapsError = "The lobby’s default map is no longer offered. Choose another map.";
      this.pendingTemplate = "";
    }
    this.update();
  }

  // The builder opens in its own window. Opening it again focuses that window
  // instead of reloading it, which would lose the map being built. It must
  // not open with noopener, because the builder reaches this page through
  // window.opener.
  //
  // After this page reloads, it has forgotten the window but the browser has
  // not, so it asks for the window by name without a URL and then decides.
  openBuilder() {
    if (this.builder && !this.builder.closed) {
      this.builder.focus();
      return;
    }
    const opened = window.open("", builderWindowName);
    if (!opened) return;
    this.builder = opened;
    if (builderWindowAction(opened, location.origin) === "keep") {
      // Its opener is the page before the reload, so name this page instead.
      opened.postMessage({ type: lobbyPageMessageType }, location.origin);
      opened.focus();
    } else {
      opened.location.href = builderURL;
    }
  }

  // receive takes the map that the builder sends. The latest map or file the
  // host picks wins.
  receive(event) {
    let map;
    try {
      map = mapFromBuilderMessage(event, { origin: location.origin, builder: this.builder });
    } catch (error) {
      this.chooseFile(null, error.message, "file");
      return;
    }
    if (map) this.chooseFile(map, "", "builder");
  }

  chooseFile(map, error, source) {
    this.file = map;
    this.fileError = error;
    this.fileSource = source;
    this.pendingTemplate = "";
    this.fileInput.value = "";
    this.select.value = "file";
    this.update();
  }

  async readFile() {
    const [file] = this.fileInput.files;
    this.file = null;
    this.fileError = "";
    this.fileSource = "file";
    if (file) {
      try { this.file = readMapFile(await file.text()); } catch (error) { this.fileError = error.message; }
    }
    this.update();
  }

  update() {
    const loading = this.select.value === "file";
    this.fileInput.hidden = !loading;
    this.teamsLabel.hidden = this.select.value !== "";
    if (loading) {
      this.status.textContent = this.fileError || (this.file
        ? `${fileSourceLabels[this.fileSource]}${describeMap(this.file.map_name, this.file.player_amount, this.file.plane_size_x, this.file.plane_size_y)}. Teams: ${teamNames(this.file.player_amount).join(", ")}.`
        : "Choose a map file made with the map builder.");
    } else if (this.pendingTemplate) {
      this.status.textContent = this.mapsError || "Loading the lobby’s default map…";
    } else {
      const map = this.select.value && this.chosenDefaultMap();
      const teams = `Teams: ${teamNames(map ? map.team_count : Number(this.teams.value)).join(", ")}.`;
      this.status.textContent = this.mapsError ? `${teams} ${this.mapsError}` : teams;
    }
    this.onChange();
  }

  chosenDefaultMap() {
    const id = Number(this.select.value.replace("template:", ""));
    return this.maps.find((map) => map.map_template_id === id);
  }

  // The lobby's game_settings and team names. The generated map is made for
  // the lobby's teams, as many as the host chose.
  // Throws when the choice is not complete.
  choice() {
    if (this.pendingTemplate) {
      throw new Error(this.mapsError ? "Could not load the default maps. Pick another map, or reload the page." : "The default maps are still loading. Try again in a moment.");
    }
    const timeout = { turn_timeout_seconds: this.timeout.value === "none" ? null : Number(this.timeout.value) };
    if (this.select.value === "file") {
      if (!this.file) throw new Error(this.fileError || "Choose a map file to load, or pick another map.");
      return { settings: { map: this.file, ...timeout }, teams: teamNames(this.file.player_amount) };
    }
    if (this.select.value) {
      const map = this.chosenDefaultMap();
      return { settings: { map_template_id: map.map_template_id, ...timeout }, teams: teamNames(map.team_count) };
    }
    return { settings: timeout, teams: teamNames(Number(this.teams.value)) };
  }
}
