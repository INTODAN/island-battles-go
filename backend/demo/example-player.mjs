import { BotError, defaultBotPort, deliverGameEnded, deliverTurn } from "./localhost-bot.mjs";

// Castles and soldiers with this color belong to no player team.
export const NEUTRAL_TEAM_COLOR = 0;

// The problem type of the 409 that rejects commands for a finished game.
export const GAME_ENDED_PROBLEM = "urn:island-battles:problem:game-ended";

// The server runs no timers, so a waiting player asks it to apply the turn's
// deadline this long after it. A turn that is still open then, for example
// because the server's clock is behind, is asked about again after
// DEADLINE_RETRY_MS, doubling up to DEADLINE_RETRY_MAX_MS.
export const DEADLINE_SLACK_MS = 1000;
const DEADLINE_RETRY_MS = 2000;
const DEADLINE_RETRY_MAX_MS = 30000;

// A deliberately small strategy: send one soldier, then finish this turn.
export function singleSoldierCommands(state) {
  const castles = [...state.generation.castles].sort((left, right) => left.id - right.id);
  const origin = castles.find((castle) => castle.team_color === state.team_color && castle.soldiers > 0);
  const enemy = castles.find((castle) => castle.team_color !== NEUTRAL_TEAM_COLOR && castle.team_color !== state.team_color);
  if (!origin || !enemy) return [];
  return [{
    command_type: "send_soldier_group",
    parameters: { origin_castle_id: origin.id, destination_castle_id: enemy.id, soldier_amount: 1 },
  }];
}

// How long the player waits before it asks an unreachable localhost bot again.
const BOT_RETRY_MS = 2000;

// The example player has two jobs. In a demo (the default), it plays the
// example strategy while it is enabled. Outside a demo, it delivers the turns
// of a player whose bot is the localhost bot to that bot, on the port that
// GenerationState.bot names: it reads the player's turn delivery and posts it
// to the bot, which submits the turn itself; the page submits nothing. A
// player with a default bot level is only followed, since the backend plays
// for them. A player also relays the localhost bot seats that they added as
// the host, GenerationState.relayed_bots, each to its own port (seat
// relays, below). In both, a player that isn't playing a turn, or whose bot
// has the turn, asks the server to apply its deadline. Once the game has an
// outcome, it tells each of those localhost bots, its own and the seats', that
// the game ended: it reads the bot's game-ended delivery and posts it to the
// bot's /game-ended, once per bot per game, and ignores any failure.
//
// A lobby plays one game per round, and the lobby page makes a player for each
// round, with lobbyRound. The player takes only the states of that round's
// game: until the next game exists, the lobby's newest game is the previous
// round's, whose states it drops. Without lobbyRound it takes any round's.
//
// It reads and writes the game through fetcher, which the lobby page passes
// with the player's token for the game; the player sends no credential of
// its own.
// The status of a player whom this page does not play: a hosted or uploaded
// bot that the server plays, or a person who plays by hand, as
// [title, detail], or null for another bot.
export function serverBotStatus(bot) {
  if (bot?.kind === "manual") return ["Play this seat in the 3D client", "This seat is played by hand in the 3D client; this page submits nothing for it."];
  if (bot?.kind === "hosted") return ["Played by the server", `The server sends each turn to your hosted bot${bot.host ? ` at ${bot.host}` : ""}; this page relays nothing.`];
  if (bot?.kind === "uploaded") return ["Played by the server", "The server runs your uploaded bot each turn; this page relays nothing."];
  return null;
}

