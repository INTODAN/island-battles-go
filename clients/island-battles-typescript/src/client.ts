import createClient from "openapi-fetch";
import { GameError, protocol } from "./errors.js";
import type { components, paths } from "./generated/api.js";
import { type Generation, generationFromState } from "./models.js";
import { parseEvent, parseState, parseToken } from "./protocol.js";
import {
  centrifugeFactory,
  type Realtime,
  type RealtimeFactory,
} from "./realtime.js";

type State = components["schemas"]["GenerationState"];
type Command = components["schemas"]["PublicGenerationCommand"];

export interface Player {
  readonly teamColor: number;
  onGeneration(
    generation: Generation,
    commands: CommandReceiver,
    signal: AbortSignal,
  ): Promise<void>;
}

export interface ClientOptions {
  readonly baseUrl: string;
  readonly credential: string;
  readonly lobbyId: string;
  readonly playerFactory: (teamColor: number) => Player;
  /** Requests always carry a cancellation signal. The caller owns the fetch function itself. */
  readonly fetch?: typeof globalThis.fetch;
  readonly realtimeFactory?: RealtimeFactory;
  readonly retryAttempts?: number;
  readonly subscriptionTimeoutMs?: number;
  readonly requestTimeoutMs?: number;
  readonly refreshIntervalMs?: number;
  /** Scheduling hook for deterministic tests. The client still controls retry policy. */
  readonly sleep?: (milliseconds: number, signal: AbortSignal) => Promise<void>;
}

export class CommandReceiver {
  #active = true;
  constructor(private readonly submit: (command: Command) => Promise<void>) {}
  async sendSoldierGroup(
    originCastleId: number,
    destinationCastleId: number,
    amount: number,
  ): Promise<void> {
    if (!this.#active)
      throw new GameError("command receiver has expired", "closed");
    if (
      !Number.isSafeInteger(originCastleId) ||
      originCastleId < 0 ||
      !Number.isSafeInteger(destinationCastleId) ||
      destinationCastleId < 0 ||
      !Number.isSafeInteger(amount) ||
      amount < 1
    ) {
      throw new GameError(
        "castle IDs must be nonnegative integers and amount a positive integer",
        "validation",
      );
    }
    await this.submit({
      command_type: "send_soldier_group",
      parameters: {
        origin_persistent_castle_id: originCastleId,
        destination_persistent_castle_id: destinationCastleId,
        soldier_amount: amount,
      },
    });
  }
  /** @internal */
  expire(): void {
    this.#active = false;
  }
}

class SerialQueue {
  private tail: Promise<unknown> = Promise.resolve();
  run<T>(operation: () => Promise<T>): Promise<T> {
    const current = this.tail.then(operation);
    this.tail = current.catch(() => {});
    return current;
  }
}

function cancelled(): GameError {
  return new GameError("client is closed", "closed");
}

function abortable<T>(promise: Promise<T>, signal: AbortSignal): Promise<T> {
  if (signal.aborted) {
    void promise.catch(() => {});
    return Promise.reject(cancelled());
  }
  return new Promise<T>((resolve, reject) => {
    const abort = () => reject(cancelled());
    signal.addEventListener("abort", abort, { once: true });
    promise
      .then(resolve, reject)
      .finally(() => signal.removeEventListener("abort", abort));
  });
}

async function sleep(milliseconds: number, signal: AbortSignal): Promise<void> {
  let timer: ReturnType<typeof setTimeout> | undefined;
  try {
    await abortable(
      new Promise<void>((resolve) => {
        timer = setTimeout(resolve, milliseconds);
      }),
      signal,
    );
  } finally {
    clearTimeout(timer);
  }
}

/** One authorized player's callbacks; keep the generic lobby lease owner running separately. */
export class IslandBattlesClient {
  private readonly api;
  private readonly lobbyId: string;
  private readonly lifetime = new AbortController();
  private readonly retries: number;
  private readonly delay;
  private readonly commands = new SerialQueue();
  private readonly reconciliation = new SerialQueue();
  private readonly stopped: Promise<void>;
  private stop!: () => void;
  private state?: State;
  private snapshot?: Generation;
  private identity?: string;
  private strategy?: Player;
  private realtime?: Realtime;
  private startup?: Promise<Generation>;
  private shutdown?: Promise<void>;
  private callbackTask?: Promise<void>;
  private reconcileTask?: Promise<void>;
  private receiver?: CommandReceiver;
  private pendingReconciliation = false;
  private lastCallback = 0;
  private started = false;
  private closed = false;
  private failure?: unknown;
  private pollTimer?: ReturnType<typeof setInterval>;

