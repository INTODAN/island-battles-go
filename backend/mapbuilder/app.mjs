// The map builder page. builder.mjs holds the logic; this module only draws
// the map and turns the page's events into edits.
import {
  castleRadius, checkMap, describeProblems, downloadGate, dragPosition, fieldLabel, fileName, freeSpot, lobbyGate, lobbyMessage, lobbyPageFromMessage, missingTeams,
  newMap, openMapFile, openerState, placeProblems, problemCastle, problemsFor, serialize, teamColor, upgradeLabel, withoutCastle,
} from "./builder.mjs";

const svgNamespace = "http://www.w3.org/2000/svg";
const byID = (id) => document.getElementById(id);
const plane = byID("plane");
const castleList = byID("castle-list");
const mapFields = byID("map-fields");
const downloadButton = byID("download-button");
const lobbyButton = byID("lobby-button");
const openInput = byID("open-input");
const notice = byID("notice");

// checkDelay lets typing and dragging settle before the map is sent.
const checkDelay = 200;

let map = newMap();
let selected = null;
let upgrades = [];
// check is the state of the latest check: the text sent, then its result.
let check = null;
// shownErrors are the latest check's problems. They stay on screen while the
// next check runs.
let shownErrors = [];
let checkTimer;
// sentText is the map text that was last sent to the lobby. lobbyPageClosed
// stays set once the page that opened the builder is gone.
let sentText = null;
let lobbyPageClosed = false;
// lobbyPage is the same-origin window that opened this page, if one did. The
// page keeps it, because browsers clear window.opener once that window closes.
let lobbyPage = openerState(window.opener, location.origin) === "ready" ? window.opener : null;
const lobbyPageState = () => (!lobbyPage ? "none" : lobbyPage.closed ? "closed" : "ready");
let drag = null;

const isPositive = (value) => typeof value === "number" && value > 0 && Number.isFinite(value);
const isPosition = (castle) => Number.isFinite(castle.x) && Number.isFinite(castle.y);
const numberFrom = (input) => (input.value.trim() === "" ? null : Number(input.value));
const listWords = (items) => (items.length < 2 ? items.join("") : `${items.slice(0, -1).join(", ")} and ${items.at(-1)}`);

function html(tag, attributes = {}, ...children) {
  const node = document.createElement(tag);
  for (const [name, value] of Object.entries(attributes)) {
    if (value === false || value === null || value === undefined) continue;
    if (name === "class") node.className = value;
    else if (name === "text") node.textContent = value;
    else node.setAttribute(name, value === true ? "" : value);
  }
  node.append(...children);
  return node;
}

function svg(tag, attributes = {}, text) {
  const node = document.createElementNS(svgNamespace, tag);
  for (const [name, value] of Object.entries(attributes)) node.setAttribute(name, value);
  if (text !== undefined) node.textContent = text;
  return node;
}

function showNotice(message, kind = "warning") {
  notice.textContent = message;
  notice.classList.toggle("info", kind === "info");
  notice.hidden = !message;
}

// ---- Checking

function scheduleCheck({ immediate = false } = {}) {
  const text = serialize(map);
  if (check?.text === text && check.state !== "failed") {
    // This text has a check already, for example after reopening the same
    // file, which cleared the problems on screen. Show that check's problems.
    const problems = problemsFor(text, check, shownErrors);
    if (problems !== shownErrors) {
      shownErrors = problems;
      renderErrors();
    }
    return;
  }
  check = { text, state: "pending" };
  clearTimeout(checkTimer);
  checkTimer = setTimeout(() => runCheck(text), immediate ? 0 : checkDelay);
}

async function runCheck(text) {
  let result;
  try {
    const { valid, errors } = await checkMap(text);
    result = { text, state: valid ? "valid" : "invalid", errors };
  } catch (error) {
    result = { text, state: "failed", errors: [], message: `Couldn't reach the server: ${error.message}.` };
  }
  // A newer edit has its own check coming.
  if (check?.text !== text) return;
  check = result;
  if (result.state !== "failed") shownErrors = result.errors;
  renderErrors();
  renderCheck();
}

