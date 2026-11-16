# Deployment

Island Battles runs in one AWS region:

- **Backend:** the Go backend runs on AWS Lambda behind its Function URL. Players open the game at `https://island-battles.<domain>`, which a CloudFront distribution serves from the Function URL with an ACM certificate (#75). `/` redirects to the lobby's page at `https://lobby.<domain>/lobby/ui/`.
- **The lobby and the VM** are [`INTODAN/mpl`](https://github.com/INTODAN/mpl)'s (#231, #232). It deploys the lobby at `https://lobby.<domain>`, and runs the Lightsail VM, `vm.<domain>`, with Postgres and Centrifugo in Docker Compose. Island Battles is a tenant there: its database `island_battles`, with its own role, and Centrifugo's HTTP API, which the game publishes its events with ([The lobby and the VM](#the-lobby-and-the-vm)).
- **Network:** there is no VPC or peering. The Lambda reaches Postgres and Centrifugo's HTTP API over the internet:
  - Postgres accepts only TLS (`hostssl`) logins, with scram and a generated password.
  - Centrifugo's API needs TLS and its API key.
- **DNS:** two records of the game's own, added by hand:
  - `island-battles.<domain>` points at the CloudFront distribution.
  - ACM's validation record lets it issue and renew the game's certificate.

  `vm.<domain>` and `lobby.<domain>` are INTODAN/mpl's.

This document covers creating the AWS resources with Terraform, writing the backend's settings from what INTODAN/mpl publishes for its tenant, deploying the backend to Lambda and the web client to S3, the smoke test and releases, and [the cutover](#the-cutover) that moved the VM and the lobby to INTODAN/mpl. Any machine with the AWS credentials can deploy, and so can GitHub Actions: Terraform's state and the deploy's files live in a [deploy bucket](#the-deploy-bucket). [First deployment, step by step](#first-deployment-step-by-step) puts it all in order. #26 describes the architecture, and [INTODAN/mpl's `deploy/README.md`](https://github.com/INTODAN/mpl/blob/main/deploy/README.md) the lobby and the VM.

The live deployment (#45, 2026-10-30) runs in `eu-central-1`. Players open it at `https://island-battles.elidan.org` (#75, 2026-10-30), which replaced a 2021 WebGL build of the game on Firebase Hosting. The lobby is at `https://lobby.elidan.org`, and the VM, `vm.elidan.org`, is INTODAN/mpl's. The DNS records are managed in the Squarespace domains dashboard: `elidan.org` → DNS → Custom records.

## Files

| Path | What it is |
|---|---|
| `scripts/aws.sh` | Creates the AWS resources and deploys the backend to Lambda. |
| `scripts/lambda-env.sh` | Writes the [backend's settings](#the-backends-settings), `deploy/local/lambda.env`, from INTODAN/mpl's settings for the tenant and this repository's own secrets. |
| `scripts/smoke.sh` | Runs the [smoke test](#smoke-test). |
| `scripts/deploy-web-client.sh` | Builds the [web client](#the-web-client) and uploads it to its bucket. |
| `scripts/deploy-files.sh` | Creates the [deploy bucket](#the-deploy-bucket), and syncs `deploy/local/` with it by hand. `deploy/deploy_files.py` does the work, and `deploy/test_deploy_files.py` tests it. |
| `.github/workflows/deploy.yml` | [Deploys from GitHub Actions](#deploying-from-github-actions), on demand. |
| `deploy/aws.example.tfvars` | The template for the AWS settings. |
| `deploy/terraform/` | The AWS resources: the Lambda in `lambda.tf`, the game's certificate, CloudFront distribution and [lobby redirect](#the-lobby-on-its-own-origin) in `cloudfront.tf`, the Lambda's log group, alarms, collector settings and budgets in `observability.tf`, the old database backups' bucket in `backups.tf`, the [web client's](#the-web-client) bucket and redirect function in `web_client.tf`, the [bot package](#bot-packages) bucket in `bot_packages.tf`, the [bot runners](#bot-runners) in `bot_runners.tf`, and GitHub Actions' OIDC provider and deploy role in `github.tf`. `vm.tf` holds only the `removed` blocks that took the VM out of the state at [the cutover](#the-cutover). `tests/` checks the configuration offline. |
| `deploy/github_apply_role.py` | Creates the role that the Deploy workflow's approved runs use ([Deploying from GitHub Actions](#deploying-from-github-actions)). `deploy/test_github_apply_role.py` tests it. |
| `deploy/check_deploy_plan.py` | Reads the plan for `scripts/aws.sh`: stops a backend deploy that would change more than the backend, refuses any plan that destroys what INTODAN/mpl owns, lists the distributions an apply would destroy, and tells an apply whether to run the migrations. `deploy/test_check_deploy_plan.py` tests it. |
| `deploy/check_concurrency.py` | Reads `aws.tfvars` for `scripts/aws.sh`: refuses to plan when `reserved_concurrency` is null and `account_concurrency_limit` is below the account's real limit ([The concurrency cap](#the-concurrency-cap)). `deploy/test_check_concurrency.py` tests it. |
| `deploy/ansible/` | `lambda-env.yml`, the playbook that `scripts/lambda-env.sh` runs on this machine, its role `lambda_env`, and the `env_file` filter that reads `tenant.env`. `deploy/test_lambda_env.py` tests them. |
| `deploy/smoke.py` | The smoke test that `scripts/smoke.sh` runs. |
| `deploy/local/` | Gitignored. The settings, this repository's secrets, the Lambda's settings and the smoke test's guest credential, which the scripts sync with the [deploy bucket](#the-deploy-bucket), and this machine's copy of INTODAN/mpl's `tenant.env`, built Lambda and plans, which they don't. |

## Prerequisites

- **On your machine:**
  - Terraform 1.11 or newer, Go, which builds the backend and runs its migrations, and Python 3, for the backend deploy's plan check and the deploy bucket's sync.
  - The AWS CLI 2.32 or newer. The scripts use it for the [deploy bucket](#the-deploy-bucket).
  - AWS credentials, which Terraform and the AWS CLI read from the standard AWS credential chain, e.g. `AWS_PROFILE=<profile>`. They need to manage Lambda, IAM roles and OIDC providers, CloudWatch Logs and alarms, SNS, Budgets, ACM, CloudFront and S3. Nothing in the repo stores them, and nothing else is needed: no SSH key, and no copy of `deploy/local/`.
    - `aws login --profile <profile>` signs in through the console and gives short-lived credentials, which Terraform's AWS provider reads directly. They last up to 12 hours; run it again when Terraform reports expired credentials.
  - Ansible, for `scripts/lambda-env.sh`: `uv tool install ansible-core`.
  - [uv](https://docs.astral.sh/uv/), for the smoke test, and [Bun](https://bun.sh), at the version that `clients/island-battles-web/package.json` pins, for the web client.
- **INTODAN/mpl's lobby and VM,** with Island Battles as a tenant, whose settings its VM deploy publishes in the deploy bucket ([The lobby and the VM](#the-lobby-and-the-vm)).
- **DNS** (#30): see below.

### DNS

The game needs two DNS records of its own. Squarespace has no DNS API, so you add them by hand, once. They don't change on a redeploy: the distribution and its certificate stay. `vm.<domain>`, `lobby.<domain>` and the lobby's certificate record are INTODAN/mpl's ([its "DNS records"](https://github.com/INTODAN/mpl/blob/main/deploy/README.md#dns-records)).

In the Squarespace domains dashboard, open the domain's DNS settings and add each one as a custom record. If the domain uses DNS Connect with another provider's nameservers, add them there instead. Squarespace's host field takes the name without the domain: for `_1a2b.island-battles.elidan.org.`, enter `_1a2b.island-battles`.

| Record | Type | Host | Value | When |
|---|---|---|---|---|
| The certificate's validation | `CNAME` | The name that `scripts/aws.sh output certificate_validation_record` prints, without the domain | The value it prints after `CNAME`, ending in `.acm-validations.aws.` | Once the certificate exists, before the apply that waits for it |
| The game | `CNAME` | `island-battles`: `game_hostname` without the domain | `scripts/aws.sh output distribution_domain`, `<id>.cloudfront.net` | Once the distribution exists |

- **Keep the validation record.** ACM renews the certificate only while it can still see the record.
- **CAA records:** run `dig CAA <domain> +short`. If it prints records, they must allow `amazon.com`, for ACM, or the certificate request fails. INTODAN/mpl needs `letsencrypt.org` too, for Centrifugo's certificate on `vm.<domain>`. You can also add CAA records on `island-battles.<domain>` itself. `elidan.org` had none on 2026-10-30.

## First deployment, step by step

The game needs INTODAN/mpl's lobby and VM first: its VM deploy creates the game's database and role and publishes the settings the backend needs. The game's certificate comes first among the AWS resources, because the distribution needs it issued, and ACM issues it only once you've added its DNS record:

1. **Check the account's Lambda concurrency limit.** In the Service Quotas console, look up AWS Lambda's "Concurrent executions" for your region. If it's below 1000, read [The concurrency cap](#the-concurrency-cap) first. Then create the [deploy bucket](#the-deploy-bucket): `scripts/deploy-files.sh bootstrap --region <region>`. INTODAN/mpl shares it, under `mpl/`.
2. **INTODAN/mpl adds Island Battles as a tenant** ([its "Tenants"](https://github.com/INTODAN/mpl/blob/main/deploy/README.md#tenants)): an entry in its `vm.yml` with the key `island-battles`, the database and role `island_battles`, the game's origin `https://island-battles.<domain>` in `origins`, the desktop app's `tauri://localhost` and `http://tauri.localhost` in `websocket_origins`, and the `lobby_game` that `go run ./backend/cmd/lobby-games https://island-battles.<domain>` prints here. Its VM deploy then publishes the tenant's settings, and its lobby deploy lists the game. Check that `aws s3 ls "s3://<deploy bucket>/mpl/tenants/island-battles/"` lists `tenant.env`.
3. **Write the settings.** Copy `deploy/aws.example.tfvars` to `deploy/local/aws.tfvars` and set the region, `game_hostname`, `lobby_hostname`, the host of INTODAN/mpl's lobby, and `alert_email` ([AWS settings](#aws-settings)).
4. **Write the backend's settings:**

   ```sh
   scripts/lambda-env.sh -e app_generate_secrets=true
   ```

   It generates this repository's two secrets and writes `deploy/local/lambda.env` ([The backend's settings](#the-backends-settings)). Later runs don't need the option.
5. **Request the game's certificate:**

   ```sh
   scripts/aws.sh apply -target=aws_acm_certificate.game
   ```

   The plan creates only the certificate, in `us-east-1`. Terraform warns that a targeted plan may be incomplete, which is expected here. Then `scripts/aws.sh output certificate_validation_record` prints its validation record: add it ([DNS](#dns)).

   On the live account, the next step at first failed with `Your account must be verified before you can add new CloudFront resources`. See [Troubleshooting](#troubleshooting).
6. **Create the AWS resources:**

   ```sh
   scripts/aws.sh apply
   ```

   It builds the backend and shows the plan: the Lambda's role, the function and its Function URL, the bot runners, the certificate's validation, the CloudFront distribution with its functions, the web client's and the bot packages' buckets, the function's log group, metric filter, alarms, alert topic and budgets ([Logs, alerts and traces](#logs-alerts-and-traces)), and the rest of [AWS resources](#aws-resources). Type `yes`. `lambda.env` exists, so it first runs the migrations, which create the game's schema in its new database, and then publishes the backend with its settings.

   - It waits until ACM has issued the certificate, usually within minutes of the record resolving, and then until CloudFront has deployed the distribution, which takes a few minutes more.
   - SNS then emails `alert_email` a confirmation link. Follow it, or the alarms send nothing.
   - Without `lambda.env`, Terraform refuses the plan. Only an apply that must run before step 4 passes `-var allow_missing_lambda_settings=true`: it creates the function without its settings, skips the migrations, and the function fails every request until `scripts/aws.sh deploy` publishes them.
7. **Add the DNS record** `island-battles.<domain>` for the distribution ([DNS](#dns)), and wait until `https://island-battles.<domain>/healthz` answers 204.
8. **Upload the web client:** `scripts/deploy-web-client.sh` ([The web client](#the-web-client)).
9. **Run the smoke test:** `scripts/smoke.sh` ([Smoke test](#smoke-test)). Players can now open `https://island-battles.<domain>`, and the lobby at `https://lobby.<domain>` lists the game.
10. **Nothing to back up by hand:** every script pushed the settings, the secrets and `lambda.env` to the deploy bucket, and Terraform keeps its state there. Any machine with the AWS credentials can deploy from here on. INTODAN/mpl backs up the game's database with every other one on the VM ([Backups](#backups)).

## AWS resources

`deploy/terraform/` creates the game's AWS resources (#32). The DNS records are the only manual step.

| Resource | Details |
|---|---|
| Lambda | The `provided.al2023` runtime on arm64, 512 MB, a 30-second timeout, a reserved concurrency of 5, JSON logs filtered at WARN, Active tracing off, and the OpenTelemetry collector layer. Its settings are [the backend's settings](#the-backends-settings) and those Terraform sets itself. |
| IAM role | Basic execution and X-Ray writes: the function writes its logs, and its collector layer sends traces to X-Ray. In S3 it may put, get and delete objects in the bot package bucket, and nothing else: it presigns the uploads, reads each zip's directory and manifest, and deletes the zips it replaces or refuses. It invokes the two bot runner functions, and no other function, and reads metrics with `cloudwatch:GetMetricStatistics`, for the runners' [monthly compute guard](#the-monthly-compute-guard). It calls nothing else in AWS. |
| Function URL | Auth type `NONE` and the `RESPONSE_STREAM` invoke mode, which the backend's `lambdaurl` host needs. The provider adds the public invoke permissions. |
| Certificate | An ACM certificate for `game_hostname` in `us-east-1`, where CloudFront takes certificates from, validated by DNS. ACM renews it on its own while its validation record stays. |
| CloudFront distribution | Serves `game_hostname` with the certificate, over HTTPS only: plain HTTP is redirected. Its default origin is the Function URL, over HTTPS. Every method reaches the Lambda uncached (`Managed-CachingDisabled`), except GET, HEAD and OPTIONS under `/game/demo/*`, `/game/demo-viewer/*` and `/map/builder/*` (the game's modules, the demo viewer and the map builder, #162), which follow the Lambda's `Cache-Control` (the distribution's own cache policy `<name>-origin-cache-control`: scripts and styles `public, max-age=300`, HTML `no-store`, so a deploy reaches players within five minutes; the cache key is the path alone, because the managed `UseOriginCacheControlHeaders` policy would send the viewer's `Host` to the Function URL, which refuses it). Both kinds pass every viewer header, cookie and query string but `Host` on (`Managed-AllViewerExceptHostHeader`), so the Function URL sees its own `Host` and the backend gets `Authorization`. Streamed answers stream through. `/lobby/ui` and `/lobby/ui/*`, the lobby page's old address, never reach the Lambda: the CloudFront Function `<name>-lobby-redirect` answers them with a redirect to the lobby ([The lobby on its own origin](#the-lobby-on-its-own-origin)). No origin access control: for a Function URL it would need clients to send a SHA-256 of every POST body, and the Function URL is public anyway. A second origin, the web client's bucket, serves `/play/*` and `/play` ahead of the default behaviour: cached and compressed (`Managed-CachingOptimized`), GET, HEAD and OPTIONS only, with a CloudFront Function that serves `/play/` as `play/index.html` and redirects `/play` to `/play/`. Price class 100, no logs and no WAF. |
| Web client bucket | `<name>-web-client-<account>`, private, unversioned, encrypted, TLS only, with all public access blocked. Holds the built client under `play/`. Only this distribution may read it, through an origin access control. See [The web client](#the-web-client). |
| Bot package bucket | `<name>-bot-packages`, private, unversioned, encrypted (SSE-S3), TLS only, with all public access blocked. Holds the bot packages that members upload to lobbies, under `lobbies/<lobby_id>/<package_id>.zip`, each deleted 8 days after its upload. Browsers upload to it with POST from `https://<game_hostname>` and `https://<lobby_hostname>`. See [Bot packages](#bot-packages). |
| Bot runners | `<name>-bot-runner-python` (`python3.14`) and `<name>-bot-runner-native` (`provided.al2023`), arm64, 1024 MB, a 20-second timeout, 2048 MB of `/tmp`, a reserved concurrency of 10 each, asynchronous invocations never retried and dropped after 60 seconds, and only `GAME_SUBMIT_HOST` as a setting. Their role, `<name>-bot-runner`, may only write their logs, kept for 7 days in `/aws/lambda/<name>-bot-runner-*`. The backend's role may invoke them, and no other function. See [Bot runners](#bot-runners). |
| Log group | `/aws/lambda/<name>`, kept for 7 days. |
| Metric filter and alarms | One metric that counts error lines, and two alarms, on it and on the function's `Errors`, that email `alert_email` through an SNS topic. |
| Budgets | A zero-spend cost budget that emails `alert_email`, and a $5 monthly cost budget that emails `budget_email` (or `alert_email`) once actual costs pass 80 %. Both cover the whole account, INTODAN/mpl's resources included. |
| Backups bucket | `<name>-backups-<account>`, private, versioned, TLS only, which holds the database dumps the VM made until [the cutover](#the-cutover) until they expire. Neither repository's GitHub roles can read them. See [Backups](#backups). |
| GitHub OIDC provider and deploy role | With `github_repository` set: the account's OIDC provider for GitHub Actions, which INTODAN/mpl's roles trust too, and the role the Deploy workflow assumes ([Deploying from GitHub Actions](#deploying-from-github-actions)). |

There's no VPC, peering or EventBridge. [Logs, alerts and traces](#logs-alerts-and-traces) explains the log group, the alarms and the budgets, and why they stay inside AWS's free tier, and [Free allowances](#free-allowances) covers CloudFront's.

### AWS settings

Copy `deploy/aws.example.tfvars` to `deploy/local/aws.tfvars` and fill in `region`, `game_hostname`, `lobby_hostname` and `alert_email`. `alert_email` stays in that gitignored file: never commit it. `deploy/terraform/variables.tf` describes every other variable, all of which have defaults.

`lobby_hostname` is the host of INTODAN/mpl's lobby, such as `lobby.elidan.org`. Terraform refuses a `lambda.env` whose `ISLAND_BATTLES_LOBBY_URL` isn't `https://<lobby_hostname>`, so the two can't drift apart.

### State

Terraform's state is `terraform/terraform.tfstate` in the [deploy bucket](#the-deploy-bucket), through Terraform's S3 backend. `scripts/aws.sh` finds the bucket and its region from the AWS credentials and passes them to `terraform init`. `.gitignore` covers `.terraform/` and `*.tfstate*`, in case a copy ends up in the checkout.

- **The state holds the Lambda's secrets:** its environment, with the database password, Centrifugo's API key, the hooks' shared secret, the admin API key and the submit token secret. Never commit a copy.
- **One apply at a time.** Terraform locks the state with `terraform/terraform.tfstate.tflock` in the bucket while it plans and applies. A plan on another machine meanwhile fails with `Error acquiring the state lock`. If a run was killed and left the lock behind, `terraform -chdir=deploy/terraform force-unlock <lock ID>` removes it, once you're sure nothing else is applying.
- **Every earlier state is kept:** the bucket is versioned. To go back to one, find it in the S3 console's versions of the object.
- **If the state is lost,** the resources keep running, but Terraform no longer knows them. Bring each back with `terraform import` before applying again, or Terraform tries to create them a second time.

### The deploy bucket

`island-battles-deploy-<account ID>` is a private S3 bucket in the deployment's own account, which `scripts/deploy-files.sh bootstrap --region <region>` creates once. Set `ISLAND_BATTLES_DEPLOY_BUCKET` to use another name. The bootstrap blocks public access, turns on versioning and SSE-S3 encryption, refuses plain HTTP, and expires copies that were replaced more than 90 days ago, keeping the last 10 of each file. Run it again to bring an existing bucket's settings up to date. The bucket isn't part of the Terraform configuration, which keeps its state in it.

It holds:

- `terraform/`: Terraform's state and its lock.
- `files/`: a copy of `deploy/local/`: `aws.tfvars`, `secrets/`, `lambda.env` and `smoke-session.json`, and any other file there, except `terraform/` (plans and an old local state), `lambda/` (the build), `dumps/` and `tenant.env`. `secrets/` holds only this repository's two secrets ([The backend's settings](#the-backends-settings)).
- `plans/`: the plans that the Deploy workflow's `plan` target saved, deleted after 3 days.
- `mpl/`: INTODAN/mpl's own part ([its "The deploy bucket"](https://github.com/INTODAN/mpl/blob/main/deploy/README.md#the-deploy-bucket)). This repository only reads `mpl/tenants/island-battles/tenant.env` from it ([The backend's settings](#the-backends-settings)), and its GitHub roles can't read mpl's files, state or plans ([Deploying from GitHub Actions](#deploying-from-github-actions)).

`scripts/aws.sh apply`, `deploy`, `plan` and `apply-plan`, `scripts/lambda-env.sh` and `scripts/smoke.sh` **pull** the files first and **push** them back when they end, even after a failure, so new secrets and a new `lambda.env` never stay on one machine. A new machine or worktree needs nothing but the AWS credentials. To change a setting, edit it in `deploy/local/` and run a script, or `scripts/deploy-files.sh push`.

Each machine remembers what it last pulled or pushed, in `deploy/local/.sync-base.json`, so a sync can tell which side changed a file:

- **Changed only in the bucket**, or missing here: a pull takes the bucket's copy.
- **Changed only here:** a push sends it. The upload only succeeds while the bucket still has the copy it compared against, so two machines can't overwrite each other.
- **Changed on both sides:** a conflict, and the script stops before deploying. `scripts/deploy-files.sh status` lists the differences. Keep one: `scripts/deploy-files.sh pull --force` takes the bucket's copies, `push --force` sends this machine's.
- **Deleted:** nothing is ever deleted from the bucket, and a pull brings back a file deleted here. Delete a file in the S3 console to remove it everywhere; versioning keeps its earlier copies.

#### Moving an existing deployment to the bucket

A deployment from before the deploy bucket keeps its state and secrets in one machine's `deploy/local/`. On that machine, with a checkout of this change:

1. `scripts/deploy-files.sh bootstrap --region <region>`, with the deployment's region.
2. `scripts/deploy-files.sh push`, which sends `deploy/local/`'s settings and secrets.
3. `scripts/aws.sh migrate-state`, which moves `deploy/local/terraform/terraform.tfstate` into the bucket and renames the local file to `terraform.tfstate.migrated`. Until then, `scripts/aws.sh` refuses to plan on that machine.
4. Set `github_repository` in `deploy/local/aws.tfvars` for [GitHub Actions](#deploying-from-github-actions), and run `scripts/aws.sh apply`.

Every other machine and worktree then pulls from the bucket on its own. Keep the old `deploy/local/` until another machine has deployed.

### The concurrency cap

Every Lambda instance has its own database pool, to Island Battles' database, so the reserved concurrency caps the backend's Postgres connections ([backend/README.md](../backend/README.md#the-connection-pool)). The lobby runs on its own, with its own pool. INTODAN/mpl runs Postgres, and reserves `tenant_database_connections` of its connections for the game, 20 ([its "Postgres connections"](https://github.com/INTODAN/mpl/blob/main/deploy/README.md#postgres-connections)). Terraform checks that **`reserved_concurrency` × `database_max_open_connections` ≤ `tenant_database_connections`**, whose default here is the same 20. It also sets `ISLAND_BATTLES_DATABASE_MAX_OPEN_CONNECTIONS` itself, so the two can't drift apart.

The same cap is C in the [free-tier budget](#the-budget): every instance logs and traces up to its own limits. Terraform passes the cap to the backend as `ISLAND_BATTLES_CONCURRENCY` and checks the budget too, so a bigger cap needs lower log and trace limits.

- The defaults use at most 5 × 2 = 10 of the game's 20 connections. More needs INTODAN/mpl to reserve more first, in its own `tenant_database_connections`, and then the same value here: every connection costs memory on the 1 GB VM.
- **Accounts with a low concurrency limit:** Lambda always keeps part of the account's concurrency unreserved, 100 on a normal account. On an account limited to 10, that's all of it, and any reserved concurrency fails with `decreases account's UnreservedConcurrentExecution below its minimum value`. Either request an increase of "Concurrent executions" in Service Quotas, which is free, or set `reserved_concurrency = null`.
- **Without a reservation,** the account limit is the cap, so Terraform checks **`account_concurrency_limit` × `database_max_open_connections`** instead, and the budget for that many instances. Set `account_concurrency_limit` in `aws.tfvars` to the limit Service Quotas shows. It defaults to 10, which gives 10 × 2 = 20 connections.
  - The default log and trace limits fit 5 instances, not 10. For 10, also set `log_lines_per_minute = 2` and `trace_interval_seconds = 360`: 1.2 GB of logs and 74,400 traces at most.
  - **`scripts/aws.sh` rechecks the limit for you.** While `reserved_concurrency` is null, `apply`, `deploy` and `plan` read the account's limit with `aws lambda get-account-settings` (in the region from `aws.tfvars`) before they plan, and refuse when `account_concurrency_limit` is below it, so a limit that AWS raised can't leave the checks on a stale cap. The message says to reserve concurrency again or to set `account_concurrency_limit` to the new limit. A reservation, or a limit at or above the account's, passes. `deploy/check_concurrency.py` reads `aws.tfvars` (`null`, a number, a missing variable and comments) and `deploy/test_check_concurrency.py` tests it. The call needs only `lambda:GetAccountSettings`, which the [GitHub deploy role](#deploying-from-github-actions) has through `ReadOnlyAccess`, so the Deploy workflow runs the same guard.
  - **A limit of 1000 lets 2,000 connections at Postgres:** AWS raises a new account's limit on its own after some use, which is what the guard catches.
  - Once it's raised, set `account_concurrency_limit` to the new value. The checks then fail, which tells you to reserve concurrency: set `reserved_concurrency` again, for example back to 5.
- **The bot runners** reserve 10 each ([Bot runners](#bot-runners)), whatever `reserved_concurrency` is. They don't connect to Postgres, so they count only against the account's limit: it must leave 100 unreserved after the backend's reservation, their 20, and INTODAN/mpl's lobby's.
- **Lingering connections:** a frozen or killed Lambda environment never closes its connections. Postgres's TCP keepalives on the VM, which INTODAN/mpl sets, close them about two minutes after the environment stops answering, so they don't pile up against Postgres's limit.

## The lobby and the VM

[INTODAN/mpl](https://github.com/INTODAN/mpl) deploys the lobby at `https://lobby.<domain>` and runs the VM: a Lightsail instance at `vm.<domain>`, with its static IP and firewall, Postgres and Centrifugo in Docker Compose, Centrifugo's Let's Encrypt certificate, the read-only admin role and the daily backups ([its `deploy/README.md`](https://github.com/INTODAN/mpl/blob/main/deploy/README.md)). Island Battles is one of its tenants ([its "Tenants"](https://github.com/INTODAN/mpl/blob/main/deploy/README.md#tenants)): it has its own database, `island_battles`, owned by its own role, `island_battles`, and publishes its events with Centrifugo's HTTP API.

This repository neither deploys nor logs in to the VM. Its Terraform let go of the VM at [the cutover](#the-cutover), with `removed` blocks in `deploy/terraform/vm.tf` that took the VM's instance, firewall, static IP, attachment and key pair out of the state without destroying them, and `check_deploy_plan.py` refuses any plan that would destroy a Lightsail resource. Dumps, restores and moving to a new VM are INTODAN/mpl's ([its "Dumps, restores and moving to a new VM"](https://github.com/INTODAN/mpl/blob/main/deploy/README.md#dumps-restores-and-moving-to-a-new-vm)). A move needs nothing here: the game's settings name `vm.<domain>`, not an address.

### The backend's settings

`deploy/local/lambda.env` holds the settings the backend gets from outside this repository's Terraform: what INTODAN/mpl publishes for its tenant, and this repository's two own secrets. `scripts/lambda-env.sh`, or the Deploy workflow's `lambda-env` target, writes it:

1. It pulls `deploy/local/` from the deploy bucket.
2. `deploy_files.py tenant` copies the deploy bucket's `mpl/tenants/island-battles/tenant.env`, which INTODAN/mpl's VM deploy writes ([its "Tenants"](https://github.com/INTODAN/mpl/blob/main/deploy/README.md#tenants) lists the keys), to `deploy/local/tenant.env`, readable only by you. That copy stays on this machine: it's INTODAN/mpl's, so the sync never pushes it.
3. It runs `ansible-playbook lambda-env.yml` in `deploy/ansible`, on this machine only. The playbook checks the tenant's settings: every key it needs, the key `island-battles`, a Postgres URL, an `https` lobby and Centrifugo API, and a hooks secret of at least 32 bytes. It checks this repository's secrets, `deploy/local/secrets/admin_api_key` and `submit_token_secret`, and stops if either is missing, unless `-e app_generate_secrets=true` lets it generate them: only for a new deployment, because new ones lock out the admin key's users and invalidate every bot's submit token. Then it writes `deploy/local/lambda.env`, readable only by you.
4. It pushes `deploy/local/` back, `lambda.env` and any new secret included, even after a failure.

| Variable | Value |
|---|---|
| `ISLAND_BATTLES_DATABASE_URL` | The tenant's `DATABASE_URL`: `postgres://island_battles:<password>@vm.<domain>:5432/island_battles?sslmode=require` |
| `ISLAND_BATTLES_CENTRIFUGO_API_URL` | The tenant's `CENTRIFUGO_API_URL`: `https://vm.<domain>/api` |
| `ISLAND_BATTLES_CENTRIFUGO_API_KEY` | The tenant's `CENTRIFUGO_API_KEY`, Centrifugo's API key |
| `ISLAND_BATTLES_LOBBY_URL` | The tenant's `LOBBY_URL`, without a trailing slash: `https://lobby.<domain>`, where the game calls the lobby's game API and reads its player token keys |
| `ISLAND_BATTLES_LOBBY_HOOK_SECRET` | The tenant's `LOBBY_GAME_SECRET`: the lobby signs its calls of the game's hooks with it, and the game its calls of the lobby's game API |
| `ISLAND_BATTLES_ADMIN_API_KEY` | `secrets/admin_api_key` (#37): send it in the `X-Admin-API-Key` header to create default maps with `POST /map/create` (see [map files](../backend/README.md#map-files)). Without it, the Lambda refuses every map creation. |
| `ISLAND_BATTLES_SUBMIT_TOKEN_SECRET` | `secrets/submit_token_secret` (#215): signs the bots' one-turn submit tokens. A new one only invalidates the tokens of turns being played at that moment. |

Terraform adds the rest of the function's settings: `ISLAND_BATTLES_PUBLIC_URL`, `https://<game_hostname>`, where turn deliveries tell bots to submit their commands; `ISLAND_BATTLES_LOBBY_ORIGINS`, `https://<lobby_hostname>`, the lobby page's origin, which the backend answers with CORS; and the pool, concurrency, log, trace, collector and bot settings ([Deploying the backend](#deploying-the-backend)). It refuses a `lambda.env` whose `ISLAND_BATTLES_LOBBY_URL` isn't `https://<lobby_hostname>`.

- **Run `lambda-env` again** whenever INTODAN/mpl changes the tenant's settings, such as the database's password, Centrifugo's API key or the hooks' secret, and then a `backend` deploy, `scripts/aws.sh deploy`, which publishes them.
- **The URLs use the hostnames,** not an IP, so they stay the same when INTODAN/mpl moves to a new VM.
- **`sslmode=require`** encrypts the connection but doesn't check the server's certificate, a self-signed one for `vm.<domain>` that INTODAN/mpl keeps with its secrets (`postgres-tls/server.crt`).
- **The player token key** that signs the lobby's player tokens, and its rotation, are INTODAN/mpl's ([its "Rotating the player token key"](https://github.com/INTODAN/mpl/blob/main/deploy/README.md#rotating-the-player-token-key)). The game reads the keys from the lobby's JWKS at `ISLAND_BATTLES_LOBBY_URL`.
- **The VM's secrets are INTODAN/mpl's:** the Postgres passwords and TLS files, Centrifugo's API key, the connection token secret, the proxy secret, the player token key and the lobby's game secret. INTODAN/mpl copied them to its own part of the bucket when it took the VM over ([its "Taking over a running VM"](https://github.com/INTODAN/mpl/blob/main/deploy/README.md#taking-over-a-running-vm), step 2). On 2026-11-16 their copies here, with every earlier version, the old backup key, `vm.yml`, and the versions of `lambda.env`, the state and the saved plans from before the cutover were deleted (#264), so neither of this repository's roles can read them. A machine that still has one of those files in `deploy/local/` gets a conflict on its next push: delete the file there.

Nothing secret is committed. Never copy anything from `deploy/local/` into the repo.

### The lobby on its own origin

The lobby runs at `https://<lobby_hostname>`, and the game's origin serves none of its routes (#232):

- **`/`** on the game's name redirects (307) to the lobby's page, `https://<lobby_hostname>/lobby/ui/`. The backend answers it, from `ISLAND_BATTLES_LOBBY_URL`.
- **`/lobby/ui` and `/lobby/ui/*`,** the lobby page's old address, such as an old invitation link: the distribution sends them to the CloudFront Function `<name>-lobby-redirect` (`aws_cloudfront_function.lobby_redirect`, at the viewer request, uncached), which answers 301 to the same path and query string on the lobby's name, with `Cache-Control: max-age=86400`, so browsers keep the redirect for a day. `/lobby/ui` goes to `/lobby/ui/`. An invitation's code is in the URL's fragment, which the browser keeps across the redirect. Neither the Lambda nor CloudFront's cache sees these requests. `deploy/test_lobby_redirect_function.py` runs the function in node.
- **Anything else under `/lobby`,** such as an old client's `POST /lobby/v1/sessions`, reaches the backend, which answers **410 Gone** with an `application/problem+json` problem whose `lobby_url` names the lobby, so an old page or SDK fails clearly ([backend/README.md](../backend/README.md#binding-to-the-lobby)).
- **The lobby page** loads the game's modules from the game's name and calls its API: the backend answers `ISLAND_BATTLES_LOBBY_ORIGINS` with CORS, and the [bot packages'](#bot-packages) bucket admits uploads from the lobby's origin as well as the game's.
- **The 3D client** at `/play/` reads the lobby's URL from `/play/config.json`, which [`scripts/deploy-web-client.sh`](#the-web-client) writes.
- **Centrifugo,** on the VM, accepts WebSockets from the lobby's origin and from the game's, which INTODAN/mpl's tenant entry lists in `origins` and `websocket_origins`, and calls the lobby's subscribe proxy, `centrifugo_proxy_url` in INTODAN/mpl's settings.

## Deploying the backend

One command publishes a new backend version (#38):

```sh
scripts/aws.sh deploy
```

1. **Builds** `deploy/local/lambda/bootstrap` with the build command from [backend/README.md](../backend/README.md#hosts-and-deploys). Terraform zips it with `archive_file`.
2. **Plans** the Terraform configuration with `deploy/local/aws.tfvars` and `deploy/local/lambda.env`.
3. **Checks the plan** with `deploy/check_deploy_plan.py`, which allows only creating or updating resources outside Lightsail, ACM and CloudFront. A plan that touches a Lightsail resource, even only to take it out of the state, or that changes the game's certificate or distribution, or that deletes or replaces anything, such as the Function URL, stops the deploy before the migrations. Review those changes with `scripts/aws.sh apply` instead. A deploy that created the certificate would wait for a DNS record nobody has added yet.
4. **Migrates Island Battles' database** from your machine, straight to Postgres over TLS: `go run ./backend/cmd/migrate` with `ISLAND_BATTLES_DATABASE_URL` from `lambda.env`. It stops when `lambda.env` lacks it. The lobby's database is INTODAN/mpl's, whose lobby deploy migrates it. The old code serves requests against the new schema until the next step finishes.
5. **Applies the checked plan:** the new code and settings. Then it prints the Function URL.

It doesn't ask for confirmation, and never changes a Lightsail resource or the game's name. The plan file holds the settings, so the script deletes it afterwards.

**Settings:** every `KEY=value` line of `lambda.env` becomes one of the function's environment variables, so a new setting that `scripts/lambda-env.sh` writes needs no Terraform change. Values are taken literally, without quotes. Plans show the environment only as `(sensitive value)`.

- Run `scripts/aws.sh deploy` again whenever `scripts/lambda-env.sh` rewrites `lambda.env`.
- `ISLAND_BATTLES_DATABASE_MAX_OPEN_CONNECTIONS` comes from the `database_max_open_connections` variable instead, and the log and trace limits from `log_lines_per_minute`, `log_line_bytes`, `trace_interval_seconds` and the concurrency cap. Terraform refuses them in `lambda.env` and `extra_lambda_environment` ([The concurrency cap](#the-concurrency-cap), [The budget](#the-budget)). It also sets the collector's settings and `OTEL_EXPORTER_OTLP_ENDPOINT`, `ISLAND_BATTLES_BOT_PACKAGES_BUCKET`, the [bot package bucket](#bot-packages) it creates, `ISLAND_BATTLES_BOT_RUNNER_PYTHON` and `ISLAND_BATTLES_BOT_RUNNER_NATIVE`, the [bot runners](#bot-runners) it creates, and `ISLAND_BATTLES_PUBLIC_URL` and `ISLAND_BATTLES_LOBBY_ORIGINS` from the hostnames ([The backend's settings](#the-backends-settings)), and refuses those too.
- Non-secret settings of your own, such as the pool's idle time, go in `extra_lambda_environment` in `aws.tfvars`.
- The Lambda needs no game retention setting: games are deleted 7 days after their last turn by default. To change the period, set `ISLAND_BATTLES_GAME_RETENTION_PERIOD` there as a Go duration, such as `336h` for 14 days ([Game retention](../backend/README.md#game-retention)).

**Why Terraform publishes the code,** rather than the AWS CLI with Terraform only creating the function:

- **One owner.** If the CLI published the code and settings, Terraform would have to ignore both. The pool size that Terraform checks against the reserved concurrency would then be set by another tool, and nothing would check the two together.
- **One tool.** A deploy needs Terraform, Go and Python, which the rest of the deployment already uses. The AWS CLI only syncs the deploy bucket.
- **Checked offline.** `terraform test` covers the settings pass-through and the concurrency check with mocked providers.
- **The cost:** every deploy plans the whole configuration. That takes a few seconds longer and could pick up unrelated changes, which the plan check stops.
- Either way the state holds the secrets, because Terraform reads the function's environment into its state even when it ignores changes to it.

## Redeploy

### The backend

Run `scripts/aws.sh deploy` from the commit you want to publish. See [Deploying the backend](#deploying-the-backend).

### The AWS resources

Change `deploy/local/aws.tfvars` or `deploy/terraform/`, then run `scripts/aws.sh apply`. It shows the plan and applies it only when you type `yes`.

- **Read the plan.** A plan that deletes or replaces a Lightsail resource, or the account's GitHub OIDC provider, which INTODAN/mpl's roles trust too, is refused outright, and nothing allows it: such a plan is a mistake in the configuration or in `aws.tfvars`. Keep `github_repository` set, or the plan would delete the provider.
- **Destroying the distribution needs its name.** When the plan deletes or replaces the distribution, the apply lists it and asks you to type its domain, `<id>.cloudfront.net`, before the usual `yes`: a new distribution gets a new one, and the `island-battles` CNAME must follow it. A wrong answer applies nothing. Without a terminal, list the domains in `ISLAND_BATTLES_ALLOW_DESTROY`, separated by commas.
- **It may publish the backend too.** When the plan changes the function, the apply first runs the migrations and then publishes the backend built from your checkout, so run it from the commit you last deployed or a newer one. A plan that leaves the function alone runs no migrations.

## The cutover

The cutover (#232) moves Island Battles off the VM and off its own lobby: its clients use the lobby at `https://lobby.<domain>`, which INTODAN/mpl deploys, its Lambda serves no lobby route, and this repository's Terraform lets go of the VM, which INTODAN/mpl owns from then on. Open lobbies are dropped at the switch, as the owner accepted: players who stay on an old page get 410s and must reload.

**Before:** INTODAN/mpl has taken the VM over and deploys the lobby at `https://lobby.<domain>` (#231; [its "Taking over a running VM"](https://github.com/INTODAN/mpl/blob/main/deploy/README.md#taking-over-a-running-vm), steps 1 to 9): its `vm` run wrote the marker `mpl/vm-owner` and published the tenant's settings, `mpl/tenants/island-battles/tenant.env`, and its `smoke` run passed. The change that brings this section, #232, is merged, and nothing has been deployed from it yet. Pick a quiet time: the steps take about half an hour.

1. **Announce it.** Players should finish their games first. Bot authors: from the release, the SDKs take the lobby's URL apart from the game's, `--lobby-url https://lobby.<domain>`, and `serve` admits the lobby page at `https://lobby.<domain>`; older SDKs need `--allow-origin https://lobby.<domain>`. The release notes list every break.
2. **INTODAN/mpl deploys the tenant's database and secrets.** Its `vm` run of step 8 did: check that `aws s3 ls "s3://<deploy bucket>/mpl/tenants/island-battles/"` lists `tenant.env`. If INTODAN/mpl changed the tenant since, run its `vm` target again first.
3. **The data stays where it is.** The tenant's database is Island Battles' own: INTODAN/mpl's takeover names the running database and role, `island_battles` ([its "Taking over a running VM"](https://github.com/INTODAN/mpl/blob/main/deploy/README.md#taking-over-a-running-vm), step 3), and its password is the one the backend uses today. Nothing is copied or renamed, so no game is lost. `tenant.env`'s `DATABASE_NAME` must say `island_battles`, and step 4's `lambda-env` refuses settings that name another database: a new, empty one would start the game without its maps and games. Then fix the tenant in INTODAN/mpl's `vm.yml` instead.
4. **Write the backend's settings and apply here.**
   1. A Deploy run with target `lambda-env`, or `scripts/lambda-env.sh`: it copies `tenant.env` and writes `deploy/local/lambda.env` from it ([The backend's settings](#the-backends-settings)).
   2. Pull `deploy/local/` (`scripts/deploy-files.sh pull`), set `lobby_hostname` in `deploy/local/aws.tfvars`, delete the VM's variables there (`vm_instances`, `static_ip_instance`, `ssh_public_key`, `ssh_allowed_cidrs`, `postgres_port`, `postgres_max_connections`, `postgres_spare_connections`; Terraform warns about each one left), and push it (`scripts/deploy-files.sh push`).
   3. A Deploy run with target `plan`. Its summary must read:
      - **removed from the state, not destroyed:** the instance, its firewall, the static IP, its attachment and the key pair, if any. Terraform lists them as "will no longer be managed by Terraform".
      - **created:** `aws_cloudfront_function.lobby_redirect`.
      - **updated in place:** the distribution (the lobby's two behaviours), the backend function (its code and settings), the bot packages' CORS, the GitHub deploy role's policy.
      - **destroyed:** the old backup user, its policy and key, and the `<name>-database-backups` alarm ([Backups](#backups)).

      `check_deploy_plan.py` stops the run before it saves a plan that would destroy anything INTODAN/mpl owns.
   4. A Deploy run with target `apply` and that run's `plan_run_id`; `allow_destroy` stays empty. It migrates Island Battles' database, which finds nothing to do, and publishes the backend without its lobby.
5. **Deploy the web client:** a Deploy run with target `web`. It writes `/play/config.json` with the lobby's URL.
6. **Switch Centrifugo's proxy in INTODAN/mpl**, right after step 4: [its "The cutover"](https://github.com/INTODAN/mpl/blob/main/deploy/README.md#the-cutover), step 1 (`centrifugo_proxy_url` to its own `function_url`, then its `vm` target). From step 4 until then, Centrifugo's subscribe calls reach a backend that no longer serves them, so live updates stop until clients reconnect. Doing this step before step 4 avoids that gap: until step 4, both lobbies use the same `lobby` database and the same secrets, so INTODAN/mpl's lobby answers Centrifugo for players of the old one as well.
7. **Smoke both:** a Deploy run with target `smoke` here ([Smoke test](#smoke-test)), and one in INTODAN/mpl. Then, in INTODAN/mpl, the rest of [its "The cutover"](https://github.com/INTODAN/mpl/blob/main/deploy/README.md#the-cutover): the firewall, now out of this state, and stopping this repository's old `island-battles-backup.timer` on the VM.
8. **Release `v1.1.0`:** the Release workflow with bump `minor`, the owner's decision of 2026-11-14, then paste the breaking changes that #119 collects at the top of its notes ([Releasing](#releasing)). It deploys the same code again with its version, and publishes `island-battles-python` and the Go SDK.

**Rolling back:**

- **The web client and the redirects** come back with a revert: a Deploy `web` run of the earlier commit restores `/play/`. Its `config.json` stays, and is ignored. Browsers keep the 301 of `/lobby/ui/` for a day (`max-age=86400`).
- **Centrifugo's proxy** switches back in INTODAN/mpl: `centrifugo_proxy_url` to this backend's `function_url`, then its `vm` target.
- **The backend's own lobby can't come back with a Deploy run.** `v1.0.0`'s Terraform declares the VM, which this state no longer holds, so a `backend` run with `release_tag: v1.0.0` stops at its plan check. Going back is a revert of #232 on `main` and a new pull request: restore the earlier `deploy/local/lambda.env` from the deploy bucket's versions of `files/lambda.env`, then `plan` and `apply`. The lobby's data is safe either way: both lobbies use the same `lobby` database.
- **The old backup key** is gone for good; a revert creates a new one. INTODAN/mpl backs up every database on the VM, Island Battles' too.

## Deploying from GitHub Actions

The Deploy workflow, `.github/workflows/deploy.yml`, runs the same scripts on a GitHub runner. Start it from the repository's **Actions** tab, with `gh workflow run deploy.yml -f target=<target>`, or let an agent start it:

| Target | Runs | Role |
|---|---|---|
| `backend` | `scripts/aws.sh deploy` | deploy |
| `lambda-env` | `scripts/lambda-env.sh`: copies INTODAN/mpl's settings for the tenant and writes `lambda.env` ([The backend's settings](#the-backends-settings)). A `backend` run publishes them. It can't pass `-e app_generate_secrets=true`. | deploy |
| `web` | `scripts/deploy-web-client.sh`: builds the 3D web client and publishes it at `/play/` ([The web client](#the-web-client)) | deploy |
| `smoke` | `scripts/smoke.sh` ([Smoke test](#smoke-test)) | deploy |
| `plan` | `scripts/aws.sh plan`: plans every AWS resource, like `scripts/aws.sh apply`, and saves the plan | deploy |
| `apply` | `scripts/aws.sh apply-plan` with the plan that the `plan_run_id` run saved | apply |

`release_tag` works with `backend`, `web` and `smoke`: it deploys that release instead of `main` ([Releasing](#releasing)).

**Only `main` deploys.** Every run is in the `production` environment, which is limited to `main`, and both roles trust only that environment. So only merged code gets AWS credentials, and merging a pull request is the approval. GitHub can't require a reviewer's approval for each run in a private repository on a personal plan. Runs wait for each other, and Terraform's lock keeps them apart from a machine's.

**Applying:**

1. Start a `plan` run. Its summary shows the plan, and the run ID to apply it with. It saves the plan, and the build it packages, under `plans/<run ID>/` in the deploy bucket. The bucket deletes a saved plan after 3 days.
2. Read the plan. If it's right, start an `apply` run with `plan_run_id` set to that run ID. It applies exactly that plan, then deletes it. It refuses a plan made from another commit than `main`'s, and Terraform refuses one whose state changed since: plan again.
3. A plan that deletes or replaces the distribution stops the apply unless `allow_destroy` lists its domain, like `ISLAND_BATTLES_ALLOW_DESTROY`. A plan that deletes or replaces a Lightsail resource or the account's GitHub OIDC provider fails the `plan` run before it's saved, and the apply would refuse it too: `allow_destroy` can't allow it.

**The roles:**

- **Deploy role**, `island-battles-github-deploy` (`deploy/terraform/github.tf`): reads the account (`ReadOnlyAccess`), which every plan needs, and writes only what a deploy writes: the backend function's code and settings, the [bot runners'](#bot-runners) code, its own part of the [deploy bucket](#the-deploy-bucket), the web client's files under `play/` in its bucket (it may put them, and list the bucket), and invalidations of the game's distribution. In the deploy bucket, which INTODAN/mpl shares, it writes only the state and its lock, `files/` and `plans/`. It reads `mpl/tenants/island-battles/tenant.env` for the `lambda-env` target, and deletes nothing but the state's lock.
- **Apply role**, `island-battles-github-apply` (`deploy/github_apply_role.py`): reads the account, and may change what `deploy/terraform/` manages: Lambda, CloudWatch logs and alarms, SNS, Budgets, ACM and CloudFront, Terraform's three buckets (the backups, the web client and the bot packages), IAM roles named `island-battles-*` and GitHub's OIDC provider. It may change no Lightsail resource and no IAM user. In the deploy bucket it may list the bucket and use only what an apply uses: the state and its lock, `files/` (it reads and writes them) and `plans/` (it reads and deletes them). It deletes nothing else there, writes nothing under `mpl/`, and can't change the bucket itself. It can't change its own role. Because it can write IAM policies for `island-battles-*` roles, treat it as close to an administrator of the account: reviewing what merges to `main` is the control. It isn't part of the Terraform configuration, so an apply can't widen it.
- Neither role can read the [backups](#backups), nor can INTODAN/mpl's two. GitHub stores no AWS keys: each run gets credentials for an hour.
- Neither role can read INTODAN/mpl's part of the deploy bucket either, except the tenant's settings, `mpl/tenants/island-battles/tenant.env`, which the `lambda-env` target reads. `ReadOnlyAccess` reads every bucket, so both roles' policies have an explicit `Deny` of `s3:GetObject*` on mpl's files and secrets (`mpl/files/`), its Terraform state and lock (`mpl/terraform/`), its saved plans (`mpl/plans/`) and the VM owner marker (`mpl/vm-owner`). It covers every read of an object, including the earlier versions that the versioned bucket keeps (`s3:GetObjectVersion`). Listing the bucket's keys stays allowed, and shows each object's ETag, which for these uploads is the MD5 of its content: the generated secrets are long enough that this reveals nothing. INTODAN/mpl's roles have the matching `Deny` on this repository's `files/`, `terraform/` and `plans/` (INTODAN/mpl#10).
  - The `Deny` lists mpl's keys, the ones in its `deploy/deploy_files.py`, rather than all of `mpl/`. A `Deny` on `mpl/*` would deny the tenant's settings too, and no `Allow` overrides a `Deny`. IAM has no way to say "`mpl/*` except `mpl/tenants/island-battles/*`": a resource has only the `*` and `?` wildcards, no S3 condition key holds an object's key, and a `NotResource` would deny reading every other bucket too. So a new top-level key in mpl's part needs adding to `mpl_unreadable_keys` in `deploy/terraform/github.tf` and `MPL_UNREADABLE_KEYS` in `deploy/github_apply_role.py`, and another tenant's settings under `mpl/tenants/` stay readable. Island Battles is mpl's only tenant today.

**Deploying from an agent:** an agent in the cloud has no AWS credentials of its own. Once you've merged its pull request, it starts a `plan` run on `main`, reads the plan from the run's log and tells you what it changes. When you say so, it starts the `apply` run for that plan, then any `lambda-env`, `backend`, `web` or `smoke` runs the change needs, and checks the result.

**Setting it up**, once:

1. Set `github_repository = "<owner>/<name>"` in `deploy/local/aws.tfvars` and run `scripts/aws.sh apply`. It creates GitHub's OIDC provider in IAM and the deploy role. `scripts/aws.sh output github_deploy_role_arn` prints the role. An account has one provider for GitHub, which INTODAN/mpl's roles trust too: if it exists already, import it into this state rather than create a second, and keep `github_repository` set from then on.
2. In the repository's **Settings → Environments**, create `production` and set **Deployment branches and tags** to **Selected branches and tags**, with only `main`. Both roles rely on that limit.
3. Add two repository variables, which aren't secret: `AWS_DEPLOY_ROLE_ARN`, the deploy role, and `AWS_REGION`, the deployment's region. The workflow derives the apply role's ARN from the deploy role's; set `AWS_APPLY_ROLE_ARN` only to use another one.

   ```sh
   gh variable set AWS_DEPLOY_ROLE_ARN --body "$(scripts/aws.sh output github_deploy_role_arn)"
   gh variable set AWS_REGION --body <region>
   ```

4. Create the apply role, with credentials that can manage IAM. Run it again after changing `deploy/github_apply_role.py`.

   ```sh
   python3 -I deploy/github_apply_role.py create --repository <owner>/<name>
   ```

5. Run `scripts/deploy-files.sh bootstrap --region <region>` again, which adds the rule that deletes saved plans after 3 days.

**What still needs your own machine:** a new deployment's `scripts/lambda-env.sh -e app_generate_secrets=true` and its certificate's targeted apply, and the setup above.

## Releasing

A release deploys the backend, uploads the web client, runs the smoke test, tags the commit and publishes a GitHub release and the clients. The lobby is released, tagged, published and deployed from its own repository, [`INTODAN/mpl`](https://github.com/INTODAN/mpl); this repository uses its Go module at the version `go.mod` pins (the root README's [The lobby's version](../README.md#the-lobbys-version)). Merging to `main` releases nothing: a person starts the Release workflow, `.github/workflows/release.yml`, by hand.

```sh
gh workflow run release.yml --ref main -f bump=patch   # or minor
```

Or use the **Actions** tab, **Release**, **Run workflow** on `main`.

**Starting it is the approval.** Only people with write access can start it, only from `main` (any other branch fails at once, before anything runs), and its deploy jobs run in the `production` environment, which is limited to `main`, like every Deploy run. GitHub offers no required reviewer on a private repository's environment on a personal plan, so the run doesn't wait for a second approval. Read what merged since the last release before you start it. Start it when no other Deploy run is going: a manual Deploy that runs during a release can make the release's smoke step fail, and then no tag is created.

**What it does,** in order. A failure anywhere stops the rest and leaves no tag and no release:

1. **Checks** the commit with the same checks as a pull request (`checks.yml`).
2. **Plans** the version with `scripts/release-plan.sh`. The previous release is the highest `vX.Y.Z` tag. The first release, with no tag yet, is `v0.1.0`, whatever `bump` says. The run's summary shows the version, the previous release and the bump.
3. **Deploys the backend,** then **the web client** (`web` target, below), then runs the **smoke test**, each one a call of the Deploy workflow, one at a time. The backend is built with the version, and the smoke test fails unless `/healthz` reports it. The web client is built from the same commit and uploaded to `/play/`, so the backend and the client that talks to it go live together; the smoke test then fails unless `/play/` returns 200, loads the script that was just uploaded, and names the lobby in `/play/config.json`.
4. **Releases:** creates the annotated tags `vX.Y.Z` and `clients/island-battles-go/vX.Y.Z` on the deployed commit, and a GitHub release with generated notes, which name the Go SDK's tag and say that the lobby and the VM are deployed from INTODAN/mpl. Releases up to `v0.2.0` also tagged `lobby/vX.Y.Z` for the lobby module, which now has its own versions in `INTODAN/mpl`, continuing from `v0.3.0`. The `clients/island-battles-go/vX.Y.Z` tag is the version of the [Go SDK](../clients/island-battles-go/README.md), whose module path `github.com/INTODAN/island-battles-go/clients/island-battles-go` is go-gettable: `go get github.com/INTODAN/island-battles-go/clients/island-battles-go@vX.Y.Z` resolves that tag. The plan fails if either tag already exists.

If the tags were pushed but `gh release create` failed, re-running fails at the tag push. Create the release by hand with `gh release create vX.Y.Z --verify-tag --generate-notes`.

**The bump is the one you ask for.** Ask for `major` only when the release breaks clients, and not past `v1` yet: the plan refuses `v2.0.0` and later, because Go refuses a `clients/island-battles-go/v2.x.y` tag for a module path without `/v2`. Move the Go SDK's module path to `.../v2` first, or release a minor version. When a release breaks clients, paste its breaking changes at the top of its notes by hand once it's made. `v1.1.0`, the [cutover's](#the-cutover) release, is minor by the owner's decision of 2026-11-14, and #119 collects its breaking changes.

**The web client** is uploaded by every release, after the backend deploy and before the smoke test, with `scripts/deploy-web-client.sh` ([The web client](#the-web-client)). The `web` target sets up Bun at the version pinned in `clients/island-battles-web/package.json`, and passes the entry script of the build it uploaded (such as `assets/index-1a2b3c4d.js`) to the smoke step. To upload a client on its own, start the Deploy workflow with `target` `web` (`gh workflow run deploy.yml --ref main -f target=web`). The upload is not tied to the version tag: a rollback of the client is `web` with `release_tag` set to an earlier release, which builds that tag's client. That works only for releases that include `scripts/deploy-web-client.sh` and the client's `packageManager` pin; an earlier release has neither, so roll back the client by hand, as described below.

**Releases can't create AWS resources.** The deploy role only updates the function. When a release needs a new or changed resource, such as a Terraform change under `deploy/terraform/`, merge it first and run `plan` and then `apply` on `main` ([Deploying from GitHub Actions](#deploying-from-github-actions)) before you start the release. The release's backend deploy refuses a plan that touches Lightsail, the certificate or the distribution.

**The version.** `/healthz` still answers 204 and reports the build's version in the `X-Island-Battles-Version` header (`dev` for builds that set none). `ISLAND_BATTLES_VERSION=v1.1.0 scripts/aws.sh deploy` builds with a version, and `ISLAND_BATTLES_EXPECTED_VERSION=v1.1.0 scripts/smoke.sh` checks it. A plain `backend` deploy, or a `plan`/`apply` that publishes the backend, builds the commit as `dev`; the next release sets the version again.

**Rolling back:** start the Deploy workflow on `main` with `target` `backend` and `release_tag` set to the previous release, for example:

```sh
gh workflow run deploy.yml --ref main -f target=backend -f release_tag=v1.1.0
gh workflow run deploy.yml --ref main -f target=smoke -f release_tag=v1.1.0
```

A rollback is only for releases whose difference from the tag is code only: no change under `deploy/terraform/` since that tag. A rollback runs the tag's `scripts/aws.sh` and the tag's Terraform configuration, so it can refuse, fail with AccessDenied, or revert Terraform-managed Lambda settings. No rollback reaches a release before `v1.1.0`: their Terraform declares the VM ([The cutover](#the-cutover)). Otherwise fix forward with a new release.

The workflow itself must run on `main`, because the `production` environment accepts nothing else. `release_tag` makes it check out that tag, which must be on `main`, build it with its version, and (for `smoke`) expect it. A rollback creates no tag or release, and the migrations only go forward: the older code runs against the newer schema, so roll back only past releases whose migrations the older code tolerates.

**Later additions,** publishing the Python clients (#117), are jobs that `need` the `release` job in `release.yml`. A release made with the workflow's token doesn't trigger `on: release` workflows, so they can't be separate workflows.

### Publishing the Python clients

After the release is made, the `publish-python` job in `release.yml` publishes `island-battles-python` (import name `island_battles_client`, from `clients/island-battles-python/`) to PyPI at the release's version, `X.Y.Z` for the tag `vX.Y.Z`, in the GitHub environment `pypi-island-battles`. It builds the package with `uv build` and checks that its wheel and sdist exist.

The lobby's client, `mpl-lobby-client` (`lobby_client` and `lobby_api_client`), was published from here, in the environment `pypi`, until the lobby moved to its own repository (#230). Now [`INTODAN/mpl`](https://github.com/INTODAN/mpl) releases it, with its own release workflow and its own `pypi` environment, and tags the lobby's Go module `vX.Y.Z` there; this repository's releases no longer tag `lobby/vX.Y.Z`. The PyPI trusted publisher of `mpl-lobby-client` must name `INTODAN/mpl` instead of this repository (#119).

The version is set from the tag in the job's working copy (`uv version --frozen`, never committed), and it uploads with PyPI **trusted publishing**: the workflow exchanges a short-lived OIDC token for an upload token, so the repository holds no PyPI token. The repository keeps the placeholder version `0.0.0.dev0`. The owner chose the names: the generic multiplayer infrastructure is called "mpl", so the lobby client is `mpl-lobby-client`, and the Island Battles client stays `island-battles-python`.

**Owner setup, once, before the first release** (an agent can't do it on PyPI):

1. On pypi.org, the project `island-battles-python` has a trusted publisher with owner `INTODAN`, repository `island-battles-go`, workflow `release.yml` and environment `pypi-island-battles` (a pending publisher creates the project on the first upload).
2. On GitHub, in **Settings → Environments**, make sure the `pypi-island-battles` environment exists (the first run creates it) and, under deployment branches, limit it to `main`, as `production` is. The `pypi` environment here served `mpl-lobby-client` and is no longer used.

**Failures.** The job runs after `release`, so a failed publish fails the run visibly, but the deploy, the tags and the GitHub release stay. Typical causes are a missing or misspelled pending publisher (the environment or workflow name must match exactly) or a PyPI outage. Fix the cause and re-run the `publish-python` job from the run's page; `skip-existing` makes it skip the files that an earlier partial run uploaded. Don't upload by hand with a token.

**Installing:** `pip install mpl-lobby-client island-battles-python`, or `uv add mpl-lobby-client==X.Y.Z island-battles-python==X.Y.Z` in your own project. Each client's README has the details.

## The web client

The 3D client in `clients/island-battles-web/` is served at `https://<game_hostname>/play/` (#104). It is a static build in a private S3 bucket, `<name>-web-client-<account>`, behind the game's own distribution:

- **The game on the same origin.** The client's game calls stay on the game's host, so they need no CORS. It calls the lobby at the URL that `/play/config.json` names, `https://<lobby_hostname>`; the lobby answers the game's origin with CORS, and Centrifugo accepts its WebSockets, because INTODAN/mpl's tenant entry lists it ([The lobby on its own origin](#the-lobby-on-its-own-origin)).
- **Cached at the edge.** `/play/*` uses `Managed-CachingOptimized` with compression, and never reaches the Lambda. Everything else still goes to the Lambda, uncached, except the static files of the game's other pages (see the CloudFront row of the resources table).
- **Private bucket.** Public access is blocked, and the bucket policy lets only this distribution read `play/*`, through an origin access control.
- **`/play/` and `/play/?join=CODE`** serve `play/index.html`: a CloudFront Function adds `index.html` to a path that ends in a slash. `/play` without the slash is redirected to `/play/`. Query strings aren't part of the cache key, so every `?join=CODE` gets the same cached page.

**Creating the resources, once.** The change adds the bucket with its public access block, encryption setting, ownership controls and policy, the origin access control and the CloudFront function, and updates the distribution. It replaces and deletes nothing. Apply it like any other AWS change: start the Deploy workflow's `plan` target on `main`, read the plan, then start `apply` with that run's ID ([Deploying from GitHub Actions](#deploying-from-github-actions)), or run `scripts/aws.sh apply` on your machine. The apply role may create the bucket because `deploy/github_apply_role.py` names it in `ManageItsBuckets`: a new bucket in `deploy/terraform/` needs its name there, and a new run of the script before the apply that creates it. A backend deploy (`scripts/aws.sh deploy`) is unaffected afterwards: the new resources are unchanged in its plans, and it stops only on a plan that changes CloudFront.

**Order for a fresh setup:** plan and apply the new resources, run `scripts/deploy-web-client.sh`, then run the backend deploy or release that shows the **Play in 3D** link, so the link never points at an empty bucket. Until the one-time apply has created the CloudFront resources, `backend` deploy runs are refused, because their plan would create them.

**Uploading a build:**

```sh
scripts/deploy-web-client.sh
```

It reads the bucket, the distribution and the lobby's URL from Terraform's outputs (`web_client_bucket`, `distribution_id`, `lobby_url`), then:

1. runs `bun run build` in `clients/island-battles-web/`, which builds for `/play/` with hashed files under `dist/assets/`;
2. uploads `dist/assets/` to `s3://<bucket>/play/assets/` with `Cache-Control: public, max-age=31536000, immutable`;
3. writes the runtime config, `play/config.json`, `{"lobby_url": "<lobby_url>"}`, with `Cache-Control: no-cache`;
4. uploads `index.html` last, with `Cache-Control: no-cache`, so a page never names a file that isn't there yet;
5. invalidates `/play/`, `/play/index.html` and `/play/config.json`, so the new build shows as soon as the script ends.

It never deletes files, so the hashed files of earlier builds stay and tabs that are open keep working. They are small; remove old ones from the bucket by hand if they ever matter. It needs `bun`, the AWS CLI and Terraform, and the credentials that `scripts/aws.sh` uses. Every release uploads it ([Releasing](#releasing)), and the Deploy workflow's `web` target does it on its own. If `ISLAND_BATTLES_WEB_BUNDLE_FILE` names a file, the script writes the build's entry script there, which is what the workflow hands to the smoke test.

**Once, for the release to upload:** the deploy role needs the permissions that #114 adds (`s3:PutObject` on `play/*` of the bucket, `s3:ListBucket` on it, and `cloudfront:CreateInvalidation` on the distribution). They are a change to the role's policy in `deploy/terraform/github.tf`, so after merging, start a `plan` run on `main` (it shows only the role policy changing), read it, then start `apply` with that run's ID. Until then the `web` target fails with AccessDenied, and `backend` deploys, which would apply that policy change themselves without the permission to, fail too: apply first, then release.

**Rolling back:** check out the commit of the earlier build and run `scripts/deploy-web-client.sh` again. Its `index.html` names the earlier hashed files, which are still in the bucket, and the invalidation makes it live.

**A missing file answers 403**, not 404: the bucket policy grants only `s3:GetObject`, so S3 doesn't reveal which keys exist.

**Checking it:** `curl -sI https://<game_hostname>/play/assets/<file>` shows `x-cache: Hit from cloudfront` on the second request, and `/play` answers `301` to `/play/`.

## Bot packages

Members upload bot packages, the zips that the SDKs' `package` command builds, to their lobby (#221), into the private bucket `<name>-bot-packages` (`deploy/terraform/bot_packages.tf`):

- **The zip never passes through the Lambda,** whose Function URL takes at most 6 MB. The backend hands out a presigned POST, valid for 15 minutes, for 1 byte to 250 MiB, and the browser or the CLI posts the zip straight to the bucket. Its CORS rule allows POST from `https://<game_hostname>`, where the 3D client is, and from `https://<lobby_hostname>`, where the lobby page is.
- **The backend reads only what it checks:** the zip's central directory and `island-battles-bot.json`, with range requests. Its role may put, get and delete objects in this bucket and nothing more. It deletes a zip that fails its check, the previous package of a member who uploads a new one, and a package its owner deletes.
- **Everything is deleted after 8 days:** a lifecycle rule deletes each zip 8 days after its upload, and the backend's [game retention](../backend/README.md#game-retention) deletes the packages' rows as long after. Versioning is off, so nothing is kept longer.
- **Settings:** Terraform passes the bucket's name to the function as `ISLAND_BATTLES_BOT_PACKAGES_BUCKET`, and the region is the function's own. Without it, the backend answers the bot package endpoints with 503.

**Creating the resources, once.** The change adds the bucket with its public access block, encryption, ownership controls, lifecycle rule, CORS rule and policy, and a policy on the function's role. It replaces and deletes nothing. Plan and apply it from `main` (the Deploy workflow's `plan`, then `apply` with that run's ID, or `scripts/aws.sh apply`) before the next backend deploy: the GitHub deploy role may not create buckets, so a `backend` run that would create them fails. The apply role may, because `deploy/github_apply_role.py` names the bucket in `ManageItsBuckets`: a new bucket in `deploy/terraform/` needs its name there, and a new run of the script before the apply that creates it.

## Bot runners

Two Lambda functions play the turns of uploaded bot packages, and deliver hosted bots' turns, one turn of one bot per invocation (#223), in `deploy/terraform/bot_runners.tf`. [backend/README.md](../backend/README.md#bot-runners) describes what they do. The backend invokes them asynchronously when each turn opens (#225, [how server-run bots play](../backend/README.md#how-server-run-bots-play)), and never waits for them.

| Function | Runtime | Code |
|---|---|---|
| `<name>-bot-runner-python` | `python3.14`, handler `runner.handler` | `backend/botrunner/python/runner.py`, zipped by Terraform |
| `<name>-bot-runner-native` | `provided.al2023`, a Go `bootstrap` | `backend/cmd/bot-runner-native`, built by `scripts/aws.sh` into `deploy/local/lambda/bot-runner-native/` |

- **Both:** arm64, 1024 MB, a 20-second timeout (up to 10 seconds to load a package, 2 to play, and the rest to submit) and 2048 MB of `/tmp`, for a 250 MiB zip unpacking to at most 1 GiB. A reserved concurrency of 10 each, so a runaway can't take the concurrency the backend needs ([The concurrency cap](#the-concurrency-cap)). Asynchronous invocations are never retried (`maximum_retry_attempts = 0`) and are dropped after 60 seconds: a retry would submit a turn twice or after its deadline.
- **Settings:** only `GAME_SUBMIT_HOST`, the game's name (`game_hostname`). A runner submits only there.
- **Role:** `<name>-bot-runner`, which may write the two functions' logs and nothing else: no S3, no secrets. A package arrives as a presigned GET in the event.
- **Logs:** `/aws/lambda/<name>-bot-runner-python` and `…-native`, kept for 7 days. As for the backend, Lambda filters them at WARN, so only failures are kept: one line for each turn a bot passes, and none of the start and report lines of every invocation.
- **Invoked by the backend:** its role gets `lambda:InvokeFunction` on the two functions and nothing else (the inline policy `bot-runners` of `<name>-lambda`), and Terraform passes their names to it as `ISLAND_BATTLES_BOT_RUNNER_PYTHON` and `ISLAND_BATTLES_BOT_RUNNER_NATIVE`. Naming them makes Terraform create the functions before it updates the backend. Hosted and uploaded bots are on by default (`ISLAND_BATTLES_SERVER_BOTS`); to turn them off without a code change, set `ISLAND_BATTLES_SERVER_BOTS = "off"` in `extra_lambda_environment`. A turn whose invocation fails, such as one Lambda throttles past the reserved concurrency, is logged as an error (so the error alarm counts it), and the player's game log says the server couldn't start their bot.
- **Code:** published like the backend's. `scripts/aws.sh` builds the native runner with the backend, Terraform zips both runners, and every `apply` and `deploy` publishes what changed, `backend` deploys and releases included. The [GitHub deploy role](#deploying-from-github-actions) may update the two functions' code (`lambda:UpdateFunctionCode`) and nothing else of theirs.

**Creating the resources, once.** The change adds the two functions, their role and its policy, their two log groups and two invoke settings, and a statement in the GitHub deploy role's policy. It replaces and deletes nothing. Plan and apply it from `main` (the Deploy workflow's `plan`, then `apply` with that run's ID, or `scripts/aws.sh apply`) before the next backend deploy: the deploy role may not create functions or roles, so a `backend` run whose plan would create them fails. The apply role may, because the role's name starts with `island-battles-`. The account needs 20 more reserved concurrency than before, so its "Concurrent executions" limit must be at least 125 with the backend's default of 5 (Lambda keeps 100 unreserved); on an account still limited to 10, the apply fails until AWS raises the limit.

**Checking it**, once the apply has published both: invoke a runner with an event whose submit URL is on another host, which it refuses without submitting anything, and read the refusal:

```sh
aws lambda invoke --function-name island-battles-bot-runner-native --cli-binary-format raw-in-base64-out \
  --payload '{"hosted": {"url": "https://example.org/"}, "delivery": {"turn": {"game_id": 1, "generation": {"id": 1}}, "submit": {"url": "https://example.org/generation/commands", "token": "x"}}}' /dev/stdout
```

It answers `"errorMessage":"the submit URL \"https://example.org/generation/commands\" is not on the game's host island-battles.<domain>"`. The same event fails the Python runner with `EventError`.

**Letting the backend invoke them, once (#225).** The change adds the backend role's inline policy `bot-runners` and the two settings of the backend function. It replaces and deletes nothing. Plan and apply it from `main` before the next `backend` deploy, as above: the deploy role may not change the backend's role, so a `backend` run whose plan would add the policy fails. From then on, every game with a hosted or uploaded bot invokes the runners. To check it, play a game against a level 1 seat with a hosted or uploaded bot of your own, with every tab closed, and read the runners' log groups for passes.

### The monthly compute guard

The runners share Lambda's always-free 400,000 GB-s and 1 million requests a month with the backend. The backend keeps them to 300,000 GB-s a month (#226, [backend/README.md](../backend/README.md#monthly-compute-guard)): once they have used it, no new game with an uploaded bot starts until the first of the next month (UTC), and both bot pickers grey out uploaded bots. Games already running go on.

- **Measured:** the backend reads both runners' month-to-date `Duration` and `InitDuration` from CloudWatch (`cloudwatch:GetMetricStatistics`, which IAM can't scope to resources, so the backend role's inline policy `bot-compute` allows it alone, on `*`), times their 1 GB, at most once per 10 minutes, and keeps it in the database. A read is 4 API requests, inside CloudWatch's free requests: see [Free allowances](#free-allowances).
- **Settings:** `ISLAND_BATTLES_BOT_COMPUTE_BUDGET_GB_SECONDS`, 300000 by default, in `extra_lambda_environment` to change it; `0` stops every new game with an uploaded bot. Turning server bots off (`ISLAND_BATTLES_SERVER_BOTS = "off"`) stops them all.
- **Safety net:** the $5 monthly cost budget (`<name>-monthly`) emails `budget_email`, or `alert_email` while it is unset, at 80 % of actual costs ([Logs, alerts and traces](#logs-alerts-and-traces)).

**Adding the guard, once (#226).** The change adds the backend role's inline policy `bot-compute` and the budget `<name>-monthly`. It replaces and deletes nothing. To send the budget's emails somewhere else than `alert_email`, set `budget_email` in `deploy/local/aws.tfvars` and push it to the deploy bucket first. Plan and apply it from `main` (the Deploy workflow's `plan`, then `apply` with that run's ID, or `scripts/aws.sh apply`) before the next `backend` deploy: the deploy role may not change the backend's role, so a `backend` run whose plan would add the policy fails, and a backend without it can't read the runners' compute, so every start with an uploaded bot would answer 503. To check it, open the bot picker of a lobby with server bots: `GET /game/bot-compute` answers 200 with this month's `used_gb_seconds`.

## Smoke test

```sh
scripts/smoke.sh
```

It reads `game_url` and `lobby_url` from Terraform's outputs and runs `deploy/smoke.py`, with the Python SDK's locked dependencies through uv, which checks the game, the lobby and the VM together, through both names' CloudFront distributions:

1. The game's `/healthz` returns 204, with a valid certificate for the game's name. With `ISLAND_BATTLES_EXPECTED_VERSION` set, its `X-Island-Battles-Version` header must name that version ([Releasing](#releasing)).
2. The game's `/` leads to the lobby's page, on the lobby's URL.
3. The lobby's old routes on the game's URL say where it went: `/lobby/ui/?join=SMOKE` redirects (301) to the same path and query on the lobby's URL, and `POST /lobby/v1/sessions` answers 410 with a problem that names the lobby ([The lobby on its own origin](#the-lobby-on-its-own-origin)).
4. A guest credential works at the lobby.
5. The lobby issues a connection token for it, so it reaches its Postgres.
6. An authenticated GET reaches the lobby with its `Authorization` header. CloudFront strips the header from GET requests unless the origin request policy forwards it.
7. The lobby calls the game's hooks, signed with their shared secret: it refuses a lobby whose map doesn't exist with the game's own answer, `invalid_game_settings`. A failure here points at the game's `LOBBY_GAMES` entry in INTODAN/mpl's tenant, or at the hooks' secret.
8. The game takes the lobby's player token on an authenticated GET: the header reaches the backend, the backend reads the lobby's keys at `ISLAND_BATTLES_LOBBY_URL`, and it reaches its own Postgres.
9. Centrifugo accepts the connection token at the `websocket_url` the lobby gave with it, `wss://vm.<domain>/connection/websocket`, with a valid certificate and the game's URL as the browser's origin.
10. Centrifugo subscribes the guest to its personal channel, so it reaches the lobby's subscribe proxy (INTODAN/mpl's `centrifugo_proxy_url`).

With `ISLAND_BATTLES_EXPECTED_WEB_BUNDLE` set to the entry script of a build, such as `assets/index-1a2b3c4d.js`, it also checks that `/play/` returns 200, that its `index.html` loads that script, that the script is served, and that `/play/config.json` names the lobby's URL. CloudFront may serve the previous page for a few seconds after an invalidation, so it tries for up to 90 seconds. The release sets it from the `web` target's upload. A plain `smoke` run checks nothing of the web client, so it passes while nothing has been uploaded yet.

The guest's credential is the lobby's, kept in `deploy/local/smoke-session.json`, and later runs reuse it: a credential that still works shows the lobby's database came along after a move. Guest credentials expire after 24 hours, and the test replaces one that expires within two hours. If the saved credential is rejected for another reason, delete the file and run the test again.

## The admin MCP server

The [admin MCP server](../backend/README.md#admin-mcp-server) (#79) answers questions about lobbies, players and games, such as "How many lobbies are currently active?", from Claude Code or another MCP client on your machine. Nothing of it is deployed: it runs on your machine and connects straight to Postgres on the VM, to Island Battles' database and to the lobby's, as the read-only role `island_battles_admin`.

**The role is INTODAN/mpl's.** Its VM deploy creates it and lets it read the lobby's database and every tenant's, Island Battles' included, but not `lobby_sessions.bearer_hash`, the digest of each guest's credential. Its password is `postgres_admin_password` in INTODAN/mpl's secrets, which it copied from this repository's when it took the VM over. If a migration outruns the grants, so that the role could read `bearer_hash`, the server's `query` refuses to run in that database until INTODAN/mpl's `vm` target grants them again ([backend/README.md](../backend/README.md#admin-mcp-server)).

**The connection strings,** one per database. The certificate is self-signed for `vm.<domain>`, and INTODAN/mpl keeps it with its secrets, so check it with `verify-full`. In a checkout of INTODAN/mpl, `scripts/deploy-files.sh pull` brings both files into its `deploy/local/secrets/`:

```text
postgres://island_battles_admin:<password>@vm.<domain>:5432/island_battles?sslmode=verify-full&sslrootcert=<mpl>/deploy/local/secrets/postgres-tls/server.crt
postgres://island_battles_admin:<password>@vm.<domain>:5432/lobby?sslmode=verify-full&sslrootcert=<mpl>/deploy/local/secrets/postgres-tls/server.crt
```

Use an absolute path for `sslrootcert`. The password has only letters and digits, so it needs no escaping.

**Registering it** with Claude Code, from this repository's root, with `mpl` set to the absolute path of that INTODAN/mpl checkout:

```sh
mpl=<absolute path of an INTODAN/mpl checkout>
claude mcp add island-battles-admin-production \
  --env ISLAND_BATTLES_ADMIN_DATABASE_URL="postgres://island_battles_admin:$(cat "$mpl/deploy/local/secrets/postgres_admin_password")@vm.<domain>:5432/island_battles?sslmode=verify-full&sslrootcert=$mpl/deploy/local/secrets/postgres-tls/server.crt" \
  --env ISLAND_BATTLES_ADMIN_LOBBY_DATABASE_URL="postgres://island_battles_admin:$(cat "$mpl/deploy/local/secrets/postgres_admin_password")@vm.<domain>:5432/lobby?sslmode=verify-full&sslrootcert=$mpl/deploy/local/secrets/postgres-tls/server.crt" \
  -- go run ./backend/cmd/admin-mcp
```

Claude Code keeps the URLs, password included, in `~/.claude.json`, which only you can read. To run a built binary instead, and other clients, see the [backend README](../backend/README.md#admin-mcp-server).

## Backups

INTODAN/mpl backs up every database on the VM, Island Battles' included, every day, to its own backups bucket, and alarms when a backup fails ([its "Backups"](https://github.com/INTODAN/mpl/blob/main/deploy/README.md#backups)). Restoring a dump and moving to a new VM are INTODAN/mpl's too ([its "Dumps, restores and moving to a new VM"](https://github.com/INTODAN/mpl/blob/main/deploy/README.md#dumps-restores-and-moving-to-a-new-vm)).

This repository's own backup ran until [the cutover](#the-cutover): the VM's `island-battles-backup.timer` dumped the backend's and the lobby's databases every night to `s3://<name>-backups-<account>/postgres/`, with the upload-only key of the IAM user `<name>-database-backups`, and the `<name>-database-backups` alarm emailed when none succeeded for 26 hours. The cutover's apply deleted the user, its policy and key, and the alarm, and INTODAN/mpl's cutover stops the timer on the VM.

- **The bucket stays,** with `prevent_destroy`, and so does its lifecycle rule: each old dump expires `database_backup_retention_days`, 7 by default, after it was made, and versioning keeps it for as long again. `scripts/aws.sh output database_backup_bucket` names it.
- **Its policy still keeps the dumps from the GitHub roles:** this repository's two and those in `backup_denied_roles`, by default INTODAN/mpl's `mpl-github-deploy` and `mpl-github-apply`, which share the account and read it through `ReadOnlyAccess`. Reading a dump takes the account's own credentials.

## Logs, alerts and traces

The backend Lambda's logs, error alarms and traces stay inside AWS's free monthly allowances, worst case included (#46). AWS has no hard spending cap, so every source is bounded by the backend's code and by Terraform, for the function at its concurrency cap for a whole month.

- **Logs** go to `/aws/lambda/<name>`, kept for 7 days. Lambda filters application and system logs at WARN. The backend writes nothing below WARN anyway, and each instance writes at most N lines a minute of at most L bytes ([backend/README.md](../backend/README.md#logs-and-traces)).
- **Errors:** every 5xx answer and every recovered panic is one ERROR line with the operation, the error chain and the IDs. One metric filter counts ERROR lines. Two alarms, on that metric and on the function's `Errors`, email `alert_email` through an SNS topic when they enter ALARM.
- **Traces:** the backend traces each request, database query and Centrifugo API call with the OpenTelemetry Go SDK. Each instance starts at most one trace every T seconds. It hands the spans over localhost to the collector layer, which sends them to X-Ray. Lambda's own Active tracing stays off.
- **Tripwire:** a zero-spend AWS Budgets cost budget of $0.01 emails `alert_email` once anything in the account costs money, INTODAN/mpl's resources included. Budgets refreshes its data about three times a day, so a charge is reported within a day. It stops nothing; the limits do.
- **Safety net:** a second cost budget, `<name>-monthly`, of $5 a month, emails `budget_email` once actual costs pass 80 % of it, $4 (#226). Unset, `budget_email` is `alert_email`. It covers the whole account, not just bots, and stops nothing either.
- **The VM's logs,** Postgres's and Centrifugo's, are INTODAN/mpl's ([its `deploy/README.md`](https://github.com/INTODAN/mpl/blob/main/deploy/README.md)).

### Never turn these on

Each is billed outside the limits below, and nothing in `deploy/terraform/` turns them on:

- Lambda Active tracing (`tracing_config` stays `PassThrough`, which the Terraform tests check)
- Lambda Insights (its layer is a second layer; the tests check the function has only the collector)
- CloudWatch Transaction Search, an account-wide X-Ray setting
- Application Signals
- CloudWatch RUM and Synthetics
- Contributor Insights
- Detailed monitoring
- On the CloudFront distribution: standard logs, real-time logs, additional metrics, Origin Shield and Lambda@Edge. The Terraform tests check that it has no logs.

### Free allowances

Checked on 2026-10-29. Accounts created after 15 July 2025 get credits instead of the old 12-month free tier, but the "always free" allowances below apply to every account, on the free and the paid plan ([AWS Free Tier](https://docs.aws.amazon.com/awsaccountbilling/latest/aboutv2/free-tier.html)).

| Service | Free each month | Source |
|---|---|---|
| CloudWatch Logs | 5 GB of ingestion, archive storage and Logs Insights scans together. 1,800 minutes of Live Tail. | [CloudWatch pricing](https://aws.amazon.com/cloudwatch/pricing/) |
| CloudWatch metrics | 10 custom metrics (custom and detailed monitoring together). Lambda's own metrics, such as `Errors`, are free. | [CloudWatch pricing](https://aws.amazon.com/cloudwatch/pricing/) |
| CloudWatch alarms | 10 standard-resolution alarm metrics. | [CloudWatch pricing](https://aws.amazon.com/cloudwatch/pricing/) |
| X-Ray | 100,000 traces recorded, and 1,000,000 traces retrieved or scanned. | [CloudWatch pricing](https://aws.amazon.com/cloudwatch/pricing/), which [X-Ray pricing](https://aws.amazon.com/xray/pricing/) now points to |
| SNS | 1,000 email notifications, 1 million requests. | [SNS FAQ](https://aws.amazon.com/sns/faqs/) |
| CloudWatch API requests | 1,000,000 requests, such as `GetMetricStatistics`. `GetMetricData` isn't among them and is billed from the first metric, so nothing here calls it. The [monthly compute guard](#the-monthly-compute-guard) makes 4 `GetMetricStatistics` requests at most once per 10 minutes: at most 17,856 a month. | [CloudWatch pricing](https://aws.amazon.com/cloudwatch/pricing/) |
| AWS Budgets | Budgets and their notifications are free. Only action-enabled budgets past the first two, and emailed reports, cost money. | [Budgets pricing](https://aws.amazon.com/aws-cost-management/aws-budgets/pricing/) |
| CloudFront | 1 TB of data transfer out and 10,000,000 HTTP or HTTPS requests, on the always-free tier of the pay-as-you-go pricing. Transfer from AWS origins, such as the Function URL, to CloudFront is free. Checked on 2026-10-30. | [CloudFront pricing](https://aws.amazon.com/cloudfront/pricing/pay-as-you-go/), [CloudFront FAQ](https://aws.amazon.com/cloudfront/faqs/) |
| ACM | Public certificates are free. | [CloudFront pricing](https://aws.amazon.com/cloudfront/pricing/pay-as-you-go/) ("Free SSL certificates") |

These allowances are the account's: INTODAN/mpl's lobby, alarms and distribution, in the same account, share them.

### The budget

The worst case is a 31-day month, 44,640 minutes, with the function at its concurrency cap C the whole time. With the defaults, C = 5, N = 5 lines a minute, L = 1,024 bytes and T = 180 seconds:

| Source | How it's bounded | Worst case a month |
|---|---|---|
| The backend's warning and error lines: 5xx answers, panics, failed and slow queries, failed Centrifugo pushes, a failed start | Each instance writes at most N lines a minute, each cut to L bytes. Each line is counted at L + 128 bytes, for CloudWatch's 26 bytes per event and anything Lambda adds. | 5 × 44,640 × 5 × 1,152 B = 1.29 GB |
| The line that counts dropped lines | At most one a minute per instance, under 256 bytes | 5 × 44,640 × 384 B = 0.09 GB |
| **Ingestion that the backend checks** | **C × 44,640 × (N × (L + 128) + 384) ≤ 1.5 GB**, checked by Terraform and at startup | **1.37 GB** |
| The backend's INFO lines, such as one per request | Not written on Lambda, and Lambda's WARN filter would drop them | 0 |
| Text that isn't JSON: a Go crash's stack trace, the Lambda runtime library's own messages | Lambda counts a line without a JSON `level` as INFO, so the WARN filter drops it | 0 |
| Lambda's platform lines: `start` and `report` for every invocation, and the init lines | INFO or DEBUG when they succeed, so the WARN filter drops them | 0 |
| Lambda's platform lines for a failure: `initRuntimeDone`, `initReport`, `runtimeDone` and `report` with a status other than success, a failed extension, `logsDropped` | The backend never fails its start (it answers 503 instead), recovers every panic, and ends a request's work 2 seconds before the Lambda timeout. What's left takes a bug: a fatal runtime error, running out of memory, or a loop that ignores its deadline. Counted at 1 KB per failed invocation. | Not bounded by code; see below |
| The collector layer's own lines | It logs errors only (`service.telemetry.logs.level: error` and `OPENTELEMETRY_EXTENSION_LOG_LEVEL=error`), and only exports when a sampled trace ends. Its lines are zap JSON with a lower-case `level` and an epoch `ts` instead of an RFC 3339 `timestamp`, so Lambda probably counts them as INFO and drops them. In #45, a failed export logged nothing within a minute, even with the application log level at DEBUG. Counted as if they arrive anyway, at 2 lines of 1 KB per trace. | 74,400 × 2 KB = 0.15 GB |
| Storage | 7 days' retention keeps at most 7/31 of a month's ingestion | 0.35 GB |
| **Total of the bounded sources** | | **1.87 GB of 5 GB** |
| Logs Insights scans and failed invocations | The other 3.1 GB. A query over the whole log group scans at most its 7 days, about 0.35 GB. A crash loop of one failed invocation a second all month comes to 2.7 GB. The `Errors` alarm emails within the hour of the first. | 3.1 GB left |
| Custom metrics | The error-line metric filter, without dimensions. The SDK's and GORM plugin's metrics aren't exported, and the collector has no metrics pipeline. INTODAN/mpl's two in the same account make 3. | 1 of 10 |
| Alarms | The two error alarms. INTODAN/mpl's three in the same account make 5. | 2 of 10 |
| Alarm emails | Each error alarm looks at one hour and notifies only on entering ALARM, after at least an hour in OK, so at most once every 2 hours: 372 each. INTODAN/mpl's alarms share the account's allowance. | 744 of 1,000 |
| Budget emails | Sent by Budgets, not SNS | free |
| Traces recorded | Each instance starts at most one every T, and its first T after it starts: **C × 2,678,400 / T ≤ 80,000**, checked by Terraform and at startup | 74,400 of 100,000 |
| Traces retrieved or scanned | Opening traces in the console, by hand | far under 1,000,000 |
| CloudFront's standard and real-time logs | Off: the distribution has no logging configuration, which the Terraform tests check | 0 |
| CloudFront's requests and transfer out | Not bounded by code: anyone can call the public name. Only the static files of the Lambda's pages are cached (for five minutes), so most requests are also Lambda requests. | of 10,000,000 requests and 1 TB; see below |

Two assumptions sit under the per-instance limits. Each Lambda instance is one process with its own limits, and the concurrency cap bounds how many run at once. An instance that replaces another, such as after a crash, starts with a fresh minute of N lines but no trace until it has run for T. The 3.1 GB of headroom also covers that.

Lambda's requests and compute, and CloudFront's requests and transfer, are outside this budget. Players stay far under CloudFront's free 10 million requests a month: a lobby tab left open for a whole month sends a heartbeat every 15 seconds, about 180,000 requests, which reach the lobby's distribution in the same account. The developer accepted the cost risk of the public Function URL and the public name (#26, #75).

### The collector layer

The function runs the [AWS Distro for OpenTelemetry (ADOT)](https://aws-otel.github.io/docs/getting-started/lambda/lambda-go) collector layer, researched on 2026-10-29:

- **Why ADOT:** its build has the `awsxray` exporter, which sends spans to X-Ray's `PutTraceSegments` API, billed per trace recorded. The upstream [opentelemetry-lambda](https://github.com/open-telemetry/opentelemetry-lambda) collector layer (0.24.0 of 2026-10-22, `arn:aws:lambda:<region>:184161586896:layer:opentelemetry-collector-arm64-0_24_0:1`) has no `awsxray` exporter. It could only reach X-Ray through X-Ray's OTLP endpoint, which needs Transaction Search on. AWS's docs now call collector layers the legacy approach and recommend Application Signals layers, which have no Go version and need Application Signals.
- **ARN pattern:** `arn:aws:lambda:<region>:901920570463:layer:aws-otel-collector-arm64-ver-<collector version, dashed>:<layer version>`. Terraform uses `aws-otel-collector-arm64-ver-0-156-0:4`: ADOT Collector for Lambda v0.49.0, built on OpenTelemetry Collector v0.156.0 ([CHANGELOG](https://github.com/aws-observability/aws-otel-lambda/blob/main/CHANGELOG.md), 2026-08-25). Override it with `collector_layer_arn`.
- **Configuration:** Terraform passes it in the environment, `OPENTELEMETRY_COLLECTOR_CONFIG_URI=env:ISLAND_BATTLES_COLLECTOR_CONFIG`, so the zip holds only `bootstrap`: an OTLP/HTTP receiver on `localhost:4318`, the `awsxray` exporter in the function's region, and error-only logs.
  - **Checked live on 2026-10-30 (#45):** the ADOT build reads the `env:` configuration, the extension starts, and sampled traces reach X-Ray. If a later change makes the collector reject its configuration, its extension stops, and every invocation fails: Lambda doesn't start a function whose extension failed. Set `collector_layer_arn = ""` in `aws.tfvars` and apply: the function then runs without the layer and without tracing until the configuration is fixed.
- **A failing export doesn't show in CloudWatch.** The collector logs zap JSON: a lower-case `level` and an epoch `ts`, with no RFC 3339 `timestamp`, so Lambda probably counts its lines as INFO. Step 5 of [Checking it live](#checking-it-live-45) went further on 2026-10-30. With the exporter pointed at an invalid region, a sampled request produced no collector line within a minute, even with the application log level at DEBUG. The backend logged no `Spans could not be exported` warning either, presumably because the collector accepts the spans before it exports them. A failing export therefore shows only as traces missing from X-Ray, and the error metric never sees it.
- **Cold-start cost:** neither the ADOT docs nor AWS's publish a number. They say each extension must finish starting before Lambda invokes the function, and call the collector "an added overhead". An [upstream issue's logs](https://github.com/open-telemetry/opentelemetry-lambda/issues/2101) show about 410 ms from launch to ready. To measure it, set the system log level to INFO in the Lambda console for a few cold starts and compare the `report` lines' `initDurationMs` with and without the layer. The next `scripts/aws.sh deploy` sets the level back.
- **Export time:** the ADOT build has no `batch` or `decouple` processor, so the collector sends spans to X-Ray while the backend flushes them. A sampled request finishes later by that round trip, once every T per instance at most. The backend flushes only after requests that recorded spans.

### Reading the logs

Logs Insights queries scan, and the bytes they scan count against the same 5 GB as ingestion. Read the logs these ways instead:

- **The stream view:** CloudWatch → Log groups → `/aws/lambda/<name>` → the newest log stream. Each instance writes its own stream.
- **Live Tail,** for watching while you test: 1,800 minutes a month are free, counted while a session is open. Stop the session when you're done.
- **Logs Insights only when you need it,** with the time range cut to the minutes around the alarm, and on this log group alone.
- **A trace:** copy `xray_trace_id` from the error line and open it in CloudWatch → X-Ray traces → Traces. Only lines from sampled requests carry one.

An alarm email names the alarm. `<name>-error-lines` means the backend logged an error; `<name>-lambda-errors` means an invocation failed, a crash or a timeout. A line `Dropped log lines over this instance's limit` counts what an instance left out in a minute.

### Checking it live (#45)

These checks first ran on 2026-10-30, with `reserved_concurrency = null`, `account_concurrency_limit = 10`, `log_lines_per_minute = 2` and `trace_interval_seconds = 360`. The results are noted under each step. Steps 3 and 4 stopped containers on the VM, as `deploy`, and called the lobby's routes on the Function URL, where the lobby ran then. Since [the cutover](#the-cutover) the VM is INTODAN/mpl's and those routes answer 410: to repeat the steps, have INTODAN/mpl's owner stop the container, and call a game route that needs Postgres or Centrifugo instead.

1. **The collector starts.** Right after the first `scripts/aws.sh deploy`, call `/healthz` on the Function URL. A 204 means the collector extension accepted its configuration. If the call fails and the log group shows a failed `extension` or `initReport` line, the configuration is wrong: set `collector_layer_arn = ""`, deploy again, and fix it before turning tracing back on ([The collector layer](#the-collector-layer)).
   - 2026-10-30: 204 on the first cold start.
2. Follow the SNS confirmation link from the first apply.
3. **An email from a failing request:** stop Postgres for a minute: `docker compose stop db`. Call `POST /lobby/v1/sessions` on the Function URL, which then answers 5xx, and start Postgres again with `docker compose start db`. The `<name>-error-lines` alarm emails within about an hour.
   - 2026-10-30: the request answered 503 and wrote one ERROR line. The alarm entered ALARM 39 seconds later, and the email arrived.
4. **A trace:** close every browser tab with the lobby UI first, because their heartbeats use up each instance's one trace per T. Then stop Centrifugo, `docker compose stop centrifugo`. Create a guest session with `POST /lobby/v1/sessions`, wait T (3 minutes with the defaults) without other requests, and then create a lobby with `POST /lobby/v1/lobbies` (see `/docs`). The instance samples it, its push to Centrifugo fails, and its `Centrifugo push failed` error line carries `xray_trace_id`. The trace in X-Ray holds the request's span, its database spans and its Centrifugo API spans. If the line has no trace ID, another request used up the instance's trace, or the request reached an instance younger than T: wait T and create another lobby. Start Centrifugo again.
   - 2026-10-30: the first try had no trace ID, because open lobby tabs' heartbeats were sampled instead. The second try, with no tabs open, logged a trace ID. Its trace held the request, its database spans, and spans for the three failed Centrifugo API attempts.
5. **Whether the collector's errors arrive.** Make one export fail: in the Lambda console, set `ISLAND_BATTLES_COLLECTOR_CONFIG`'s `awsxray` exporter to `{"region":"xx-invalid-1"}` (the next `scripts/aws.sh deploy` restores it), wait T on a warm instance, and send a request. Then look in the log stream for the collector's error line. If it isn't there, X-Ray failures show only as missing traces. To see everything the collector writes, also set the application log level to DEBUG for the test; the next deploy sets it back.
   - 2026-10-30: no collector line arrived within a minute, at WARN or at DEBUG, and no trace reached X-Ray. X-Ray failures show only as missing traces ([The collector layer](#the-collector-layer)).
6. Check in the CloudWatch console that Transaction Search is off (Settings → X-Ray traces), and in the Billing console that the zero-spend budget exists. With the AWS CLI: `aws xray get-trace-segment-destination` prints `XRay` while Transaction Search is off, and `aws budgets describe-budgets --account-id <account>` lists the budget.
   - 2026-10-30: Transaction Search off, and the budget exists. Application Signals lists the function as `UNINSTRUMENTED`: it sends Application Signals no data.
7. **After a day of use,** check the Billing console: CloudWatch, X-Ray, SNS and Budgets show no charges. The zero-spend budget emails as soon as anything costs money, the Lightsail instance included, so its email alone doesn't mean these services cost anything.
   - Pending: the deployment went live on 2026-10-30.

## Troubleshooting

- **`.../deploy/local/secrets lacks admin_api_key, submit_token_secret`** from `scripts/lambda-env.sh`: the deploy bucket doesn't have this repository's secrets. Push `deploy/local/` from the machine that has them ([The deploy bucket](#the-deploy-bucket)). Only a new deployment passes `-e app_generate_secrets=true` ([The backend's settings](#the-backends-settings)).
- **`mpl/tenants/island-battles/tenant.env doesn't exist yet`** from `scripts/lambda-env.sh`: INTODAN/mpl's VM deploy hasn't published the tenant's settings. It needs Island Battles in its `vm.yml`'s `tenants` and a `vm` run ([its "Tenants"](https://github.com/INTODAN/mpl/blob/main/deploy/README.md#tenants)).
- **`tenant.env isn't the settings of the tenant island-battles`** from `scripts/lambda-env.sh`: the file lacks a key, names another tenant, or has a malformed value; the message says which keys are missing. Fix the tenant in INTODAN/mpl and run its `vm` target again.
- **`These files changed both here and in the bucket`:** see [The deploy bucket](#the-deploy-bucket) for keeping one copy.
- **`is Terraform's old local state`:** this machine still has the state from before the deploy bucket. Run `scripts/aws.sh migrate-state` ([Moving an existing deployment to the bucket](#moving-an-existing-deployment-to-the-bucket)).
- **`This plan changes more than the backend`:** `scripts/aws.sh deploy` found changes to Lightsail resources or the game's certificate or distribution, or a deletion, in the plan, and stopped before migrating. Run `scripts/aws.sh apply`, read the plan, and deploy again once it's applied. The cutover's plan, which takes the VM out of the state, is one: apply it with `plan` and `apply` ([The cutover](#the-cutover)). Right after `game_hostname` is first set, request the certificate first ([First deployment, step by step](#first-deployment-step-by-step), step 5).
- **`This plan destroys what INTODAN/mpl owns, so nothing is applied`:** the plan deletes or replaces a Lightsail resource or the account's GitHub OIDC provider, and nothing allows that. Take a Lightsail resource out of the state with a `removed` block (`lifecycle { destroy = false }`), as `deploy/terraform/vm.tf` does, and keep `github_repository` set, so the provider stays.
- **`lambda.env must set ISLAND_BATTLES_LOBBY_URL to https://<lobby_hostname>`** from Terraform: `lambda.env` is from before the cutover, or `lobby_hostname` doesn't match INTODAN/mpl's lobby. Run `scripts/lambda-env.sh` again, or fix `lobby_hostname` in `aws.tfvars`.
- **The apply waits at `aws_acm_certificate_validation.game: Still creating...`:** ACM hasn't seen the validation record. `dig CNAME <the record's name> +short` must print the value that `scripts/aws.sh output certificate_validation_record` shows. Terraform gives up after 75 minutes; run the apply again once the record resolves.
- **`https://island-battles.<domain>` doesn't resolve, or shows a certificate for another name:** `dig +short island-battles.<domain>` must lead to `scripts/aws.sh output distribution_domain`. A new CNAME may take the zone's negative TTL, 5 minutes for `elidan.org`, to reach a resolver that looked the name up before.
- **A page or an SDK gets 410 Gone from `/lobby/...` on the game's name:** it still calls the lobby there. Reload the page; bot authors upgrade the SDK, or pass `--lobby-url https://lobby.<domain>` ([The lobby on its own origin](#the-lobby-on-its-own-origin)).
- **The game loads, but live updates never arrive:** Centrifugo accepts browsers only from the origins in INTODAN/mpl's settings: the lobby's, and the game's `origins` and `websocket_origins` in its tenant entry. Open the game at `game_url`, not at the Function URL, and check the tenant's origins in INTODAN/mpl's `vm.yml`, then its `vm` target. A connection that opens but whose subscriptions fail points at Centrifugo's proxy, `centrifugo_proxy_url`, which must name INTODAN/mpl's lobby.
- **Authenticated requests answer 401 through CloudFront, but not on the Function URL:** the distribution doesn't forward `Authorization`. Check its origin request policy, `Managed-AllViewerExceptHostHeader`.
- **The migrations or the apply fail during `scripts/aws.sh deploy`:** fix the cause, for example the database being unreachable, and run `scripts/aws.sh deploy` again. Migrations that already ran are skipped. If the apply failed after the migrations, the old code keeps serving against the migrated schema until a deploy succeeds.
- **`lambda.env is missing`** from Terraform or `scripts/aws.sh`: push `deploy/local/` from the machine that has it, or run `scripts/lambda-env.sh`, which writes it. Only an apply that must run before it passes `-var allow_missing_lambda_settings=true`.
- **`account_concurrency_limit is N in aws.tfvars and the account's limit is M`** from `scripts/aws.sh`: `reserved_concurrency` is null and AWS has raised the account's limit. Reserve concurrency again or set `account_concurrency_limit` ([The concurrency cap](#the-concurrency-cap)).
- **`decreases account's UnreservedConcurrentExecution below its minimum value`:** the account's concurrency limit is too low. See [The concurrency cap](#the-concurrency-cap).
- **`... over the ... connections that INTODAN/mpl reserves for the game`** from Terraform: the concurrency cap times `database_max_open_connections` exceeds `tenant_database_connections`. Lower one, or have INTODAN/mpl reserve more first ([The concurrency cap](#the-concurrency-cap)).
- **`AccessDenied: Your account must be verified before you can add new CloudFront resources`:** the account can't create distributions yet. The error asks you to contact AWS Support: open a case in the Support Center with the error message and its request ID, then run `scripts/aws.sh apply` again. Until the distribution exists, `scripts/aws.sh deploy` refuses its plan, which creates it; publish the backend with `scripts/aws.sh apply` instead.
  - 2026-10-30, the live account: the first two full applies failed this way. The first had already created the certificate's validation and updated the function. A third, later the same day, created the distribution in 3 minutes.
- **The Function URL answers `Internal Server Error`:** read the function's log, the CloudWatch log group `/aws/lambda/<name>`, `/aws/lambda/island-battles` by default, as [Reading the logs](#reading-the-logs) describes. After an apply with `allow_missing_lambda_settings`, the function has no settings yet: run `scripts/lambda-env.sh` and `scripts/aws.sh deploy`. A `Forbidden` means the Function URL's public permissions are missing.
- **The Function URL answers `the backend failed to start` (503):** the log's `Backend failed to start` line says why, such as missing settings, an unreachable database or limits over the budget. The instance tries again every 10 seconds.
- **`... over the free-tier budget`** from Terraform: the concurrency cap and the log or trace limits together could leave the free tier. Lower one of them ([The budget](#the-budget)).
- **`Terraform sets ... itself`:** `lambda.env` or `extra_lambda_environment` sets a setting that Terraform sets: the pool size, a log or trace limit, a collector setting, the bot settings, `ISLAND_BATTLES_PUBLIC_URL` or `ISLAND_BATTLES_LOBBY_ORIGINS`. Use the Terraform variables instead.
- **No alarm emails:** follow the link in SNS's confirmation email, or resend it from the SNS console's subscription page.
- **Postgres from your machine:** `psql "$(sed -n 's/^ISLAND_BATTLES_DATABASE_URL=//p' deploy/local/lambda.env)"` connects as the game's role to its database. To only read, use the [admin MCP server's](#the-admin-mcp-server) role.
- **The admin MCP server answers `permission denied`:** the role has no grants yet in that database, for example a new one before its first migration. INTODAN/mpl's `vm` target grants them ([The admin MCP server](#the-admin-mcp-server)). A query that names `bearer_hash`, or `SELECT *` from `lobby_sessions`, is denied on purpose.

## Validating changes

Never apply Terraform to an account you don't own. To check changes to the Lambda settings' playbook, which only runs on this machine, run these from `deploy/ansible`:

```sh
ansible-lint
ansible-playbook --syntax-check lambda-env.yml
```

To check changes to the Terraform configuration, run these from the repository root. They need no AWS credentials: the tests mock both providers, so they never call AWS.

```sh
terraform -chdir=deploy/terraform init -backend=false
terraform -chdir=deploy/terraform fmt -check -recursive
terraform -chdir=deploy/terraform validate
terraform -chdir=deploy/terraform test
python3 -I -m unittest discover -s deploy -p 'test_*.py'   # the plan checks, the deploy bucket, the Lambda settings' playbook, the CloudFront functions, the web client page and the apply role, offline
```

`deploy/test_lambda_env.py` runs the playbook only when `ansible-playbook` is installed, and the CloudFront functions' tests only when `node` is.