  constructor(private readonly options: ClientOptions) {
    this.retries = options.retryAttempts ?? 3;
    if (
      !Number.isSafeInteger(this.retries) ||
      this.retries < 1 ||
      [
        options.subscriptionTimeoutMs ?? 10000,
        options.requestTimeoutMs ?? 10000,
        options.refreshIntervalMs ?? 1,
      ].some((value) => !Number.isFinite(value) || value <= 0)
    ) {
      throw new GameError(
        "attempts and timeouts must be positive",
        "validation",
      );
    }
    if (
      !/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(
        options.lobbyId,
      )
    )
      throw new GameError("invalid lobby ID", "validation");
    this.lobbyId = options.lobbyId.toLowerCase();
    this.delay = options.sleep ?? sleep;
    this.stopped = new Promise((resolve) => {
      this.stop = resolve;
    });
    const fetcher = options.fetch ?? globalThis.fetch;
    this.api = createClient<paths>({
      baseUrl: options.baseUrl.replace(/\/$/, ""),
      headers: { Authorization: `Bearer ${options.credential}` },
      fetch: async (request) => {
        try {
          return await fetcher(request);
        } catch {
          if (this.closed) throw cancelled();
          throw new GameError("network error", "transport", undefined, true);
        }
      },
    });
  }

  get generation(): Generation | undefined {
    return this.snapshot;
  }
  get player(): Player | undefined {
    return this.strategy;
  }
  get isClosed(): boolean {
    return this.closed;
  }

  private checkOpen(): void {
    if (this.closed) throw cancelled();
    if (this.failure !== undefined) throw this.failure;
    if (!this.options.credential)
      throw new GameError("missing credential", "authentication", 401);
  }
  private fail(error: unknown): void {
    if (this.closed || this.failure !== undefined) return;
    this.failure = error;
    this.lifetime.abort();
    this.receiver?.expire();
    if (this.pollTimer) clearInterval(this.pollTimer);
    this.stop();
  }
  private signal(): AbortSignal {
    return AbortSignal.any([
      this.lifetime.signal,
      AbortSignal.timeout(this.options.requestTimeoutMs ?? 10000),
    ]);
  }