function renderCheck() {
  const gate = downloadGate(serialize(map), check);
  downloadButton.disabled = !gate.ready;
  byID("download-reason").textContent = gate.reason;
  const status = byID("check-status");
  const state = check?.state ?? "pending";
  status.className = `check-status ${state}`;
  status.textContent = {
    pending: "Checking the map…",
    valid: "The map is valid and ready to download.",
    invalid: "The map isn't valid yet.",
    failed: "The map couldn't be checked.",
  }[state];
  byID("check-again").hidden = state !== "failed";
  renderLobby();
}

// ---- Lobby hand-off

// renderLobby shows Use in lobby only on a page that a same-origin window
// opened, such as the lobby's create form, and says why it is disabled.
function renderLobby() {
  const opener = lobbyPageState();
  if (opener === "closed") lobbyPageClosed = true;
  const text = serialize(map);
  const gate = lobbyGate(text, check);
  lobbyButton.hidden = opener !== "ready";
  lobbyButton.disabled = !gate.ready;
  // The download's reason already covers a map that isn't checked or valid.
  byID("lobby-reason").textContent = opener === "ready" && downloadGate(text, check).ready ? gate.reason : "";
  byID("lobby-status").textContent = lobbyPageClosed
    ? "The lobby page is closed, so download the map and load it there instead."
    : opener === "ready" && sentText === text ? "Sent to the lobby. You can close this tab or keep editing." : "";
}

lobbyButton.addEventListener("click", () => {
  const text = serialize(map);
  if (!lobbyGate(text, check).ready) return;
  if (lobbyPageState() === "ready") {
    lobbyPage.postMessage(lobbyMessage(text), location.origin);
    sentText = text;
  }
  renderLobby();
});

// A lobby form that reloaded names itself as the page to send maps to.
window.addEventListener("message", (event) => {
  const page = lobbyPageFromMessage(event, location.origin, window);
  if (!page) return;
  lobbyPage = page;
  lobbyPageClosed = false;
  renderLobby();
});

// A closed lobby page raises no event on this page, so look again now and then.
setInterval(renderLobby, 1000);

function renderErrors() {
  const placed = placeProblems(shownErrors);

  for (const slot of mapFields.querySelectorAll("[data-error-for]")) slot.textContent = "";
  for (const input of mapFields.querySelectorAll("[data-map-field]")) input.removeAttribute("aria-invalid");
  const mapErrors = byID("map-errors");
  mapErrors.replaceChildren();
  for (const problem of placed.map) {
    const slot = mapFields.querySelector(`[data-error-for="${CSS.escape(problem.field)}"]`);
    if (slot) {
      slot.textContent = slot.textContent ? `${slot.textContent} ${problem.message}` : problem.message;
      mapFields.querySelector(`[data-map-field="${CSS.escape(problem.field)}"]`)?.setAttribute("aria-invalid", "true");
    } else {
      mapErrors.append(html("li", { text: problem.field ? `${fieldLabel(problem.field)}: ${problem.message}` : problem.message }));
    }
  }

  for (const card of castleList.children) {
    const index = Number(card.dataset.index);
    const problems = placed.castles.get(index) ?? [];
    card.classList.toggle("has-errors", problems.length > 0);
    for (const slot of card.querySelectorAll("[data-error-for]")) slot.textContent = "";
    for (const input of card.querySelectorAll("[aria-invalid]")) input.removeAttribute("aria-invalid");
    const general = card.querySelector("[data-castle-errors]");
    general.replaceChildren();
    for (const problem of problems) {
      const slot = problem.field && card.querySelector(`[data-error-for="${CSS.escape(problem.field)}"]`);
      if (slot) {
        slot.textContent = slot.textContent ? `${slot.textContent} ${problem.message}` : problem.message;
        card.querySelector(`[data-field="${CSS.escape(problem.field)}"]`)?.setAttribute("aria-invalid", "true");
      } else {
        general.append(html("li", { text: problem.field ? `${fieldLabel(problem.field)}: ${problem.message}` : problem.message }));
      }
    }
  }
  for (const [index, problems] of placed.castles) {
    if (index >= map.castles.length) for (const problem of problems) mapErrors.append(html("li", { text: `Castle ${index}: ${problem.message}` }));
  }

  const list = byID("problem-list");
  list.replaceChildren(...shownErrors.map((error, position) => {
    const text = describeProblems([error])[0];
    const index = problemCastle(error);
    if (index === null || index >= map.castles.length) return html("li", { text });
    const button = html("button", { type: "button", text, "data-problem": position });
    button.addEventListener("click", () => {
      select(index, { reveal: true });
      castleCard(index)?.querySelector("input, select")?.focus();
    });
    return html("li", {}, button);
  }));
  renderPlane();
}