export class ExamplePlayer {
  constructor({ lobbyID, lobbyRound = null, playerID, subscription, enabled = true, demo = true, pending = null, savePending = () => {},
    onState = () => {}, onStatus = () => {}, retryDelay = 500, botRetryDelay = BOT_RETRY_MS, deliver = (delivery, port) => deliverTurn(delivery, { port }),
    deliverEnd = (delivery, port) => deliverGameEnded(delivery, { port }), fetcher = (...args) => globalThis.fetch(...args), now = () => Date.now() }) {
    Object.assign(this, { lobbyID, lobbyRound, playerID, subscription, enabled, demo, pending, savePending, onState, onStatus, retryDelay,
      botRetryDelay, deliver, deliverEnd, fetcher, now });
    // The generation whose turn the localhost bot failed to accept.
    this.failed = null;
    // The generation whose turn the localhost bot has, or that has nothing
    // left to deliver.
    this.delivered = null;
    this.stopped = false;
    // Set once the server rejects a command because the game has ended.
    this.ended = false;
    // Why the server's rejection paused this player, until it is resumed.
    this.attention = null;
    this.inFlight = false;
    // The requests in flight, which stop() aborts.
    this.controllers = new Set();
    // The player's own status, [title, detail], and the relays of the
    // localhost bot seats that the player relays, by the seat's player ID.
    this.own = null;
    this.seats = new Map();
    // The localhost bots told that their game ended, or being told, by
    // "game:player" ("game:" for the player's own bot).
    this.endsSent = new Set();
    this.commandRetry = null;
    this.commandFailures = 0;
    this.state = null;
    this.refreshing = null;
    this.refreshRetry = null;
    this.refreshFailures = 0;
    this.requestController = null;
    this.deadlineTimer = null;
    // The generation whose deadline was applied, and how often.
    this.deadlineAttempts = { key: null, count: 0, at: 0 };
  }

  // Whether this player takes the turn of a state: the demo's player while it
  // is enabled, otherwise a player with a localhost bot.
  playing(state) {
    return this.enabled && (this.demo || state?.bot?.kind === "localhost");
  }

  // Override to try another strategy. Returning [] passes the turn.
  nextGeneration(state) { return singleSoldierCommands(state); }

  async request(path, body, method = body ? "POST" : "GET") {
    const controller = new AbortController();
    this.controllers.add(controller);
    const timeout = setTimeout(() => controller.abort(), 8000);
    try {
      const response = await this.fetcher(path, {
        method, signal: controller.signal,
        headers: { "Content-Type": "application/json" },
        ...(body ? { body: JSON.stringify(body) } : {}),
      });
      const result = await response.json();
      if (!response.ok) {
        const error = new Error(result.detail || "Could not load this generation.");
        error.status = response.status;
        error.type = result.type;
        throw error;
      }
      return result;
    } finally {
      clearTimeout(timeout);
      this.controllers.delete(controller);
    }
  }

  remember(pending) { this.pending = pending; this.savePending(pending); }

  receive(state) {
    if (this.stopped || state.lobby_id !== this.lobbyID || state.player_id !== this.playerID ||
        (this.lobbyRound !== null && state.lobby_round !== this.lobbyRound) ||
        (this.state && (state.game_id !== this.state.game_id || state.generation_id < this.state.generation_id))) return;
    // Late publications must not reopen a turn already completed over HTTP.
    if (this.state?.generation_id === state.generation_id && this.state.submitted) state = { ...state, submitted: true };
    // A slow read or deadline answer for the same generation can arrive after
    // the game ended; the outcome is final.
    if (this.state?.generation_id === state.generation_id && this.state.outcome && !state.outcome) state = { ...state, outcome: this.state.outcome };
    this.state = state;
    if (this.pending && (this.pending.game_id !== state.game_id || this.pending.generation_id !== state.generation_id || state.submitted)) {
      this.remember(null);
      clearTimeout(this.commandRetry);
      this.commandRetry = null;
      this.commandFailures = 0;
    }
    this.onState(state);
    this.scheduleDeadline();
    this.scheduleTurn();
    this.relaySeats();
    this.relayGameEnded();
  }

  // Reports the player's own status, with a line for each localhost bot
  // seat that the player relays.
  status(title, detail) {
    this.own = [title, detail];
    this.report();
  }

