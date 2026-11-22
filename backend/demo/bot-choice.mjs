// Island Battles' bot choice for the lobby frontend's waiting room. Every
// member chooses their bot before getting ready, in their lobby member
// settings, {"bot": ...}, which the page saves with the lobby's
// PUT …/member-settings: their localhost bot, which island-battles-bot serve
// runs on their machine and their game client relays turns to, or a default
// bot level that the backend plays itself. When the server offers them
// (server_bots in GET /bots/defaults), also their hosted bot at a URL or one
// of their uploaded packages, which the server plays without the page. The
// room lists every member's choice from the snapshot's member_settings, and
// each bot seat's bot from its seat_settings. The host can also seat a bot
// on a team, through the lobby page's seat controls, which seatOptions
// fills: among them localhost bots on the host's machine, each on its own
// port, which only the page of the host who added them relays. Below the
// choice, bot-packages.mjs lists the lobby's bot packages and uploads the
// player's.

import { describePackage, PackagesPanel, packagesPath } from "./bot-packages.mjs";
import { checkBotHealth, defaultBotPort, nextFreePort, parseBotPort, portProblem, serveCommandFor } from "./localhost-bot.mjs";

// How often the room asks the localhost bot's server whether it is running
// while the player has chosen it.
export const healthPollMilliseconds = 2000;

// The default bot levels the select offers when GET /bots/defaults can't be
// read.
const fallbackLevels = [1, 2, 3];

// The option for playing by hand, which only the 3D client can choose: listed
// here disabled, so a member who chose it there sees it as their choice.
function manualOption() {
  const option = new Option("Play by hand (3D client only)", "manual");
  option.disabled = true;
  return option;
}

// The select's value for a choice, "" for none. A hosted bot's URL is not in
// it.
export function choiceValue(bot) {
  if (!bot) return "";
  switch (bot.kind) {
    case "localhost": return "localhost";
    case "manual": return "manual";
    case "hosted": return "hosted";
    case "uploaded": return `package-${bot.package_id}`;
    default: return `level-${bot.level}`;
  }
}

