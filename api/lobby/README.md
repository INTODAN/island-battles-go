# The lobby's API documents

Copies of the lobby's player API (`openapi.json`) and event schema (`events.json`) from [`github.com/INTODAN/mpl`](https://github.com/INTODAN/mpl), at the version `go.mod` pins. The web client generates its lobby types from them (`bun run generate` in [`clients/island-battles-web`](../../clients/island-battles-web/README.md)).

Don't edit them here: the lobby's API changes in `INTODAN/mpl` first. After moving the pin, run `scripts/sync-lobby-api.sh` from the repository root, then `bun run generate`. `scripts/sync-lobby-api.sh --check`, which CI's API contract job runs, fails when the copies differ from the pinned module's.