// ---- Editing

function changed() {
  renderPlane();
  renderTeamHint();
  scheduleCheck();
  renderCheck();
}

function replaceMap(nextMap) {
  map = nextMap;
  selected = null;
  shownErrors = [];
  syncMapFields();
  renderCastles();
  renderErrors();
  changed();
}

// select highlights a castle. reveal scrolls its card into view, which a drag
// on the plane must not do, because the page would move under the pointer.
function select(index, { reveal = false } = {}) {
  selected = index;
  for (const card of castleList.children) card.classList.toggle("selected", Number(card.dataset.index) === index);
  if (reveal && index !== null) castleCard(index)?.scrollIntoView({ block: "nearest" });
  renderPlane();
}

function addCastle(position) {
  map.castles.push({ team_color: 0, x: position.x, y: position.y });
  renderCastles();
  renderErrors();
  select(map.castles.length - 1, { reveal: true });
  changed();
}

function deleteCastle(index) {
  map.castles.splice(index, 1);
  shownErrors = withoutCastle(shownErrors, index);
  selected = null;
  renderCastles();
  renderErrors();
  changed();
}

function moveCastle(index, position) {
  const castle = map.castles[index];
  if (castle.x === position.x && castle.y === position.y) return;
  castle.x = position.x;
  castle.y = position.y;
  const card = castleCard(index);
  card.querySelector('[data-field="x"]').value = position.x;
  card.querySelector('[data-field="y"]').value = position.y;
  changed();
}

function setUpgrade(castle, type, level) {
  const levels = typeof castle.upgrades === "object" && castle.upgrades !== null && !Array.isArray(castle.upgrades) ? castle.upgrades : {};
  if (level === null) delete levels[type];
  else levels[type] = level;
  if (Object.keys(levels).length === 0) delete castle.upgrades;
  else castle.upgrades = levels;
}

function syncMapFields() {
  for (const input of mapFields.querySelectorAll("[data-map-field]")) {
    const value = map[input.dataset.mapField];
    input.value = value === null || value === undefined ? "" : value;
  }
}

function renderTeamHint() {
  const hint = byID("team-hint");
  const missing = missingTeams(map);
  hint.hidden = missing.length === 0;
  if (missing.length === 0) return;
  const shown = missing.length > 8 ? [...missing.slice(0, 8).map(String), "more"] : missing.map(String);
  hint.textContent = `${missing.length === 1 ? "Team" : "Teams"} ${listWords(shown)} ${missing.length === 1 ? "owns" : "own"} no castle. ` +
    "A map for N teams, from 2 to 16 with one player each, needs a castle of every team colour from 1 to N, so give every team a castle.";
}

mapFields.addEventListener("input", (event) => {
  const field = event.target.dataset.mapField;
  if (!field) return;
  map[field] = field === "map_name" ? event.target.value : numberFrom(event.target);
  // Team choices and default soldier placeholders depend on these.
  if (field === "player_amount" || field === "neutral_starting_soldier_amount") {
    renderCastles();
    renderErrors();
  }
  changed();
});

// ---- Castle cards