// A hosted bot's URL as typed: trimmed, with https:// in front when it names no
// scheme, as the web client's botUrlFromInput does. A typed scheme stays;
// "https:/" or "https//" are read as "https://". Only slashes is empty.
export function botUrlFromInput(text) {
  const url = text.trim();
  if (/^[a-z][a-z0-9+.-]*:\/\//i.test(url)) return url;
  const typo = /^(https?)(?::\/*|\/+)(?=[^/])/i.exec(url);
  if (typo) return `${typo[1]}://${url.slice(typo[0].length)}`;
  const rest = url.replace(/^\/+/, "");
  return rest === "" ? "" : `https://${rest}`;
}

// The choice a select value stands for, or null for none. "hosted" takes the
// URL typed next to it, and is null while that is empty. "localhost" takes
// the port typed next to it, which is left out when it is the default one.
export function choiceFromValue(value, url = "", port = defaultBotPort) {
  if (value === "localhost") return port === defaultBotPort ? { kind: "localhost" } : { kind: "localhost", port };
  if (value === "hosted") {
    const full = botUrlFromInput(url);
    return full ? { kind: "hosted", url: full } : null;
  }
  if (value.startsWith("package-") && value.length > "package-".length) return { kind: "uploaded", package_id: value.slice("package-".length) };
  const level = /^level-([1-9][0-9]*)$/.exec(value);
  return level ? { kind: "default", level: Number(level[1]) } : null;
}

// The host name of a hosted bot's URL, or the URL when it can't be read.
export function urlHost(url) {
  try {
    return new URL(url).hostname || url;
  } catch {
    return url;
  }
}

// A few words naming a member's bot. A localhost bot seat's names the person
// who relays it, relayer, or says that the start is refused when relayer is null: they left.
export function describeBot(bot, relayer) {
  if (!bot) return "no bot yet";
  switch (bot.kind) {
    case "localhost":
      if (bot.relayed && relayer === null) return `localhost bot on port ${bot.port}, whoever added it left, so the start is refused until it is removed`;
      if (bot.relayed) return `localhost bot on ${relayer ? `${relayer}'s` : "the host's"} port ${bot.port}`;
      return bot.port && bot.port !== defaultBotPort ? `own bot (localhost:${bot.port})` : "own bot (localhost)";
    case "hosted": return `hosted bot (${urlHost(bot.url ?? "")})`;
    case "uploaded": return "uploaded bot";
    case "manual": return "plays by hand";
    default: return `level ${bot.level} bot`;
  }
}

// A snapshot member's bot: a person's from their member settings, a bot
// seat's from its seat settings (a default bot of its level, a localhost bot
// on a port of the machine of the host who added it, relayed: true, with
// relay, their player ID, a hosted bot's URL or an uploaded package), or null
// for a person who hasn't chosen.
export function memberBot(member) {
  if (member.kind === "seat") {
    const settings = member.seat_settings ?? {};
    if (Number.isInteger(settings.bot_level)) return { kind: "default", level: settings.bot_level };
    const port = parseBotPort(settings.bot_localhost_port);
    if (port !== null) return { kind: "localhost", port, relayed: true, relay: settings.bot_relay_player_id ?? null };
    if (typeof settings.bot_url === "string") return { kind: "hosted", url: settings.bot_url };
    if (typeof settings.bot_package_id === "string") return { kind: "uploaded", package_id: settings.bot_package_id };
    return null;
  }
  const bot = member.member_settings?.bot;
  return bot && typeof bot.kind === "string" ? bot : null;
}

// The display name of the person in the lobby who relays a localhost bot
// seat, bot, or null when they left: only they may relay it, so the start is refused.
export function seatRelayer(members, bot) {
  const relayer = members.find((member) => member.player_id === bot.relay && member.kind !== "seat" && member.active !== false);
  return relayer ? relayer.display_name : null;
}

// One line listing every member's bot, such as "Alex: own bot (localhost) ·
// Sam (you): level 2 bot", from the snapshot's members.
export function describeChoices(members, playerID) {
  return members.map((member) => {
    const bot = memberBot(member);
    const relayer = bot?.relayed ? seatRelayer(members, bot) : undefined;
    return `${member.display_name}${member.player_id === playerID ? " (you)" : ""}: ${describeBot(bot, relayer)}`;
  }).join(" · ");
}

// The default bots' levels, and whether the server offers hosted and
// uploaded bots, loaded once per page from the game's site, gameURL (the
// page's own origin without one).
const defaultBots = new Map();

function loadOptions(gameURL = "") {
  if (!defaultBots.has(gameURL)) {
    defaultBots.set(gameURL, (async () => {
      const response = await fetch(`${gameURL}/bots/defaults`, { headers: { Accept: "application/json" } });
      if (!response.ok) throw new Error(`HTTP ${response.status}`);
      const body = await response.json();
      return { levels: body.bots.map((bot) => bot.level), serverBots: body.server_bots === true };
    })().catch(() => {
      defaultBots.delete(gameURL);
      return { levels: fallbackLevels, serverBots: false };
    }));
  }
  return defaultBots.get(gameURL);
}

const monthNames = ["January", "February", "March", "April", "May", "June", "July", "August", "September", "October", "November", "December"];

// Why uploaded bots can't be chosen, once the bot runners have used the
// month's compute (GET /game/bot-compute), such as "Uploaded bots have used
// this month's free compute. They're back on 1 December.", the sentence of
// the server's refused start; null while they can.
export function computeSpentMessage(compute) {
  if (!compute || compute.available !== false) return null;
  const month = Number(/^\d{4}-(\d{2})$/.exec(String(compute.month))?.[1]);
  const back = month >= 1 && month <= 12 ? `on 1 ${monthNames[month % 12]}` : "next month";
  return `Uploaded bots have used this month's free compute. They're back ${back}.`;
}

// Whether games with uploaded bots may start this month, read when a room is
// shown; null when it can't be read, which greys out nothing: the start
// refuses them all the same.
async function loadCompute(gameURL = "") {
  try {
    const response = await fetch(`${gameURL}/game/bot-compute`, { headers: { Accept: "application/json" } });
    return response.ok ? await response.json() : null;
  } catch {
    return null;
  }
}

// The lobby's packages, newest first, through the room's request.
async function loadPackages(lobbyID, request) {
  if (!lobbyID || !request) return [];
  const answer = await request("GET", packagesPath(lobbyID));
  return answer.status === 200 ? answer.body.packages : [];
}

// The player of each room panel, by lobby ID: the lobby page asks for the
// seat options without saying who asks, after it builds the room's panel.
const roomPlayers = new Map();

// The bot seats that the host can add to a team, whose seat_settings the
// lobby page sends with POST …/seats: a default bot of each level, which the
// backend plays, a localhost bot on the port of the host's machine that the
// host types, which only the host's own page relays, as the seat's
// bot_relay_player_id, playerID, records, and, when the server offers them,
// any package of the lobby and a hosted bot at the URL that the host types,
// which the server plays. The packages are greyed out (disabled) once the
// month's compute is spent. Without the player's ID, from the room's panel,
// there's no localhost bot seat.
export async function seatOptions({ lobbyID, request, gameURL = "", playerID = roomPlayers.get(lobbyID), options = loadOptions, packages = loadPackages, compute = loadCompute } = {}) {
  const { levels, serverBots } = await options(gameURL);
  const choices = levels.map((level) => ({ label: `Default bot, level ${level}`, settings: { bot_level: level } }));
  if (playerID) {
    choices.push({ label: "My localhost bot (port)", settings: { bot_relay_player_id: playerID }, input: { name: "bot_localhost_port", label: "Port", placeholder: String(defaultBotPort + 1) } });
  }
  if (serverBots) {
    const [items, usage] = await Promise.all([packages(lobbyID, request).catch(() => []), compute(gameURL).catch(() => null)]);
    const spent = computeSpentMessage(usage) !== null;
    for (const item of items) {
      choices.push({ label: `Uploaded: ${item.owner_name}'s bot (${item.language})`, settings: { bot_package_id: item.package_id }, ...(spent ? { disabled: true } : {}) });
    }
    choices.push({ label: "Hosted bot (URL)", settings: {}, input: { name: "bot_url", label: "Bot URL", placeholder: "https://bot.example.com/turn" } });
  }
  return { label: "Add bot", choices };
}

// What the localhost bot's indicator says.
export function describeHealth(running, port = defaultBotPort) {
  return running ? `Connected: your bot is running on 127.0.0.1:${port}.` : "Not running: start your bot with the command below.";
}

// The localhost bot seats of a snapshot: each active seat with
// bot_localhost_port, {name, port, relay}, where relay is the player ID of
// the host who added it, whose page alone relays it, in the order of the
// snapshot's members.
export function localhostSeats(snapshot) {
  return (snapshot?.members ?? []).flatMap((member) => {
    if (member.kind !== "seat" || member.active === false) return [];
    const bot = memberBot(member);
    return bot?.kind === "localhost" ? [{ name: member.display_name, port: bot.port, relay: bot.relay }] : [];
  });
}

// The styles of the panel's boxes for the localhost bot, the hosted bot and
// the packages, and of the localhost bot's indicator. The generic lobby
// page's stylesheet knows nothing of bots, so the panel adds them to the page
// once.
export const panelStylesID = "island-battles-bot-choice-styles";
export const panelStyles = `
.local-bot { margin: 10px 0; }
.local-bot code { display: inline-block; margin: 0 6px 0 2px; padding: 3px 8px; background: #eef1f7; border-radius: 6px; font-size: .82rem; overflow-wrap: anywhere; }
.bot-health { margin: 0 0 6px; font-weight: 600; }
.bot-health::before { content: "●"; margin-right: 6px; color: #9aa3b5; }
.bot-health[data-state="connected"]::before { color: #1f9d55; }
.bot-health[data-state="stopped"]::before { color: #d64545; }
.local-bot input.bot-port { width: 7em; }
.local-seats { margin: 10px 0; }
.local-seats .bot-health { font-weight: 400; }
`;

export function addPanelStyles(page = globalThis.document) {
  if (!page?.head || page.getElementById(panelStylesID)) return;
  const style = page.createElement("style");
  style.id = panelStylesID;
  style.textContent = panelStyles;
  page.head.append(style);
}

// The note for browsers that block a page from reaching localhost.
export const safariNote = "Safari does not let an https page reach a bot on localhost. Use Chrome, Edge or Firefox to play with your own bot, or open this page over http on localhost.";

// The player's bot select and every member's choice, shown while the lobby
// waits. saveMemberSettings(settings) saves the player's member settings
// through the page and resolves to the snapshot. With the localhost bot
// chosen, it also shows whether that bot is running. When the server offers
// them, the select also has "My hosted bot (URL)", with a field for the URL,
// and "Uploaded: …" for each of the player's packages; the page's
// refreshSeatOptions is called when the lobby's packages change, so the
// host's Add bot offers them. Once the month's compute is spent, the
// uploaded choices are greyed out, with the sentence that says why.
export class RoomPanel {
  constructor({ container, lobbyID, playerID, saveMemberSettings, request, refreshSeatOptions = () => {}, gameURL = globalThis.location?.origin ?? "", checkHealth = checkBotHealth, copy = (text) => navigator.clipboard.writeText(text), postToStorage, options = loadOptions, compute = loadCompute }) {
    addPanelStyles();
    this.container = container;
    this.checkHealth = checkHealth;
    this.copy = copy;
    this.polling = null;
    this.lobbyID = lobbyID;
    this.playerID = playerID;
    roomPlayers.set(lobbyID, playerID);
    this.saveMemberSettings = saveMemberSettings;
    this.refreshSeatOptions = refreshSeatOptions;
    this.chosen = null;
    this.saving = false;
    this.serverBots = false;
    // The lobby's packages as last listed, and the player's own among them.
    this.lobbyPackages = [];
    this.ownPackages = [];
    // The select shows "hosted" while the player types a URL to save.
    this.typingURL = false;
    this.select = document.createElement("select");
    this.select.append(new Option("Choose your bot", ""), new Option("My localhost bot", "localhost"), manualOption());
    this.levelOptions = [];
    const label = document.createElement("label");
    label.append("Your bot", this.select);
    // Only the 3D client plays by hand, so this page lists the option disabled.
    const manualHint = document.createElement("p");
    manualHint.className = "hint";
    manualHint.textContent = "To play by hand, choose Play by hand in the 3D client; this page can't play a seat.";
    this.summary = document.createElement("p");
    this.summary.className = "hint";
    this.summary.setAttribute("role", "status");
    // Why uploaded bots are greyed out this month, or null while they aren't.
    this.computeSpent = null;
    this.spentNote = document.createElement("p");
    this.spentNote.className = "hint";
    this.spentNote.hidden = true;
    this.local = this.buildLocalHelp();
    this.hosted = this.buildHostedHelp();
    // The localhost bot seats that the player added, which they relay, each
    // with its own indicator, and their ports' health as last read; and the
    // seats whose adder left, which stop the start.
    this.seats = [];
    this.orphanSeats = [];
    this.seatHealth = new Map();
    this.localSeats = document.createElement("div");
    this.localSeats.className = "local-seats";
    this.localSeats.hidden = true;
    container.append(label, manualHint, this.spentNote, this.local, this.hosted, this.localSeats, this.summary);
    // The lobby's bot packages, which the page's room requests reach.
    this.packages = request ? new PackagesPanel({ container, lobbyID, playerID, request, gameURL, copy, onList: (items) => this.listed(items), ...(postToStorage ? { post: postToStorage } : {}) }) : null;
    this.select.addEventListener("change", () => {
      if (this.select.value === "hosted") {
        this.typingURL = true;
        this.restore();
        this.url.focus();
        return;
      }
      this.typingURL = false;
      void this.save(this.select.value);
    });
    void options(gameURL).then(({ levels, serverBots }) => {
      this.levelOptions = levels.map((level) => new Option(`Default bot, level ${level}`, `level-${level}`));
      this.serverBots = serverBots;
      this.packages?.setChoosable(serverBots);
      this.fillSelect();
      if (serverBots) {
        void compute(gameURL).catch(() => null).then((usage) => {
          this.computeSpent = computeSpentMessage(usage);
          this.spentNote.textContent = this.computeSpent ?? "";
          this.spentNote.hidden = this.computeSpent === null;
          if (this.computeSpent !== null) this.fillSelect();
        });
      }
    });
  }

  // The select's options: the localhost bot, the default levels, and, when
  // the server offers them, the hosted bot and the player's packages.
  fillSelect() {
    const options = [new Option("Choose your bot", ""), new Option("My localhost bot", "localhost"), manualOption(), ...this.levelOptions];
    if (this.serverBots) {
      options.push(new Option("My hosted bot (URL)", "hosted"));
      const uploaded = this.ownPackages.map((item) => new Option(`Uploaded: ${describePackage(item)}`, `package-${item.package_id}`));
      // A chosen package that was replaced or deleted stays shown until the
      // player chooses again.
      if (this.chosen?.kind === "uploaded" && !this.ownPackages.some((item) => item.package_id === this.chosen.package_id)) {
        uploaded.push(new Option("Uploaded: replaced or deleted, choose again", choiceValue(this.chosen)));
      }
      // Greyed out once the month's compute is spent; spentNote says why.
      for (const option of uploaded) option.disabled = this.computeSpent !== null;
      options.push(...uploaded);
    }
    this.select.replaceChildren(...options);
    this.restore();
  }

  // The packages panel listed the lobby's packages.
  listed(items) {
    const key = (list) => list.map((item) => item.package_id).join();
    if (key(items) === key(this.lobbyPackages)) return;
    this.lobbyPackages = items;
    this.ownPackages = items.filter((item) => item.owner_player_id === this.playerID);
    if (this.serverBots) {
      this.fillSelect();
      this.refreshSeatOptions();
    }
  }

  buildHostedHelp() {
    const box = document.createElement("div");
    box.className = "local-bot hosted-bot";
    box.hidden = true;
    const hint = document.createElement("p");
    hint.className = "hint";
    hint.textContent = "Your own bot server at a public https:// URL, which speaks the turn delivery protocol. The server sends it each turn; this page relays nothing.";
    this.url = document.createElement("input");
    // Text, not "url", so a URL typed without https:// isn't marked invalid.
    this.url.type = "text";
    this.url.inputMode = "url";
    this.url.setAttribute("autocapitalize", "off");
    this.url.setAttribute("autocorrect", "off");
    this.url.spellcheck = false;
    this.url.maxLength = 2048;
    this.url.placeholder = "https://bot.example.com/turn";
    this.url.setAttribute("aria-label", "Your hosted bot's URL");
    this.url.addEventListener("keydown", (event) => {
      if (event.key === "Enter") void this.save("hosted");
    });
    this.useURL = document.createElement("button");
    this.useURL.type = "button";
    this.useURL.className = "secondary";
    this.useURL.textContent = "Use this URL";
    this.useURL.addEventListener("click", () => void this.save("hosted"));
    const row = document.createElement("p");
    row.append(this.url, " ", this.useURL);
    box.append(hint, row);
    return box;
  }

  buildLocalHelp() {
    const box = document.createElement("div");
    box.className = "local-bot";
    box.hidden = true;
    this.health = document.createElement("p");
    this.health.className = "bot-health";
    this.health.setAttribute("role", "status");
    // The port the bot serves on, 41414 unless the player changes it.
    this.port = document.createElement("input");
    this.port.className = "bot-port";
    this.port.inputMode = "numeric";
    this.port.maxLength = 5;
    this.port.value = String(defaultBotPort);
    this.port.setAttribute("aria-label", "Your localhost bot's port");
    this.port.addEventListener("keydown", (event) => {
      if (event.key === "Enter") void this.save("localhost");
    });
    this.usePort = document.createElement("button");
    this.usePort.type = "button";
    this.usePort.className = "secondary";
    this.usePort.textContent = "Use this port";
    this.usePort.addEventListener("click", () => void this.save("localhost"));
    const portRow = document.createElement("p");
    portRow.className = "hint";
    portRow.append("Port on 127.0.0.1: ", this.port, " ", this.usePort);
    const run = document.createElement("p");
    run.className = "hint";
    run.append("Run this on your computer, in the folder of your bot: ");
    const command = document.createElement("code");
    command.textContent = serveCommandFor();
    this.command = command;
    this.copyButton = document.createElement("button");
    this.copyButton.type = "button";
    this.copyButton.className = "secondary";
    this.copyButton.textContent = "Copy command";
    this.copyButton.addEventListener("click", () => void this.copyCommand());
    run.append(command, " ", this.copyButton);
    const safari = document.createElement("p");
    safari.className = "hint";
    safari.textContent = safariNote;
    box.append(this.health, portRow, run, safari);
    this.setHealth(null);
    return box;
  }

  // The saved localhost bot's port, the default one for any other bot.
  chosenPort() {
    return this.chosen?.kind === "localhost" && this.chosen.port ? this.chosen.port : defaultBotPort;
  }

  // The ports of the localhost bot seats that the player added and relays.
  seatPorts() {
    return this.seats.map((seat) => seat.port);
  }

  async copyCommand() {
    try {
      await this.copy(serveCommandFor(this.chosenPort()));
      this.copyButton.textContent = "Copied";
    } catch {
      this.copyButton.textContent = "Select the command to copy it";
    }
    setTimeout(() => { this.copyButton.textContent = "Copy command"; }, 2000);
  }

  // running is true, false, or null before the first answer.
  setHealth(running) {
    this.running = running;
    this.health.dataset.state = running === null ? "checking" : running ? "connected" : "stopped";
    this.health.textContent = running === null ? "Checking for your bot…" : describeHealth(running, this.chosenPort());
  }

  // The localhost bot seats that the player relays, each with its indicator
  // and the command that serves it, and a warning for a port taken twice;
  // and, for everyone, the seats whose adder left, which stop the start.
  showSeats() {
    const seats = this.seats;
    const orphans = this.orphanSeats.map((seat) => {
      const row = document.createElement("p");
      row.className = "hint seat-orphan";
      row.textContent = `${seat.name} can't play: the player who added it left, and only they can relay it, so the game won't start until the host removes it. The host can add it again from their own page.`;
      return row;
    });
    this.localSeats.hidden = seats.length === 0 && orphans.length === 0;
    if (seats.length === 0) {
      this.localSeats.replaceChildren(...orphans);
      return;
    }
    const heading = document.createElement("p");
    heading.className = "hint";
    heading.textContent = "Your localhost bot seats. This page relays each one's turns to its port, as it does your own bot's, so keep it open during the game:";
    const rows = seats.map((seat) => {
      const row = document.createElement("p");
      row.className = "bot-health";
      const running = this.seatHealth.get(seat.port);
      row.dataset.state = running === undefined ? "checking" : running ? "connected" : "stopped";
      const command = document.createElement("code");
      command.textContent = serveCommandFor(seat.port);
      row.append(`${seat.name}: ${running === undefined ? "checking…" : running ? "connected" : "not running"} `, command);
      return row;
    });
    const ports = [...(this.chosen?.kind === "localhost" ? [this.chosenPort()] : []), ...seats.map((seat) => seat.port)];
    const taken = ports.find((port, index) => ports.indexOf(port) !== index);
    if (taken !== undefined) {
      const warning = document.createElement("p");
      warning.className = "hint";
      warning.textContent = `Port ${taken} is used by two of your localhost bots, so the game won't start: change your own port, or remove a seat and add it on another port.`;
      rows.push(warning);
    }
    this.localSeats.replaceChildren(heading, ...rows, ...orphans);
  }

  // The localhost bot's help and indicator show for the saved choice, and the
  // bot is asked about while the room is shown. The hosted bot's URL field
  // shows for a saved hosted bot, and while the player types one.
  showLocal() {
    const local = this.chosen?.kind === "localhost" && !this.typingURL;
    this.local.hidden = !local;
    if (local && document.activeElement !== this.port) this.port.value = String(this.chosenPort());
    this.command.textContent = serveCommandFor(this.chosenPort());
    this.showSeats();
    if (!local && this.seatPorts().length === 0) this.stopPolling();
    else if (!this.polling) this.poll();
    this.hosted.hidden = !(this.typingURL || this.chosen?.kind === "hosted");
    if (this.chosen?.kind === "hosted" && !this.typingURL && document.activeElement !== this.url) this.url.value = this.chosen.url ?? "";
  }

  stopPolling() {
    clearTimeout(this.polling);
    this.polling = null;
  }

  poll() {
    this.stopPolling();
    const tick = async () => {
      if (this.container.closest("[hidden]")) return this.stopPolling();
      const local = !this.local.hidden;
      const ports = [...new Set(this.seatPorts())];
      const [running, ...seats] = await Promise.all([local ? this.checkHealth({ port: this.chosenPort() }) : null, ...ports.map((port) => this.checkHealth({ port }))]);
      if (this.polling === null) return;
      if (local) this.setHealth(running);
      this.seatHealth = new Map(ports.map((port, index) => [port, seats[index]]));
      this.showSeats();
      this.polling = setTimeout(tick, healthPollMilliseconds);
    };
    this.polling = setTimeout(tick, 0);
  }

  // Every snapshot carries every member's member settings, so the choices
  // follow the lobby without a request of their own.
  update(snapshot) {
    // The page hides the panel once the lobby starts, and the bot is then
    // the page's concern; it asks again if the panel is shown.
    this.show(snapshot);
    if (this.container.closest("[hidden]")) {
      this.stopPolling();
      this.packages?.stop();
    } else {
      this.packages?.follow();
    }
  }

  async save(value) {
    // A localhost bot keeps the port in its field, which must be free among
    // the localhost bots that this page relays.
    let port = defaultBotPort;
    if (value === "localhost" && this.chosen?.kind === "localhost") {
      const problem = portProblem(this.port.value, this.seatPorts());
      if (problem) {
        this.summary.textContent = problem;
        this.restore();
        return;
      }
      port = parseBotPort(this.port.value);
    } else if (value === "localhost") {
      // A first choice takes the first port that the seats leave free.
      port = nextFreePort(this.seatPorts());
    }
    const choice = choiceFromValue(value, this.url.value, port);
    // The select shows only what is saved: the placeholder, a choice that
    // is already saved, and a change while a save is in flight (the select
    // is disabled then, but not every browser agrees) go back to it.
    const same = value === choiceValue(this.chosen) && (choice?.kind !== "hosted" || choice.url === this.chosen.url) &&
      (choice?.kind !== "localhost" || (choice.port ?? defaultBotPort) === this.chosenPort());
    if (!choice || this.saving || same) {
      if (value === "hosted" && !choice) this.summary.textContent = "Type your bot's URL first, such as https://bot.example.com/turn.";
      this.restore();
      return;
    }
    this.saving = true;
    this.select.disabled = true;
    this.useURL.disabled = true;
    this.usePort.disabled = true;
    try {
      this.show(await this.saveMemberSettings({ bot: choice }));
      this.typingURL = false;
    } catch (error) {
      this.summary.textContent = error.message || "Could not save your bot. Try again.";
    } finally {
      this.saving = false;
      this.select.disabled = false;
      this.useURL.disabled = false;
      this.usePort.disabled = false;
      this.restore();
    }
  }

  // Shows the saved choice, or the placeholder when there is none; "hosted"
  // while the player types a URL.
  restore() {
    this.select.value = this.typingURL ? "hosted" : choiceValue(this.chosen);
    this.showLocal();
  }

  // Shows the player's saved choice and every member's, from a snapshot.
  show(snapshot) {
    const you = snapshot.members.find((member) => member.player_id === this.playerID);
    const before = choiceValue(this.chosen);
    this.chosen = you ? memberBot(you) : null;
    const seats = localhostSeats(snapshot);
    this.seats = seats.filter((seat) => seat.relay === this.playerID);
    this.orphanSeats = seats.filter((seat) => seatRelayer(snapshot.members, seat) === null);
    // A changed choice may need an option of its own, such as a package
    // that was replaced since.
    if (this.serverBots && choiceValue(this.chosen) !== before) this.fillSelect();
    else this.restore();
    this.summary.textContent = describeChoices(snapshot.members, this.playerID);
  }
}

// The demo's example players submit their turns from the page, so each demo
// player plays as their own bot, which the backend leaves to the page, rather
// than as a default bot that the backend would play for them.
export async function prepareDemo({ saveMemberSettings }) {
  await saveMemberSettings({ bot: { kind: "localhost" } });
}
