// Island Battles' bot choice for the lobby frontend's waiting room. Every
// member chooses their bot before getting ready, with
// PUT /game/lobbies/{lobby_id}/bot: their localhost bot, which
// island-battles-bot serve runs on their machine and their game client relays
// turns to, or a default bot level that the backend plays itself. The room
// lists every member's choice from GET /game/lobbies/{lobby_id}/bots. The
// host can also seat a default bot on a team, through the lobby page's seat
// controls, which seatOptions fills.

import { checkBotHealth, serveCommand } from "./localhost-bot.mjs";

// How often the room asks the localhost bot's server whether it is running
// while the player has chosen it.
export const healthPollMilliseconds = 2000;

// The default bot levels the select offers when GET /bots/defaults can't be
// read.
const fallbackLevels = [1, 2, 3];

// The select's value for a choice, "" for none.
export function choiceValue(bot) {
  if (!bot) return "";
  return bot.kind === "localhost" ? "localhost" : `level-${bot.level}`;
}

// The choice a select value stands for, or null for none.
export function choiceFromValue(value) {
  if (value === "localhost") return { kind: "localhost" };
  const level = /^level-([1-9][0-9]*)$/.exec(value);
  return level ? { kind: "default", level: Number(level[1]) } : null;
}

// A few words naming a member's bot.
export function describeBot(bot) {
  if (!bot) return "no bot yet";
  return bot.kind === "localhost" ? "own bot (localhost)" : `level ${bot.level} bot`;
}

// One line listing every member's bot, such as "Alex: own bot (localhost) ·
// Sam (you): level 2 bot".
export function describeChoices(members, playerID) {
  return members.map((member) => `${member.display_name}${member.player_id === playerID ? " (you)" : ""}: ${describeBot(member.bot)}`).join(" · ");
}

let defaultBots = null;

// The default bots' levels, loaded once per page.
function loadLevels() {
  defaultBots ||= (async () => {
    const response = await fetch("/bots/defaults", { headers: { Accept: "application/json" } });
    if (!response.ok) throw new Error(`HTTP ${response.status}`);
    return (await response.json()).bots.map((bot) => bot.level);
  })().catch(() => {
    defaultBots = null;
    return fallbackLevels;
  });
  return defaultBots;
}

// The bot seats that the host can add to a team: a default bot of each level,
// whose seat_settings {bot_level} the lobby page sends with POST …/seats.
// The backend plays the seat with that bot.
export async function seatOptions({ levels = loadLevels } = {}) {
  return {
    label: "Add bot",
    choices: (await levels()).map((level) => ({ label: `Default bot, level ${level}`, settings: { bot_level: level } })),
  };
}

// botsPath is the path of the lobby's bot endpoints.
function botsPath(lobbyID, last) {
  return `/game/lobbies/${encodeURIComponent(lobbyID)}/${last}`;
}

// What the localhost bot's indicator says.
export function describeHealth(running) {
  return running ? "Connected: your bot is running on 127.0.0.1:41414." : "Not running: start your bot with the command below.";
}

// The note for browsers that block a page from reaching localhost.
export const safariNote = "Safari does not let an https page reach a bot on localhost. Use Chrome, Edge or Firefox to play with your own bot, or open this page over http on localhost.";

// The player's bot select and every member's choice, shown while the lobby
// waits. request(method, path, body) calls the game's API through the page.
// With the localhost bot chosen, it also shows whether that bot is running.
export class RoomPanel {
  constructor({ container, lobbyID, playerID, request, checkHealth = checkBotHealth, copy = (text) => navigator.clipboard.writeText(text) }) {
    this.container = container;
    this.checkHealth = checkHealth;
    this.copy = copy;
    this.polling = null;
    this.lobbyID = lobbyID;
    this.playerID = playerID;
    this.request = request;
    this.seen = null;
    this.saving = false;
    this.select = document.createElement("select");
    this.select.append(new Option("Choose your bot", ""), new Option("My localhost bot", "localhost"));
    const label = document.createElement("label");
    label.append("Your bot", this.select);
    this.summary = document.createElement("p");
    this.summary.className = "hint";
    this.summary.setAttribute("role", "status");
    this.local = this.buildLocalHelp();
    container.append(label, this.local, this.summary);
    this.select.addEventListener("change", () => void this.save(this.select.value));
    void loadLevels().then((levels) => {
      this.select.append(...levels.map((level) => new Option(`Default bot, level ${level}`, `level-${level}`)));
      this.restore();
    });
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
  // bot is asked about while the room is shown.
  showLocal() {
    const local = this.chosen?.kind === "localhost";
    this.local.hidden = !local;
    if (!local) this.stopPolling();
    else if (!this.polling) this.poll();
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

  // Each new lobby version may bring new members, so the list is read again.
  update(snapshot) {
    // The page hides the panel once the lobby starts, and the bot is then
    // the page's concern; it asks again if the panel is shown.
    if (this.container.closest("[hidden]")) this.stopPolling();
    else this.showLocal();
    const seen = `${snapshot.lobby.version}:${snapshot.members.map((member) => member.player_id).join(",")}`;
    if (seen === this.seen) return;
    this.seen = seen;
    void this.refresh();
  }

  async refresh() {
    try {
      const response = await this.request("GET", botsPath(this.lobbyID, "bots"));
      if (response.status === 200) this.show(response.body);
    } catch (error) {
      this.summary.textContent = error.message;
    }
  }

  async save(value) {
    const choice = choiceFromValue(value);
    // The select shows only what is saved: the placeholder, a choice that
    // is already saved, and a change while a save is in flight (the select
    // is disabled then, but not every browser agrees) go back to it.
    if (!choice || this.saving || value === choiceValue(this.chosen)) {
      this.restore();
      return;
    }
    this.saving = true;
    this.select.disabled = true;
    try {
      const response = await this.request("PUT", botsPath(this.lobbyID, "bot"), choice);
      if (response.status === 200) this.show(response.body);
      else this.summary.textContent = response.body?.detail || "Could not save your bot. Try again.";
    } catch (error) {
      this.summary.textContent = error.message;
    } finally {
      this.saving = false;
      this.select.disabled = false;
      this.restore();
    }
  }

  // Shows the saved choice, or the placeholder when there is none.
  restore() {
    this.select.value = choiceValue(this.chosen);
    this.showLocal();
  }

  show(bots) {
    this.chosen = bots.members.find((member) => member.player_id === this.playerID)?.bot || null;
    this.restore();
    this.summary.textContent = describeChoices(bots.members, this.playerID);
  }
}

// The demo's example players submit their turns from the page, so each demo
// player plays as their own bot, which the backend leaves to the page, rather
// than as a default bot that the backend would play for them.
export async function prepareDemo({ lobbyID, request }) {
  const response = await request("PUT", botsPath(lobbyID, "bot"), { kind: "localhost" });
  if (response.status !== 200) throw new Error(response.body?.detail || "Could not choose the demo player's bot.");
}