const castleCard = (index) => castleList.querySelector(`[data-index="${index}"]`);

function teamOptions(castle) {
  const players = Number.isInteger(map.player_amount) && map.player_amount > 0 ? Math.min(map.player_amount, 99) : 0;
  const teams = [0, ...Array.from({ length: players }, (_, offset) => offset + 1)];
  const options = [];
  // A file can lack a team or name one this map doesn't have. Show it as it
  // is, so the server's problem matches what the select shows.
  if (castle.team_color === undefined) options.push(html("option", { value: "", selected: true, text: "Choose a team" }));
  else if (!teams.includes(castle.team_color)) teams.push(castle.team_color);
  return [...options, ...teams.map((team) => html("option", {
    value: JSON.stringify(team),
    selected: team === castle.team_color,
    text: team === 0 ? "Neutral" : Number.isInteger(team) && team <= players ? `Team ${team}` : `${JSON.stringify(team)} (not a team of this map)`,
  }))];
}

function upgradeRows(castle) {
  const levels = typeof castle.upgrades === "object" && castle.upgrades !== null && !Array.isArray(castle.upgrades) ? castle.upgrades : {};
  const rows = upgrades.map(({ type, max_level: maxLevel }) => {
    const current = levels[type] ?? 1;
    const choices = Array.from({ length: maxLevel }, (_, offset) => offset + 1);
    if (!choices.includes(current)) choices.push(current);
    const select = html("select", { "data-upgrade": type, "data-field": `upgrades.${type}`, "aria-label": `${upgradeLabel(type)} level` },
      ...choices.map((level) => html("option", { value: JSON.stringify(level), selected: level === current, text: level === 1 ? "Level 1 (default)" : `Level ${level}` })));
    return html("div", {},
      html("label", { class: "upgrade-row" }, upgradeLabel(type), select),
      html("small", { class: "field-error", "data-error-for": `upgrades.${type}` }));
  });
  const known = new Set(upgrades.map(({ type }) => type));
  for (const type of Object.keys(levels).filter((type) => !known.has(type))) {
    rows.push(html("div", {},
      html("div", { class: "upgrade-row" }, `${type}: ${JSON.stringify(levels[type])}`, html("button", { type: "button", class: "danger", "data-remove-upgrade": type, text: "Remove" })),
      html("small", { class: "field-error", "data-error-for": `upgrades.${type}` })));
  }
  return rows;
}

function castleCardFor(castle, index) {
  const valueOf = (value) => (value === null || value === undefined ? "" : String(value));
  const defaultSoldiers = Number.isFinite(map.neutral_starting_soldier_amount) ? `Default (${map.neutral_starting_soldier_amount})` : "Default";
  const card = html("li", { class: "castle-card", "data-index": index },
    html("header", {},
      html("h3", {}, html("button", { type: "button", "data-select": "", text: `Castle ${index}` })),
      html("button", { type: "button", class: "danger", "data-delete": "", "aria-label": `Delete castle ${index}`, text: "Delete" })),
    html("ul", { class: "card-errors", "data-castle-errors": "" }),
    html("div", { class: "castle-fields" },
      html("label", { class: "wide" }, "Team",
        html("select", { "data-field": "team_color" }, ...teamOptions(castle)),
        html("small", { class: "field-error", "data-error-for": "team_color" })),
      html("label", {}, "X",
        html("input", { type: "number", step: "any", "data-field": "x", value: valueOf(castle.x) }),
        html("small", { class: "field-error", "data-error-for": "x" })),
      html("label", {}, "Y",
        html("input", { type: "number", step: "any", "data-field": "y", value: valueOf(castle.y) }),
        html("small", { class: "field-error", "data-error-for": "y" })),
      html("label", { class: "wide" }, "Starting soldiers",
        html("input", { type: "number", min: "0", step: "1", inputmode: "numeric", placeholder: defaultSoldiers, "data-field": "starting_soldier_amount", value: valueOf(castle.starting_soldier_amount) }),
        html("small", { class: "field-error", "data-error-for": "starting_soldier_amount" })),
      html("div", { class: "wide upgrades" },
        ...upgradeRows(castle),
        html("small", { class: "field-error", "data-error-for": "upgrades" }))));
  card.style.setProperty("--team", teamColor(castle.team_color));
  card.classList.toggle("selected", index === selected);
  return card;
}