  report() {
    const seats = [...this.seats.values()].filter((seat) => seat.line);
    if (seats.length === 0) {
      if (this.own) this.onStatus(...this.own);
      return;
    }
    const lines = seats.map((seat) => `${seat.bot.display_name}: ${seat.line}`);
    const [title, detail] = this.own ?? ["Your localhost bot seats", ""];
    this.onStatus(title, [detail, ...lines].filter(Boolean).join(" · "));
  }

  // The seat relays: outside a demo, the player who added each
  // localhost bot seat delivers its turn to its bot, on the seat's port, as it
  // does its own. Each seat's turn is read with player_id, and the seat's bot
  // submits it with the seat's submit token. A seat's bot that isn't running
  // is asked again until it is; one that refused the turn waits for the next.
  relaySeats() {
    const state = this.state;
    if (this.demo || this.stopped || !state) return;
    const relayed = state.relayed_bots ?? [];
    if (relayed.length === 0 && this.seats.size === 0) return;
    for (const [playerID, seat] of this.seats) {
      if (!relayed.some((bot) => bot.player_id === playerID)) {
        clearTimeout(seat.retry);
        this.seats.delete(playerID);
      }
    }
    for (const bot of relayed) {
      if (!this.seats.has(bot.player_id)) this.seats.set(bot.player_id, { bot, done: null, inFlight: false, retry: null, failures: 0, line: "" });
      const seat = this.seats.get(bot.player_id);
      seat.bot = bot;
      const key = `${state.game_id}:${state.generation_id}`;
      if (this.ended || state.outcome) {
        clearTimeout(seat.retry);
        seat.retry = null;
        seat.line = "game over.";
      } else if (seat.done !== key && !seat.inFlight && !seat.retry) {
        void this.deliverSeat(seat, key);
      }
    }
    this.report();
  }

  setSeatLine(seat, line) {
    seat.line = line;
    this.report();
  }

  async deliverSeat(seat, key) {
    const { bot } = seat;
    seat.inFlight = true;
    this.setSeatLine(seat, `sending this turn to the bot on port ${bot.port}.`);
    try {
      const delivery = await this.request(`/game/from-lobby/${encodeURIComponent(this.lobbyID)}/turn-delivery?player_id=${encodeURIComponent(bot.player_id)}`);
      // A newer turn that arrived meanwhile is sent by relaySeats in finally.
      if (this.stopped || `${this.state.game_id}:${this.state.generation_id}` !== key) return;
      if (delivery.turn?.generation?.id !== this.state.generation_id) {
        // The server is past this state's turn, whose next state brings the
        // newer one; asking again for this turn would only repeat the read.
        seat.done = key;
        return;
      }
      await this.deliver(delivery, bot.port);
      if (this.stopped) return;
      seat.done = key;
      seat.failures = 0;
      this.setSeatLine(seat, "has this turn and submits it itself.");
    } catch (error) {
      if (this.stopped) return;
      const again = (ms) => {
        seat.retry = setTimeout(() => {
          seat.retry = null;
          this.relaySeats();
        }, ms);
      };
      if (error instanceof BotError && error.kind === "unreachable") {
        this.setSeatLine(seat, `not running. ${error.message} Trying again…`);
        again(this.botRetryDelay);
      } else if (error instanceof BotError) {
        seat.done = key;
        this.setSeatLine(seat, error.kind === "too-slow" ? error.message : `refused the turn. ${error.message}`);
      } else if (error.status === 409) {
        // Nothing to play: the seat's team is out, its turn is complete, or
        // the game ended, which the next state says.
        seat.done = key;
        this.setSeatLine(seat, error.type === GAME_ENDED_PROBLEM ? "game over." : "nothing to play this turn.");
      } else if ([401, 403, 404, 422].includes(error.status)) {
        seat.done = key;
        this.setSeatLine(seat, error.message);
      } else {
        seat.failures++;
        this.setSeatLine(seat, "reconnecting; the turn is sent when the server responds.");
        again(Math.min(this.retryDelay * 2 ** Math.min(seat.failures - 1, 5), 10000));
      }
    } finally {
      seat.inFlight = false;
      // relaySeats skipped this seat while its delivery was in flight, so a
      // turn that arrived meanwhile is sent now.
      if (!this.stopped) this.relaySeats();
    }
  }

