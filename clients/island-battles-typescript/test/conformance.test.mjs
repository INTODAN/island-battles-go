import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";
import { setImmediate } from "node:timers/promises";
import { Ajv2020 } from "ajv/dist/2020.js";
import { GameError, IslandBattlesClient } from "../dist/index.js";

const directory = new URL("../../conformance/", import.meta.url);
const fixture = JSON.parse(await readFile(new URL("state.json", directory)));
const suite = JSON.parse(await readFile(new URL("scenarios.json", directory)));
const format = JSON.parse(await readFile(new URL("schema.json", directory)));
const validate = new Ajv2020().compile(format);
assert.ok(validate(suite), JSON.stringify(validate.errors));

// All I/O is in memory; these are execution opportunities, not elapsed waits.
async function drain() {
  for (let i = 0; i < 40; i++) await setImmediate();
}

class Adapter {
  state;
  requests = [];
  accepted = new Map();
  trace = [];
  callbacks = [];
  colors = [];
  signals = [];
  activeCallbacks = 0;
  gets = 0;
  get_status = 0;
  post_status = 0;
  transport_failures = 0;
  disconnected = false;
  unsubscribed = false;
  subscribed = false;
  receiver;
  release;
  reject;
  realtime;
  markedGets;
  client;
  constructor(scenario) {
    this.state = {
      ...structuredClone(fixture),
      ...structuredClone(scenario.initial ?? {}),
    };
    this.client = new IslandBattlesClient({
      baseUrl: "http://test",
      credential: "conformance-guest",
      lobbyId: fixture.lobby_id,
      sleep: async () => {
        await setImmediate();
      },
      fetch: async (request) => {
        this.signals.push(request.signal);
        assert.equal(
          request.headers.get("Authorization"),
          "Bearer conformance-guest",
        );
        const path = new URL(request.url).pathname;
        if (path === "/lobby/v1/connection-token") {
          assert.equal(request.method, "POST");
          this.trace.push("token");
          return Response.json({
            token: "connection-token",
            expires_at: "2099-01-01T00:00:00Z",
            websocket_url: "ws://test/connection/websocket",
          });
        }
        if (request.method === "GET") {
          assert.equal(path, `/game/from-lobby/${fixture.lobby_id}/generation`);
          this.trace.push("get");
          this.gets++;
          return this.get_status
            ? Response.json(
                { detail: "read failed" },
                { status: this.get_status },
              )
            : Response.json(this.state);
        }
        assert.equal(request.method, "POST");
        assert.equal(path, "/generation/commands");
        this.trace.push("post");
        const body = await request.json();
        assert.match(
          body.request_id,
          /^[a-f\d]{8}-[a-f\d]{4}-4[a-f\d]{3}-[89ab][a-f\d]{3}-[a-f\d]{12}$/,
        );
        this.requests.push(body);
        if (this.post_status)
          return Response.json(
            { detail: "command failed" },
            { status: this.post_status },
          );
        if (this.accepted.has(body.request_id))
          assert.deepEqual(this.accepted.get(body.request_id), body);
        this.accepted.set(body.request_id, body);
        if (body.complete && body.generation_id === this.state.generation_id)
          this.state.submitted = true;
        if (this.transport_failures) {
          this.transport_failures--;
          throw new TypeError("response lost after acceptance");
        }
        return Response.json(this.state);
      },
      realtimeFactory: (options) => {
        this.realtime = options;
        return {
          connect: () => {
            this.trace.push("connect");
          },
          subscribe: async (channel) => {
            assert.equal(channel, fixture.channel);
            this.subscribed = true;
            this.trace.push("subscribe", "ack");
            options.onSubscribed();
          },
          close: () => {
            this.disconnected = true;
            this.unsubscribed = this.subscribed;
          },
        };
      },
      playerFactory: (teamColor) => {
        this.colors.push(teamColor);
        return {
          teamColor,
          onGeneration: async (generation, receiver, signal) => {
            assert.equal(this.activeCallbacks, 0, "callbacks must be serial");
            this.activeCallbacks++;
            this.callbacks.push(generation.id);
            this.receiver = receiver;
            let onAbort;
            try {
              await new Promise((resolve, reject) => {
                this.release = resolve;
                this.reject = reject;
                onAbort = () =>
                  reject(new GameError("callback cancelled", "closed"));
                signal.addEventListener("abort", onAbort, { once: true });
                if (signal.aborted) onAbort();
              });
            } finally {
              signal.removeEventListener("abort", onAbort);
              this.activeCallbacks--;
            }
          },
        };
      },
    });
  }
  async perform(step) {
    switch (step.action) {
      case "start":
        await this.client.start();
        break;
      case "server":
        Object.assign(this.state, structuredClone(step.state ?? {}));
        for (const key of ["get_status", "post_status", "transport_failures"])
          if (key in step) this[key] = step[key];
        break;
      case "send":
        await this.receiver.sendSoldierGroup(
          step.origin,
          step.destination,
          step.amount,
        );
        break;
      case "return":
        this.release();
        break;
      case "fail":
        this.reject(new Error("scripted strategy failure"));
        break;
      case "wait_error":
        await this.client.wait();
        break;
      case "publish":
        this.realtime.onPublication({
          schema_version: step.schema_version ?? 1,
          type: step.type ?? "game.next_generation",
          state: {
            ...structuredClone(this.state),
            ...structuredClone(step.state ?? {}),
          },
        });
        break;
      case "reconnect":
        this.realtime.onSubscribed();
        break;
      case "refresh":
        await this.client.refresh();
        break;
      case "expired_send": {
        const before = this.requests.length;
        await assert.rejects(
          this.receiver.sendSoldierGroup(0, 1, 1),
          /expired|closed/,
        );
        assert.equal(this.requests.length, before);
        break;
      }
      case "close":
        await this.client.close();
        break;
      case "mark_gets":
        this.markedGets = this.gets;
        break;
      case "assert":
        this.observe(step);
        break;
      default:
        assert.fail(`unsupported shared action ${step.action}`);
    }
  }
  observe(step) {
    if ("callbacks" in step) assert.deepEqual(this.callbacks, step.callbacks);
    if ("factory_colors" in step)
      assert.deepEqual(this.colors, step.factory_colors);
    if ("generation" in step)
      assert.equal(this.client.generation.id, step.generation);
    if ("requests" in step)
      assert.deepEqual(
        this.requests.map(({ request_id, ...body }) => body),
        step.requests,
      );
    for (const [first, second] of step.same_requests ?? [])
      assert.deepEqual(this.requests[first], this.requests[second]);
    for (const [first, second] of step.different_request_ids ?? [])
      assert.notEqual(
        this.requests[first].request_id,
        this.requests[second].request_id,
      );
    if (step.startup_order) {
      const index = (name) => this.trace.indexOf(name);
      assert.ok(
        index("token") < index("connect") && index("connect") < index("get"),
      );
      assert.ok(
        index("get") < index("subscribe") && index("subscribe") < index("ack"),
      );
      assert.ok(this.trace.slice(index("ack") + 1).includes("get"));
    }
    if (step.unchanged_gets) {
      assert.notEqual(this.markedGets, undefined);
      assert.equal(this.gets, this.markedGets);
    }
    if (step.closed) {
      assert.equal(this.client.isClosed, true);
      assert.equal(this.activeCallbacks, 0);
      for (const signal of this.signals)
        assert.equal(signal.aborted, true, "owned requests must be cancelled");
      if (this.realtime) assert.equal(this.disconnected, true);
      if (this.subscribed) assert.equal(this.unsubscribed, true);
    }
  }
}

for (const scenario of suite.scenarios) {
  test(`shared: ${scenario.id}`, { timeout: 5000 }, async () => {
    const adapter = new Adapter(scenario);
    try {
      for (const [index, step] of scenario.steps.entries()) {
        try {
          if (step.error)
            await assert.rejects(adapter.perform(step), (error) =>
              step.error === "strategy"
                ? error.message === "scripted strategy failure"
                : error instanceof GameError && error.kind === step.error,
            );
          else await adapter.perform(step);
          await drain();
        } catch (cause) {
          throw new Error(
            `${scenario.id} step ${index}: ${JSON.stringify(step)}`,
            { cause },
          );
        }
      }
    } finally {
      await adapter.client.close();
    }
  });
}
