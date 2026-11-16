# Agent guide

## Where the work is

Open work in this repository and in its sibling (INTODAN/mpl, the lobby) is tracked on one project board, [**Island Battles**](https://github.com/users/INTODAN/projects/1). Its Status column is **Parked** (ideas: don't start them unless the owner says), **Todo**, **In progress**, **In review** (a pull request waits) and **Done**. Steps only the owner can do are in INTODAN/island-battles-go#119. When asked to start implementing, continue, or "do the next issues":

1. Take **Todo** items whose *Blocked by* issues are all closed. Check with `gh api repos/INTODAN/island-battles-go/issues/N/dependencies/blocked_by`.
2. Move the item to **In progress** before you start, so parallel sessions don't take it too, and to **In review** when its pull request is open. `gh project` needs the `project` scope; if `gh` reports it missing, ask the owner.
   ```sh
   gh project item-list 1 --owner INTODAN --format json -L 200 --query "status:Todo"   # what's ready
   gh project item-edit 1 --owner INTODAN --url <issue URL> --field Status --value "In progress"
   ```
3. Implement one issue per pull request, with `Closes #N` in its body (or `Closes INTODAN/mpl#N` for an issue in the sibling repository). Independent items can run in parallel in separate branches, worktrees or subagents.
4. Don't merge, unless the owner told your session to. Then merge only after an independent review, with the branch up to date and its checks green. Move the item to **Done** once it's merged and its steps after the merge are done.
5. **Every issue you file goes on the board:** `gh project item-add 1 --owner INTODAN --url <issue URL>`, then set its Status (**Parked** for a `discussion` issue, otherwise **Todo**). When it waits on another issue, add a *Blocked by* relationship on the issue (`gh api -X POST repos/INTODAN/island-battles-go/issues/N/dependencies/blocked_by -F issue_id=<the blocking issue's numeric id>`), rather than writing it in the text.

Each issue is written to be implemented on its own: read the whole issue, plus the decisions and issues it links, before writing code.

## Labels

- `agent-ready`: fully specified; implement it as written.
- `design-first`: post the mockup or design on the issue, then stop until the owner picks.
- `needs-decision`: blocked on the owner. Ask on the issue; don't guess.
- `discussion`: a future idea. Don't implement it.
- `owner-action`: needs the owner. Ask on the issue first: most of these an agent can do with the owner's `gh` login and `aws login`.
- `epic`: a parent issue. Work on its sub-issues, not on the epic itself.

## Steps after a merge

When your change needs a step after it merges (a plan and apply, an apply-role script, a setting, a release, a check), write it in the PR. Name who does it, and assume an agent does it with the owner's `gh` login and `aws login`, asking in chat before an apply, a release, or anything irreversible. Only these go to #119, the owner's checklist:

- sign-ins an agent can't make: GitHub's `user` scope, PyPI's website, DNS in Squarespace, the owner's inbox;
- checks on real devices;
- decisions.

## How to work in this repository

- Commands for building, formatting, testing and checking the OpenAPI documents and generated models are under **Commands** in [README.md](README.md).
- Skills in [`.agents/skills/`](.agents/skills/) hold the repository's conventions. Read the one that applies before changing that area:
  - `backend-development`: Huma endpoints, services and GORM persistence
  - `breaking-changes`: any change to the player or lobby API, events, stored data or client SDKs
  - `browser-testing`: checking the lobby page, demo or replay in a browser
  - `game-state-testing`: generation, command and game-state tests
- The lobby lives in [`INTODAN/mpl`](https://github.com/INTODAN/mpl); lobby changes go there first. This repository uses it at the version that README.md's [The lobby's version](README.md#the-lobbys-version) names, and changes the pin only to take a lobby change that is merged there. Island Battles specifics belong in `backend/`.
- **The repository is public.** Never commit a secret, not even for local development: `.env.example` holds placeholders only, and production's secrets live in the deploy bucket.
- After changing an API, re-export the OpenAPI documents and regenerate the clients in the same change. On a rebase conflict in generated files, regenerate them instead of merging by hand.

## Deploying

Only merged code deploys. When the user asks you to deploy or apply a change, use the Deploy workflow (`.github/workflows/deploy.yml`) on `main`, as [deploy/README.md](deploy/README.md#deploying-from-github-actions) describes. In a session where the owner has run `aws login`, you may also read AWS, edit the deploy bucket's settings files, and, when the owner says, run the apply-role script (`deploy/github_apply_role.py`). The VM is INTODAN/mpl's: see its AGENTS.md. A step that a PR names for an agent still needs the owner's go-ahead before an `apply` or a release.

1. The change must be merged first: by the owner, or by you when the owner told your session to merge.
2. For AWS resources, start a `plan` run on `main`. Read the plan from its log or summary, and tell the user what it creates, changes and destroys.
3. Only when the user says to, start an `apply` run with `plan_run_id` set to that plan run's ID. Never pass `allow_destroy` unless the user named each resource.
4. Then start the `lambda-env`, `backend`, `web` or `smoke` runs the change needs, and check the result: the job's log, and the AWS resources it should have changed.