  // Once the game has an outcome, tells the player's own localhost bot and
  // each localhost bot seat that they relay that the game ended, whatever
  // became of their teams: reads each one's game-ended delivery (a seat's
  // with player_id) and posts it to the bot's port. Each bot is told once per
  // game, and nothing waits for it: a bot that isn't running, an older SDK
  // that answers 404, or a failed read is only logged.
  relayGameEnded() {
    const state = this.state;
    if (this.demo || this.stopped || !state?.outcome) return;
    const bots = (state.relayed_bots ?? []).map((bot) => ({ playerID: bot.player_id, port: bot.port }));
    if (state.bot?.kind === "localhost") bots.unshift({ playerID: null, port: state.bot.port ?? defaultBotPort });
    for (const { playerID, port } of bots) {
      const key = `${state.game_id}:${playerID ?? ""}`;
      if (this.endsSent.has(key)) continue;
      this.endsSent.add(key);
      void this.sendGameEnded(state.game_id, playerID, port);
    }
  }

  async sendGameEnded(gameID, playerID, port) {
    const query = playerID === null ? "" : `?player_id=${encodeURIComponent(playerID)}`;
    try {
      const delivery = await this.request(`/game/from-lobby/${encodeURIComponent(this.lobbyID)}/game-ended-delivery${query}`);
      if (this.stopped || delivery.game_id !== gameID) return;
      await this.deliverEnd(delivery, port);
    } catch (error) {
      if (!this.stopped) console.debug(`The end of the game was not sent to the localhost bot on port ${port}:`, error?.message ?? error);
    }
  }

  // A player that waits for the others, because it completed its turn or
  // doesn't play (paused, or only following the game), asks the server to
  // apply the turn's deadline a second after it. The answer is this player's
  // state, with the outcome when someone didn't answer in time.
  scheduleDeadline() {
    clearTimeout(this.deadlineTimer);
    this.deadlineTimer = null;
    const state = this.state;
    const key = state && `${state.game_id}:${state.generation_id}`;
    if (this.stopped || this.ended || !state || state.outcome || !state.turn_deadline_at ||
        (this.playing(state) && !state.submitted && this.failed !== key && this.delivered !== key)) return;
    const { count: attempts, at } = this.deadlineAttempts.key === key ? this.deadlineAttempts : { count: 0, at: 0 };
    let wait = Date.parse(state.turn_deadline_at) + DEADLINE_SLACK_MS - this.now();
    if (attempts) wait = Math.max(wait, at + Math.min(DEADLINE_RETRY_MS * 2 ** (attempts - 1), DEADLINE_RETRY_MAX_MS) - this.now());
    this.deadlineTimer = setTimeout(() => void this.applyDeadline(key, attempts + 1), Math.max(0, wait));
  }

  async applyDeadline(key, attempts) {
    this.deadlineTimer = null;
    this.deadlineAttempts = { key, count: attempts, at: this.now() };
    try {
      const state = await this.request(`/game/from-lobby/${encodeURIComponent(this.lobbyID)}/deadline`, null, "POST");
      if (!this.stopped) this.receive(state);
    } catch {
      // The next attempt waits longer.
      if (!this.stopped) this.scheduleDeadline();
    }
  }

