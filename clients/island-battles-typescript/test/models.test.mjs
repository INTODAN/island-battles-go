import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";
import { GameError, IslandBattlesClient } from "../dist/index.js";
import { generationFromState } from "../dist/models.js";
import { parseEvent, parseState, parseToken } from "../dist/protocol.js";

const fixture = JSON.parse(
  await readFile(new URL("../../conformance/state.json", import.meta.url)),
);
const token = {
  token: "connection-token",
  expires_at: "2099-01-01T00:00:00Z",
  websocket_url: "ws://test/connection/websocket",
};

function stateWithGroup() {
  const state = structuredClone(fixture);
  state.generation.soldier_groups.push({
    id: 0,
    soldiers: 4,
    team_color: 1,
    x: -1,
    y: 0,
    origin_persistent_castle_id: 0,
    destination_persistent_castle_id: 1,
    clash_radius: 0.1,
  });
  state.generation.clashes = [
    {
      id: 1,
      time: 0.4,
      participants: [
        {
          group_id: 99,
          team_color: 2,
          x: 0,
          y: 0,
          clash_radius: 0.2,
          soldiers_before: 2,
          soldiers_after: 0,
        },
      ],
    },
  ];
  return state;
}

test("snapshot resolves persistent destinations, distances and team relationships", () => {
  const state = stateWithGroup();
  const snapshot = generationFromState(parseState(state));
  const [castle, enemy] = snapshot.castles;
  const [group] = snapshot.soldierGroups;
  assert.equal(castle.id, 0);
  assert.equal(group.destinationCastle, enemy);
  assert.equal(castle.distance(enemy), 4);
  assert.equal(group.distanceLeft(), 3);
  assert.equal(castle.compareTeam(group), "friendly");
  assert.equal(castle.compareTeam(enemy), "enemy");
  assert.equal(castle.compareTeam({ teamColor: 0 }), "neutral");
  assert.equal(snapshot.clashes[0].participants[0].groupId, 99);
  assert.equal(snapshot.clashes[0].time, 0.4);
  assert.deepEqual(Object.keys(snapshot).sort(), [
    "castles",
    "clashes",
    "id",
    "soldierGroups",
  ]);
  assert.throws(() => {
    castle.soldiers = 1;
  }, TypeError);
  assert.throws(() => {
    snapshot.castles.push(castle);
  }, TypeError);
  assert.throws(() => {
    snapshot.clashes[0].participants[0].soldiersAfter = 3;
  }, TypeError);
  state.generation.castles[0].soldiers = 999;
  assert.equal(castle.soldiers, 30);
  assert.deepEqual(generationFromState(parseState(fixture)).clashes, []);
});

test("wire validators reject non-finite, coerced, unsafe and missing values", () => {
  for (const bad of [
    true,
    "1",
    null,
    Infinity,
    NaN,
    Number.MAX_SAFE_INTEGER + 1,
  ]) {
    const state = structuredClone(fixture);
    state.generation.castles[0].soldiers = bad;
    assert.throws(() => parseState(state), { kind: "protocol" });
  }
  for (const bad of [true, "1", null, Infinity, NaN]) {
    const state = structuredClone(fixture);
    state.generation.castles[0].x = bad;
    assert.throws(() => parseState(state), { kind: "protocol" });
  }
  const state = structuredClone(fixture);
  delete state.generation.castles[0].x;
  assert.throws(() => parseState(state), { kind: "protocol" });
  assert.throws(() => parseState({ ...fixture, lobby_id: "invalid" }), {
    kind: "protocol",
  });
  assert.throws(() => parseState({ ...fixture, generation_id: 0 }), {
    kind: "protocol",
  });
  assert.throws(
    () =>
      parseEvent({
        schema_version: true,
        type: "game.next_generation",
        state: fixture,
      }),
    { kind: "protocol" },
  );
  assert.throws(() => parseToken({ ...token, expires_at: "tomorrow" }), {
    kind: "protocol",
  });
});

test("missing destination and invalid clash fractions are protocol errors", () => {
  const state = stateWithGroup();
  state.generation.soldier_groups[0].destination_persistent_castle_id = 100;
  assert.throws(() => generationFromState(parseState(state)), {
    kind: "protocol",
  });
  state.generation.clashes[0].time = 1.1;
  assert.throws(() => parseState(state), { kind: "protocol" });
});

function fakeOptions(overrides = {}) {
  return {
    baseUrl: "http://test",
    credential: "guest",
    lobbyId: fixture.lobby_id,
    fetch: async (request) =>
      Response.json(
        new URL(request.url).pathname.includes("connection-token")
          ? token
          : { ...fixture, submitted: true },
      ),
    realtimeFactory: () => ({ connect() {}, async subscribe() {}, close() {} }),
    playerFactory: (teamColor) => ({ teamColor, async onGeneration() {} }),
    ...overrides,
  };
}

test("malformed HTTP JSON closes resources before surfacing a protocol error", async () => {
  let closed = false;
  const client = new IslandBattlesClient(
    fakeOptions({
      fetch: async (request) =>
        new URL(request.url).pathname.includes("connection-token")
          ? Response.json(token)
          : new Response("not-json"),
      realtimeFactory: () => ({
        connect() {},
        async subscribe() {},
        close() {
          closed = true;
        },
      }),
    }),
  );
  await assert.rejects(client.start(), { kind: "protocol" });
  assert.equal(closed, true);
  assert.equal(client.isClosed, true);
});