function renderCastles() {
  castleList.replaceChildren(...map.castles.map(castleCardFor));
  byID("castle-count").textContent = `(${map.castles.length})`;
}

castleList.addEventListener("input", (event) => {
  const card = event.target.closest("[data-index]");
  if (!card) return;
  const index = Number(card.dataset.index);
  const castle = map.castles[index];
  const { field, upgrade } = event.target.dataset;
  if (upgrade) {
    const level = JSON.parse(event.target.value);
    setUpgrade(castle, upgrade, level === 1 ? null : level);
  } else if (field === "team_color") {
    if (event.target.value === "") delete castle.team_color;
    else castle.team_color = JSON.parse(event.target.value);
    card.style.setProperty("--team", teamColor(castle.team_color));
  } else if (field === "starting_soldier_amount") {
    const soldiers = numberFrom(event.target);
    if (soldiers === null) delete castle.starting_soldier_amount;
    else castle.starting_soldier_amount = soldiers;
  } else if (field === "x" || field === "y") {
    castle[field] = numberFrom(event.target);
  } else {
    return;
  }
  changed();
});

castleList.addEventListener("click", (event) => {
  const card = event.target.closest("[data-index]");
  if (!card) return;
  const index = Number(card.dataset.index);
  if (event.target.closest("[data-delete]")) {
    deleteCastle(index);
    return;
  }
  const removed = event.target.closest("[data-remove-upgrade]");
  if (removed) {
    setUpgrade(map.castles[index], removed.dataset.removeUpgrade, null);
    renderCastles();
    renderErrors();
    changed();
    return;
  }
  if (event.target.closest("[data-select]")) select(index);
});

castleList.addEventListener("focusin", (event) => {
  const card = event.target.closest("[data-index]");
  if (card && Number(card.dataset.index) !== selected) select(Number(card.dataset.index));
});

byID("add-castle").addEventListener("click", () => addCastle(freeSpot(map)));

// ---- Plane

function computeView() {
  const positioned = map.castles.filter(isPosition);
  let left = -8;
  let right = 8;
  let top = -5;
  let bottom = 5;
  if (isPositive(map.plane_size_x)) [left, right] = [-map.plane_size_x / 2, map.plane_size_x / 2];
  if (isPositive(map.plane_size_y)) [top, bottom] = [-map.plane_size_y / 2, map.plane_size_y / 2];
  for (const castle of positioned) {
    left = Math.min(left, castle.x);
    right = Math.max(right, castle.x);
    top = Math.min(top, castle.y);
    bottom = Math.max(bottom, castle.y);
  }
  const extent = Math.max(right - left, bottom - top);
  // Castles are drawn at their replay size, or larger on big planes so that
  // they stay visible and easy to grab.
  const radius = Math.max(castleRadius, extent / 50);
  const margin = radius * 2;
  return { left: left - margin, top: top - margin, width: right - left + 2 * margin, height: bottom - top + 2 * margin, extent, radius };
}

function gridStep(extent) {
  const rough = extent / 20;
  const power = 10 ** Math.floor(Math.log10(Math.max(rough, 1e-9)));
  return [1, 2, 5, 10].map((factor) => factor * power).find((step) => step >= rough) ?? 10 * power;
}

// The background stays in place until the plane or the view changes, so a
// double-click on an empty spot lands on the same element twice.
const background = svg("g", { "data-layer": "background" });
const castleLayer = svg("g", { "data-layer": "castles" });
plane.append(background, castleLayer);
let backgroundKey = "";