  scheduleTurn() {
    if (this.stopped || this.inFlight || this.commandRetry || this.refreshing || this.refreshRetry || !this.state) return;
    // A default bot's player has nothing to relay or report. The server
    // plays a hosted or an uploaded bot without the page, which says so.
    if (!this.demo && this.state.bot?.kind !== "localhost") {
      const served = serverBotStatus(this.state.bot);
      if (served) this.status(...served);
      return;
    }
    const key = `${this.state.game_id}:${this.state.generation_id}`;
    if (this.failed === key) return;
    if (this.ended || this.state.outcome) {
      // The final generation has no turn to play.
      this.status("Game over", this.state.outcome?.summary || "The game has ended.");
    } else if (!this.playing(this.state)) {
      // A rejected turn pauses the player until it is resumed.
      if (this.attention) this.status("Player needs attention", this.attention);
      else this.status("Paused", "This player will wait before submitting its next turn.");
    } else if (this.state.submitted) {
      this.status("Turn submitted", "Waiting for the other player to finish.");
    } else if (this.delivered === key) {
      // The bot has the turn, and the next generation or the deadline ends it.
    } else if (!this.demo) {
      void this.deliverTurn();
    } else {
      // Live generations drive simulation immediately; replay controls own pacing.
      void this.submitTurn();
    }
  }

  async submitTurn() {
    if (this.stopped || this.inFlight || !this.playing(this.state) || !this.state || this.state.submitted || this.ended || this.state.outcome) return;
    this.inFlight = true;
    try {
      if (!this.pending) {
        const state = this.state;
        const turn = { commands: await this.nextGeneration(state) };
        if (this.stopped || !this.enabled) return;
        this.remember({ game_id: state.game_id, generation_id: state.generation_id, request_id: crypto.randomUUID(), complete: true, ...turn });
      }
      const submitted = this.pending;
      const result = await this.request("/generation/commands", submitted);
      if (this.stopped) return;
      this.commandFailures = 0;
      this.remember(null);
      this.receive(result);
      const order = submitted.commands[0]?.parameters;
      this.status("Example player running", order
        ? `Sent 1 soldier: Castle ${order.origin_castle_id + 1} → Castle ${order.destination_castle_id + 1}.`
        : "No enemy castle or available soldiers. Passed this turn.");
    } catch (error) {
      if (this.stopped) return;
      if (error.status === 409 && error.type === GAME_ENDED_PROBLEM) {
        // A finished game accepts no commands; read its final state once.
        this.remember(null);
        this.ended = true;
        await this.refresh();
      } else if (error.status === 409) {
        const rejected = this.pending?.generation_id;
        this.remember(null);
        await this.refresh();
        // Retry only a stale turn. The same open turn would be rejected again.
        if (this.refreshFailures === 0 && this.state?.generation_id === rejected && !this.state.submitted && !this.state.outcome) {
          this.attention = error.message;
          this.enabled = false;
        }
      } else if ([401, 403, 404, 422].includes(error.status)) {
        this.attention = error.message;
        this.enabled = false;
      } else {
        // A lost command response is retried with the original UUID and body.
        this.status("Reconnecting", "The same turn will be retried when the server responds.");
        this.commandFailures++;
        this.commandRetry = setTimeout(() => {
          this.commandRetry = null;
          this.scheduleTurn();
        }, Math.min(this.retryDelay * 2 ** Math.min(this.commandFailures - 1, 5), 10000));
      }
    } finally {
      this.inFlight = false;
      this.scheduleTurn();
    }
  }

