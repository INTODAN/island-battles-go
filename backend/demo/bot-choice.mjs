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
// fills. Below the choice, bot-packages.mjs lists the lobby's bot packages
// and uploads the player's.

import { describePackage, PackagesPanel, packagesPath } from "./bot-packages.mjs";
import { checkBotHealth, serveCommand } from "./localhost-bot.mjs";

// How often the room asks the localhost bot's server whether it is running
// while the player has chosen it.
export const healthPollMilliseconds = 2000;

// The default bot levels the select offers when GET /bots/defaults can't be
// read.
const fallbackLevels = [1, 2, 3];

// The select's value for a choice, "" for none. A hosted bot's URL is not in
// it.
export function choiceValue(bot) {
  if (!bot) return "";
  switch (bot.kind) {
    case "localhost": return "localhost";
    case "hosted": return "hosted";
    case "uploaded": return `package-${bot.package_id}`;
    default: return `level-${bot.level}`;
  }
}

// The choice a select value stands for, or null for none. "hosted" takes the
// URL typed next to it, and is null while that is empty.
export function choiceFromValue(value, url = "") {
  if (value === "localhost") return { kind: "localhost" };
  if (value === "hosted") return url.trim() ? { kind: "hosted", url: url.trim() } : null;
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

// A few words naming a member's bot.
export function describeBot(bot) {
  if (!bot) return "no bot yet";
  switch (bot.kind) {
    case "localhost": return "own bot (localhost)";
    case "hosted": return `hosted bot (${urlHost(bot.url ?? "")})`;
    case "uploaded": return "uploaded bot";
    default: return `level ${bot.level} bot`;
  }
}

// A snapshot member's bot: a person's from their member settings, a bot
// seat's from its seat settings (a default bot of its level, a hosted bot's
// URL or an uploaded package), or null for a person who hasn't chosen.
export function memberBot(member) {
  if (member.kind === "seat") {
    const settings = member.seat_settings ?? {};
    if (Number.isInteger(settings.bot_level)) return { kind: "default", level: settings.bot_level };
    if (typeof settings.bot_url === "string") return { kind: "hosted", url: settings.bot_url };
    if (typeof settings.bot_package_id === "string") return { kind: "uploaded", package_id: settings.bot_package_id };
    return null;
  }
  const bot = member.member_settings?.bot;
  return bot && typeof bot.kind === "string" ? bot : null;
}

// One line listing every member's bot, such as "Alex: own bot (localhost) ·
// Sam (you): level 2 bot", from the snapshot's members.
export function describeChoices(members, playerID) {
  return members.map((member) => `${member.display_name}${member.player_id === playerID ? " (you)" : ""}: ${describeBot(memberBot(member))}`).join(" · ");
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

// The bot seats that the host can add to a team, whose seat_settings the
// lobby page sends with POST …/seats: a default bot of each level, which the
// backend plays, and, when the server offers them, any package of the lobby
// and a hosted bot at the URL that the host types, which the server plays.
// The packages are greyed out (disabled) once the month's compute is spent.
export async function seatOptions({ lobbyID, request, gameURL = "", options = loadOptions, packages = loadPackages, compute = loadCompute } = {}) {
  const { levels, serverBots } = await options(gameURL);
  const choices = levels.map((level) => ({ label: `Default bot, level ${level}`, settings: { bot_level: level } }));
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
export function describeHealth(running) {
  return running ? "Connected: your bot is running on 127.0.0.1:41414." : "Not running: start your bot with the command below.";
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
    this.container = container;
    this.checkHealth = checkHealth;
    this.copy = copy;
    this.polling = null;
    this.lobbyID = lobbyID;
    this.playerID = playerID;
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
    this.select.append(new Option("Choose your bot", ""), new Option("My localhost bot", "localhost"));
    this.levelOptions = [];
    const label = document.createElement("label");
    label.append("Your bot", this.select);
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
    container.append(label, this.spentNote, this.local, this.hosted, this.summary);
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
    const options = [new Option("Choose your bot", ""), new Option("My localhost bot", "localhost"), ...this.levelOptions];
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
    this.url.type = "url";
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
    const run = document.createElement("p");
    run.className = "hint";
    run.append("Run this on your computer, in the folder of your bot: ");
    const command = document.createElement("code");
    command.textContent = serveCommand;
    this.copyButton = document.createElement("button");
    this.copyButton.type = "button";
    this.copyButton.className = "secondary";
    this.copyButton.textContent = "Copy command";
    this.copyButton.addEventListener("click", () => void this.copyCommand());
    run.append(command, " ", this.copyButton);
    const safari = document.createElement("p");
    safari.className = "hint";
    safari.textContent = safariNote;
    box.append(this.health, run, safari);
    this.setHealth(null);
    return box;
  }

  async copyCommand() {
    try {
      await this.copy(serveCommand);
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
    this.health.textContent = running === null ? "Checking for your bot…" : describeHealth(running);
  }

  // The localhost bot's help and indicator show for the saved choice, and the
  // bot is asked about while the room is shown. The hosted bot's URL field
  // shows for a saved hosted bot, and while the player types one.
  showLocal() {
    const local = this.chosen?.kind === "localhost" && !this.typingURL;
    this.local.hidden = !local;
    if (!local) this.stopPolling();
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
      const running = await this.checkHealth();
      if (this.polling === null) return;
      this.setHealth(running);
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
    const choice = choiceFromValue(value, this.url.value);
    // The select shows only what is saved: the placeholder, a choice that
    // is already saved, and a change while a save is in flight (the select
    // is disabled then, but not every browser agrees) go back to it.
    const same = value === choiceValue(this.chosen) && (choice?.kind !== "hosted" || choice.url === this.chosen.url);
    if (!choice || this.saving || same) {
      if (value === "hosted" && !choice) this.summary.textContent = "Type your bot's URL first, such as https://bot.example.com/turn.";
      this.restore();
      return;
    }
    this.saving = true;
    this.select.disabled = true;
    this.useURL.disabled = true;
    try {
      this.show(await this.saveMemberSettings({ bot: choice }));
      this.typingURL = false;
    } catch (error) {
      this.summary.textContent = error.message || "Could not save your bot. Try again.";
    } finally {
      this.saving = false;
      this.select.disabled = false;
      this.useURL.disabled = false;
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
