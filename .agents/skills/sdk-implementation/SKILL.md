---
name: sdk-implementation
description: Implement an Island Battles or generic lobby client SDK in a new language, or migrate an SDK to contract-generated HTTP code, with a handwritten runtime and shared behavioral conformance tests.
---

# Implement a client SDK

Generate HTTP operations and wire models from the server contract. Implement the remaining runtime and public conveniences against a shared, language-neutral behavior specification. A working generated package alone is not a complete player SDK.

## Establish the shared contract

1. Inventory the SDKs, exported contracts, generation scripts, behavior specifications, and test harnesses in the current checkout. Distinguish reusable lobby behavior from Island Battles gameplay. Examples and browser demos are affected consumers when relevant, but are not automatically full SDK implementations.
2. Read [the runtime and conformance guide](references/runtime-contract.md) before designing the runtime or its tests. Reuse the repository's existing shared specification and scenarios. If they do not exist, establish `docs/sdk/behavior.md` and `clients/conformance/`, using the existing SDKs, tests, backend behavior, and the user's requirements as evidence. Resolve inconsistencies explicitly; do not copy an implementation bug into the specification.
3. Write the observable behavior and shared scenarios before implementing the new runtime. Record which behavior is common to all SDKs and which is lobby- or game-specific. If the task changes shared behavior, also follow [sdk-behavior-change](../sdk-behavior-change/SKILL.md) and update existing SDKs; do not create a language-specific protocol variant.
4. Inspect the actual exported schema, not only Go structs. Ensure public operations have stable operation IDs, typed inputs and outputs, bearer security, errors, enums, numeric constraints, and intentional required/optional/null semantics. Generators cannot infer constraints enforced only in service code or prose.
5. Close schema gaps in the Go/Huma source before generation. Model public command parameters explicitly rather than as arbitrary JSON; use a discriminated union when multiple public command variants require one. Give realtime envelopes named DTOs and register/export their schemas. Reuse the HTTP state schema inside events instead of duplicating its definition.
6. Provide a deterministic offline export and drift check for the relevant public API and event schemas. Retain the standalone reusable lobby contract. The running combined `/openapi.json` is useful for inspection, but reproducible generation must not require a running backend or database. Keep internal and trusted proxy routes out of public SDK exports.

## Generate the transport layer

- Choose a maintained generator suitable for the target language and the contract's OpenAPI version. Consult current official documentation when choosing tooling. Use Huma's 3.0.3 export if a selected generator needs it; check that conversion preserves the needed schema semantics.
- Pin the generator and relevant formatting tools, commit their configuration, and provide one documented regeneration command plus a non-mutating check mode. Treat warnings about skipped operations, unsupported types, or lost constraints as issues to resolve, not successful generation.
- Generate HTTP calls, authentication plumbing, serialization, and wire models into a separate replaceable package or directory. Never hand-edit generated files. Put custom behavior in wrappers/adapters; use reproducible generator configuration or templates only where necessary.
- Verify the output includes every expected operation and model, including explicitly registered event models. Check actual serialization and validation behavior: generated types do not necessarily enforce the same runtime constraints as the existing Python models.

## Implement the runtime and player API

- Use generated operations and wire models throughout the transport boundary. Reuse an official Centrifugo SDK where available for connection and subscription mechanics. Event schema generation supplies payload types, not reconnect reconciliation or callback scheduling.
- Implement the behavior specification: authenticated identity checks, startup/subscription ordering, recovery, stale-state protection, deduplication, command request identity, generation scoping, callback completion/failure, cancellation, and cleanup. Apply the lobby lease/heartbeat rules to lobby clients without introducing game semantics into them.
- Keep the language's public API idiomatic while preserving shared observable behavior. Handwritten conveniences can include `Player`, a callback-scoped command receiver, distance/team helpers, and resolving a soldier group's destination castle from its ID. Avoid leaking transport metadata into the strategy API merely because generated models contain it.
- Document generation commands, runtime lifecycle, how lobby access stays alive during gameplay, a minimal playable example, and how to run verification. Keep documentation consistent with the implemented capabilities.

## Verify completion

1. Implement a small language adapter for the shared conformance scenarios. Assert outgoing requests, callback invocations, public state, and surfaced errors using controlled HTTP/realtime inputs. Use deterministic transport failures and scheduling rather than wall-clock sleeps.
2. Add meaningful tests for language-specific public conveniences and transport/model integration. Run the shared scenarios for the new SDK and affected existing SDKs; do not replace them with tests that only restate generated code.
3. Run generation drift checks, compilation/type checks, formatting/lint checks, and relevant existing tests. Add or extend focused backend/Centrifugo integration coverage for real authentication, subscriptions, and recovery when the change affects those boundaries.
4. Integrate reproducible export, generation, and conformance checks into the repository's existing verification mechanism, including CI if present. When first establishing the shared contract workflow, exercise the reference Python SDK and the new language to expose assumptions specific to one generator or runtime.
5. Report the generated/runtime split, SDKs covered, exact verification performed, and any unavailable tooling or unverified behavior. Do not call an SDK complete while required behavior or supported operations are missing.

## Repository starting points

These paths are discovery aids, not an exhaustive SDK inventory. Resolve them from the current repository root.

- `lobby/cmd/export-lobby-openapi/`, `lobby/api/`: existing offline OpenAPI and event export.
- `lobby/clients/lobby-python/scripts/generate.py`, `generator-config.yaml`, and `tools/pyproject.toml`: pinned generation and drift-check pattern.
- `lobby/clients/lobby-python/lobby_client/`: handwritten lobby runtime over a generated HTTP client.
- `backend/routes/`, `backend/models/generation_input.go`, `backend/models/clash.go`, `backend/services/generation_input.go`: game registration, wire DTOs, and event production.
- `clients/island-battles-python/island_battles_client/` and `tests/`: reference game API, runtime, and behavior tests.