  private async request(
    operation: () => Promise<{
      data?: unknown;
      error?: unknown;
      response: Response;
    }>,
    retry = false,
  ): Promise<unknown> {
    for (let attempt = 0; ; attempt++) {
      this.checkOpen();
      let result: Awaited<ReturnType<typeof operation>>;
      try {
        result = await operation();
      } catch (error) {
        if (
          !(error instanceof GameError && error.networkFailure) &&
          !(error instanceof TypeError)
        )
          throw error;
        if (!retry || attempt + 1 >= this.retries)
          throw error instanceof GameError
            ? error
            : new GameError("network error", "transport", undefined, true);
        await this.delay(
          Math.min(100 * 2 ** attempt, 1000),
          this.lifetime.signal,
        );
        continue;
      }
      if (!result.response.ok) {
        const status = result.response.status;
        const problem = result.error;
        const detail =
          typeof problem === "object" &&
          problem !== null &&
          "detail" in problem &&
          typeof problem.detail === "string"
            ? problem.detail
            : `request failed (${status})`;
        const safeDetail = this.options.credential
          ? detail.replaceAll(this.options.credential, "[redacted]")
          : detail;
        const error = new GameError(
          safeDetail,
          status === 401
            ? "authentication"
            : status === 429 || status >= 500
              ? "transport"
              : "http",
          status,
        );
        if (status === 401) this.fail(error);
        throw error;
      }
      try {
        return typeof result.data === "string"
          ? JSON.parse(result.data)
          : result.data;
      } catch {
        throw protocol("invalid JSON response");
      }
    }
  }
  private async token() {
    const data = await this.request(() =>
      this.api.POST("/lobby/v1/connection-token", {
        body: {},
        parseAs: "text",
        signal: this.signal(),
      }),
    );
    return parseToken(data);
  }
  private async readState(): Promise<State> {
    const data = await this.request(() =>
      this.api.GET("/game/from-lobby/{lobby_id}/generation", {
        params: { path: { lobby_id: this.lobbyId } },
        parseAs: "text",
        signal: this.signal(),
      }),
    );
    return parseState(data);
  }
  private validateIdentity(state: State): void {
    if (
      state.lobby_id !== this.lobbyId ||
      state.channel !== `player:${state.player_id}` ||
      (this.identity !== undefined &&
        this.identity !== this.stateIdentity(state))
    )
      throw protocol(
        "generation has wrong lobby, game, player, channel, or team",
      );
  }
  private stateIdentity(state: State): string {
    return JSON.stringify([
      state.game_id,
      state.player_id,
      state.channel,
      state.team_color,
    ]);
  }
  private install(state: State): Generation {
    this.validateIdentity(state);
    const snapshot = generationFromState(state);
    if (
      this.state &&
      (state.generation_id < this.state.generation_id ||
        (state.generation_id === this.state.generation_id &&
          this.state.submitted &&
          !state.submitted))
    ) {
      if (!this.snapshot)
        throw protocol("synchronized state has no public snapshot");
      return this.snapshot;
    }
    this.state = state;
    this.snapshot = snapshot;
    this.scheduleCallbacks();
    return snapshot;
  }
  private publication(data: unknown): void {
    if (this.closed || this.failure !== undefined) return;
    try {
      this.install(parseEvent(data).state);
    } catch {
      /* HTTP reconciliation is authoritative. */
    }
  }

  async start(): Promise<Generation> {
    this.checkOpen();
    this.startup ??= this.initialize();
    return this.startup;
  }
  private async initialize(): Promise<Generation> {
    try {
      const token = await this.token();
      this.realtime = (this.options.realtimeFactory ?? centrifugeFactory)({
        url: token.websocket_url,
        token: token.token,
        getToken: async () => (await this.token()).token,
        onPublication: (data) => this.publication(data),
        onReconcile: () => {
          this.pendingReconciliation = true;
          if (this.started) this.scheduleReconciliation();
        },
        onSubscribed: () => {
          this.pendingReconciliation = true;
          if (this.started) this.scheduleReconciliation();
        },
      });
      await this.realtime.connect();
      const initial = await this.readState();
      this.validateIdentity(initial);
      generationFromState(initial);
      this.identity = this.stateIdentity(initial);
      this.strategy = this.options.playerFactory(initial.team_color);
      if (this.strategy.teamColor !== initial.team_color)
        throw protocol("player factory returned a different team color");
      const timeout = this.options.subscriptionTimeoutMs ?? 10000;
      await abortable(
        this.realtime.subscribe(initial.channel, timeout),
        AbortSignal.any([this.lifetime.signal, AbortSignal.timeout(timeout)]),
      );
      const snapshot = this.install(await this.readState());
      this.started = true;
      if (this.pendingReconciliation) this.scheduleReconciliation();
      this.scheduleCallbacks();
      if (this.options.refreshIntervalMs !== undefined)
        this.pollTimer = setInterval(
          () => this.scheduleReconciliation(),
          this.options.refreshIntervalMs,
        );
      return snapshot;
    } catch (error) {
      await this.close();
      throw error;
    }
  }

