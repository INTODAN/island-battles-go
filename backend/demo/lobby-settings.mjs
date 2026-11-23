// Island Battles' map choice, turn timeout, turn pace and game length for the lobby
// frontend's create form. The host picks a default map from GET /map/templates, the first one
// unless they pick another, or loads a map file made with the map builder.
// The chosen map sets the lobby's teams: one per team colour, named after the
// colour the board draws. The turn timeout sets how long each turn lasts, the turn pace how
// often a real-time game, one with a player who plays by hand, plays a turn, and the game
// length the most turns of the game.
//
// The lobby page passes gameURL, the game's site, which is not the lobby
// page's origin when the lobby runs on its own: the form reads the default
// maps and opens the map builder there. Without it, the page's own origin is
// used.

// The board's team colours, in team colour order (see replay.html).
const colourNames = ["Blue", "Red", "Green", "Purple", "Amber", "Teal"];

export function teamName(colour) {
  return colourNames[colour - 1] || `Team ${colour}`;
}

// The lobby teams for a map made for teamCount teams, in team colour order.
export function teamNames(teamCount) {
  return Array.from({ length: teamCount }, (_, index) => teamName(index + 1));
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

// The turn paces the form offers, as game_settings.turn_pace_milliseconds:
// how long each turn of a real-time game lasts. The server accepts 500 to
// 5000 milliseconds and, when the settings leave it out, uses 1000.
export const turnPaces = [
  { milliseconds: 500, label: "0.5 s" },
  { milliseconds: 1000, label: "1 s" },
  { milliseconds: 2000, label: "2 s" },
];
const defaultTurnPace = 1000;

// A turn pace in seconds, such as "0.5 s".
function paceLabel(milliseconds) {
  return `${milliseconds / 1000} s`;
}

// A short line naming a lobby's turn pace, from its game_settings.
export function describeTurnPace(settings) {
  return `Turn pace: ${paceLabel(settings?.turn_pace_milliseconds ?? defaultTurnPace)}`;
}

// The game lengths the form offers, as game_settings.max_generations: the
// most turns a game may last. The server accepts 50 to 1200 and, when the
// settings leave it out, uses 300.
export const gameLengths = [150, 300, 600, 1200];
const defaultGameLength = 300;

// A short line naming a lobby's game length when it isn't the default, from
// its game_settings, and "" when it is.
export function describeGameLength(settings) {
  const turns = settings?.max_generations ?? defaultGameLength;
  return turns === defaultGameLength ? "" : `Game length: ${turns} turns`;
}

// The default maps by the game's URL, loaded once per page and shared by the
// form and the room.
const defaultMaps = new Map();

export function loadDefaultMaps(gameURL = "") {
  if (!defaultMaps.has(gameURL)) {
    defaultMaps.set(gameURL, (async () => {
      const response = await fetch(`${gameURL}/map/templates`, { headers: { Accept: "application/json" } });
      if (!response.ok) throw new Error(`HTTP ${response.status}`);
      return (await response.json()).maps;
    })().catch((error) => {
      defaultMaps.delete(gameURL);
      throw error;
    }));
  }
  return defaultMaps.get(gameURL);
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
// A builder on the game's site, which is not this page's origin, can't see
// which page opened it, so it says hello to its opener with this message,
// and the form answers with lobbyPageMessageType.
const builderHelloType = "island-battles.map-builder";

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
// window.open("", name) returned. A window already on the builder, at
// builderOrigin, is kept, because navigating it would reload the builder and
// lose its unsaved map. Anything else, such as a fresh blank window, is sent
// to the builder. A window of another origin can't be read: when the builder
// is on another origin than pageOrigin, the window of the builder's name is
// that builder, and otherwise it is taken over.
export function builderWindowAction(opened, builderOrigin, pageOrigin = builderOrigin) {
  try {
    if (opened.location.origin === builderOrigin && opened.location.pathname.startsWith(builderURL)) return "keep";
  } catch {
    return builderOrigin === pageOrigin ? "navigate" : "keep";
  }
  return "navigate";
}

// builderHello tells whether a message is the hello of the builder window
// that the form opened, on the builder's origin.
export function builderHello(event, { origin, builder }) {
  return Boolean(builder) && event.origin === origin && event.source === builder && event.data?.type === builderHelloType;
}

function describeMap(name, teamCount, width, height) {
  return `${name} · ${teamCount} teams · ${width}×${height}`;
}

// A short line naming a rated lobby, from its game_settings, and "" for any
// other. Only the 3D client signs in, so only it plays a rated lobby.
export function describeRated(settings) {
  return settings?.rated === true ? "Rated (3D client only)" : "";
}

// A short line for the room, naming the map that a lobby's settings chose and
// its turn timeout and turn pace, its game length when that isn't 300 turns,
// and whether it is rated.
export async function describeSettings(settings, { gameURL = "" } = {}) {
  return [await describeMapChoice(settings, gameURL), describeTurnTimeout(settings), describeTurnPace(settings), describeGameLength(settings), describeRated(settings)].filter(Boolean).join(" · ");
}

async function describeMapChoice(settings, gameURL) {
  if (settings?.map) return `Map: ${settings.map.map_name}`;
  if (settings?.map_template_id) {
    try {
      const chosen = (await loadDefaultMaps(gameURL)).find((map) => map.map_template_id === settings.map_template_id);
      if (chosen) return `Map: ${chosen.map_name}`;
    } catch { /* Name the map by its ID instead. */ }
    return `Map: default map #${settings.map_template_id}`;
  }
  // Only a lobby created before every lobby needed a map has none.
  return "No map chosen";
}

// The status line's start for a map file, by where it came from.
const fileSourceLabels = { file: "", builder: "From the map builder: ", lobby: "The lobby’s map file: " };

// The label of the room's control that opens this form for a waiting lobby.
export const changeSettingsLabel = "Change map";

// settingsSelection reads a lobby's game_settings as the form shows them: the
// map select's value ("template:ID", "file", or "" for no map), the
// map file the settings carry, and the turn timeout select's value ("none" for
// no limit), the turn pace select's value, the game length select's value and
// whether the lobby is rated.
export function settingsSelection(settings) {
  const seconds = settings && "turn_timeout_seconds" in settings ? settings.turn_timeout_seconds : defaultTurnTimeout;
  let map = "";
  if (settings?.map) map = "file";
  else if (settings?.map_template_id) map = `template:${settings.map_template_id}`;
  return { map, file: settings?.map || null, timeout: seconds === null ? "none" : String(seconds),
    pace: String(settings?.turn_pace_milliseconds ?? defaultTurnPace), length: String(settings?.max_generations ?? defaultGameLength),
    rated: settings?.rated === true };
}

function node(tag, properties = {}, ...children) {
  const element = Object.assign(document.createElement(tag), properties);
  element.append(...children);
  return element;
}

// The create form's map, turn timeout, turn pace and game length controls, and
// a Rated checkbox that this page can't change: rated games are set up in the
// 3D client, where players sign in. The lobby page constructs
// it with a container for its controls and an onChange callback, and calls
// choice() when the host creates a lobby. The room's form for changing a
// waiting lobby's settings is the same, constructed with current, the lobby's
// game_settings, which it shows as chosen. Without a map to show, the form
// chooses the first default map once the default maps load, and keeps the
// lobby's rated setting. destroy() stops a form that the page removes from
// listening to the map builder.
export class SettingsForm {
  constructor({ container, onChange = () => {}, current = null, gameURL = "" }) {
    this.onChange = onChange;
    this.gameURL = gameURL;
    // The builder is on the game's site.
    this.builderOrigin = gameURL ? new URL(gameURL).origin : location.origin;
    this.maps = [];
    // Set once the default maps have loaded, even when there are none.
    this.mapsLoaded = false;
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
    // Shown until a map is chosen: while the default maps load, or when none could be.
    this.noMap = new Option("Loading the default maps…", "");
    this.select = node("select", {}, this.noMap, new Option("Load a map file…", "file"));
    this.fileInput = node("input", { type: "file", accept: ".json,application/json", hidden: true });
    this.fileInput.setAttribute("aria-label", "Map file");
    this.status = node("p", { className: "hint" });
    this.status.setAttribute("role", "status");
    this.builderButton = node("button", { type: "button", className: "secondary" }, "Build a map…");
    this.timeout = node("select", {},
      ...turnTimeouts.map(({ seconds, label }) => new Option(label, seconds === null ? "none" : String(seconds), false, seconds === defaultTurnTimeout)));
    this.pace = node("select", {},
      ...turnPaces.map(({ milliseconds, label }) => new Option(label, String(milliseconds), false, milliseconds === defaultTurnPace)));
    this.length = node("select", {},
      ...gameLengths.map((turns) => new Option(`${turns} turns`, String(turns), false, turns === defaultGameLength)));
    this.rated = node("input", { type: "checkbox", disabled: true, checked: false });
    container.append(node("label", {}, "Map", this.select), this.fileInput, this.builderButton, this.status,
      node("label", {}, "Turn timeout", this.timeout),
      node("p", { className: "hint" }, "In a game of bots, a team whose bot doesn’t answer in this time loses. In a real-time game, a player who doesn’t respond for this long has left."),
      node("label", {}, "Turn pace", this.pace),
      node("p", { className: "hint" }, "Used when someone plays by hand: the game is then real-time, with a turn this often. 0.5 s needs a fast connection."),
      node("label", {}, "Game length", this.length),
      node("p", { className: "hint" }, "The game ends after this many turns; the team with the most castles wins."),
      node("label", {}, this.rated, "Rated"),
      node("p", { className: "hint" }, "Rated games are set up in the 3D client."));
    this.builderButton.addEventListener("click", () => this.openBuilder());
    window.addEventListener("message", (event) => this.receive(event), { signal: this.listening.signal });
    this.select.addEventListener("change", () => {
      this.pendingTemplate = "";
      if (this.select.value) this.removeNoMap();
      this.update();
    });
    this.fileInput.addEventListener("change", () => this.readFile());
    if (current) this.showCurrent(current);
    this.update();
    this.loadMaps();
  }

  destroy() {
    this.listening.abort();
  }

  // showCurrent selects the map, turn timeout, turn pace and game length of a lobby's settings. A
  // default map is selected once the default maps load.
  showCurrent(settings) {
    const selection = settingsSelection(settings);
    if (![...this.timeout.options].some((option) => option.value === selection.timeout)) {
      this.timeout.append(new Option(`${selection.timeout} s`, selection.timeout));
    }
    this.timeout.value = selection.timeout;
    if (![...this.pace.options].some((option) => option.value === selection.pace)) {
      this.pace.append(new Option(paceLabel(Number(selection.pace)), selection.pace));
    }
    this.pace.value = selection.pace;
    if (![...this.length.options].some((option) => option.value === selection.length)) {
      this.length.append(new Option(`${selection.length} turns`, selection.length));
    }
    this.length.value = selection.length;
    this.rated.checked = selection.rated;
    if (selection.file) {
      this.file = selection.file;
      this.fileSource = "lobby";
      this.select.value = "file";
      this.removeNoMap();
    } else {
      this.pendingTemplate = selection.map;
    }
  }

  removeNoMap() {
    if ([...this.select.options].includes(this.noMap)) this.select.removeChild(this.noMap);
  }

  async loadMaps() {
    try {
      this.maps = await loadDefaultMaps(this.gameURL);
    } catch {
      this.mapsError = "Could not load the default maps. You can still load a map file.";
      this.noMap.text = "Pick a map";
      this.update();
      return;
    }
    this.mapsLoaded = true;
    const options = this.maps.map((map) => new Option(describeMap(map.map_name, map.team_count, map.plane_size_x, map.plane_size_y), `template:${map.map_template_id}`));
    const fileOption = [...this.select.options].find((option) => option.value === "file");
    this.select.insertBefore(node("optgroup", { label: "Default maps" }, ...options), fileOption);
    if (this.pendingTemplate) {
      if (options.some((option) => option.value === this.pendingTemplate)) this.select.value = this.pendingTemplate;
      else this.mapsError = "The lobby’s default map is no longer offered. Choose another map.";
      this.pendingTemplate = "";
    } else if (!this.select.value && options.length) {
      // A new lobby starts on the first default map.
      this.select.value = options[0].value;
    }
    if (this.select.value) this.removeNoMap();
    else this.noMap.text = "Pick a map";
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
    if (builderWindowAction(opened, this.builderOrigin, location.origin) === "keep") {
      // Its opener is the page before the reload, so name this page instead.
      opened.postMessage({ type: lobbyPageMessageType }, this.builderOrigin);
      opened.focus();
    } else {
      opened.location.href = `${this.gameURL}${builderURL}`;
    }
  }

  // receive takes the map that the builder sends. The latest map or file the
  // host picks wins. A builder on another origin says hello first, and learns
  // from the answer that this is the page to send maps to.
  receive(event) {
    if (builderHello(event, { origin: this.builderOrigin, builder: this.builder })) {
      this.builder.postMessage({ type: lobbyPageMessageType }, this.builderOrigin);
      return;
    }
    let map;
    try {
      map = mapFromBuilderMessage(event, { origin: this.builderOrigin, builder: this.builder });
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
    this.removeNoMap();
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
    if (loading) {
      this.status.textContent = this.fileError || (this.file
        ? `${fileSourceLabels[this.fileSource]}${describeMap(this.file.map_name, this.file.player_amount, this.file.plane_size_x, this.file.plane_size_y)}. Teams: ${teamNames(this.file.player_amount).join(", ")}.`
        : "Choose a map file made with the map builder.");
    } else if (this.pendingTemplate) {
      this.status.textContent = this.mapsError || "Loading the lobby’s default map…";
    } else if (!this.select.value) {
      this.status.textContent = this.mapsError || (this.mapsLoaded ? "Pick a default map, or load a map file." : "");
    } else {
      const teams = `Teams: ${teamNames(this.chosenDefaultMap().team_count).join(", ")}.`;
      this.status.textContent = this.mapsError ? `${teams} ${this.mapsError}` : teams;
    }
    this.onChange();
  }

  chosenDefaultMap() {
    const id = Number(this.select.value.replace("template:", ""));
    return this.maps.find((map) => map.map_template_id === id);
  }

  // The lobby's game_settings and team names: one team per colour of the
  // chosen map. Throws when the choice is not complete.
  choice() {
    if (this.pendingTemplate) {
      throw new Error(this.mapsError ? "Could not load the default maps. Pick another map, or reload the page." : "The default maps are still loading. Try again in a moment.");
    }
    const limits = { turn_timeout_seconds: this.timeout.value === "none" ? null : Number(this.timeout.value), turn_pace_milliseconds: Number(this.pace.value), max_generations: Number(this.length.value) };
    // The page can't change rated, so a rated lobby stays rated.
    if (this.rated.checked) limits.rated = true;
    if (this.select.value === "file") {
      if (!this.file) throw new Error(this.fileError || "Choose a map file to load, or pick another map.");
      return { settings: { map: this.file, ...limits }, teams: teamNames(this.file.player_amount) };
    }
    if (!this.select.value) {
      throw new Error(this.mapsError || (this.mapsLoaded ? "Pick a default map, or load a map file." : "The default maps are still loading. Try again in a moment."));
    }
    const map = this.chosenDefaultMap();
    return { settings: { map_template_id: map.map_template_id, ...limits }, teams: teamNames(map.team_count) };
  }
}
