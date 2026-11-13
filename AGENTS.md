# Agent guide

## Where the work is

The open work lives in GitHub issues on `INTODAN/island-battles-go`, and **issue #120 is the work queue**. When asked to start implementing, continue, or "do the next issues", don't pick issues yourself:

1. Read #120. Find the first wave with unticked items, and the items in it whose **needs** are all closed and that nobody has claimed.
2. Claim each item you take by editing #120, as it describes.
3. Implement one issue per pull request, with `Closes #N` in its body. Items marked ∥ can run in parallel in separate branches, worktrees or subagents. Items marked → wait for the item named.
4. Don't merge. The owner reviews and merges, and ticks the item in #120.

Each issue is written to be implemented on its own: read the whole issue, plus the decisions and issues it links, before writing code.

## Labels

- `agent-ready`: fully specified; implement it as written.
- `design-first`: post the mockup or design on the issue, then stop until the owner picks.
- `needs-decision`: blocked on the owner. Ask on the issue; don't guess.
- `discussion`: a future idea. Don't implement it.
- `owner-action`: needs the owner's credentials or settings. Don't do it. When your change needs a manual step, say so in the PR and add it to #119, the owner's checklist.
- `epic`: a parent issue. Work on its sub-issues, not on the epic itself.

## How to work in this repository

- Commands for building, formatting, testing and checking the OpenAPI documents and generated models are under **Commands** in [README.md](README.md).
- Skills in [`.agents/skills/`](.agents/skills/) hold the repository's conventions. Read the one that applies before changing that area:
  - `backend-development`: Huma endpoints, services and GORM persistence
  - `breaking-changes`: any change to the player or lobby API, events, stored data or client SDKs
  - `browser-testing`: checking the lobby page, demo or replay in a browser
  - `game-state-testing`: generation, command and game-state tests
- The lobby lives in [`INTODAN/mpl`](https://github.com/INTODAN/mpl); lobby changes go there first. This repository uses it at the version that README.md's [The lobby's version](README.md#the-lobbys-version) names, and changes the pin only to take a lobby change that is merged there. Island Battles specifics belong in `backend/`.
- After changing an API, re-export the OpenAPI documents and regenerate the clients in the same change. On a rebase conflict in generated files, regenerate them instead of merging by hand.

## Deploying

You have no AWS credentials, and only merged code deploys. When the user asks you to deploy or apply a change, use the Deploy workflow (`.github/workflows/deploy.yml`) on `main`, as [deploy/README.md](deploy/README.md#deploying-from-github-actions) describes, and only when asked:

1. The change must be merged first. Open the pull request, and wait for the owner to merge it.
2. For AWS resources, start a `plan` run on `main`. Read the plan from its log or summary, and tell the user what it creates, changes and destroys.
3. Only when the user says to, start an `apply` run with `plan_run_id` set to that plan run's ID. Never pass `allow_destroy` unless the user named each resource.
4. Then start the `vm`, `backend` or `smoke` runs the change needs, and check the result: the job's log, and the AWS resources it should have changed.
