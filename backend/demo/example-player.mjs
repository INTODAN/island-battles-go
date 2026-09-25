// Castles and soldiers with this color belong to no player team.
export const NEUTRAL_TEAM_COLOR = 0;

// A deliberately small strategy: send one soldier, then finish this turn.
export function singleSoldierCommands(state) {
  const castles = [...state.generation.castles].sort((left, right) => left.id - right.id);
  const origin = castles.find((castle) => castle.team_color === state.team_color && castle.soldiers > 0);
  const enemy = castles.find((castle) => castle.team_color !== NEUTRAL_TEAM_COLOR && castle.team_color !== state.team_color);
  if (!origin || !enemy) return [];
  return [{
    command_type: "send_soldier_group",
    parameters: { origin_logical_castle_id: origin.id, destination_logical_castle_id: enemy.id, soldier_amount: 1 },
  }];
}

export class ExamplePlayer {
  constructor({ lobbyID, playerID, credential, subscription, enabled = true, pending = null, savePending = () => {},
    onState = () => {}, onStatus = () => {}, interval = 1500, fetcher = (...args) => globalThis.fetch(...args) }) {
    Object.assign(this, { lobbyID, playerID, credential, subscription, enabled, pending, savePending, onState, onStatus, interval, fetcher });
    this.stopped = false;
    this.inFlight = false;
    this.timer = null;
    this.state = null;
    this.refreshing = null;
    this.refreshRetry = null;
    this.refreshFailures = 0;
    this.requestController = null;
  }

  // Override to try another strategy. Returning [] passes the turn.
  nextGeneration(state) { return singleSoldierCommands(state); }

  async request(path, body) {
    const controller = new AbortController();
    this.requestController = controller;
    const timeout = setTimeout(() => controller.abort(), 8000);
    try {
      const response = await this.fetcher(path, {
        method: body ? "POST" : "GET", signal: controller.signal,
        headers: { Authorization: `Bearer ${this.credential}`, "Content-Type": "application/json" },
        ...(body ? { body: JSON.stringify(body) } : {}),
      });
      const result = await response.json();
      if (!response.ok) {
        const error = new Error(result.detail || "Could not load this generation.");
        error.status = response.status;
        throw error;
      }
      return result;
    } finally {
      clearTimeout(timeout);
      if (this.requestController === controller) this.requestController = null;
    }
  }

  remember(pending) { this.pending = pending; this.savePending(pending); }

  receive(state) {
    if (this.stopped || state.lobby_id !== this.lobbyID || state.player_id !== this.playerID ||
        (this.state && (state.game_id !== this.state.game_id || state.generation_id < this.state.generation_id))) return;
    // Late publications must not reopen a turn already completed over HTTP.
    if (this.state?.generation_id === state.generation_id && this.state.submitted) state = { ...state, submitted: true };
    this.state = state;
    if (this.pending && (this.pending.game_id !== state.game_id || this.pending.generation_id !== state.generation_id || state.submitted)) this.remember(null);
    this.onState(state);
    this.scheduleTurn();
  }

  scheduleTurn() {
    if (this.stopped || this.inFlight || this.timer || !this.state) return;
    if (!this.enabled) {
      this.onStatus("Paused", "This player will wait before submitting its next turn.");
    } else if (this.state.submitted) {
      this.onStatus("Turn submitted", "Waiting for the other player to finish.");
    } else {
      this.timer = setTimeout(() => { this.timer = null; void this.submitTurn(); }, this.interval);
    }
  }

  async submitTurn() {
    if (this.stopped || this.inFlight || !this.enabled || !this.state || this.state.submitted) return;
    this.inFlight = true;
    try {
      if (!this.pending) {
        const state = this.state;
        const commands = await this.nextGeneration(state);
        if (this.stopped || !this.enabled) return;
        this.remember({ game_id: state.game_id, generation_id: state.generation_id, request_id: crypto.randomUUID(), complete: true, commands });
      }
      const submitted = this.pending;
      const result = await this.request("/generation/commands", submitted);
      if (this.stopped) return;
      this.remember(null);
      this.receive(result);
      const order = submitted.commands[0]?.parameters;
      this.onStatus("Example player running", order
        ? `Sent 1 soldier: Castle ${order.origin_logical_castle_id + 1} → Castle ${order.destination_logical_castle_id + 1}.`
        : "No enemy castle or available soldiers. Passed this turn.");
    } catch (error) {
      if (this.stopped) return;
      if (error.status === 409) {
        this.remember(null);
        await this.refresh();
      } else if ([401, 403, 404, 422].includes(error.status)) {
        this.enabled = false;
        this.onStatus("Player needs attention", error.message);
      } else {
        // A lost command response is retried with the original UUID and body.
        this.onStatus("Reconnecting", "The same turn will be retried when the server responds.");
      }
    } finally {
      this.inFlight = false;
      this.scheduleTurn();
    }
  }

  refresh() {
    if (this.refreshing || this.stopped) return this.refreshing;
    clearTimeout(this.refreshRetry);
    this.refreshing = this.request(`/game/from-lobby/${encodeURIComponent(this.lobbyID)}/generation`)
      .then((state) => { this.refreshFailures = 0; this.receive(state); })
      .catch((error) => {
        if (this.stopped) return;
        this.onStatus("Waiting for connection", error.message);
        if (!error.status || error.status >= 500) {
          this.refreshFailures++;
          this.refreshRetry = setTimeout(() => { void this.refresh(); }, Math.min(500 * 2 ** Math.min(this.refreshFailures - 1, 5), 10000));
        }
      }).finally(() => { this.refreshing = null; });
    return this.refreshing;
  }

  start() {
    // Reconcile only at subscription/reconnect. New generations arrive in full.
    this.subscription.on("subscribed", () => { void this.refresh(); });
    this.subscription.on("publication", ({ data }) => {
      if (data?.schema_version === 1 && data.type === "game.next_generation" && data.state) this.receive(data.state);
    });
    this.subscription.subscribe();
  }

  setEnabled(enabled) {
    this.enabled = enabled;
    clearTimeout(this.timer);
    this.timer = null;
    if (enabled && !this.state) void this.refresh();
    this.scheduleTurn();
  }

  stop() {
    this.stopped = true;
    clearTimeout(this.timer);
    clearTimeout(this.refreshRetry);
    this.requestController?.abort();
    this.subscription.unsubscribe();
  }
}
