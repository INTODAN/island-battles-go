# Deployment

Island Battles runs in one AWS region:

- **Backend:** the Go backend runs on AWS Lambda behind its Function URL. Players open the game at `https://island-battles.<domain>`, which a CloudFront distribution serves from the Function URL with an ACM certificate (#75). `/` redirects to the lobby UI at `/lobby/ui/`.
- **VM:** a Lightsail instance runs Postgres and Centrifugo in Docker Compose. Nothing else runs on it.
- **Network:** there is no VPC or peering. The Lambda reaches Postgres and Centrifugo's HTTP API over the internet:
  - Postgres accepts only TLS (`hostssl`) logins, with scram and a generated password.
  - Centrifugo's API needs TLS and its API key.
- **DNS:** three records, added by hand:
  - `vm.<domain>` points at the VM's static IP. Centrifugo gets a Let's Encrypt certificate for it, and browsers connect to `wss://vm.<domain>/connection/websocket`.
  - `island-battles.<domain>` points at the CloudFront distribution.
  - ACM's validation record lets it issue and renew the game's certificate.

This document covers creating the AWS resources with Terraform, setting up the VM with Ansible, deploying the backend to Lambda, backups, redeploys and moving to a new VM. Any machine with the AWS credentials can deploy, and so can GitHub Actions: Terraform's state and the deploy's files live in a [deploy bucket](#the-deploy-bucket), and the VM deploy logs in with [temporary keys from Lightsail](#logging-in-to-the-vm). [First deployment, step by step](#first-deployment-step-by-step) puts it all in order. #26 describes the architecture.

The live deployment (#45, 2026-10-30) runs in `eu-central-1`, and its VM is `vm.elidan.org`. Players open it at `https://island-battles.elidan.org` (#75, 2026-10-30), which replaced a 2021 WebGL build of the game on Firebase Hosting. Its DNS records are managed in the Squarespace domains dashboard: `elidan.org` → DNS → Custom records.

## Files

| Path | What it is |
|---|---|
| `scripts/aws.sh` | Creates the AWS resources and deploys the backend to Lambda. |
| `scripts/vm.sh` | Deploys the VM, moves its database and runs the smoke test. |
| `scripts/deploy-web-client.sh` | Builds the [web client](#the-web-client) and uploads it to its bucket. |
| `scripts/deploy-files.sh` | Creates the [deploy bucket](#the-deploy-bucket), and syncs `deploy/local/` with it by hand. `deploy/deploy_files.py` does the work, and `deploy/test_deploy_files.py` tests it. |
| `.github/workflows/deploy.yml` | [Deploys from GitHub Actions](#deploying-from-github-actions), on demand. |
| `deploy/aws.example.tfvars` | The template for the AWS settings. |
| `deploy/vm.example.yml` | The template for the VM settings. |
| `deploy/terraform/` | The AWS resources: the Lightsail VM in `vm.tf`, the Lambda in `lambda.tf`, the game's certificate and CloudFront distribution in `cloudfront.tf`, the Lambda's log group, alarms, collector settings and budget in `observability.tf`, the database backups' bucket, key and alarm in `backups.tf`, the [web client's](#the-web-client) bucket and redirect function in `web_client.tf`, the [bot package](#bot-packages) bucket in `bot_packages.tf`, the [bot runners](#bot-runners) in `bot_runners.tf`, and GitHub Actions' deploy role in `github.tf`. `tests/` checks the configuration offline. |
| `deploy/github_apply_role.py` | Creates the role that the Deploy workflow's approved runs use ([Deploying from GitHub Actions](#deploying-from-github-actions)). `deploy/test_github_apply_role.py` tests it. |
| `deploy/check_deploy_plan.py` | Reads the plan for `scripts/aws.sh`: stops a backend deploy that would change more than the backend, lists what an apply would destroy, and tells an apply whether to run the migrations. `deploy/test_check_deploy_plan.py` tests it. |
| `deploy/check_concurrency.py` | Reads `aws.tfvars` for `scripts/aws.sh`: refuses to plan when `reserved_concurrency` is null and `account_concurrency_limit` is below the account's real limit ([The concurrency cap](#the-concurrency-cap)). `deploy/test_check_concurrency.py` tests it. |
| `deploy/ansible/` | The playbooks: `site.yml` deploys, `dump.yml` and `restore.yml` copy the database. The `base` role prepares Ubuntu, and the `app` role deploys the Compose project and the [daily backup](#backups), whose script `deploy/test_database_backup.py` tests. |
| `deploy/smoke.py` | The smoke test that `scripts/vm.sh smoke` runs. |
| `deploy/local/` | Gitignored. The settings, the generated secrets, the Lambda's settings and the smoke test's guest credential, which the scripts sync with the [deploy bucket](#the-deploy-bucket), and this machine's built Lambda, plans and dumps, which they don't. |

## Prerequisites

- **On your machine:**
  - Terraform 1.11 or newer, Go, which builds the backend and runs its migrations, and Python 3, for the backend deploy's plan check and the deploy bucket's sync.
  - The AWS CLI 2.32 or newer. The scripts use it for the [deploy bucket](#the-deploy-bucket) and the VM's [temporary SSH keys](#logging-in-to-the-vm).
  - AWS credentials, which Terraform and the AWS CLI read from the standard AWS credential chain, e.g. `AWS_PROFILE=<profile>`. They need to manage Lightsail, Lambda, IAM roles and OIDC providers, CloudWatch Logs and alarms, SNS, Budgets, ACM and CloudFront, and the deploy bucket in S3. Nothing in the repo stores them, and nothing else is needed: no SSH key, and no copy of `deploy/local/`.
    - `aws login --profile <profile>` signs in through the console and gives short-lived credentials, which Terraform's AWS provider reads directly. They last up to 12 hours; run it again when Terraform reports expired credentials.
  - Ansible: `uv tool install ansible-core`. `scripts/vm.sh` installs the collections it needs into `deploy/ansible/.collections/` (gitignored) on first use.
  - [uv](https://docs.astral.sh/uv/), for the smoke test.
- **Lightsail** (Terraform creates these, see [AWS resources](#aws-resources)):
  - An Ubuntu 24.04 LTS instance. The 1 GB plan is enough. Both images publish amd64 and arm64 builds.
  - A static IP attached to it.
  - Firewall rules for 22, 443 and 5432. Port 22 stays open to all, so that any machine or GitHub runner can deploy: logins need a key, and password logins are off. Port 80 stays closed, because Centrifugo answers Let's Encrypt's challenge on 443.
  - Ansible doesn't set up a host firewall: ports that Docker publishes bypass ufw, so the Lightsail firewall is the one that counts.
- **The Lambda's Function URL:** `scripts/aws.sh output function_url`. Centrifugo calls the backend's subscribe proxy through it.
- **The game's URL:** `scripts/aws.sh output game_url`. Centrifugo accepts browser connections only from its origin and the desktop app's (`tauri://localhost`, `http://tauri.localhost`; `app_desktop_origins`, #105).
- **DNS** (#30): see below.

### DNS

The deployment needs three DNS records. Squarespace has no DNS API, so you add them by hand, once. They don't change on a redeploy or when you move to a new VM: the static IP moves with the deployment, and the distribution and its certificate stay.

In the Squarespace domains dashboard, open the domain's DNS settings and add each one as a custom record. If the domain uses DNS Connect with another provider's nameservers, add them there instead. Squarespace's host field takes the name without the domain: for `_1a2b.island-battles.elidan.org.`, enter `_1a2b.island-battles`.

| Record | Type | Host | Value | When |
|---|---|---|---|---|
| The certificate's validation | `CNAME` | The name that `scripts/aws.sh output certificate_validation_record` prints, without the domain | The value it prints after `CNAME`, ending in `.acm-validations.aws.` | Once the certificate exists, before the apply that waits for it |
| The VM | `A` | `vm` | `scripts/aws.sh output static_ip` | Before the VM deploy |
| The game | `CNAME` | `island-battles`: `game_hostname` without the domain | `scripts/aws.sh output distribution_domain`, `<id>.cloudfront.net` | Once the distribution exists |

- **Keep the validation record.** ACM renews the certificate only while it can still see the record.
- **CAA records:** run `dig CAA <domain> +short`. If it prints records, they must allow `letsencrypt.org`, for `vm`, and `amazon.com`, for ACM, or the certificate requests fail. You can also add CAA records on `vm.<domain>` and `island-battles.<domain>` themselves. `elidan.org` had none on 2026-10-30.
- **Wait until `dig +short vm.<domain>` prints the static IP** before the VM deploy. A resolver that looked the name up before the record existed keeps the empty answer for the zone's negative TTL, 5 minutes for `elidan.org`. The playbook's check resolves the name on the VM, which hasn't looked it up before.

Let's Encrypt allows only a few failed validations per hostname per hour. The playbook therefore stops before touching the VM if `vm.<domain>` doesn't resolve to the static IP yet.

### Settings

Copy `deploy/vm.example.yml` to `deploy/local/vm.yml` and fill it in:

| Setting | Meaning |
|---|---|
| `deploy_domain` | The domain. The VM's hostname is `vm.<deploy_domain>`. |
| `lambda_function_url` | The backend Lambda's Function URL, e.g. `https://<id>.lambda-url.<region>.on.aws/`. `scripts/aws.sh output function_url` prints it. |
| `game_url` | Where players open the game, e.g. `https://island-battles.<domain>`. `scripts/aws.sh output game_url` prints it. |
| `app_letsencrypt_email` | Optional. The contact address for the Let's Encrypt account. |

Everything else derives from these three: the autocert host whitelist, `ISLAND_BATTLES_CENTRIFUGO_PUBLIC_URL` and the API URL from the domain, Centrifugo's proxy endpoints from the Function URL, and its allowed origin from the game's URL. Other variables are in `deploy/ansible/group_vars/all.yml` and the roles' `defaults/main.yml`, and any of them can be overridden with `-e name=value`.

## First deployment, step by step

The pieces depend on each other in a circle. Terraform creates the Lambda, whose Function URL the VM deploy needs for Centrifugo. The VM deploy writes `deploy/local/lambda.env`, which the Lambda needs. The first apply therefore creates the function without its settings, and the backend deploy publishes them once the VM is up. The game's certificate comes first of all, because the distribution needs it issued, and ACM issues it only once you've added its DNS record:

1. **Check the account's Lambda concurrency limit.** In the Service Quotas console, look up AWS Lambda's "Concurrent executions" for your region. If it's below 1000, read [The concurrency cap](#the-concurrency-cap) first. Then create the [deploy bucket](#the-deploy-bucket): `scripts/deploy-files.sh bootstrap --region <region>`.
2. **Write the settings.**
   - Copy `deploy/aws.example.tfvars` to `deploy/local/aws.tfvars` and set the region, your SSH public key, the instance, `game_hostname` and `alert_email` ([AWS settings](#aws-settings)).
   - Copy `deploy/vm.example.yml` to `deploy/local/vm.yml` and set `deploy_domain` ([Settings](#settings)). The URLs come in step 5.
3. **Request the game's certificate:**

   ```sh
   scripts/aws.sh apply -target=aws_acm_certificate.game
   ```

   The plan creates only the certificate, in `us-east-1`. Terraform warns that a targeted plan may be incomplete, which is expected here. Then `scripts/aws.sh output certificate_validation_record` prints its validation record: add it ([DNS](#dns)).

   On the live account, step 4 at first failed with `Your account must be verified before you can add new CloudFront resources`. See [Troubleshooting](#troubleshooting).
4. **Create the AWS resources:**

   ```sh
   scripts/aws.sh apply -var allow_missing_lambda_settings=true
   ```

   It builds the backend and shows the plan: the instance, its firewall, the static IP and its attachment, the key pair, the Lambda's role, the function and its Function URL, the certificate's validation and the CloudFront distribution, the function's log group, metric filter, alarms, alert topic and zero-spend budget ([Logs, alerts and traces](#logs-alerts-and-traces)), and the database backups' bucket, key and alarm ([Backups](#backups)). Type `yes`. It also writes the backups' key to `deploy/local/secrets/database_backups.json` for the VM deploy. There's no `lambda.env` yet, so it skips the migrations, and the function fails every request until step 8.

   - It waits until ACM has issued the certificate, usually within minutes of the record resolving, and then until CloudFront has deployed the distribution, which takes a few minutes more.
   - SNS then emails `alert_email` a confirmation link. Follow it, or the alarms send nothing.
   - The `<name>-database-backups` alarm emails within the hour: there's no backup yet. It stops once the first one is made, the night after step 8.
5. **Feed the outputs to the VM deploy.** `scripts/aws.sh output` prints the static IP, the Function URL and the game's URL. Put `function_url` into `deploy/local/vm.yml` as `lambda_function_url`, and `game_url` as `game_url`.
6. **Add the DNS records** `vm.<domain>` for the static IP and `island-battles.<domain>` for the distribution, and wait until `vm.<domain>` resolves ([DNS](#dns)).
7. **Deploy the VM.** Give the new instance a minute to boot first.

   ```sh
   scripts/vm.sh deploy "$(scripts/aws.sh output static_ip)" -e app_generate_secrets=true
   ```

   It generates the [secrets](#secrets) and writes `deploy/local/lambda.env` ([First VM deploy](#first-vm-deploy)).
8. **Deploy the backend:** `scripts/aws.sh deploy`. Its plan only updates the function's settings. The migrations create the schema, then the function gets its settings ([Deploying the backend](#deploying-the-backend)).
9. **Run the smoke test:** `scripts/vm.sh smoke` ([Smoke test](#smoke-test)). Players can now open `https://island-battles.<domain>`.
10. **Nothing to back up by hand:** every script pushed the settings, the secrets and `lambda.env` to the deploy bucket, and Terraform keeps its state there. Any machine with the AWS credentials can deploy from here on.

### Adding the game's name to a running deployment

A deployment from before #75 serves players at the Function URL. To move them to `https://island-battles.<domain>`:

1. Set `game_hostname` in `deploy/local/aws.tfvars`, request the certificate with `scripts/aws.sh apply -target=aws_acm_certificate.game`, and add its validation record ([DNS](#dns)).
2. Run `scripts/aws.sh apply`. Its plan creates the certificate's validation and the distribution, and waits for both. Built from a checkout with #75, it also publishes the backend, whose `/` redirects to the lobby UI, as [any apply that changes the function](#the-aws-resources) does.
3. Add the `island-battles` CNAME for `scripts/aws.sh output distribution_domain`, and wait until `https://island-battles.<domain>/healthz` answers.
4. Set `game_url` in `deploy/local/vm.yml` and run `scripts/vm.sh deploy <static IP>`. It recreates the Centrifugo container with the new allowed origin, which drops every WebSocket connection. From here, live updates work only on pages opened at the game's URL, not at the Function URL.
5. Run `scripts/vm.sh smoke`.

## AWS resources

`deploy/terraform/` creates everything in AWS (#32). The DNS records are the only manual step.

| Resource | Details |
|---|---|
| Lightsail instance | Ubuntu 24.04 LTS on the 1 GB plan (`micro_3_0`), IPv4 only. Automatic snapshots are off: the database is [backed up to S3](#backups) instead. |
| Static IP | Attached to the instance named by `static_ip_instance`. Terraform refuses to destroy it, because `vm.<domain>` points at it. |
| Firewall | 22 from `ssh_allowed_cidrs`, and 443 and 5432 from anywhere. Nothing else, not even 80. Changing `ssh_allowed_cidrs` replaces the rules: their `port_info` forces replacement. On 2026-10-30 that closed every port for about 7 seconds. |
| Key pair | Imported from `ssh_public_key`. Without it, Lightsail uses the region's default key, which you download from the Lightsail console. |
| Lambda | The `provided.al2023` runtime on arm64, 512 MB, a 30-second timeout, a reserved concurrency of 5, JSON logs filtered at WARN, Active tracing off, and the OpenTelemetry collector layer. |
| IAM role | Basic execution and X-Ray writes: the function writes its logs, and its collector layer sends traces to X-Ray. In S3 it may put, get and delete objects in the bot package bucket, and nothing else: it presigns the uploads, reads each zip's directory and manifest, and deletes the zips it replaces or refuses. It invokes the two bot runner functions, and no other function, and reads metrics with `cloudwatch:GetMetricData`, for the runners' [monthly compute guard](#the-monthly-compute-guard). It calls nothing else in AWS. |
| Function URL | Auth type `NONE` and the `RESPONSE_STREAM` invoke mode, which the backend's `lambdaurl` host needs. The provider adds the public invoke permissions. |
| Certificate | An ACM certificate for `game_hostname` in `us-east-1`, where CloudFront takes certificates from, validated by DNS. ACM renews it on its own while its validation record stays. |
| CloudFront distribution | Serves `game_hostname` with the certificate, over HTTPS only: plain HTTP is redirected. Its default origin is the Function URL, over HTTPS. Every method reaches the Lambda uncached (`Managed-CachingDisabled`), except GET, HEAD and OPTIONS under `/lobby/ui/*`, `/game/demo/*`, `/game/demo-viewer/*` and `/map/builder/*` (the lobby page, the game's modules, the demo viewer and the map builder, #162), which follow the Lambda's `Cache-Control` (the distribution's own cache policy `<name>-origin-cache-control`: scripts and styles `public, max-age=300`, HTML and the catalog `no-store`, so a deploy reaches players within five minutes; the cache key is the path alone, because the managed `UseOriginCacheControlHeaders` policy would send the viewer's `Host` to the Function URL, which refuses it). Both kinds pass every viewer header, cookie and query string but `Host` on (`Managed-AllViewerExceptHostHeader`), so the Function URL sees its own `Host` and the backend gets `Authorization`. Streamed answers stream through. No origin access control: for a Function URL it would need clients to send a SHA-256 of every POST body, and the Function URL is public anyway. A second origin, the web client's bucket, serves `/play/*` and `/play` ahead of the default behaviour: cached and compressed (`Managed-CachingOptimized`), GET, HEAD and OPTIONS only, with a CloudFront Function that serves `/play/` as `play/index.html` and redirects `/play` to `/play/`. Price class 100, no logs and no WAF. |
| Web client bucket | `<name>-web-client-<account>`, private, unversioned, encrypted, TLS only, with all public access blocked. Holds the built client under `play/`. Only this distribution may read it, through an origin access control. See [The web client](#the-web-client). |
| Bot package bucket | `<name>-bot-packages`, private, unversioned, encrypted (SSE-S3), TLS only, with all public access blocked. Holds the bot packages that members upload to lobbies, under `lobbies/<lobby_id>/<package_id>.zip`, each deleted 8 days after its upload. Browsers upload to it with POST from `https://<game_hostname>`. See [Bot packages](#bot-packages). |
| Bot runners | `<name>-bot-runner-python` (`python3.14`) and `<name>-bot-runner-native` (`provided.al2023`), arm64, 1024 MB, a 20-second timeout, 2048 MB of `/tmp`, a reserved concurrency of 10 each, asynchronous invocations never retried and dropped after 60 seconds, and only `GAME_SUBMIT_HOST` as a setting. Their role, `<name>-bot-runner`, may only write their logs, kept for 7 days in `/aws/lambda/<name>-bot-runner-*`. The backend's role may invoke them, and no other function. See [Bot runners](#bot-runners). |
| Log group | `/aws/lambda/<name>`, kept for 7 days. |
| Metric filter and alarms | One metric that counts error lines, and two alarms, on it and on the function's `Errors`, that email `alert_email` through an SNS topic. |
| Budgets | A zero-spend cost budget that emails `alert_email`, and a $5 monthly cost budget that emails `budget_email` (or `alert_email`) once actual costs pass 80 %. |
| Backups bucket | `<name>-backups-<account>`, private, versioned, TLS only. Each dump under `postgres/` expires after `database_backup_retention_days`, 7 by default, and an overwritten one is kept as long again. The GitHub deploy role can't read it. See [Backups](#backups). |
| Backup key and alarm | An IAM user, `<name>-database-backups`, whose access key may only upload to `postgres/` and publish the `IslandBattles/VM` `DatabaseBackups` metric. An alarm on the metric emails `alert_email` when no backup succeeded for 26 hours. |

There's no VPC, peering or EventBridge. [Logs, alerts and traces](#logs-alerts-and-traces) explains the last three rows, and why they stay inside AWS's free tier, and [Free allowances](#free-allowances) covers CloudFront's.

### AWS settings

Copy `deploy/aws.example.tfvars` to `deploy/local/aws.tfvars` and fill in `region`, `ssh_public_key`, `vm_instances`, `static_ip_instance`, `game_hostname` and `alert_email`. `alert_email` stays in that gitignored file: never commit it. `deploy/terraform/variables.tf` describes every other variable, all of which have defaults. The instances have no default, so a lost `aws.tfvars` can't plan to replace the instance that runs with a default one. Keep it in step with the instances, for example after a move. If Lightsail rejects your key, use an RSA key: `ssh-keygen -t rsa -b 4096`.

### State

Terraform's state is `terraform/terraform.tfstate` in the [deploy bucket](#the-deploy-bucket), through Terraform's S3 backend. `scripts/aws.sh` finds the bucket and its region from the AWS credentials and passes them to `terraform init`. `.gitignore` covers `.terraform/` and `*.tfstate*`, in case a copy ends up in the checkout.

- **The state holds the Lambda's secrets:** its environment, with the database password, Centrifugo's API key, the connection token secret and the proxy secret. Never commit a copy.
- **One apply at a time.** Terraform locks the state with `terraform/terraform.tfstate.tflock` in the bucket while it plans and applies. A plan on another machine meanwhile fails with `Error acquiring the state lock`. If a run was killed and left the lock behind, `terraform -chdir=deploy/terraform force-unlock <lock ID>` removes it, once you're sure nothing else is applying.
- **Every earlier state is kept:** the bucket is versioned. To go back to one, find it in the S3 console's versions of the object.
- **If the state is lost,** the resources keep running, but Terraform no longer knows them. Bring each back with `terraform import` before applying again, or Terraform tries to create them a second time.

### The deploy bucket

`island-battles-deploy-<account ID>` is a private S3 bucket in the deployment's own account, which `scripts/deploy-files.sh bootstrap --region <region>` creates once. Set `ISLAND_BATTLES_DEPLOY_BUCKET` to use another name. The bootstrap blocks public access, turns on versioning and SSE-S3 encryption, refuses plain HTTP, and expires copies that were replaced more than 90 days ago, keeping the last 10 of each file. Run it again to bring an existing bucket's settings up to date. The bucket isn't part of the Terraform configuration, which keeps its state in it.

It holds:

- `terraform/`: Terraform's state and its lock.
- `files/`: a copy of `deploy/local/`: `aws.tfvars`, `vm.yml`, `secrets/`, `lambda.env` and `smoke-session.json`, and any other file there, except `terraform/` (plans and an old local state), `lambda/` (the build) and `dumps/`.

`scripts/aws.sh apply` and `deploy`, and every `scripts/vm.sh` command, **pull** the files first and **push** them back when they end, even after a failure, so new secrets and a new `lambda.env` never stay on one machine. A new machine or worktree needs nothing but the AWS credentials. To change a setting, edit it in `deploy/local/` and run a script, or `scripts/deploy-files.sh push`.

Each machine remembers what it last pulled or pushed, in `deploy/local/.sync-base.json`, so a sync can tell which side changed a file:

- **Changed only in the bucket**, or missing here: a pull takes the bucket's copy.
- **Changed only here:** a push sends it. The upload only succeeds while the bucket still has the copy it compared against, so two machines can't overwrite each other.
- **Changed on both sides:** a conflict, and the script stops before deploying. `scripts/deploy-files.sh status` lists the differences. Keep one: `scripts/deploy-files.sh pull --force` takes the bucket's copies, `push --force` sends this machine's.
- **Deleted:** nothing is ever deleted from the bucket, and a pull brings back a file deleted here. Delete a file in the S3 console to remove it everywhere; versioning keeps its earlier copies.

The VM deploy refuses to run while the bucket has the deployment's Terraform state but no secrets: with `-e app_generate_secrets=true`, it would otherwise replace every password and key of the deployment that runs.

#### Moving an existing deployment to the bucket

A deployment from before the deploy bucket keeps its state and secrets in one machine's `deploy/local/`. On that machine, with a checkout of this change:

1. `scripts/deploy-files.sh bootstrap --region <region>`, with the deployment's region.
2. `scripts/deploy-files.sh push`, which sends `deploy/local/`'s settings and secrets.
3. `scripts/aws.sh migrate-state`, which moves `deploy/local/terraform/terraform.tfstate` into the bucket and renames the local file to `terraform.tfstate.migrated`. Until then, `scripts/aws.sh` refuses to plan on that machine.
4. Remove `ssh_allowed_cidrs` from `deploy/local/aws.tfvars`, so the firewall opens 22 to all, and set `github_repository` for [GitHub Actions](#deploying-from-github-actions). Run `scripts/aws.sh apply`: changing the rules closes every port for a few seconds ([AWS resources](#aws-resources)).

Every other machine and worktree then pulls from the bucket on its own. Keep the old `deploy/local/` until another machine has deployed.

### Logging in to the VM

The VM deploy logs in as `ubuntu` with a temporary SSH key from Lightsail, as the console's browser-based SSH does. For each IP it deploys to, `scripts/vm.sh` finds the instance with that public IP in the region from `aws.tfvars`, asks Lightsail for a key and its certificate with `aws lightsail get-instance-access-details`, and hands them to Ansible. The instance trusts certificates from Lightsail's own CA, so no key of yours needs to be on it, and the key is deleted when the script ends.

This needs only the AWS credentials, with `lightsail:GetInstanceAccessDetails`, and port 22 open to the machine that deploys, which is why `ssh_allowed_cidrs` stays open to all. `ssh_public_key` still installs your own key for `ubuntu`, and the VM deploy lets it log in as `deploy` too, for logging in by hand.

### The concurrency cap

Every Lambda instance has its own database pool, so the reserved concurrency caps the backend's Postgres connections ([backend/README.md](../backend/README.md#the-connection-pool)). Terraform checks that **`reserved_concurrency` × `database_max_open_connections` ≤ `postgres_max_connections` − `postgres_spare_connections`**. It also sets `ISLAND_BATTLES_DATABASE_MAX_OPEN_CONNECTIONS` itself, so the two can't drift apart.

The same cap is C in the [free-tier budget](#the-budget): every instance logs and traces up to its own limits. Terraform passes the cap to the backend as `ISLAND_BATTLES_CONCURRENCY` and checks the budget too, so a bigger cap needs lower log and trace limits.

- The defaults use at most 5 × 2 = 10 of Postgres's 100 connections. They leave at least 10 free: 3 that Postgres reserves for superusers, 2 for the migrate command, and the rest for administration. A bigger cap still fits under `max_connections`, but every connection costs memory on the 1 GB VM.
- **Accounts with a low concurrency limit:** Lambda always keeps part of the account's concurrency unreserved, 100 on a normal account. On an account limited to 10, that's all of it, and any reserved concurrency fails with `decreases account's UnreservedConcurrentExecution below its minimum value`. Either request an increase of "Concurrent executions" in Service Quotas, which is free, or set `reserved_concurrency = null`.
- **Without a reservation,** the account limit is the cap, so Terraform checks **`account_concurrency_limit` × `database_max_open_connections`** instead, and the budget for that many instances. Set `account_concurrency_limit` in `aws.tfvars` to the limit Service Quotas shows. It defaults to 10, which gives 10 × 2 = 20 connections.
  - The default log and trace limits fit 5 instances, not 10. For 10, also set `log_lines_per_minute = 2` and `trace_interval_seconds = 360`: 1.2 GB of logs and 74,400 traces at most.
  - **`scripts/aws.sh` rechecks the limit for you.** While `reserved_concurrency` is null, `apply`, `deploy` and `plan` read the account's limit with `aws lambda get-account-settings` (in the region from `aws.tfvars`) before they plan, and refuse when `account_concurrency_limit` is below it, so a limit that AWS raised can't leave the checks on a stale cap. The message says to reserve concurrency again or to set `account_concurrency_limit` to the new limit. A reservation, or a limit at or above the account's, passes. `deploy/check_concurrency.py` reads `aws.tfvars` (`null`, a number, a missing variable and comments) and `deploy/test_check_concurrency.py` tests it. The call needs only `lambda:GetAccountSettings`, which the [GitHub deploy role](#deploying-from-github-actions) has through `ReadOnlyAccess`, so the Deploy workflow runs the same guard.
  - **A limit of 1000 lets 2,000 connections at Postgres:** AWS raises a new account's limit on its own after some use, which is what the guard catches.
  - Once it's raised, set `account_concurrency_limit` to the new value. The checks then fail, which tells you to reserve concurrency: set `reserved_concurrency` again, for example back to 5.
- **The bot runners** reserve 10 each ([Bot runners](#bot-runners)), whatever `reserved_concurrency` is. They don't connect to Postgres, so they count only against the account's limit: it must leave 100 unreserved after the backend's reservation and their 20.
- **Lingering connections:** a frozen or killed Lambda environment never closes its connections. Postgres's TCP keepalives on the VM close them about two minutes after the environment stops answering, so they don't pile up against `max_connections`.

## First VM deploy

```sh
scripts/vm.sh deploy <static IP> -e app_generate_secrets=true
```

`app_generate_secrets=true` lets this first deploy create the [secrets](#secrets). Later deploys don't need it.

The deploy logs in as `ubuntu`, which Lightsail's Ubuntu images use, and:

1. **Checks** the settings, that the secrets exist, the Ubuntu version and the DNS record. It also refuses a VM that a move left behind (see [Moving to a new VM](#moving-to-a-new-vm)).
2. **Base role:** installs Docker and the Compose plugin from Ubuntu's archive, enables unattended security upgrades, keeps containers running while the Docker daemon restarts, adds a 1 GB swap file, and creates the `deploy` user that owns the Compose project. Your SSH keys log in as `deploy` too.
3. **App role:** writes the Compose project to `/opt/island-battles` and starts it with `docker compose up --wait`:
   - `db` is `postgres:18-alpine` with a named volume. It publishes 5432 with TLS, using a self-signed certificate, and its `pg_hba.conf` allows only `hostssl` scram logins to the backend's database, by the backend's role and by the [admin MCP server's](#the-admin-mcp-server) read-only role. The `postgres` superuser only connects through the container's local socket. TCP keepalives close a connection about two minutes after its client stops answering, such as a frozen Lambda environment ([The concurrency cap](#the-concurrency-cap)).
   - `centrifugo` is `centrifugo/centrifugo:v6.9.6`. Host port 443 maps to its public port, which serves TLS with an autocert certificate for `vm.<domain>`, the client endpoints and the HTTP API (`http_api.external`). Its health endpoint listens only inside the container, for the healthcheck. The admin UI is off. The `lobby`, `player` and `watch` namespaces keep history and force recovery, so reconnecting clients receive what they missed. `watch` carries a lobby's game to the people who watch it with the join code. The proxy timeout is 10 seconds, to allow for a Lambda cold start on top of the backend's push retry. The certificate cache is a named volume.
   - Both services rotate their Docker logs at 10 MB, keeping five files.
4. **Creates** the backend's role and its database. The role owns the database but isn't a superuser.
   - Owning the database gives the role `CREATE` on it. The migration runner (#23) needs that: on a database without migration history, it creates a scratch schema in a transaction to check migration 1, then rolls it back.
   - The deploy creates the database with the role as its owner, and a restore recreates it the same way.
   - The VM doesn't create the schema. The migrate step in the Lambda deploy does (#29, #38), connecting as this role.
   - It also creates `island_battles_admin`, the read-only role of the [admin MCP server](#the-admin-mcp-server), and grants it read access. Every deploy and restore grants it again.
5. **Checks** that the role logs in over TLS and is refused without it, and that the admin role logs in over TLS and can't read `lobby_sessions.bearer_hash`. It also checks that `https://vm.<domain>/api/info` answers with a valid certificate when called with the API key, and refuses calls without it. That first TLS connection also makes Centrifugo request its certificate.
6. **Writes** `deploy/local/lambda.env`: the settings the backend Lambda needs to reach the VM.

Then [deploy the backend](#deploying-the-backend) and run the [smoke test](#smoke-test).

### Secrets

The first deploy generates these into `deploy/local/secrets/`. Every later deploy and every new VM reuses them:

- The Postgres passwords: the superuser's, the backend's role's, and the admin MCP server's read-only role's, `postgres_admin_password`.
- Centrifugo's API key, the connection token secret and the lobby proxy secret. Reusing them keeps issued tokens valid when you move to a new VM.
- The backend's admin API key, `admin_api_key`. Only the Lambda reads it, from `lambda.env`. Send it in the `X-Admin-API-Key` header to create default maps with `POST /map/create` (see [map files](../backend/README.md#map-files)). Without it, the Lambda refuses every map creation.
- The secret of the bots' one-turn submit tokens, `submit_token_secret` (#215). Only the Lambda reads it, from `lambda.env`. A new one only invalidates the tokens of turns being played at that moment.
- The lobby's player token key, `lobby_player_token_key` (#218): the Ed25519 seed that signs the short-lived player tokens games verify. Only the Lambda reads it, from `lambda.env`, as `LOBBY_PLAYER_TOKEN_KEYS`. To replace it, see [Rotating the player token key](#rotating-the-player-token-key).
- The shared secret of the lobby and Island Battles, `lobby_game_secret_island_battles` (#224): the lobby signs its calls of the game's hooks with it, and the game its calls of the lobby's game API. Only the Lambda reads it, from `lambda.env`, by two names: `LOBBY_GAME_SECRET_ISLAND_BATTLES` for the lobby and `ISLAND_BATTLES_LOBBY_HOOK_SECRET` for the game. A new one takes effect with the next backend deploy; nothing issued with it outlives a request.
- Postgres's TLS key and its self-signed certificate.

The scripts keep them in the [deploy bucket](#the-deploy-bucket), so every machine and worktree gets them with its first pull.

A deploy stops if any of these files is missing, because new secrets would replace the live VM's password and keys. Pass `-e app_generate_secrets=true` only for a first deploy, or if the secrets are lost for good. In that case the deploy resets the backend's password and Centrifugo's keys, and you redeploy the backend with the new `lambda.env`: `scripts/aws.sh deploy`. Players then need new connection tokens, which clients fetch on their own.

`admin_api_key` (#37), `postgres_admin_password` (#79), `submit_token_secret` (#215), `lobby_player_token_key` (#218) and `lobby_game_secret_island_battles` (#224) were added after the first secrets, so an existing `deploy/local/secrets/` without them fails this check. Run one deploy with `-e app_generate_secrets=true`: it generates only the missing files and reads the others unchanged, so no password or key on the VM changes. A new `admin_api_key`, `submit_token_secret`, `lobby_player_token_key` or `lobby_game_secret_island_battles` also needs the Lambda redeployed with the new `lambda.env`; the backend doesn't start without `ISLAND_BATTLES_SUBMIT_TOKEN_SECRET`, `LOBBY_PLAYER_TOKEN_KEYS`, `LOBBY_GAME_SECRET_ISLAND_BATTLES` or `ISLAND_BATTLES_LOBBY_HOOK_SECRET`.

### Rotating the player token key

The lobby signs player tokens with the first key of `LOBBY_PLAYER_TOKEN_KEYS`, a comma-separated list of `kid:seed` pairs, and publishes every key at `GET /lobby/v1/jwks.json`, which games keep for up to 5 minutes and fetch again when a token names a key they don't have. A token lasts at most 15 minutes. So a new key goes first while the old one stays published, and the old one goes 15 minutes later:

1. Note the current `LOBBY_PLAYER_TOKEN_KEYS` line of `deploy/local/lambda.env`, the old `kid:seed`.
2. Write a new key over `deploy/local/secrets/lobby_player_token_key` (a pull would bring a deleted file back): `python3 -c 'import secrets, string; print("".join(secrets.choice(string.ascii_letters + string.digits) for _ in range(43)), end="")' > deploy/local/secrets/lobby_player_token_key`. Then deploy the VM with `-e '{"lobby_player_token_previous_keys": ["<old kid:seed>"]}'`, which pushes the new key to the deploy bucket, and the backend with `scripts/aws.sh deploy`. The new key, whose ID comes from the key itself, signs; tokens of the old one still verify.
3. 15 minutes later, deploy the VM again without the extra variable, and the backend. The old key is gone.

A key that leaked is removed at once instead, with step 2 alone and no previous key: every token it signed stops working, and clients get a new one from the lobby when the game answers 401.

Nothing secret is committed. Never copy anything from `deploy/local/` into the repo.

### The Lambda's settings

`deploy/local/lambda.env` holds the twelve settings the backend reads from the VM deploy, including the admin API key (see `backend/dependencies/config.go`). [`scripts/aws.sh deploy`](#deploying-the-backend) passes every line of it to the Lambda:

| Variable | Value |
|---|---|
| `ISLAND_BATTLES_DATABASE_URL` | `postgres://island_battles:<password>@vm.<domain>:5432/island_battles?sslmode=require` |
| `ISLAND_BATTLES_CENTRIFUGO_PUBLIC_URL` | `wss://vm.<domain>/connection/websocket` |
| `ISLAND_BATTLES_CENTRIFUGO_API_URL` | `https://vm.<domain>/api` |
| `ISLAND_BATTLES_CENTRIFUGO_API_KEY` | Centrifugo's API key |
| `ISLAND_BATTLES_CONNECTION_TOKEN_HMAC_SECRET` | The connection token secret |
| `ISLAND_BATTLES_LOBBY_PROXY_SECRET` | The secret Centrifugo sends to the subscribe proxy |
| `ISLAND_BATTLES_ADMIN_API_KEY` | The admin API key that `POST /map/create` requires |
| `ISLAND_BATTLES_SUBMIT_TOKEN_SECRET` | The secret that signs the bots' one-turn submit tokens |
| `ISLAND_BATTLES_PUBLIC_URL` | `game_url` from `vm.yml`, where turn deliveries tell bots to submit their commands, and the `iss` of the lobby's player tokens |
| `LOBBY_PLAYER_TOKEN_KEYS` | `<kid>:<seed>` of `lobby_player_token_key`, the key that signs player tokens, then any previous keys during a [rotation](#rotating-the-player-token-key) |
| `LOBBY_GAME_SECRET_ISLAND_BATTLES` | `lobby_game_secret_island_battles`: the lobby signs its calls of Island Battles' hooks with it, and checks the game's calls of its game API |
| `ISLAND_BATTLES_LOBBY_HOOK_SECRET` | The same secret, by the game's name: the game checks the lobby's hook calls and signs its game API calls with it |

The URLs use the hostname, not an IP, so they stay the same when you move to a new VM.

`sslmode=require` encrypts the connection but doesn't check the server's certificate. To check it, ship `deploy/local/secrets/postgres-tls/server.crt` with the Lambda and use `sslmode=verify-full&sslrootcert=<path>`. The certificate is issued for `vm.<domain>` and is reused on new VMs.

## Deploying the backend

One command publishes a new backend version (#38):

```sh
scripts/aws.sh deploy
```

1. **Builds** `deploy/local/lambda/bootstrap` with the build command from [backend/README.md](../backend/README.md#hosts-and-deploys). Terraform zips it with `archive_file`.
2. **Plans** the Terraform configuration with `deploy/local/aws.tfvars` and `deploy/local/lambda.env`.
3. **Checks the plan** with `deploy/check_deploy_plan.py`, which allows only creating or updating resources outside Lightsail, ACM and CloudFront. A plan that changes the VM, its firewall or its static IP, or the game's certificate or distribution, or that deletes or replaces anything, such as the Function URL, stops the deploy before the migrations. Review those changes with `scripts/aws.sh apply` instead. A deploy that created the certificate would wait for a DNS record nobody has added yet.
4. **Migrates the database** from your machine: `go run ./backend/cmd/migrate`, with `ISLAND_BATTLES_DATABASE_URL` from `lambda.env`, straight to Postgres over TLS. The old code serves requests against the new schema until the next step finishes.
5. **Applies the checked plan:** the new code and settings. Then it prints the Function URL.

It doesn't ask for confirmation, never connects to the VM and never changes a Lightsail resource or the game's name. The plan file holds the settings, so the script deletes it afterwards.

**Settings:** every `KEY=value` line of `lambda.env` becomes one of the function's environment variables, so a new setting that the VM deploy writes needs no Terraform change. Values are taken literally, without quotes. Plans show the environment only as `(sensitive value)`.

- Run `scripts/aws.sh deploy` again whenever the VM deploy rewrites `lambda.env`, for example after new secrets.
- `ISLAND_BATTLES_DATABASE_MAX_OPEN_CONNECTIONS` comes from the `database_max_open_connections` variable instead, and the log and trace limits from `log_lines_per_minute`, `log_line_bytes`, `trace_interval_seconds` and the concurrency cap. Terraform refuses them in `lambda.env` and `extra_lambda_environment` ([The concurrency cap](#the-concurrency-cap), [The budget](#the-budget)). It also sets the collector's settings and `OTEL_EXPORTER_OTLP_ENDPOINT`, `ISLAND_BATTLES_BOT_PACKAGES_BUCKET`, the [bot package bucket](#bot-packages) it creates, and `ISLAND_BATTLES_BOT_RUNNER_PYTHON` and `ISLAND_BATTLES_BOT_RUNNER_NATIVE`, the [bot runners](#bot-runners) it creates.
- Non-secret settings of your own, such as the pool's idle time, go in `extra_lambda_environment` in `aws.tfvars`.
- The Lambda needs no game retention setting: games are deleted 7 days after their last turn by default. To change the period, set `ISLAND_BATTLES_GAME_RETENTION_PERIOD` there as a Go duration, such as `336h` for 14 days ([Game retention](../backend/README.md#game-retention)).

**Why Terraform publishes the code,** rather than the AWS CLI with Terraform only creating the function:

- **One owner.** If the CLI published the code and settings, Terraform would have to ignore both. The pool size that Terraform checks against the reserved concurrency would then be set by another tool, and nothing would check the two together.
- **One tool.** A deploy needs Terraform, Go and Python, which the rest of the deployment already uses. The AWS CLI only syncs the deploy bucket.
- **Checked offline.** `terraform test` covers the settings pass-through and the concurrency check with mocked providers.
- **The cost:** every deploy plans the whole configuration. That takes a few seconds longer and could pick up unrelated changes, which the plan check stops.
- Either way the state holds the secrets, because Terraform reads the function's environment into its state even when it ignores changes to it.

## Redeploy

### The VM

Run the same command again. It's idempotent: when nothing changed, nothing restarts. A changed configuration file recreates only its container. Redeploying the Lambda doesn't touch the VM.

```sh
scripts/vm.sh deploy <static IP>
scripts/vm.sh deploy <static IP> -e app_pull=always   # also pull newer image patch releases
```

A Centrifugo restart drops every WebSocket connection together with its in-memory history. Clients reconnect on their own and re-read their state over HTTP.

### The backend

Run `scripts/aws.sh deploy` from the commit you want to publish. See [Deploying the backend](#deploying-the-backend).

### The AWS resources

Change `deploy/local/aws.tfvars` or `deploy/terraform/`, then run `scripts/aws.sh apply`. It shows the plan and applies it only when you type `yes`.

- **Read the plan.** Changing an instance's zone, image or plan replaces it, and replacing an instance deletes its disk, and with it everything since the last [backup](#backups). Add a new instance and [move to it](#moving-to-a-new-vm) instead.
- **Destroying needs the name.** When the plan deletes or replaces an instance, the static IP, a disk or the distribution, the apply lists them and asks you to type each one's name before the usual `yes`. For the distribution, that's its domain, `<id>.cloudfront.net`: a new distribution gets a new one, and the `island-battles` CNAME must follow it. A wrong answer applies nothing. If you didn't mean to destroy it, `aws.tfvars` probably doesn't match the instances that run. Without a terminal, list the names in `ISLAND_BATTLES_ALLOW_DESTROY`, separated by commas.
- **It may publish the backend too.** When the plan changes the function, the apply first runs the migrations and then publishes the backend built from your checkout, so run it from the commit you last deployed or a newer one. A plan that leaves the function alone, such as moving the static IP, runs no migrations: in the middle of a move, the old VM refuses the backend.

## Deploying from GitHub Actions

The Deploy workflow, `.github/workflows/deploy.yml`, runs the same scripts on a GitHub runner. Start it from the repository's **Actions** tab, with `gh workflow run deploy.yml -f target=<target>`, or let an agent start it:

| Target | Runs | Role |
|---|---|---|
| `backend` | `scripts/aws.sh deploy` | deploy |
| `vm` | `scripts/vm.sh deploy "$(scripts/aws.sh output static_ip)"` | deploy |
| `web` | `scripts/deploy-web-client.sh`: builds the 3D web client and publishes it at `/play/` ([The web client](#the-web-client)) | deploy |
| `smoke` | `scripts/vm.sh smoke` | deploy |
| `plan` | `scripts/aws.sh plan`: plans every AWS resource, like `scripts/aws.sh apply`, and saves the plan | deploy |
| `apply` | `scripts/aws.sh apply-plan` with the plan that the `plan_run_id` run saved | apply |

**Only `main` deploys.** Every run is in the `production` environment, which is limited to `main`, and both roles trust only that environment. So only merged code gets AWS credentials, and merging a pull request is the approval. GitHub can't require a reviewer's approval for each run in a private repository on a personal plan. Runs wait for each other, and Terraform's lock keeps them apart from a machine's.

**Applying:**

1. Start a `plan` run. Its summary shows the plan, and the run ID to apply it with. It saves the plan, and the build it packages, under `plans/<run ID>/` in the deploy bucket. The bucket deletes a saved plan after 3 days.
2. Read the plan. If it's right, start an `apply` run with `plan_run_id` set to that run ID. It applies exactly that plan, then deletes it. It refuses a plan made from another commit than `main`'s, and Terraform refuses one whose state changed since: plan again.
3. A plan that deletes or replaces an instance, the static IP, a disk or the distribution stops the apply unless `allow_destroy` lists each one's name, like `ISLAND_BATTLES_ALLOW_DESTROY`.

**The roles:**

- **Deploy role**, `island-battles-github-deploy` (`deploy/terraform/github.tf`): reads the account (`ReadOnlyAccess`), which every plan needs, and writes only what a deploy writes: the backend function's code and settings, the [bot runners'](#bot-runners) code, the deploy bucket's state, files and plans, the web client's files under `play/` in its bucket (it may put them, and list the bucket), invalidations of the game's distribution, and a temporary SSH key for the VM. In the deploy bucket it deletes nothing but the state's lock.
- **Apply role**, `island-battles-github-apply` (`deploy/github_apply_role.py`): reads the account, and changes what `deploy/terraform/` manages: Lightsail, Lambda, CloudWatch logs and alarms, SNS, Budgets, ACM and CloudFront, buckets named `island-battles-*`, and IAM roles and users named `island-battles-*`. It can't change its own role. Because it can write IAM policies for `island-battles-*` roles, treat it as close to an administrator of the account: reviewing what merges to `main` is the control. It isn't part of the Terraform configuration, so an apply can't widen it.
- Neither role can read the [backups](#backups). GitHub stores no AWS keys: each run gets credentials for an hour.

**Deploying from an agent:** an agent in the cloud has no AWS credentials of its own. Once you've merged its pull request, it starts a `plan` run on `main`, reads the plan from the run's log and tells you what it changes. When you say so, it starts the `apply` run for that plan, then any `vm`, `backend` or `smoke` runs the change needs, and checks the result.

**Setting it up**, once:

1. Set `github_repository = "<owner>/<name>"` in `deploy/local/aws.tfvars` and run `scripts/aws.sh apply`. It creates GitHub's OIDC provider in IAM and the deploy role. `scripts/aws.sh output github_deploy_role_arn` prints the role.
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

**What still needs your own machine:** `-e app_generate_secrets=true`, dumps, restores and moves, and the setup above.

## Releasing

A release deploys the backend and the lobby, which is compiled into the same Lambda, tags the commit and publishes a GitHub release. Merging to `main` releases nothing: a person starts the Release workflow, `.github/workflows/release.yml`, by hand.

```sh
gh workflow run release.yml --ref main -f bump=patch   # or minor, or major
```

Or use the **Actions** tab, **Release**, **Run workflow** on `main`.

**Starting it is the approval.** Only people with write access can start it, only from `main` (any other branch fails at once, before anything runs), and its deploy jobs run in the `production` environment, which is limited to `main`, like every Deploy run. GitHub offers no required reviewer on a private repository's environment on a personal plan, so the run doesn't wait for a second approval. Read what merged since the last release before you start it. Start it when no other Deploy run is going: a manual Deploy that runs during a release can make the release's smoke step fail, and then no tag is created.

**What it does,** in order. A failure anywhere stops the rest and leaves no tag and no release:

1. **Checks** the commit with the same checks as a pull request (`checks.yml`).
2. **Plans** the version. The previous release is the highest `vX.Y.Z` tag. The first release, with no tag yet, is `v0.1.0`, whatever `bump` says. The run's summary shows the version, the previous release and why the VM is or isn't deployed.
3. **Deploys the VM** when the plan says so, then **the backend**, then **the web client** (`web` target, below), then runs the **smoke test**, each one a call of the Deploy workflow, one at a time. The backend is built with the version, and the smoke test fails unless `/healthz` reports it. The web client is built from the same commit and uploaded to `/play/`, so the backend and the client that talks to it go live together; the smoke test then fails unless `/play/` returns 200 and loads the script that was just uploaded.
4. **Releases:** creates the annotated tags `vX.Y.Z`, `lobby/vX.Y.Z` and `clients/island-battles-go/vX.Y.Z` on the deployed commit, and a GitHub release with generated notes that say whether the VM was redeployed. The `lobby/vX.Y.Z` tag marks the version of the lobby module, which is the same version; the module path `islandBattles/lobby` isn't go-gettable, so no Go module can fetch it by that path. The `clients/island-battles-go/vX.Y.Z` tag is the version of the [Go SDK](../clients/island-battles-go/README.md), whose module path `github.com/INTODAN/island-battles-go/clients/island-battles-go` is go-gettable: `go get github.com/INTODAN/island-battles-go/clients/island-battles-go@vX.Y.Z` resolves that tag. The plan fails if any of the three tags already exists.

If the tags were pushed but `gh release create` failed, re-running fails at the tag push. Create the release by hand with `gh release create vX.Y.Z --verify-tag --generate-notes`.

**The VM deploy is automatic.** If anything `scripts/vm.sh deploy` uses changed since the previous release, the release redeploys the VM first and **the bump is major**, whatever you asked for, so nobody has to remember. Those inputs are `deploy/ansible/` (playbooks, roles and variables), `deploy/vm.example.yml`, `scripts/vm.sh` and `deploy/deploy_files.py`; the list is `vm_inputs` in `scripts/release-plan.sh`. The first release has nothing to compare with, so it deploys the VM too. Redeploying is safe: the playbooks are idempotent. Anything the VM deploy needs that isn't in the repository, such as `-e app_generate_secrets=true`, still needs your machine.

**The web client** is uploaded by every release, after the backend deploy and before the smoke test, with `scripts/deploy-web-client.sh` ([The web client](#the-web-client)). The `web` target sets up Bun at the version pinned in `clients/island-battles-web/package.json`, and passes the entry script of the build it uploaded (such as `assets/index-1a2b3c4d.js`) to the smoke step. To upload a client on its own, start the Deploy workflow with `target` `web` (`gh workflow run deploy.yml --ref main -f target=web`). The upload is not tied to the version tag: a rollback of the client is `web` with `release_tag` set to an earlier release, which builds that tag's client. That works only for releases that include `scripts/deploy-web-client.sh` and the client's `packageManager` pin (from this change on); an earlier release has neither, so roll back the client by hand, as described below.

**Releases can't create AWS resources.** The deploy role only updates the function. When a release needs a new or changed resource, such as a Terraform change under `deploy/terraform/`, merge it first and run `plan` and then `apply` on `main` ([Deploying from GitHub Actions](#deploying-from-github-actions)) before you start the release. The release's backend deploy refuses a plan that touches Lightsail, the certificate or the distribution.

**The version.** `/healthz` still answers 204 and reports the build's version in the `X-Island-Battles-Version` header (`dev` for builds that set none). `ISLAND_BATTLES_VERSION=v0.1.0 scripts/aws.sh deploy` builds with a version, and `ISLAND_BATTLES_EXPECTED_VERSION=v0.1.0 scripts/vm.sh smoke` checks it. A plain `backend` deploy, or a `plan`/`apply` that publishes the backend, builds the commit as `dev`; the next release sets the version again.

**Rolling back:** start the Deploy workflow on `main` with `target` `backend` and `release_tag` set to the previous release, for example:

```sh
gh workflow run deploy.yml --ref main -f target=backend -f release_tag=v0.1.0
gh workflow run deploy.yml --ref main -f target=smoke -f release_tag=v0.1.0
```

A rollback is only for releases whose difference from the tag is code only: no change under `deploy/terraform/` and no VM change since that tag. A rollback runs the tag's `scripts/aws.sh` and the tag's Terraform configuration, so it can refuse, fail with AccessDenied, or revert Terraform-managed Lambda settings. Otherwise fix forward with a new release.

Don't add `-f target=vm` to a rollback. Running older Ansible against a VM configured by newer playbooks can downgrade containers, including Postgres. Don't do it without a database backup and the owner's machine.

The workflow itself must run on `main`, because the `production` environment accepts nothing else. `release_tag` makes it check out that tag, which must be on `main`, build it with its version, and (for `smoke`) expect it. A rollback creates no tag or release, and the migrations only go forward: the older code runs against the newer schema, so roll back only past releases whose migrations the older code tolerates.

**Later additions,** publishing the Python clients (#117), are jobs that `need` the `release` job in `release.yml`. A release made with the workflow's token doesn't trigger `on: release` workflows, so they can't be separate workflows.

**First release, once after merging:** start `release.yml` on `main`. It will be `v0.1.0` and deploys the VM as well.

### Publishing the Python clients

After the release is made, the `publish-python` job in `release.yml` publishes two packages to PyPI at the release's version, `X.Y.Z` for the tag `vX.Y.Z`:

| PyPI project | Import name | Source | GitHub environment |
| --- | --- | --- | --- |
| `mpl-lobby-client` | `lobby_client` and `lobby_api_client` | `lobby/clients/lobby-python/`: the wrapper and the generated HTTP SDK it needs, in one wheel | `pypi` |
| `island-battles-python` | `island_battles_client` | `clients/island-battles-python/` | `pypi-island-battles` |

The job runs once per package, each in its own GitHub environment, because PyPI refuses two pending publishers with identical settings (owner, repository, workflow and environment). Each run builds only its own package with `uv build` and checks that its wheel and sdist exist and, for the lobby client, that the `mpl-lobby-client` wheel holds both import packages. The version is set from the tag in its own working copy (`uv version --frozen`, never committed), and it uploads with PyPI **trusted publishing**: the workflow exchanges a short-lived OIDC token for an upload token, so the repository holds no PyPI token. The repository keeps the placeholder version `0.0.0.dev0` in both projects. The owner chose the names: the generic multiplayer infrastructure is called "mpl", so the lobby client is `mpl-lobby-client`, and the Island Battles client stays `island-battles-python`. Only the distribution names carry the prefix; the import names are unchanged. The generated HTTP SDK is not a distribution of its own: it is the `lobby_api_client` package inside `mpl-lobby-client`, so there is no `mpl-lobby-api-client` on PyPI. Both names were free on PyPI when chosen (a name like `lobby-python` is taken by another project).

**Owner setup, once, before the first release** (an agent can't do it on PyPI):

1. On pypi.org, under your account's **Publishing**, add two **pending publishers**, named `mpl-lobby-client` and `island-battles-python`. Both take owner `INTODAN`, repository `island-battles-go` and workflow `release.yml`, but different environments: `mpl-lobby-client` uses `pypi` and `island-battles-python` uses `pypi-island-battles`. A pending publisher creates its project on the first upload.
2. On GitHub, in **Settings → Environments**, make sure the `pypi` and `pypi-island-battles` environments exist (the first run creates them) and, under deployment branches, limit both to `main`, as `production` is.

**Failures.** The job runs after `release`, so a failed publish fails the run visibly, but the deploy, the tags and the GitHub release stay. Typical causes are a missing or misspelled pending publisher (the environment or workflow name must match exactly) or a PyPI outage. Fix the cause and re-run the `publish-python` job from the run's page; `skip-existing` makes it skip the files that an earlier partial run uploaded. Don't upload by hand with a token.

**Installing:** `pip install mpl-lobby-client island-battles-python`, or `uv add mpl-lobby-client==X.Y.Z island-battles-python==X.Y.Z` in your own project. Each client's README has the details.

## The web client

The 3D client in `clients/island-battles-web/` is served at `https://<game_hostname>/play/` (#104). It is a static build in a private S3 bucket, `<name>-web-client-<account>`, behind the game's own distribution:

- **Same origin.** API calls stay on the game's host, so there's no CORS and Centrifugo's allowed origin needs no change.
- **Cached at the edge.** `/play/*` uses `Managed-CachingOptimized` with compression, and never reaches the Lambda. Everything else still goes to the Lambda, uncached, except the static files of the lobby page and the game's other pages (see the CloudFront row of the resources table).
- **Private bucket.** Public access is blocked, and the bucket policy lets only this distribution read `play/*`, through an origin access control.
- **`/play/` and `/play/?join=CODE`** serve `play/index.html`: a CloudFront Function adds `index.html` to a path that ends in a slash. `/play` without the slash is redirected to `/play/`. Query strings aren't part of the cache key, so every `?join=CODE` gets the same cached page.

**Creating the resources, once.** The change adds the bucket with its public access block, encryption setting, ownership controls and policy, the origin access control and the CloudFront function, and updates the distribution. It replaces and deletes nothing. Apply it like any other AWS change: start the Deploy workflow's `plan` target on `main`, read the plan, then start `apply` with that run's ID ([Deploying from GitHub Actions](#deploying-from-github-actions)), or run `scripts/aws.sh apply` on your machine. The apply role may create the bucket because its name starts with `island-battles-`. A backend deploy (`scripts/aws.sh deploy`) is unaffected afterwards: the new resources are unchanged in its plans, and it stops only on a plan that changes CloudFront.

**Order for a fresh setup:** plan and apply the new resources, run `scripts/deploy-web-client.sh`, then run the backend deploy or release that shows the **Play in 3D** link, so the link never points at an empty bucket. Until the one-time apply has created the CloudFront resources, `backend` deploy runs are refused, because their plan would create them.

**Uploading a build:**

```sh
scripts/deploy-web-client.sh
```

It reads the bucket and the distribution from Terraform's outputs (`web_client_bucket`, `distribution_id`), then:

1. runs `bun run build` in `clients/island-battles-web/`, which builds for `/play/` with hashed files under `dist/assets/`;
2. uploads `dist/assets/` to `s3://<bucket>/play/assets/` with `Cache-Control: public, max-age=31536000, immutable`;
3. uploads `index.html` last, with `Cache-Control: no-cache`, so a page never names a file that isn't there yet;
4. invalidates `/play/` and `/play/index.html`, so the new build shows as soon as the script ends.

It never deletes files, so the hashed files of earlier builds stay and tabs that are open keep working. They are small; remove old ones from the bucket by hand if they ever matter. It needs `bun`, the AWS CLI and Terraform, and the credentials that `scripts/aws.sh` uses. Every release uploads it ([Releasing](#releasing)), and the Deploy workflow's `web` target does it on its own. If `ISLAND_BATTLES_WEB_BUNDLE_FILE` names a file, the script writes the build's entry script there, which is what the workflow hands to the smoke test.

**Once, for the release to upload:** the deploy role needs the permissions that #114 adds (`s3:PutObject` on `play/*` of the bucket, `s3:ListBucket` on it, and `cloudfront:CreateInvalidation` on the distribution). They are a change to the role's policy in `deploy/terraform/github.tf`, so after merging, start a `plan` run on `main` (it shows only the role policy changing), read it, then start `apply` with that run's ID. Until then the `web` target fails with AccessDenied, and `backend` deploys, which would apply that policy change themselves without the permission to, fail too: apply first, then release.

**Rolling back:** check out the commit of the earlier build and run `scripts/deploy-web-client.sh` again. Its `index.html` names the earlier hashed files, which are still in the bucket, and the invalidation makes it live.

**A missing file answers 403**, not 404: the bucket policy grants only `s3:GetObject`, so S3 doesn't reveal which keys exist.

**Checking it:** `curl -sI https://<game_hostname>/play/assets/<file>` shows `x-cache: Hit from cloudfront` on the second request, and `/play` answers `301` to `/play/`.

## Bot packages

Members upload bot packages, the zips that the SDKs' `package` command builds, to their lobby (#221), into the private bucket `<name>-bot-packages` (`deploy/terraform/bot_packages.tf`):

- **The zip never passes through the Lambda,** whose Function URL takes at most 6 MB. The backend hands out a presigned POST, valid for 15 minutes, for 1 byte to 250 MiB, and the browser or the CLI posts the zip straight to the bucket. Its CORS rule allows POST from `https://<game_hostname>`, where both the 3D client and the lobby page are.
- **The backend reads only what it checks:** the zip's central directory and `island-battles-bot.json`, with range requests. Its role may put, get and delete objects in this bucket and nothing more. It deletes a zip that fails its check, the previous package of a member who uploads a new one, and a package its owner deletes.
- **Everything is deleted after 8 days:** a lifecycle rule deletes each zip 8 days after its upload, and the backend's [game retention](../backend/README.md#game-retention) deletes the packages' rows as long after. Versioning is off, so nothing is kept longer.
- **Settings:** Terraform passes the bucket's name to the function as `ISLAND_BATTLES_BOT_PACKAGES_BUCKET`, and the region is the function's own. Without it, the backend answers the bot package endpoints with 503.

**Creating the resources, once.** The change adds the bucket with its public access block, encryption, ownership controls, lifecycle rule, CORS rule and policy, and a policy on the function's role. It replaces and deletes nothing. Plan and apply it from `main` (the Deploy workflow's `plan`, then `apply` with that run's ID, or `scripts/aws.sh apply`) before the next backend deploy: the GitHub deploy role may not create buckets, so a `backend` run that would create them fails. The apply role may, because the bucket's name starts with `island-battles-`.

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

- **Measured:** the backend reads both runners' month-to-date `Duration` and `InitDuration` from CloudWatch (`cloudwatch:GetMetricData`, which IAM can't scope to resources, so the backend role's inline policy `bot-compute` allows it alone, on `*`), times their 1 GB, at most once per 10 minutes, and keeps it in the database. Each read costs a little, outside the free tier: see [Free allowances](#free-allowances).
- **Settings:** `ISLAND_BATTLES_BOT_COMPUTE_BUDGET_GB_SECONDS`, 300000 by default, in `extra_lambda_environment` to change it; `0` stops every new game with an uploaded bot. Turning server bots off (`ISLAND_BATTLES_SERVER_BOTS = "off"`) stops them all.
- **Safety net:** the $5 monthly cost budget (`<name>-monthly`) emails `budget_email`, or `alert_email` while it is unset, at 80 % of actual costs ([Logs, alerts and traces](#logs-alerts-and-traces)).

**Adding the guard, once (#226).** The change adds the backend role's inline policy `bot-compute` and the budget `<name>-monthly`. It replaces and deletes nothing. To send the budget's emails somewhere else than `alert_email`, set `budget_email` in `deploy/local/aws.tfvars` and push it to the deploy bucket first. Plan and apply it from `main` (the Deploy workflow's `plan`, then `apply` with that run's ID, or `scripts/aws.sh apply`) before the next `backend` deploy: the deploy role may not change the backend's role, so a `backend` run whose plan would add the policy fails, and a backend without it can't read the runners' compute, so every start with an uploaded bot would answer 503. To check it, open the bot picker of a lobby with server bots: `GET /game/bot-compute` answers 200 with this month's `used_gb_seconds`.

## Smoke test

```sh
scripts/vm.sh smoke
```

It reads `game_url` from your settings and checks the whole path, through CloudFront:

1. `/healthz` returns 204, with a valid certificate for the game's name. With `ISLAND_BATTLES_EXPECTED_VERSION` set, its `X-Island-Battles-Version` header must name that version ([Releasing](#releasing)).
2. `/` leads to the lobby UI.
3. A guest credential works, and the backend issues a connection token for it, which needs Postgres.
4. An authenticated GET reaches the backend with its `Authorization` header. CloudFront strips the header from GET requests unless the origin request policy forwards it.
5. Centrifugo accepts the token at `wss://vm.<domain>/connection/websocket`, with a valid certificate and the game's origin.
6. Centrifugo subscribes the guest to its personal channel through the backend's subscribe proxy, which needs the proxy secret and the Function URL.
7. Only with `ISLAND_BATTLES_EXPECTED_WEB_BUNDLE` set to the entry script of a build, such as `assets/index-1a2b3c4d.js`: `/play/` returns 200, its `index.html` loads that script, and the script is served. CloudFront may serve the previous page for a few seconds after an invalidation, so it tries for up to 90 seconds. The release sets it from the `web` target's upload. A plain `smoke` run checks nothing of the web client, so it passes while nothing has been uploaded yet.

The guest's credential is kept in `deploy/local/smoke-session.json`, and later runs reuse it. After a move to a new VM, that confirms the data came along. Guest credentials expire after 24 hours, and the test replaces one that expires within two hours. If the saved credential is rejected for another reason, delete the file and run the test again.

## The admin MCP server

The [admin MCP server](../backend/README.md#admin-mcp-server) (#79) answers questions about lobbies, players and games, such as "How many lobbies are currently active?", from Claude Code or another MCP client on your machine. Nothing of it is deployed: it runs on your machine and connects straight to Postgres on the VM, as the read-only role `island_battles_admin`.

**The role.** The VM deploy creates it, with the password in `deploy/local/secrets/postgres_admin_password`, and grants it, in one transaction:

- `SELECT` on every table, and default privileges, so that it can read the tables that later migrations create;
- on `lobby_sessions`, `SELECT` on `id`, `player_id`, `created_at` and `expires_at` only. `bearer_hash`, the digest of each guest's credential, stays unreadable, also through the server's SQL tool.

The deploy also revokes `EXECUTE` on `pg_logical_emit_message` from `PUBLIC` in the backend's database. It writes WAL that a rollback doesn't undo, so a loop of calls could fill the disk even from a read-only transaction. Nothing in the backend calls it, and the superuser keeps it.

The role logs in only over TLS with its password, like the backend's. Its sessions are read-only with a 5-second statement timeout, and it has no privilege to write. The default privileges are only set once `lobby_sessions` exists, because set before, they would let the role read all of it. So on a new VM, run the VM deploy again after the backend deploy's first migration, or the role can read nothing. A restore leaves no grants, so the restore grants them again.

The default privileges also make whatever a later migration creates readable, until the next VM deploy runs the grants again: a migration that recreates `lobby_sessions` would let the role read `bearer_hash`, and so would a new table that holds credentials. The server guards the first case: at startup and before every `query`, it checks whether its role can read `bearer_hash` without being a superuser or the table's owner. If so, `query` refuses to run and the server logs why, until you run `scripts/vm.sh deploy <static IP>`. The typed tools never read the column. A new table that holds credentials needs its own column grants in `admin-role.sql.j2`. A deployment from before #79 has neither the role nor its password: run one VM deploy with `-e app_generate_secrets=true`, as [Secrets](#secrets) describes.

**The connection string.** The certificate is self-signed for `vm.<domain>`, and you have it, so check it with `verify-full`:

```text
postgres://island_battles_admin:<password>@vm.<domain>:5432/island_battles?sslmode=verify-full&sslrootcert=<repository>/deploy/local/secrets/postgres-tls/server.crt
```

Use an absolute path for `sslrootcert`. The password has only letters and digits, so it needs no escaping.

**Registering it** with Claude Code, from the repository root:

```sh
claude mcp add island-battles-admin-production \
  --env ISLAND_BATTLES_ADMIN_DATABASE_URL="postgres://island_battles_admin:$(cat deploy/local/secrets/postgres_admin_password)@vm.<domain>:5432/island_battles?sslmode=verify-full&sslrootcert=$PWD/deploy/local/secrets/postgres-tls/server.crt" \
  -- go run ./backend/cmd/admin-mcp
```

Claude Code keeps the URL, password included, in `~/.claude.json`, which only you can read. To run a built binary instead, and other clients, see the [backend README](../backend/README.md#admin-mcp-server).

## Backups

Postgres's data is the only state on the VM that can't be rebuilt: the playbooks rebuild the rest, the secrets are in the deploy bucket, and Centrifugo gets a new certificate on its own. So the VM backs up only the database, to S3, every day. Lightsail's automatic snapshots (#33) stored the whole disk seven times over, and are off.

- **What:** a systemd timer on the VM, `island-battles-backup.timer`, runs at `app_backup_time`, 01:30 UTC by default. It dumps the database with `pg_dump`, in the same custom format as `scripts/vm.sh dump`, and uploads the dump to `s3://<name>-backups-<account>/postgres/island_battles-<time>.dump`. The VM deploy also backs up once at the end, so a wrong key fails the deploy instead of the night. Pass `-e app_backup_now=false` to skip that.
- **How long:** S3 deletes each dump after `database_backup_retention_days`, 7 by default. Versioning keeps a dump that was overwritten or deleted for as long again.
- **The key:** Lightsail instances can't take an IAM role, so the VM has an access key of the `<name>-database-backups` user. It may only upload new dumps and publish the backup metric: it can't list, read or delete a backup, so a compromised VM can't destroy them. Terraform keeps the key in its state, and `scripts/aws.sh apply` copies it to `deploy/local/secrets/database_backups.json`, which the deploy bucket shares. The VM deploy installs it as `/etc/island-battles/backup.env`, readable only by root. To rotate it, run `scripts/aws.sh apply -replace=aws_iam_access_key.backups` and deploy the VM.
- **Skipped:** a VM that a [move](#moving-to-a-new-vm) left behind, which has the moved-away marker, and a database without tables yet, such as a new VM before its restore or its first migration. A skip counts as no backup.
- **The alarm:** each successful backup adds 1 to the `DatabaseBackups` metric. When none did for 26 hours, the `<name>-database-backups` alarm emails `alert_email`: the run failed, the VM was down, or the key stopped working. Read the run's log on the VM: `sudo journalctl -u island-battles-backup`. `systemctl list-timers island-battles-backup` shows the next run.
- **What it costs:** a few MB of dumps and one upload a day, well under a cent a month. S3 has no always-free allowance, so this shows up in the zero-spend budget. The metric and the alarm are inside CloudWatch's free ones ([The budget](#the-budget)).
- **What it doesn't cover:** the backups live in the same account and region as the VM, and a restore loses everything written since the last one, up to a day. The GitHub deploy role can't read them; reading them takes the account's own credentials.

### Listing and restoring backups

```sh
bucket="$(scripts/aws.sh output database_backup_bucket)"
aws s3 ls "s3://$bucket/postgres/"
aws s3 cp "s3://$bucket/postgres/island_battles-<time>.dump" deploy/local/dumps/
scripts/vm.sh restore <IP> deploy/local/dumps/island_battles-<time>.dump
```

The restore needs a deployed VM with an empty database, or `-e app_restore_replace_existing=true` to replace one, which first dumps the database it replaces to `deploy/local/dumps/` ([Dumps on their own](#dumps-on-their-own)). If the VM itself is gone, create a new instance and deploy it as in steps 1 and 2 of [Moving to a new VM](#moving-to-a-new-vm), restore the latest backup into it, then continue with steps 5 and 6.

### Turning backups on for an existing deployment

1. `scripts/aws.sh apply`. The plan creates the bucket, the key and the alarm, and turns off the instance's automatic snapshots. The alarm emails within the hour, until step 2 backs up.
2. `scripts/vm.sh deploy <static IP>`. Its last step backs up for the first time.
3. Check that the dump is there: `aws s3 ls "s3://$(scripts/aws.sh output database_backup_bucket)/postgres/"`. Within the hour, the alarm returns to OK.
4. Check that the dump is usable: download it as in [Listing and restoring backups](#listing-and-restoring-backups), and `pg_restore --list <file>` must list the tables. To be thorough, restore it into a throwaway local Postgres.
5. Delete the old automatic snapshots: turning snapshots off keeps the existing ones, and they cost more than the backups. In the Lightsail console, open the instance's **Snapshots** tab and delete each automatic snapshot, or run `aws lightsail delete-auto-snapshot --resource-name <instance> --date <YYYY-MM-DD>` for each date that `aws lightsail get-auto-snapshots --resource-name <instance>` lists.

## Moving to a new VM

Moving copies the database with `pg_dump` and restores it on the new VM (#31). It takes a few minutes of downtime: from the moment the data is copied until the static IP points at the new VM, the backend can't reach a database. Backups are separate: they're [daily dumps in S3](#backups).

1. **Create the new instance** without moving the static IP yet. Add it to `vm_instances` in `deploy/local/aws.tfvars`, next to the old one, and leave `static_ip_instance` alone:

   ```hcl
   vm_instances = {
     "island-battles-1" = {}
     "island-battles-2" = {}   # optionally with a bundle_id or blueprint_id of its own
   }
   ```

   Run `scripts/aws.sh apply`. The plan only creates the instance and its firewall rules, the same 22, 443 and 5432 as the old one's. `scripts/aws.sh output instance_ips` prints its temporary public IP.
2. **Deploy it.** `vm.<domain>` still points at the old VM, so tell the DNS check which IP to expect:

   ```sh
   scripts/vm.sh deploy <new IP> -e vm_static_ip=<static IP>
   ```

   The TLS check is skipped, because the new VM can't get its certificate yet.
3. **Make sure you have a fresh guest credential** for step 6: run `scripts/vm.sh smoke`.
4. **Copy the data:**

   ```sh
   scripts/vm.sh move-data <static IP> <new IP>
   ```

   - On the old VM, it locks the backend's role out (`NOLOGIN`) and ends its connections, so nothing is written after the dump. It locks the admin MCP server's read-only role out too, so nobody reads the old data by mistake. The Lambda's requests fail from here until step 5 is done.
   - The lock is sticky. It leaves the marker file `/opt/island-battles/moved-away` on the old VM, and a deploy or restore there stops while the marker exists. A redeploy can't let the backend write to the old VM again, which would lose those writes.
   - It dumps the old database to `deploy/local/dumps/` and restores the dump into the new VM's empty database, in one transaction.
   - The restore refuses a dump without tables, and a database that already has tables. To overwrite one, pass `-e app_restore_replace_existing=true`: the restore first saves the database it replaces to `deploy/local/dumps/`.
   - **If the restore fails,** the old VM stays locked and the new database stays empty. Fix the cause and run the same `move-data` command again. To give up on the move instead, unlock the old VM: `scripts/vm.sh deploy <static IP> -e app_unlock_after_move=true`.
5. **Move the static IP** to the new instance: set `static_ip_instance = "island-battles-2"` in `aws.tfvars` and run `scripts/aws.sh apply`. The plan replaces only the static IP's attachment. The DNS record and the Lambda's settings don't change, because both use `vm.<domain>`. The Lambda's next connections reach the new VM.
6. **Check the new VM:**

   ```sh
   ssh-keygen -R <static IP>    # the new VM has a new host key
   ssh-keygen -R vm.<domain>    # if you also log in by name
   scripts/vm.sh deploy <static IP>
   scripts/vm.sh smoke
   ```

   The deploy changes nothing, but it now runs the TLS check, which also gets the new VM's certificate. The smoke test should report that the saved credential still works. The restored schema is the old VM's. If the backend deploy brings newer migrations, its migrate step updates it.
7. **Delete the old instance** once you're satisfied. Its backups stopped when `move-data` marked it, and the deploy in step 6 made the new VM's first one ([Backups](#backups)). Remove it from `vm_instances` and run `scripts/aws.sh apply`. The plan deletes the instance and its firewall rules, and the apply asks you to type the instance's name. To go back to the old instance instead, set `static_ip_instance` back, run `scripts/aws.sh apply`, and run `scripts/vm.sh deploy <static IP> -e app_unlock_after_move=true`. That lets the backend's role log in again and removes the marker. Anything written to the new VM in the meantime isn't copied back.

If the old VM is gone, there's nothing to dump. [Restore the latest backup](#listing-and-restoring-backups) into the new VM instead, then run steps 5 and 6.

### Dumps on their own

```sh
scripts/vm.sh dump <IP>                  # writes deploy/local/dumps/island_battles-<time>.dump
scripts/vm.sh restore <IP> <dump file>   # into an empty database
```

Dumps use `pg_dump`'s custom format, without owners or grants. The restore makes the backend's role the owner of everything, and grants the admin MCP server's role read access again. Dumps hold player data, so keep them in `deploy/local/`. The VM's daily [backups](#backups) are dumps of the same kind.

## Logs, alerts and traces

The backend Lambda's logs, error alarms and traces stay inside AWS's free monthly allowances, worst case included (#46). AWS has no hard spending cap, so every source is bounded by the backend's code and by Terraform, for the function at its concurrency cap for a whole month.

- **Logs** go to `/aws/lambda/<name>`, kept for 7 days. Lambda filters application and system logs at WARN. The backend writes nothing below WARN anyway, and each instance writes at most N lines a minute of at most L bytes ([backend/README.md](../backend/README.md#logs-and-traces)).
- **Errors:** every 5xx answer and every recovered panic is one ERROR line with the operation, the error chain and the IDs. One metric filter counts ERROR lines. Two alarms, on that metric and on the function's `Errors`, email `alert_email` through an SNS topic when they enter ALARM.
- **Traces:** the backend traces each request, database query and Centrifugo API call with the OpenTelemetry Go SDK. Each instance starts at most one trace every T seconds. It hands the spans over localhost to the collector layer, which sends them to X-Ray. Lambda's own Active tracing stays off.
- **Tripwire:** a zero-spend AWS Budgets cost budget of $0.01 emails `alert_email` once anything costs money. Budgets refreshes its data about three times a day, so a charge is reported within a day. It stops nothing; the limits do.
- **Safety net:** a second cost budget, `<name>-monthly`, of $5 a month, emails `budget_email` once actual costs pass 80 % of it, $4 (#226). Unset, `budget_email` is `alert_email`. It covers everything, not just bots, and stops nothing either.
- **The VM's logs:** Docker rotates the `db` and `centrifugo` logs at 10 MB, five files each (#27). Read them over SSH as `deploy`, which owns the Compose project: `ssh deploy@vm.<domain>`, then `cd /opt/island-battles && docker compose logs`. `ubuntu` can't open `/opt/island-battles`. They never leave the VM and cost nothing.

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
| CloudWatch `GetMetricData` | None: it is outside the free API requests, at $0.01 per 1,000 metrics requested. The [monthly compute guard](#the-monthly-compute-guard) requests 4 metrics at most once per 10 minutes: at most 17,856 a month, about $0.18, and only while someone opens a bot picker or starts a game with an uploaded bot. | [CloudWatch pricing](https://aws.amazon.com/cloudwatch/pricing/) |
| AWS Budgets | Budgets and their notifications are free. Only action-enabled budgets past the first two, and emailed reports, cost money. | [Budgets pricing](https://aws.amazon.com/aws-cost-management/aws-budgets/pricing/) |
| CloudFront | 1 TB of data transfer out and 10,000,000 HTTP or HTTPS requests, on the always-free tier of the pay-as-you-go pricing. Transfer from AWS origins, such as the Function URL, to CloudFront is free. Checked on 2026-10-30. | [CloudFront pricing](https://aws.amazon.com/cloudfront/pricing/pay-as-you-go/), [CloudFront FAQ](https://aws.amazon.com/cloudfront/faqs/) |
| ACM | Public certificates are free. | [CloudFront pricing](https://aws.amazon.com/cloudfront/pricing/pay-as-you-go/) ("Free SSL certificates") |

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
| Custom metrics | The error-line metric filter, and the VM's `DatabaseBackups` metric, both without dimensions. The SDK's and GORM plugin's metrics aren't exported, and the collector has no metrics pipeline. | 2 of 10 |
| Alarms | The two error alarms and the [backup alarm](#backups) | 3 of 10 |
| Alarm emails | Each error alarm looks at one hour and notifies only on entering ALARM, after at least an hour in OK, so at most once every 2 hours: 372 each. The backup alarm needs 26 hours without a backup, so at most 28. | 772 of 1,000 |
| Budget emails | Sent by Budgets, not SNS | free |
| Traces recorded | Each instance starts at most one every T, and its first T after it starts: **C × 2,678,400 / T ≤ 80,000**, checked by Terraform and at startup | 74,400 of 100,000 |
| Traces retrieved or scanned | Opening traces in the console, by hand | far under 1,000,000 |
| CloudFront's standard and real-time logs | Off: the distribution has no logging configuration, which the Terraform tests check | 0 |
| CloudFront's requests and transfer out | Not bounded by code: anyone can call the public name. Only the static files of the Lambda's pages are cached (for five minutes), so most requests are also Lambda requests. | of 10,000,000 requests and 1 TB; see below |

Two assumptions sit under the per-instance limits. Each Lambda instance is one process with its own limits, and the concurrency cap bounds how many run at once. An instance that replaces another, such as after a crash, starts with a fresh minute of N lines but no trace until it has run for T. The 3.1 GB of headroom also covers that.

Lambda's requests and compute, and CloudFront's requests and transfer, are outside this budget. Players stay far under CloudFront's free 10 million requests a month: a lobby tab left open for a whole month sends a heartbeat every 15 seconds, about 180,000 requests. The developer accepted the cost risk of the public Function URL and the public name (#26, #75).

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

These checks first ran on 2026-10-30, with `reserved_concurrency = null`, `account_concurrency_limit = 10`, `log_lines_per_minute = 2` and `trace_interval_seconds = 360`. The results are noted under each step. Run the commands on the VM as `deploy`: `ssh deploy@vm.<domain>`, then `cd /opt/island-battles`.

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

- **`Missing secrets: ...`:** the deploy bucket doesn't have them. Push `deploy/local/` from the machine that has them ([The deploy bucket](#the-deploy-bucket)). See [Secrets](#secrets).
- **`has the deployment's Terraform state but no secrets`:** the same, caught before the playbook runs.
- **`These files changed both here and in the bucket`:** see [The deploy bucket](#the-deploy-bucket) for keeping one copy.
- **`is Terraform's old local state`:** this machine still has the state from before the deploy bucket. Run `scripts/aws.sh migrate-state` ([Moving an existing deployment to the bucket](#moving-an-existing-deployment-to-the-bucket)).
- **`No Lightsail instance in <region> has the public IP <IP>`:** check the IP against `scripts/aws.sh output instance_ips`, and `region` in `aws.tfvars`.
- **`<IP> was moved away from`:** you're deploying the VM a move left behind. Check the IP. See step 7 of [Moving to a new VM](#moving-to-a-new-vm) to really go back to it.
- **`vm.<domain> resolves to [...], not <IP>`:** fix the A record and wait for it to resolve, or pass `-e vm_static_ip=<static IP>` when preparing a new VM.
- **The TLS check fails:**
  - Check that the Lightsail firewall opens 443 and that CAA records allow `letsencrypt.org`.
  - Read Centrifugo's log on the VM: `ssh deploy@vm.<domain>` with the key from `ssh_public_key`, or the Lightsail console's browser-based SSH and then `sudo -iu deploy`, then `cd /opt/island-battles && docker compose logs centrifugo`.
  - After repeated failures, Let's Encrypt blocks new attempts for about an hour.
- **`REMOTE HOST IDENTIFICATION HAS CHANGED`:** the static IP now points at a different instance. Run `ssh-keygen -R <static IP>`.
- **`This plan changes more than the backend`:** `scripts/aws.sh deploy` found changes to Lightsail resources or the game's certificate or distribution, or a deletion, in the plan, and stopped before migrating. Run `scripts/aws.sh apply`, read the plan, and deploy again once it's applied. Right after `game_hostname` is first set, request the certificate first ([Adding the game's name](#adding-the-games-name-to-a-running-deployment)).
- **The apply waits at `aws_acm_certificate_validation.game: Still creating...`:** ACM hasn't seen the validation record. `dig CNAME <the record's name> +short` must print the value that `scripts/aws.sh output certificate_validation_record` shows. Terraform gives up after 75 minutes; run the apply again once the record resolves.
- **`https://island-battles.<domain>` doesn't resolve, or shows a certificate for another name:** `dig +short island-battles.<domain>` must lead to `scripts/aws.sh output distribution_domain`. A new CNAME may take the zone's negative TTL, 5 minutes for `elidan.org`, to reach a resolver that looked the name up before.
- **The lobby loads, but live updates never arrive:** Centrifugo accepts browsers only from `game_url`'s origin. Open the game at `game_url`, not at the Function URL, and check that `game_url` in `deploy/local/vm.yml` matches it, then redeploy the VM.
- **Authenticated requests answer 401 through CloudFront, but not on the Function URL:** the distribution doesn't forward `Authorization`. Check its origin request policy, `Managed-AllViewerExceptHostHeader`.
- **The migrations or the apply fail during `scripts/aws.sh deploy`:** fix the cause, for example the database being unreachable, and run `scripts/aws.sh deploy` again. Migrations that already ran are skipped. If the apply failed after the migrations, the old code keeps serving against the migrated schema until a deploy succeeds.
- **`lambda.env is missing`** from Terraform or `scripts/aws.sh`: push `deploy/local/` from the machine that has it, or run the VM deploy, which writes it. Only the very first apply passes `-var allow_missing_lambda_settings=true`.
- **`account_concurrency_limit is N in aws.tfvars and the account's limit is M`** from `scripts/aws.sh`: `reserved_concurrency` is null and AWS has raised the account's limit. Reserve concurrency again or set `account_concurrency_limit` ([The concurrency cap](#the-concurrency-cap)).
- **`decreases account's UnreservedConcurrentExecution below its minimum value`:** the account's concurrency limit is too low. See [The concurrency cap](#the-concurrency-cap).
- **`AccessDenied: Your account must be verified before you can add new CloudFront resources`:** the account can't create distributions yet. The error asks you to contact AWS Support: open a case in the Support Center with the error message and its request ID, then run `scripts/aws.sh apply` again. Until the distribution exists, `scripts/aws.sh deploy` refuses its plan, which creates it; publish the backend with `scripts/aws.sh apply` instead.
  - 2026-10-30, the live account: the first two full applies failed this way. The first had already created the certificate's validation and updated the function. A third, later the same day, created the distribution in 3 minutes.
- **The VM deploy times out connecting to port 22:** the firewall doesn't let this machine in, as on 2026-10-30, when `ssh_allowed_cidrs` held one address. Remove `ssh_allowed_cidrs` from `aws.tfvars` and run `scripts/aws.sh apply` from a machine that can deploy ([Logging in to the VM](#logging-in-to-the-vm)).
- **`Permission denied (publickey)` from the VM deploy:** Lightsail's temporary certificate was refused, for example because it expired. Run the deploy again for a new one.
- **The Function URL answers `Internal Server Error`:** read the function's log, the CloudWatch log group `/aws/lambda/<name>`, `/aws/lambda/island-battles` by default, as [Reading the logs](#reading-the-logs) describes. Right after the first apply, the function has no settings yet: run `scripts/aws.sh deploy`. A `Forbidden` means the Function URL's public permissions are missing.
- **The Function URL answers `the backend failed to start` (503):** the log's `Backend failed to start` line says why, such as missing settings, an unreachable database or limits over the budget. The instance tries again every 10 seconds.
- **`... over the free-tier budget`** from Terraform: the concurrency cap and the log or trace limits together could leave the free tier. Lower one of them ([The budget](#the-budget)).
- **`Terraform sets ... itself`:** `lambda.env` or `extra_lambda_environment` sets the pool size, a log or trace limit, or a collector setting. Use the Terraform variables instead.
- **No alarm emails:** follow the link in SNS's confirmation email, or resend it from the SNS console's subscription page.
- **`The daily database backup needs .../database_backups.json`** from the VM deploy: run `scripts/aws.sh apply`, which creates the backups bucket and writes the file, then deploy again ([Backups](#backups)).
- **`The database backup failed`** from the VM deploy, or the `<name>-database-backups` alarm emails: read `sudo journalctl -u island-battles-backup` on the VM. `AccessDenied` means the key in `/etc/island-battles/backup.env` is wrong or was rotated: run `scripts/aws.sh apply` and deploy the VM. To try a fix, run `sudo systemctl start island-battles-backup` and read the log again.
- **Postgres from your machine:** `psql "host=vm.<domain> dbname=island_battles user=island_battles sslmode=require"`, with the password from `deploy/local/secrets/postgres_app_password`. To only read, use `user=island_battles_admin` and `deploy/local/secrets/postgres_admin_password`.
- **The admin MCP server answers `permission denied`:** the role has no grants yet, for example on a new VM before its first migration, or after a restore that failed before granting them. Run `scripts/vm.sh deploy <static IP>` ([The admin MCP server](#the-admin-mcp-server)). A query that names `bearer_hash`, or `SELECT *` from `lobby_sessions`, is denied on purpose.

## Validating changes

Never run the playbooks against a host you don't own, and never apply Terraform to an account you don't own. To check changes to the playbooks, run these from `deploy/ansible`:

```sh
ansible-lint
ansible-playbook -i 203.0.113.10, --syntax-check site.yml dump.yml restore.yml
```

To check changes to the Terraform configuration, run these from the repository root. They need no AWS credentials: the tests mock both providers, so they never call AWS.

```sh
terraform -chdir=deploy/terraform init -backend=false
terraform -chdir=deploy/terraform fmt -check -recursive
terraform -chdir=deploy/terraform validate
terraform -chdir=deploy/terraform test
python3 -I -m unittest discover -s deploy -p 'test_*.py'   # the plan checks, the deploy bucket, the backup script and the apply role, offline
```