  async refresh(): Promise<Generation> {
    this.checkOpen();
    if (!this.started) throw new GameError("client has not synchronized");
    return this.reconciliation.run(async () =>
      this.install(await this.readState()),
    );
  }
  private scheduleReconciliation(): void {
    if (this.closed || this.failure !== undefined) return;
    this.pendingReconciliation = true;
    if (this.reconcileTask) return;
    this.reconcileTask = (async () => {
      let failures = 0;
      while (
        this.pendingReconciliation &&
        !this.closed &&
        this.failure === undefined
      ) {
        this.pendingReconciliation = false;
        try {
          await this.refresh();
          failures = 0;
        } catch (error) {
          if (!(error instanceof GameError && error.kind === "transport"))
            throw error;
          await this.delay(
            Math.min(100 * 2 ** Math.min(failures++, 7), 10000),
            this.lifetime.signal,
          );
          this.pendingReconciliation = true;
        }
      }
    })()
      .catch((error) => this.fail(error))
      .finally(() => {
        this.reconcileTask = undefined;
        if (this.pendingReconciliation) this.scheduleReconciliation();
      });
  }
  private scheduleCallbacks(): void {
    if (
      !this.started ||
      this.closed ||
      this.failure !== undefined ||
      this.callbackTask
    )
      return;
    this.callbackTask = Promise.resolve()
      .then(async () => {
        const strategy = this.strategy;
        if (!strategy)
          throw protocol("synchronized client has no player strategy");
        while (
          !this.closed &&
          this.failure === undefined &&
          this.state &&
          this.state.generation_id > this.lastCallback
        ) {
          const state = this.state;
          this.lastCallback = state.generation_id;
          if (state.submitted) continue;
          const receiver = new CommandReceiver((command) =>
            this.register(state.generation_id, [command], false),
          );
          this.receiver = receiver;
          try {
            await abortable(
              strategy.onGeneration(
                generationFromState(state),
                receiver,
                this.lifetime.signal,
              ),
              this.lifetime.signal,
            );
          } finally {
            receiver.expire();
          }
          if (!this.closed && this.failure === undefined)
            await this.register(state.generation_id, [], true);
        }
      })
      .catch((error) => this.fail(error))
      .finally(() => {
        this.callbackTask = undefined;
        if (this.state && this.state.generation_id > this.lastCallback)
          this.scheduleCallbacks();
      });
  }
  private async register(
    generationId: number,
    commands: readonly Command[],
    complete: boolean,
  ): Promise<void> {
    this.checkOpen();
    await this.commands.run(async () => {
      this.checkOpen();
      if (!this.state) throw new GameError("client has not synchronized");
      if (
        complete &&
        (generationId < this.state.generation_id ||
          (generationId === this.state.generation_id && this.state.submitted))
      )
        return;
      const body = {
        game_id: this.state.game_id,
        generation_id: generationId,
        request_id: crypto.randomUUID(),
        complete,
        commands,
      };
      const result = await this.request(
        () =>
          this.api.POST("/generation/commands", {
            body,
            parseAs: "text",
            signal: this.signal(),
          }),
        true,
      );
      this.install(parseState(result));
    });
  }
  async wait(): Promise<void> {
    await this.stopped;
    if (this.failure !== undefined) throw this.failure;
  }
  async run(): Promise<void> {
    try {
      await this.start();
      await this.wait();
    } finally {
      await this.close();
    }
  }
  async close(): Promise<void> {
    if (this.shutdown) return this.shutdown;
    this.closed = true;
    this.receiver?.expire();
    this.lifetime.abort();
    if (this.pollTimer) clearInterval(this.pollTimer);
    this.stop();
    this.shutdown = (async () => {
      try {
        await this.realtime?.close();
      } finally {
        await Promise.allSettled(
          [this.callbackTask, this.reconcileTask].filter(
            (task) => task !== undefined,
          ),
        );
      }
    })();
    return this.shutdown;
  }
}