function renderBackground(view, line) {
  const key = JSON.stringify([view, map.plane_size_x, map.plane_size_y]);
  if (key === backgroundKey) return;
  backgroundKey = key;
  plane.setAttribute("viewBox", `${view.left} ${view.top} ${view.width} ${view.height}`);
  background.replaceChildren();
  if (!isPositive(map.plane_size_x) || !isPositive(map.plane_size_y)) return;
  const halfWidth = map.plane_size_x / 2;
  const halfHeight = map.plane_size_y / 2;
  background.append(svg("rect", { x: -halfWidth, y: -halfHeight, width: map.plane_size_x, height: map.plane_size_y, fill: "#fff", stroke: "#b7c0cf", "stroke-width": line, "data-kind": "plane" }));
  const step = gridStep(view.extent);
  const path = [];
  for (let x = Math.ceil(-halfWidth / step) * step; x <= halfWidth; x += step) path.push(`M${x} ${-halfHeight}V${halfHeight}`);
  for (let y = Math.ceil(-halfHeight / step) * step; y <= halfHeight; y += step) path.push(`M${-halfWidth} ${y}H${halfWidth}`);
  if (path.length < 2000) background.append(svg("path", { d: path.join(""), stroke: "#eef1f6", "stroke-width": line, fill: "none", "pointer-events": "none" }));
  background.append(svg("path", { d: `M${-halfWidth} 0H${halfWidth}M0 ${-halfHeight}V${halfHeight}`, stroke: "#d5dbe5", "stroke-width": line, fill: "none", "pointer-events": "none" }));
}

function renderPlane() {
  const view = drag?.view ?? computeView();
  const hadFocus = plane.contains(document.activeElement);
  const line = view.radius / 12;
  renderBackground(view, line);
  castleLayer.replaceChildren();

  const placed = placeProblems(shownErrors);
  map.castles.forEach((castle, index) => {
    if (!isPosition(castle)) return;
    const color = teamColor(castle.team_color);
    const soldiers = castle.starting_soldier_amount ?? map.neutral_starting_soldier_amount;
    const team = castle.team_color === 0 ? "neutral" : `team ${castle.team_color}`;
    const group = svg("g", {
      class: "castle", "data-castle": index, tabindex: 0, role: "button",
      transform: `translate(${castle.x} ${castle.y})`,
      "aria-label": `Castle ${index}, ${team}, at ${castle.x}, ${castle.y}`,
    });
    if (placed.castles.has(index)) group.append(svg("circle", { r: view.radius * 1.3, fill: "none", stroke: "#c2412d", "stroke-width": line * 1.5, "stroke-dasharray": `${line * 3} ${line * 2}`, "data-kind": "castle-error" }));
    if (index === selected) group.append(svg("circle", { r: view.radius * 1.55, fill: "none", stroke: "#3464e0", "stroke-width": line * 1.5, "data-kind": "castle-selected" }));
    group.append(
      svg("circle", { r: view.radius, fill: "#fff", stroke: color, "stroke-width": line * 2.5 }),
      svg("text", { class: "castle-label", y: view.radius * 0.3, "text-anchor": "middle", "font-size": view.radius * 0.85, fill: color }, soldiers ?? ""),
      svg("text", { class: "castle-id", y: view.radius * 2.05, "text-anchor": "middle", "font-size": view.radius * 0.6 }, `#${index}`),
    );
    castleLayer.append(group);
  });
  if (hadFocus && selected !== null) plane.querySelector(`[data-castle="${selected}"]`)?.focus({ preventScroll: true });
}

function worldPoint(event) {
  const matrix = plane.getScreenCTM()?.inverse();
  if (!matrix) return null;
  return new DOMPoint(event.clientX, event.clientY).matrixTransform(matrix);
}

plane.addEventListener("pointerdown", (event) => {
  const castleElement = event.target.closest("[data-castle]");
  if (!castleElement) {
    if (event.detail < 2) select(null);
    return;
  }
  const index = Number(castleElement.dataset.castle);
  drag = { index, pointerId: event.pointerId, view: computeView() };
  plane.setPointerCapture(event.pointerId);
  select(index);
  plane.querySelector(`[data-castle="${index}"]`)?.focus({ preventScroll: true });
  event.preventDefault();
});

