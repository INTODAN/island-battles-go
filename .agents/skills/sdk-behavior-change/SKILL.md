---
name: sdk-behavior-change
description: Add or modify observable behavior in Island Battles or generic lobby SDKs, including server and event contract changes, by updating the shared specification first and then every affected SDK. Excludes internal refactors that preserve behavior.
---

# SDK behavior changes

Use this workflow when a change affects what an SDK user can observe: public APIs, request or response data, event payloads, callbacks, retries, defaults, or other runtime behavior. Pure internal refactors that preserve those observations do not need this workflow.

Read [the runtime and conformance guide](../sdk-implementation/references/runtime-contract.md) for specification and testing guidance. Follow [sdk-implementation](../sdk-implementation/SKILL.md) for generation and runtime implementation details as needed.

## Establish the scope

1. Inspect the repository's current SDKs, shared specs, conformance scenarios, generators, and examples. Discover these from the checkout each time; do not rely on a fixed language or client list.
2. Identify every existing SDK and consumer affected by the requested behavior. Include examples and demos in the impact assessment; decide whether each needs an update.
3. Keep game-specific semantics in the Island Battles SDK contract. A generic lobby client or contract must not acquire game semantics unless the requested behavior explicitly changes that shared lobby product.
4. If SDK inventories, specs, or test commands are unclear, inspect the implementation, build files, and documentation before deciding what applies.

## Specify before implementing

1. Change the language-neutral shared behavior specification first, followed by its conformance scenarios. Treat them as the source of truth for the requested behavior.
2. If no shared behavior specification or conformance suite exists, bootstrap them under `docs/sdk/behavior.md` and `clients/conformance/` before changing SDK implementations.
3. Use current code and tests as evidence about existing behavior, not as automatic authority over a new behavior requested by the user. Resolve conflicts in favor of the user's request, documenting the intended observable behavior in the shared spec.
4. Write scenarios that describe observable inputs, outputs, events, defaults, and failure or retry behavior where relevant. Keep them language-neutral and useful to every applicable SDK.
5. The skill's runtime guide provides a starting checklist, not a second normative specification. Put the actual requirements and changed expectations in the repository's shared behavior specification and executable scenarios.

## Update every applicable SDK

1. For wire changes, update Go/Huma DTOs, route registration, event schemas, and affected server behavior, then export contracts and regenerate every affected generated client. Use pinned generation tools and configuration; do not hand-edit generated output.
2. Port the remaining specified behavior to every existing SDK in scope. Do not stop after the most visible client or after one language passes. Behavior-only changes do not require artificial wire schema changes.
3. Keep each SDK's public API, runtime helpers, defaults, event handling, and error behavior consistent with the shared spec, accounting for idiomatic language differences.
4. Update examples, demos, and other consumers where their use or explanation becomes incorrect. An example is not automatically a full SDK; assess it separately.
5. Known clients include `clients/island-battles-python` and `lobby/clients/lobby-python`. Discover the current inventory rather than assuming these are the only SDKs. The lobby Python client is generic and should remain free of Island Battles game rules.

## Verify and report

1. Run the shared conformance scenarios against each applicable SDK; add or extend its adapter if needed. Run export/generation drift checks for changed contracts and existing targeted build, type, lint, and integration checks relevant to the change. Missing functionality in an affected SDK is work to implement, not a reason to mark a scenario inapplicable.
2. If a language's test or build tooling is unavailable, state which SDK and check could not be run. Do not imply that an unrun check passed.
3. Review the final inventory against the initial scope. Report changed SDKs, specs and scenarios, checks run and their results, and any SDK left unverified or stale with the reason.
4. Never claim full cross-SDK verification when an applicable SDK or required check was skipped. Do not leave other SDKs silently stale.