  // Delivers the open turn to the localhost bot: reads the player's turn
  // delivery, which the server builds with a one-turn submit token, and posts
  // it to the bot, which plays and submits the turn itself.
  async deliverTurn() {
    const state = this.state;
    if (this.stopped || this.inFlight || !this.playing(state) || state.submitted || this.ended || state.outcome) return;
    const key = `${state.game_id}:${state.generation_id}`;
    this.inFlight = true;
    try {
      this.status("Your bot is playing", "Sending this turn to your bot.");
      const delivery = await this.request(`/game/from-lobby/${encodeURIComponent(this.lobbyID)}/turn-delivery`);
      if (this.stopped) return;
      // A delivery read before the next generation arrived would be refused
      // by the server, so only the current turn's is posted.
      if (delivery.turn?.generation?.id !== state.generation_id || `${this.state.game_id}:${this.state.generation_id}` !== key) return;
      await this.deliver(delivery, this.state.bot?.port ?? defaultBotPort);
      if (this.stopped) return;
      this.commandFailures = 0;
      this.delivered = key;
      this.status("Your bot is playing", "Your bot has this turn and submits it itself. Waiting for the other players.");
      this.scheduleDeadline();
    } catch (error) {
      if (this.stopped) return;
      if (error instanceof BotError) {
        this.botFailed(error);
      } else if (error.status === 409) {
        // Nothing to play: the game ended, the team is out, or the turn is
        // already complete, perhaps from another tab.
        if (error.type === GAME_ENDED_PROBLEM) this.ended = true;
        this.delivered = key;
        this.scheduleDeadline();
        await this.refresh();
      } else if ([401, 403, 404, 422].includes(error.status)) {
        this.attention = error.message;
        this.enabled = false;
      } else {
        this.status("Reconnecting", "This turn will be sent to your bot when the server responds.");
        this.commandFailures++;
        this.commandRetry = setTimeout(() => {
          this.commandRetry = null;
          this.scheduleTurn();
        }, Math.min(this.retryDelay * 2 ** Math.min(this.commandFailures - 1, 5), 10000));
      }
    } finally {
      this.inFlight = false;
      this.scheduleTurn();
    }
  }

  // A bot that isn't running is asked again until it is; one that refused
  // the turn is shown with its reason, so the turn's deadline decides. Either
  // way the player asks for the deadline to be applied.
  botFailed(error) {
    this.failed = `${this.state.game_id}:${this.state.generation_id}`;
    if (error.kind === "unreachable") {
      this.status("Your bot is not running", `${error.message} Trying again…`);
      this.commandRetry = setTimeout(() => {
        this.commandRetry = null;
        this.failed = null;
        this.scheduleTurn();
      }, this.botRetryDelay);
    } else {
      this.status(error.kind === "too-slow" ? "Your bot was too slow" : "Your bot refused the turn", error.message);
    }
    this.scheduleDeadline();
  }

  refresh() {
    if (this.refreshing || this.stopped) return this.refreshing;
    clearTimeout(this.refreshRetry);
    this.refreshRetry = null;
    this.refreshing = this.request(`/game/from-lobby/${encodeURIComponent(this.lobbyID)}/generation`)
      .then((state) => { this.refreshFailures = 0; this.receive(state); })
      .catch((error) => {
        if (this.stopped) return;
        this.status("Waiting for connection", error.message);
        if (!error.status || error.status >= 500) {
          this.refreshFailures++;
          this.refreshRetry = setTimeout(() => { this.refreshRetry = null; void this.refresh(); }, Math.min(500 * 2 ** Math.min(this.refreshFailures - 1, 5), 10000));
        }
      }).finally(() => { this.refreshing = null; this.scheduleTurn(); });
    return this.refreshing;
  }

  start() {
    // Reconcile only at subscription/reconnect. New generations arrive in full.
    this.subscription.on("subscribed", () => { void this.refresh(); });
    this.subscription.on("publication", ({ data }) => {
      // game.ended carries the final generation and the outcome.
      if (data?.schema_version === 1 && ["game.next_generation", "game.ended"].includes(data.type) && data.state) this.receive(data.state);
    });
    this.subscription.subscribe();
  }

  setEnabled(enabled) {
    this.enabled = enabled;
    this.attention = null;
    this.failed = null;
    clearTimeout(this.commandRetry);
    this.commandRetry = null;
    if (enabled && !this.state) void this.refresh();
    this.scheduleDeadline();
    this.scheduleTurn();
  }

  stop() {
    this.stopped = true;
    clearTimeout(this.commandRetry);
    clearTimeout(this.refreshRetry);
    clearTimeout(this.deadlineTimer);
    for (const seat of this.seats.values()) clearTimeout(seat.retry);
    for (const controller of this.controllers) controller.abort();
    this.subscription.unsubscribe();
  }
}