plane.addEventListener("pointermove", (event) => {
  if (!drag || event.pointerId !== drag.pointerId) return;
  const point = worldPoint(event);
  if (point) moveCastle(drag.index, dragPosition(map, point.x, point.y));
});

function endDrag(event) {
  if (!drag || event.pointerId !== drag.pointerId) return;
  drag = null;
  if (plane.hasPointerCapture(event.pointerId)) plane.releasePointerCapture(event.pointerId);
  renderPlane();
}
plane.addEventListener("pointerup", endDrag);
plane.addEventListener("pointercancel", endDrag);

plane.addEventListener("dblclick", (event) => {
  if (event.target.closest("[data-castle]")) return;
  const point = worldPoint(event);
  if (point) addCastle(dragPosition(map, point.x, point.y));
});

plane.addEventListener("keydown", (event) => {
  const castleElement = event.target.closest("[data-castle]");
  if (!castleElement) return;
  const index = Number(castleElement.dataset.castle);
  const step = event.shiftKey ? 1 : 0.1;
  const moves = { ArrowLeft: [-step, 0], ArrowRight: [step, 0], ArrowUp: [0, -step], ArrowDown: [0, step] };
  if (event.key in moves) {
    const [dx, dy] = moves[event.key];
    const castle = map.castles[index];
    moveCastle(index, dragPosition(map, castle.x + dx, castle.y + dy));
    event.preventDefault();
  } else if (event.key === "Delete") {
    // Only Delete, not Backspace, which is too easy to press by accident.
    deleteCastle(index);
    event.preventDefault();
  } else if (event.key === "Enter") {
    castleCard(index)?.querySelector("select, input")?.focus();
    event.preventDefault();
  }
});

// ---- Files

downloadButton.addEventListener("click", () => {
  const text = serialize(map);
  if (!downloadGate(text, check).ready) return;
  const url = URL.createObjectURL(new Blob([text], { type: "application/json" }));
  const link = html("a", { href: url, download: fileName(map) });
  document.body.append(link);
  link.click();
  link.remove();
  setTimeout(() => URL.revokeObjectURL(url), 1000);
});

byID("open-button").addEventListener("click", () => openInput.click());

openInput.addEventListener("change", async () => {
  const [file] = openInput.files;
  openInput.value = "";
  if (!file) return;
  const text = await file.text();
  let opened;
  try {
    opened = openMapFile(text);
  } catch (error) {
    showNotice(`Couldn't open ${file.name}. ${error.message}`);
    return;
  }
  replaceMap(opened);
  showNotice(`Opened ${file.name}.`, "info");
  if (text === serialize(opened)) return;
  // The editor may have dropped keys it can't show, so check the file as it was.
  try {
    const result = await checkMap(text);
    if (!result.valid) {
      showNotice(`${file.name} didn't pass the check as it was:\n${describeProblems(result.errors).map((problem) => `• ${problem}`).join("\n")}\n` +
        "The editor kept the keys it knows and dropped any others. Its own check is above the plane.");
    }
  } catch {
    // The editor's own check reports an unreachable server.
  }
});

byID("check-again").addEventListener("click", () => {
  scheduleCheck({ immediate: true });
  renderCheck();
});

document.addEventListener("keydown", (event) => {
  if (event.key !== "Delete" || selected === null || event.defaultPrevented) return;
  if (event.target.closest("input, select, textarea, [contenteditable]")) return;
  deleteCastle(selected);
});

// ---- Start

async function loadUpgrades() {
  try {
    const response = await fetch("./upgrades.json", { headers: { Accept: "application/json" } });
    if (!response.ok) throw new Error(`the server answered ${response.status}`);
    upgrades = await response.json();
  } catch (error) {
    showNotice(`Couldn't load the upgrade types, so castle upgrades can't be edited: ${error.message}.`);
  }
}

await loadUpgrades();
replaceMap(map);
