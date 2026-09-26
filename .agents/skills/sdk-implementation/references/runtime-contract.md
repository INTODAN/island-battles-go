# Shared SDK behavior and conformance

Use this guide when establishing or changing the language-neutral SDK specification and executable scenarios. It describes how to maintain those artifacts; it is not itself the versioned SDK specification. Discover the current implementation and requirements before adopting or changing any baseline below.

## Sources of authority

- Go/Huma DTOs and route registration define the exported wire contract. Edit their source and regenerate artifacts instead of editing generated schemas or client code.
- The shared behavior specification defines how SDKs coordinate requests, events, and user callbacks over time. Keep it consistent with the wire contract and server behavior.
- Shared scenarios exercise observable requirements from that specification. Language adapters translate those scenarios into each SDK's public API and test transport hooks.
- Existing implementation and tests provide evidence when bootstrapping the specification. New user requirements can intentionally change existing behavior; record that change in the specification before implementation.

Reuse established repository locations. When bootstrapping, use `docs/sdk/behavior.md` for normative behavior and `clients/conformance/` for shared scenarios and payload fixtures. Maintain an SDK inventory with package location, supported scope, adapter entry point, and verification commands. Avoid parallel specifications for individual languages.

## What the specification should say

Give requirements stable identifiers so scenarios can cite them. Describe inputs, preconditions, observable effects, failure behavior, and the scope of guarantees. Express ordering and state transitions without prescribing Python tasks, Go goroutines, locks, or class inheritance. Document language-specific naming separately from protocol semantics.

For the existing game client, inspect these baseline behaviors and encode them where they remain intended:

| Area | Behavior to specify and exercise |
| --- | --- |
| Identity | Credentials authorize HTTP access; the first authorized state establishes game/player/team identity. Reject or ignore foreign state according to its HTTP or event origin. Construct the player once with the authorized team. |
| Startup | Discover the authorized player channel, subscribe and await acknowledgment, then reconcile HTTP state so events missed during startup do not leave the client behind. |
| Events and recovery | Accept supported event types/versions, deduplicate repeated generations, reconcile after subscription/reconnect, and recover from temporary reconciliation failures. HTTP is authoritative for reconciliation; polling is disabled by default. |
| State ordering | Do not replace a newer generation with an older one. A late response must not change a submitted turn back to unsubmitted within the same generation. |
| Callback scheduling | Run player callbacks serially. Do not replay an already submitted turn. Define how queued generations superseded by newer state are handled. |
| Command identity | Pin each command to its callback's generation. A transport retry reuses the exact request UUID and payload; a distinct user command gets a new UUID even with identical arguments. Specify retry limits and which failures qualify. |
| Completion | Successful callback return completes its turn, including an empty turn. Exceptions or cancellation do not automatically complete it. The command receiver expires when its callback ends. |
| Lifecycle | Surface authentication and background failures, release owned resources on close, and define cancellation/ownership behavior for injected transports. |
| Public models | Define exposed snapshot fields, ID meaning, optional/absent values, destination-castle resolution, distance units, and neutral/friendly/enemy classification. Specify mutation semantics if part of the public contract. |

Keep lobby rules in their own scope: guest identity, connection-token refresh, membership, leases, heartbeat conditions, reconciliation, and cleanup. State which component owns the lobby lease during gameplay; starting a game client must not silently create a second owner or abandon required heartbeats.

Do not promise exactly-once callbacks across process restarts or delivery of every intermediate generation unless the protocol and SDK explicitly support it. In-session event deduplication and server command idempotency are separate guarantees.

## Executable scenarios

Use a shared declarative format such as JSON with these concepts:

- Stable scenario ID and referenced requirement IDs.
- Applicable scope/capabilities, explicit initial state, and wire payload fixtures.
- Ordered stimuli: player actions, HTTP results or failures, realtime notifications, disconnect/reconnect, and cancellation.
- Synchronization points or a controlled clock for retries and races.
- Expected requests, callback trace, public state, and surfaced failures.

Share both scenario inputs and expected outcomes. Do not merely translate Python tests into independent hardcoded test suites: languages would drift again. Build the smallest shared scenario vocabulary needed for real cases; avoid a general-purpose test scripting language.

Example scenario, expressed here in prose until the repository chooses its executable format:

1. Establish generation 42 and start its player callback.
2. The player sends 5 soldiers from castle A to castle B.
3. Record request R and simulate a lost response after server acceptance.
4. Advance the retry scheduler and return success to the retry.
5. Assert the retry has the same UUID, game/generation IDs, and command payload as R.
6. Issue a second identical player command and assert its UUID differs from R.

Use relationships such as “same UUID as request R” instead of literal random UUIDs. Check semantic JSON equality, not key order. Simulate failures through the transport hook where a fake HTTP status cannot represent a connection failure. Add targeted scenarios for duplicate events, missed events, stale responses, generation changes during callbacks, callback failure, completion retries, and receiver expiry.

## Language adapters and integration

Each SDK needs a thin test adapter that can start/close the client, supply a scripted player, drive controlled HTTP/realtime transports, and capture observable outputs. The SDK must perform its real parsing, state reconciliation, request construction, and callback lifecycle. Fakes control the environment; they must not reimplement the behavior being tested.

Require every SDK supporting a behavior to run the same applicable cases. Keep applicability explicit in the inventory and runner. A game scenario can be inapplicable to a generic lobby-only SDK; a missing feature in a game SDK is a gap to implement, not grounds to skip its case. Report unsupported capabilities, skipped cases, and unavailable tooling distinctly from passing results.

Use shared fake-transport scenarios for deterministic runtime verification and a smaller real-backend/Centrifugo suite for authentication, serialization, subscription behavior, and recovery. Unit conformance alone does not prove compatibility with the real transport. Reuse existing integration infrastructure and add cases according to the changed boundary.

## Changing a requirement

Update the shared specification first, then its executable scenarios, then Go/Huma contracts and every affected SDK. Preserve scenario IDs for the same requirement where practical, and rewrite expectations to the newly intended behavior. Do not silently retain old behavior in one language, hand-edit generated output, or make the test runner accept both behaviors simply to obtain a pass.
