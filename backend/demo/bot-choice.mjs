// Island Battles' bot choice for the lobby frontend's waiting room. Every
// member chooses their bot before getting ready, with
// PUT /game/lobbies/{lobby_id}/bot: their localhost bot, which
// island-battles-bot serve runs on their machine and their game client relays
// turns to, or a default bot level that the backend plays itself. The room
// lists every member's choice from GET /game/lobbies/{lobby_id}/bots. The
// host can also seat a default bot on a team, through the lobby page's seat
// controls, which seatOptions fills.

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

// The player's bot select and every member's choice, shown while the lobby
// waits. request(method, path, body) calls the game's API through the page.
export class RoomPanel {
  constructor({ container, lobbyID, playerID, request }) {
    this.lobbyID = lobbyID;
    this.playerID = playerID;
    this.request = request;
    this.seen = null;
    this.saving = false;
    this.select = document.createElement("select");
    this.select.append(new Option("Choose your bot", ""), new Option("My localhost bot (island-battles-bot serve)", "localhost"));
    const label = document.createElement("label");
    label.append("Your bot", this.select);
    this.summary = document.createElement("p");
    this.summary.className = "hint";
    this.summary.setAttribute("role", "status");
    container.append(label, this.summary);
    this.select.addEventListener("change", () => void this.save(this.select.value));
    void loadLevels().then((levels) => {
      this.select.append(...levels.map((level) => new Option(`Default bot, level ${level}`, `level-${level}`)));
      this.restore();
    });
  }

  // Each new lobby version may bring new members, so the list is read again.
  update(snapshot) {
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