test("concurrent start creates the strategy once and rejects a wrong team", async () => {
  const colors = [];
  const client = new IslandBattlesClient(
    fakeOptions({
      playerFactory: (teamColor) => {
        colors.push(teamColor);
        return { teamColor, async onGeneration() {} };
      },
    }),
  );
  try {
    const snapshots = await Promise.all([client.start(), client.start()]);
    assert.deepEqual(colors, [1]);
    assert.equal(snapshots[0], snapshots[1]);
  } finally {
    await client.close();
  }
  const wrong = new IslandBattlesClient(
    fakeOptions({
      playerFactory: () => ({ teamColor: 9, async onGeneration() {} }),
    }),
  );
  await assert.rejects(wrong.start(), { kind: "protocol" });
  assert.equal(wrong.isClosed, true);
});

test("close aborts a pending HTTP reconciliation", async () => {
  let hold = false;
  let pending;
  const started = new Promise((resolve) => {
    pending = resolve;
  });
  const client = new IslandBattlesClient(
    fakeOptions({
      fetch: async (request) => {
        if (new URL(request.url).pathname.includes("connection-token"))
          return Response.json(token);
        if (!hold) return Response.json({ ...fixture, submitted: true });
        pending();
        return new Promise((_resolve, reject) =>
          request.signal.addEventListener(
            "abort",
            () => reject(new TypeError("aborted")),
            { once: true },
          ),
        );
      },
    }),
  );
  await client.start();
  hold = true;
  const refresh = client.refresh();
  const rejection = assert.rejects(refresh, { kind: "closed" });
  await started;
  await client.close();
  await rejection;
  await client.close();
});

test("HTTP errors preserve safe status and detail without retrying", async () => {
  const client = new IslandBattlesClient(
    fakeOptions({
      fetch: async () =>
        Response.json({ detail: "authorization denied" }, { status: 403 }),
    }),
  );
  await assert.rejects(
    client.start(),
    (error) =>
      error instanceof GameError &&
      error.kind === "http" &&
      error.status === 403 &&
      error.message === "authorization denied",
  );
});

test("startup retains subscription wakeups received during reconciliation", async () => {
  let realtime;
  let gets = 0;
  let release;
  let announce;
  const reading = new Promise((resolve) => {
    announce = resolve;
  });
  const staleRead = new Promise((resolve) => {
    release = resolve;
  });
  let state = { ...fixture, submitted: true };
  const client = new IslandBattlesClient(
    fakeOptions({
      fetch: async (request) => {
        if (new URL(request.url).pathname.includes("connection-token"))
          return Response.json(token);
        gets++;
        if (gets === 2) {
          const stale = structuredClone(state);
          announce();
          await staleRead;
          return Response.json(stale);
        }
        return Response.json(state);
      },
      realtimeFactory: (options) => {
        realtime = options;
        return {
          connect() {},
          async subscribe() {
            options.onSubscribed();
          },
          close() {},
        };
      },
    }),
  );
  try {
    const starting = client.start();
    await reading;
    state = { ...state, generation_id: 2 };
    realtime.onSubscribed();
    release();
    await starting;
    for (let i = 0; i < 40; i++)
      await new Promise((resolve) => setImmediate(resolve));
    assert.equal(client.generation.id, 2);
    assert.ok(gets >= 3);
  } finally {
    await client.close();
  }
});

test("canonicalizes caller UUIDs before discovery and identity comparison", async () => {
  const lobbyId = "abcdefab-cdef-abcd-efab-cdefabcdefab";
  const client = new IslandBattlesClient(
    fakeOptions({
      lobbyId: lobbyId.toUpperCase(),
      fetch: async (request) => {
        const path = new URL(request.url).pathname;
        if (path.includes("connection-token")) return Response.json(token);
        assert.ok(path.includes(lobbyId));
        return Response.json({
          ...fixture,
          lobby_id: lobbyId,
          submitted: true,
        });
      },
    }),
  );
  try {
    await client.start();
    assert.equal(client.generation.id, 1);
  } finally {
    await client.close();
  }
});

test("permanent disconnect or unsubscribe triggers HTTP authorization reconciliation", async () => {
  let realtime;
  let revoked = false;
  const client = new IslandBattlesClient(
    fakeOptions({
      realtimeFactory: (options) => {
        realtime = options;
        return { connect() {}, async subscribe() {}, close() {} };
      },
      fetch: async (request) => {
        if (new URL(request.url).pathname.includes("connection-token"))
          return Response.json(token);
        if (revoked)
          return Response.json({ detail: "session revoked" }, { status: 401 });
        return Response.json({ ...fixture, submitted: true });
      },
    }),
  );
  try {
    await client.start();
    revoked = true;
    realtime.onReconcile();
    await assert.rejects(client.wait(), {
      kind: "authentication",
      status: 401,
    });
  } finally {
    await client.close();
  }
});
