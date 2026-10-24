# Execution plan for the open issues (October 2026)

This plan covers the order, the decisions and the rules for implementing every open issue with parallel agents that merge to `main` on their own. The orchestrating session reads all of it. Each implementing agent reads its issue (body and comments), the [rules for every agent](#rules-for-every-agent), and any [shared contract](#shared-contracts) its issue takes part in.

Decisions were made with the developer on 2026-10-24. They are also posted as comments on the affected issues. Where an issue body and a later decision comment disagree, the comment wins.

## Issues in scope

- **Implemented by agents:** #15, #16, #17, #18, #20, #21, #23, #24, #27, #28, #29, #30, #31, #32, #33, #35, #36, #37, #38, #39, #40, #41, #42, #43, #44, #46, #47.
- **Infrastructure written but not applied:** #27, #30, #31, #32, #33, #38 and the AWS parts of #46. Agents write the Terraform, Ansible, scripts and `deploy/README.md`, but they never run `terraform apply`, never touch a real VM and never use AWS credentials.
- **Done with the developer, not by agents:** #45, the session that applies the infrastructure and adds the DNS record.
- **Umbrella issues with no work of their own:** #22, #25, #26, #34. Close each one once its sub-issues are closed. #22 also needs the `PlayerChannel` item, which is folded into #36.

## Order

An issue can start as soon as every issue in its "Starts after" column has merged to `main`. Batches are only a convenience, so don't wait for a whole batch to finish.

| Issue | Starts after | Why |
|---|---|---|
| #28 push to Centrifugo directly, no queues or loops | — | Unblocks #29, #35, #24 and #44 |
| #23 versioned migrations (lobby and map templates) | — | Unblocks #29 and #24 |
| #16 clash soldier counts | — | Unblocks #15 |
| #17 one board projection | — | Unblocks #18 |
| #36 authorize games from their roster, plus #22's `PlayerChannel` item | — | Unblocks #18 and #24 |
| #40 game settings at lobby creation | — | Defines per-game registration; unblocks #43, #47 and #24 |
| #41 map file format | — | Unblocks #42, #43 and #37 |
| #39 game catalog for the lobby frontend | — | Unblocks #43 |
| #21 scrollbar bug | — | |
| #20 Podman test docs | — | |
| VM deploy code: #27 + #30 + #31 (one agent) | — | Code only |
| #15 simulation separated from persistence | #16 | #16 changes the handler that #15 moves |
| #18 replay built from generations | #17, #36 | Uses #17's projection; both edit the replay handler |
| #29 Lambda entry point and migrate command | #28, #23 | All three rewrite `main.go` and the database setup |
| #42 map builder UI | #41 | |
| #43 map choice in the lobby | #39, #40, #41 | |
| #47 every team needs a player | #40 | Extends #40's per-game registration |
| #35 end games | #15, #18, #28 | Needs pure game state, the new outcome storage and the 300 cap |
| #37 protect the map and graph endpoints | #29, #41 | |
| #24 lobby retention | #23, #28, #36, #40, #47 | |
| AWS deploy code: #32 + #33 + #38 (one agent) | #29, VM deploy code | Code only; shares `deploy/README.md` |
| #44 game retention | #18, #28, #35 | |
| #46 observability within the free tier | #29, AWS deploy code | |
| #45 deploy session | everything above | With the developer |

Batches, if it helps:

1. #28, #23, #16, #17, #36, #40, #41, #39, #21, #20, VM deploy code
2. #15, #18, #29, #42, #43, #47
3. #35, #37, #24, AWS deploy code
4. #44, #46

The longest chains are four merges deep: #16 → #15 → #35 → #44 and #17 → #18 → #35 → #44. When fewer agents fit at once, start #28, #23, #16, #17 and #36 first.

## Decisions

### Data and schema

- **Game data can be reset; lobby data and map templates can't.** README code philosophy #6 and `.agents/skills/breaking-changes/SKILL.md` now say so.
  - Game data covers games, their maps, castles, generations, soldier groups, commands, player inputs, requests, team bindings, frozen rosters and replays. It may be reset on breaking changes, with no data migrations.
  - Lobby data is every `lobby_*` table. Map templates are `map_templates` and `castle_templates`. Neither is ever reset; change them only through a new versioned migration that keeps existing rows.
- **The lobby API can still break.** The exception covers stored data only. Clients upgrade.
- **#23:**
  - Two migration sets with separate history tables: one owned by the lobby (`lobby/postgres`) and one owned by the backend for map templates.
  - Game tables stay on GORM `AutoMigrate`.
  - Hold a PostgreSQL advisory lock while migrating.
  - The library is the agent's choice; goose with embedded SQL is a reasonable default.
  - When #23 lands, replace the "(#23)" reference in the breaking-changes skill with the real migration directories.

### Delivery to clients (#28)

- **Push directly.** After the transaction commits, the request pushes to Centrifugo's HTTP API and retries once or twice within about a second. If it still fails, log an error and move on.
- **No messaging through Postgres.** Delete `lobby_outbox` and its store, the `lobby/delivery` worker, `generation_notifications`, `GenerationDelivery`, the claim and retry logic and the sweeper. After #28, no table holds messages waiting to be sent, and Postgres holds only state. Don't add a replacement queue, a Centrifugo outbox consumer or LISTEN/NOTIFY.
- **Give the `player` namespace history and recovery,** like `lobby` (`history_size`, `history_ttl`, `force_recovery`). Clients that reconnect then get missed game notifications from Centrifugo itself. Clients also keep re-reading their state over HTTP on reconnect.
- **No timer, no EventBridge, no scheduled job.** The backend starts no goroutines that outlive a request.
- **Keep the 300-generation cap.** Commands after the cap are rejected.
- **Keep the lobby's HTTP heartbeat.** Letting Centrifugo track who's connected is out of scope.
- **Update the docs** that describe "durable" invalidations and game event queues: the root README, `backend/README.md`, `lobby/README.md` and `lobby/docs/multiplayer-architecture.md`.

### Cleanup (#24, #44)

- Both run in small batches at the end of normal requests, because there is no timer. When nobody uses the game, nothing grows.
- #24 loses its step for acknowledged outbox rows, and #44 loses the notification table, because #28 removes both.
- Never delete map templates.

### Game rules

- **#35, last team standing.** A team is out once it has no castles and no soldier groups. The game ends when one team is left, and neutral castles don't need to be captured. If every remaining team is eliminated in the same turn, it's a draw.
  - At the 300-generation cap, the team with the most castles wins. Equal castle counts are a draw.
  - `game.ended` is pushed directly to every roster player.
- **#43:** the map sets the teams and the neutral castles. Picking a map fills in the lobby's teams from the map's colours. The server requires the map's team colours to be exactly 1..N for a lobby with N teams.
- **#47:** every Island Battles team needs at least one player to start, on any map, including the generated one.
- **#40:** settings are fixed when the lobby is created. There's no update endpoint.

### Deployment (#26, #27, #30, #31, #32, #33, #38)

- **Architecture:**
  - The backend runs on Lambda, and players use the Lambda Function URL itself.
  - There is no CloudFront, no ACM certificate, no VPC, no Lightsail VPC peering and no EventBridge.
  - The Lambda reaches Postgres and Centrifugo's API over the internet with TLS. Postgres accepts only `hostssl` connections, with scram and a strong generated password. Centrifugo's API needs TLS and its API key.
- **Ports on the VM:** 22, 443 and Postgres. Centrifugo's admin UI stays off.
- **DNS:** a single record, `vm.<domain>` pointing at the static IP, so Centrifugo's autocert can get a certificate. Squarespace has no API, so the record is added by hand in #45.
- **#32, Terraform:** creates the Lightsail instance, static IP, firewall ports, automatic snapshots, the Lambda, its IAM role and its Function URL. Its state is a gitignored local file. `.gitignore` covers `.terraform/` and `*.tfstate*`.
- **#31:** moving to a new VM copies the data with `pg_dump` and restore. #31 owns the dump and restore tasks.
- **#33:** backups are Lightsail automatic snapshots only, with nothing uploaded anywhere.
- **#38:** a deploy script publishes the Lambda code and settings, and runs migrations directly against Postgres, which is reachable over TLS.
- **Not a concern:** the cost risk of the public Lambda URL. The developer accepted it.

### Observability (#46)

Everything stays inside AWS's free monthly allowances, worst case included. #46 has the budget and the limits; follow it exactly.

## Rules for the orchestrator

- **Agents:** run one agent per row of the order table, each in its own git worktree. The default workflow size guideline is under 10 agents, so run batches or raise the limit in `/config`.
- **Merging:** merge one PR at a time, using the merge procedure below.
- **Browser checks:** these need `scripts/dev.sh`, which uses fixed ports (5432, 8000, 8888) and fixed container names, and Chrome on port 9222. Only one agent may run them at a time. Hold the lock with `flock /tmp/island-battles-browser.lock <command>`, or have the orchestrator hand out the turn.
- **Never start #45.** When everything else is merged, tell the developer it's ready.
- **Closing issues:** close the umbrella issues once their sub-issues are closed. The infrastructure issues stay open until #45 ticks their live acceptance boxes. For those, the PR says `Part of #n`, not `Closes #n`.

## Rules for every agent

### Before writing code

- Read your issue with its comments. Then read README "Code philosophies", `.agents/skills/backend-development`, `breaking-changes`, `game-state-testing`, and `browser-testing` if you change the UI.
- Branch from the latest `main` as `feature/<n>-<slug>` (`fix/` for bugs, `docs/` for documentation). The PR body says `Closes #n`.

### Tests

The repo has no CI, so these are the gate.

The database tests need rootless Podman settings:

```sh
export DOCKER_HOST=unix://$XDG_RUNTIME_DIR/podman/podman.sock
export TESTCONTAINERS_RYUK_DISABLED=true
```

Always run:

```sh
go fmt ./... && go -C lobby fmt ./...
go vet ./... ./lobby/...
golangci-lint run --timeout=5m ./... ./lobby/...
go test ./... ./lobby/...
go run ./lobby/cmd/export-lobby-openapi --output lobby/api/openapi.json --events-output lobby/api/events.json --check
go run ./backend/cmd/export-openapi --output backend/api/openapi.json --check
uv run --project clients/island-battles-python/tools --locked python clients/island-battles-python/scripts/generate.py --check
uv run --project lobby/clients/lobby-python/tools --locked python lobby/clients/lobby-python/scripts/generate.py --check
uv run --project lobby/clients/lobby-python --locked pytest lobby/clients/lobby-python/tests
uv run --project clients/conformance --locked pytest clients/conformance/tests/test_models.py
```

Also run, depending on the change:

- **Lobby, realtime, start or Centrifugo changes:** `./scripts/test-lobby-acceptance.sh`. Its ports are dynamic, so it can run alongside other agents.
- **Player API or game event changes:** `./scripts/test-client-conformance.sh`.
- **UI changes:** a browser check under the browser lock.

Tests that leave containers behind after an interrupted run can be found with `podman ps -a`. Never remove `island-battles-dev-db` or `island-battles-dev-centrifugo`.

### Merging

1. Rebase on the latest `main`.
2. Don't hand-merge generated files: `backend/api/openapi.json`, `lobby/api/*.json`, and the generated Python models in both clients. Regenerate them.
3. If another branch already took your migration number, renumber yours. Never edit a migration that's already on `main`.
4. Run all the tests again.
5. Ask a separate agent to review the diff, and fix what it confirms.
6. Merge with `gh pr merge --merge`, matching the existing merge commits, and delete the branch.

### Breaking changes

- Follow the breaking-changes skill: break the API and game data freely, keep lobby data and map templates.
- Say in the PR description which clients must upgrade.

### Infrastructure agents

- Never run `terraform apply`, `ansible-playbook` against a real host, or any AWS CLI command that creates or changes resources.
- Validate with `terraform validate`, `terraform fmt -check`, `ansible-lint` and `ansible-playbook --syntax-check`.
- Keep secrets and real hostnames out of the repo.

## Shared contracts

Issues that run at the same time have to agree on these.

- **Per-game registration (#40).** #40 replaces the plain list of game keys in the lobby policy with a per-game registration that holds the settings validator. #47 adds a declared start rule to it, and #24 adds idle and maximum-age overrides. Keep it declarative, and keep game names out of `lobby/`.
- **Migrations (#23, #29, #40, #41, #24).**
  - #23 provides the migration runners.
  - #29 adds the migrate command and makes `InitDB` only connect.
  - Whichever of #23 and #40 merges second adapts the `game_settings` column: either into migration 1, or as a new lobby migration.
  - The same applies to #23 and #41 for the castle template starting soldier count, in the map-template set.
  - #24's new columns, such as `last_connected_at`, are new lobby migrations.
- **Publishing (#28).**
  - #28 provides one helper that pushes to Centrifugo with a short retry. #35's `game.ended` and the lobby's updates use it.
  - Personal channels come from the lobby's `PlayerChannel` (#36).
- **Replay and outcome (#18, #35).** #18 moves the replay's static metadata (teams, display names, outcome) onto the game. #35 fills in the outcome.
- **Map templates (#41, #43, #42).** #41 owns the map JSON schema and the validator that `/map/create`, #43's lobby settings and #42's builder all use.

## Conflict hotspots

Expect rebases in these places:

- `backend/services/generation_input.go`: #15, #17, #18, #28, #35, #36
- `backend/services/lobby_game.go` and `replay.html`: #17, #18, #21, #35, #43, #47
- `backend/main.go`: #28, #29, #37, #39, #42
- lobby schema and policy: #23, #24, #40, #47
