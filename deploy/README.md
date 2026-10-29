# Deployment

Island Battles runs in one AWS region:

- **Backend:** the Go backend runs on AWS Lambda behind its Function URL. Players open the lobby UI at the Function URL itself. There is no custom domain, CloudFront or ACM certificate.
- **VM:** a Lightsail instance runs Postgres and Centrifugo in Docker Compose. Nothing else runs on it.
- **Network:** there is no VPC or peering. The Lambda reaches Postgres and Centrifugo's HTTP API over the internet:
  - Postgres accepts only TLS (`hostssl`) logins, with scram and a generated password.
  - Centrifugo's API needs TLS and its API key.
- **DNS:** one record, `vm.<domain>`, points at the VM's static IP. Centrifugo gets a Let's Encrypt certificate for it, and browsers connect to `wss://vm.<domain>/connection/websocket`.

This document covers creating the AWS resources with Terraform, setting up the VM with Ansible, deploying the backend to Lambda, backups, redeploys and moving to a new VM. [First deployment, step by step](#first-deployment-step-by-step) puts it all in order. #26 describes the architecture.

## Files

| Path | What it is |
|---|---|
| `scripts/aws.sh` | Creates the AWS resources and deploys the backend to Lambda. |
| `scripts/vm.sh` | Deploys the VM, moves its database and runs the smoke test. |
| `deploy/aws.example.tfvars` | The template for the AWS settings. |
| `deploy/vm.example.yml` | The template for the VM settings. |
| `deploy/terraform/` | The AWS resources: the Lightsail VM in `vm.tf`, the Lambda in `lambda.tf`, and its log group, alarms, collector settings and budget in `observability.tf`. `tests/` checks the configuration offline. |
| `deploy/check_deploy_plan.py` | Reads the plan for `scripts/aws.sh`: stops a backend deploy that would change more than the backend, lists what an apply would destroy, and tells an apply whether to run the migrations. `deploy/test_check_deploy_plan.py` tests it. |
| `deploy/ansible/` | The playbooks: `site.yml` deploys, `dump.yml` and `restore.yml` copy the database. The `base` role prepares Ubuntu, and the `app` role deploys the Compose project. |
| `deploy/smoke.py` | The smoke test that `scripts/vm.sh smoke` runs. |
| `deploy/local/` | Gitignored. Your settings, the generated secrets, the Lambda's settings, Terraform's state, the built Lambda, dumps and the smoke test's guest credential. |

## Prerequisites

- **On your machine:**
  - Terraform 1.9 or newer, Go, which builds the backend and runs its migrations, and Python 3, for the backend deploy's plan check.
  - AWS credentials for Terraform, which reads the standard AWS credential chain, e.g. `AWS_PROFILE=<profile>`. They need to manage Lightsail, Lambda, IAM roles, CloudWatch Logs and alarms, SNS and Budgets. Nothing in the repo stores them.
  - Ansible: `uv tool install ansible-core`. `scripts/vm.sh` installs the collections it needs into `deploy/ansible/.collections/` (gitignored) on first use.
  - [uv](https://docs.astral.sh/uv/), for the smoke test.
  - The instance's SSH key, through `ssh-agent`, `~/.ssh/config` or `ansible_ssh_private_key_file` in the settings.
- **Lightsail** (Terraform creates these, see [AWS resources](#aws-resources)):
  - An Ubuntu 24.04 LTS instance. The 1 GB plan is enough. Both images publish amd64 and arm64 builds.
  - A static IP attached to it.
  - Firewall rules for 22, 443 and 5432. Limit 22 to your own IP if you can. Port 80 stays closed, because Centrifugo answers Let's Encrypt's challenge on 443.
  - Ansible doesn't set up a host firewall: ports that Docker publishes bypass ufw, so the Lightsail firewall is the one that counts.
- **The Lambda's Function URL:** `scripts/aws.sh output function_url`. Centrifugo calls the backend's subscribe proxy through it, and accepts browser connections only from its origin.
- **DNS** (#30): see below.

### DNS

The VM needs a single DNS record. Squarespace has no DNS API, so you add it by hand, once. It doesn't change on a redeploy or when you move to a new VM, because the static IP moves with the deployment.

1. In the Squarespace domains dashboard, open the domain's DNS settings and add a custom record: type `A`, host `vm`, value the static IP. If the domain uses DNS Connect with another provider's nameservers, add it there instead.
2. Run `dig CAA <domain> +short`. If it prints records, one of them must allow `letsencrypt.org`, or the certificate request fails. You can also add a CAA record on `vm.<domain>` itself.
3. Wait until `dig +short vm.<domain>` prints the static IP.

Let's Encrypt allows only a few failed validations per hostname per hour. The playbook therefore stops before touching the VM if `vm.<domain>` doesn't resolve to the static IP yet.

### Settings

Copy `deploy/vm.example.yml` to `deploy/local/vm.yml` and fill it in:

| Setting | Meaning |
|---|---|
| `deploy_domain` | The domain. The VM's hostname is `vm.<deploy_domain>`. |
| `lambda_function_url` | The backend Lambda's Function URL, e.g. `https://<id>.lambda-url.<region>.on.aws/`. `scripts/aws.sh output function_url` prints it. |
| `ansible_ssh_private_key_file` | Optional. The instance's SSH key. |
| `app_letsencrypt_email` | Optional. The contact address for the Let's Encrypt account. |

Everything else derives from these two: the autocert host whitelist, `ISLAND_BATTLES_CENTRIFUGO_PUBLIC_URL`, the API URL, and Centrifugo's allowed origin and proxy endpoints. Other variables are in `deploy/ansible/group_vars/all.yml` and the roles' `defaults/main.yml`, and any of them can be overridden with `-e name=value`.

## First deployment, step by step

The pieces depend on each other in a circle. Terraform creates the Lambda, whose Function URL the VM deploy needs for Centrifugo. The VM deploy writes `deploy/local/lambda.env`, which the Lambda needs. The first apply therefore creates the function without its settings, and the backend deploy publishes them once the VM is up:

1. **Check the account's Lambda concurrency limit.** In the Service Quotas console, look up AWS Lambda's "Concurrent executions" for your region. If it's below 1000, read [The concurrency cap](#the-concurrency-cap) first.
2. **Write the settings.**
   - Copy `deploy/aws.example.tfvars` to `deploy/local/aws.tfvars` and set the region, your SSH public key, the instance and `alert_email` ([AWS settings](#aws-settings)).
   - Copy `deploy/vm.example.yml` to `deploy/local/vm.yml` and set `deploy_domain` ([Settings](#settings)). The Function URL comes in step 4.
3. **Create the AWS resources:**

   ```sh
   scripts/aws.sh apply -var allow_missing_lambda_settings=true
   ```

   It builds the backend and shows the plan: the instance, its firewall, the static IP and its attachment, the key pair, the Lambda's role, the function and its Function URL, and its log group, metric filter, alarms, alert topic and zero-spend budget ([Logs, alerts and traces](#logs-alerts-and-traces)). Type `yes`. There's no `lambda.env` yet, so it skips the migrations, and the function fails every request until step 7.

   SNS then emails `alert_email` a confirmation link. Follow it, or the alarms send nothing.
4. **Feed the outputs to the VM deploy.** `scripts/aws.sh output` prints the static IP and the Function URL. Put `function_url` into `deploy/local/vm.yml` as `lambda_function_url`.
5. **Add the DNS record** `vm.<domain>` for the static IP, and wait until it resolves ([DNS](#dns)).
6. **Deploy the VM.** Give the new instance a minute to boot first.

   ```sh
   scripts/vm.sh deploy "$(scripts/aws.sh output static_ip)" -e app_generate_secrets=true
   ```

   It generates the [secrets](#secrets) and writes `deploy/local/lambda.env` ([First VM deploy](#first-vm-deploy)).
7. **Deploy the backend:** `scripts/aws.sh deploy`. Its plan only updates the function's settings. The migrations create the schema, then the function gets its settings ([Deploying the backend](#deploying-the-backend)).
8. **Run the smoke test:** `scripts/vm.sh smoke` ([Smoke test](#smoke-test)).
9. **Back up `deploy/local/`:** the secrets, Terraform's state, `aws.tfvars`, `vm.yml` and `lambda.env`.

## AWS resources

`deploy/terraform/` creates everything in AWS (#32). The DNS record is the only manual step.

| Resource | Details |
|---|---|
| Lightsail instance | Ubuntu 24.04 LTS on the 1 GB plan (`micro_3_0`), IPv4 only, with daily [automatic snapshots](#backups). |
| Static IP | Attached to the instance named by `static_ip_instance`. Terraform refuses to destroy it, because `vm.<domain>` points at it. |
| Firewall | 22 from `ssh_allowed_cidrs`, and 443 and 5432 from anywhere. Nothing else, not even 80. |
| Key pair | Imported from `ssh_public_key`. Without it, Lightsail uses the region's default key, which you download from the Lightsail console. |
| Lambda | The `provided.al2023` runtime on arm64, 512 MB, a 30-second timeout, a reserved concurrency of 5, JSON logs filtered at WARN, Active tracing off, and the OpenTelemetry collector layer. |
| IAM role | Basic execution and X-Ray writes: the function writes its logs, its collector layer sends traces to X-Ray, and it calls nothing else in AWS. |
| Function URL | Auth type `NONE` and the `RESPONSE_STREAM` invoke mode, which the backend's `lambdaurl` host needs. The provider adds the public invoke permissions. |
| Log group | `/aws/lambda/<name>`, kept for 7 days. |
| Metric filter and alarms | One metric that counts error lines, and two alarms, on it and on the function's `Errors`, that email `alert_email` through an SNS topic. |
| Budget | A zero-spend cost budget that emails `alert_email`. |

There's no CloudFront, ACM certificate, VPC, peering or EventBridge. [Logs, alerts and traces](#logs-alerts-and-traces) explains the last three rows, and why they stay inside AWS's free tier.

### AWS settings

Copy `deploy/aws.example.tfvars` to `deploy/local/aws.tfvars` and fill in `region`, `ssh_public_key`, `vm_instances`, `static_ip_instance` and `alert_email`. `alert_email` stays in that gitignored file: never commit it. `deploy/terraform/variables.tf` describes every other variable, all of which have defaults. The instances have no default, so a lost `aws.tfvars` can't plan to replace the instance that runs with a default one. Keep it in step with the instances, for example after a move. If Lightsail rejects your key, use an RSA key: `ssh-keygen -t rsa -b 4096`.

### State

Terraform's state is a local file, `deploy/local/terraform/terraform.tfstate`, gitignored like the rest of `deploy/local/`. `.gitignore` also covers `.terraform/` and `*.tfstate*` anywhere else.

- **The state holds the Lambda's secrets:** its environment, with the database password, Centrifugo's API key, the connection token secret and the proxy secret. Treat it like `deploy/local/secrets/`: back it up with them, and never commit it.
- **Apply from one machine at a time.** A local state has no locking. Copy the state along with `deploy/local/` when you switch machines or worktrees.
- **If the state is lost,** the resources keep running, but Terraform no longer knows them. Bring each back with `terraform import` before applying again, or Terraform tries to create them a second time.

### The concurrency cap

Every Lambda instance has its own database pool, so the reserved concurrency caps the backend's Postgres connections ([backend/README.md](../backend/README.md#the-connection-pool)). Terraform checks that **`reserved_concurrency` × `database_max_open_connections` ≤ `postgres_max_connections` − `postgres_spare_connections`**. It also sets `ISLAND_BATTLES_DATABASE_MAX_OPEN_CONNECTIONS` itself, so the two can't drift apart.

The same cap is C in the [free-tier budget](#the-budget): every instance logs and traces up to its own limits. Terraform passes the cap to the backend as `ISLAND_BATTLES_CONCURRENCY` and checks the budget too, so a bigger cap needs lower log and trace limits.

- The defaults use at most 5 × 2 = 10 of Postgres's 100 connections. They leave at least 10 free: 3 that Postgres reserves for superusers, 2 for the migrate command, and the rest for administration. A bigger cap still fits under `max_connections`, but every connection costs memory on the 1 GB VM.
- **Accounts with a low concurrency limit:** Lambda always keeps part of the account's concurrency unreserved, 100 on a normal account. On an account limited to 10, that's all of it, and any reserved concurrency fails with `decreases account's UnreservedConcurrentExecution below its minimum value`. Either request an increase of "Concurrent executions" in Service Quotas, which is free, or set `reserved_concurrency = null`.
- **Without a reservation,** the account limit is the cap, so Terraform checks **`account_concurrency_limit` × `database_max_open_connections`** instead, and the budget for that many instances. Set `account_concurrency_limit` in `aws.tfvars` to the limit Service Quotas shows. It defaults to 10, which gives 10 × 2 = 20 connections.
  - The default log and trace limits fit 5 instances, not 10. For 10, also set `log_lines_per_minute = 2` and `trace_interval_seconds = 360`: 1.2 GB of logs and 74,400 traces at most.
  - **Recheck the limit before every apply.** AWS raises a new account's limit to 1000 on its own after some use, and a limit of 1000 lets 2,000 connections at Postgres.
  - Once it's raised, set `account_concurrency_limit` to the new value. The checks then fail, which tells you to reserve concurrency: set `reserved_concurrency` again, for example back to 5.
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
   - `db` is `postgres:18-alpine` with a named volume. It publishes 5432 with TLS, using a self-signed certificate, and its `pg_hba.conf` allows only `hostssl` scram logins by the backend's role to its own database. The `postgres` superuser only connects through the container's local socket. TCP keepalives close a connection about two minutes after its client stops answering, such as a frozen Lambda environment ([The concurrency cap](#the-concurrency-cap)).
   - `centrifugo` is `centrifugo/centrifugo:v6.9.6`. Host port 443 maps to its public port, which serves TLS with an autocert certificate for `vm.<domain>`, the client endpoints and the HTTP API (`http_api.external`). Its health endpoint listens only inside the container, for the healthcheck. The admin UI is off. The `lobby` and `player` namespaces keep history and force recovery, so reconnecting clients receive what they missed. The proxy timeout is 10 seconds, to allow for a Lambda cold start on top of the backend's push retry. The certificate cache is a named volume.
   - Both services rotate their Docker logs at 10 MB, keeping five files.
4. **Creates** the backend's role and its database. The role owns the database but isn't a superuser.
   - Owning the database gives the role `CREATE` on it. The migration runner (#23) needs that: on a database without migration history, it creates a scratch schema in a transaction to check migration 1, then rolls it back.
   - The deploy creates the database with the role as its owner, and a restore recreates it the same way.
   - The VM doesn't create the schema. The migrate step in the Lambda deploy does (#29, #38), connecting as this role.
5. **Checks** that the role logs in over TLS and is refused without it. It also checks that `https://vm.<domain>/api/info` answers with a valid certificate when called with the API key, and refuses calls without it. That first TLS connection also makes Centrifugo request its certificate.
6. **Writes** `deploy/local/lambda.env`: the settings the backend Lambda needs to reach the VM.

Then [deploy the backend](#deploying-the-backend) and run the [smoke test](#smoke-test).

### Secrets

The first deploy generates these into `deploy/local/secrets/`. Every later deploy and every new VM reuses them:

- The two Postgres passwords.
- Centrifugo's API key, the connection token secret and the lobby proxy secret. Reusing them keeps issued tokens valid when you move to a new VM.
- The backend's admin API key, `admin_api_key`. Only the Lambda reads it, from `lambda.env`. Send it in the `X-Admin-API-Key` header to create default maps with `POST /map/create` (see [map files](../backend/README.md#map-files)). Without it, the Lambda refuses every map creation.
- Postgres's TLS key and its self-signed certificate.

Back up `deploy/local/secrets/` somewhere safe, such as a password manager, and copy it into every checkout or worktree you deploy from.

A deploy stops if any of these files is missing, because new secrets would replace the live VM's password and keys. Pass `-e app_generate_secrets=true` only for a first deploy, or if the secrets are lost for good. In that case the deploy resets the backend's password and Centrifugo's keys, and you redeploy the backend with the new `lambda.env`: `scripts/aws.sh deploy`. Players then need new connection tokens, which clients fetch on their own.

`admin_api_key` was added after the first secrets (#37), so an existing `deploy/local/secrets/` without it fails this check. Run one deploy with `-e app_generate_secrets=true`: it generates only the missing files and reads the others unchanged, so no password or key on the VM changes. Then redeploy the Lambda with the new `lambda.env`.

Nothing secret is committed. Never copy anything from `deploy/local/` into the repo.

### The Lambda's settings

`deploy/local/lambda.env` holds the seven settings the backend reads from the VM deploy, including the admin API key (see `backend/dependencies/config.go`). [`scripts/aws.sh deploy`](#deploying-the-backend) passes every line of it to the Lambda:

| Variable | Value |
|---|---|
| `ISLAND_BATTLES_DATABASE_URL` | `postgres://island_battles:<password>@vm.<domain>:5432/island_battles?sslmode=require` |
| `ISLAND_BATTLES_CENTRIFUGO_PUBLIC_URL` | `wss://vm.<domain>/connection/websocket` |
| `ISLAND_BATTLES_CENTRIFUGO_API_URL` | `https://vm.<domain>/api` |
| `ISLAND_BATTLES_CENTRIFUGO_API_KEY` | Centrifugo's API key |
| `ISLAND_BATTLES_CONNECTION_TOKEN_HMAC_SECRET` | The connection token secret |
| `ISLAND_BATTLES_LOBBY_PROXY_SECRET` | The secret Centrifugo sends to the subscribe proxy |
| `ISLAND_BATTLES_ADMIN_API_KEY` | The admin API key that `POST /map/create` requires |

The URLs use the hostname, not an IP, so they stay the same when you move to a new VM.

`sslmode=require` encrypts the connection but doesn't check the server's certificate. To check it, ship `deploy/local/secrets/postgres-tls/server.crt` with the Lambda and use `sslmode=verify-full&sslrootcert=<path>`. The certificate is issued for `vm.<domain>` and is reused on new VMs.

## Deploying the backend

One command publishes a new backend version (#38):

```sh
scripts/aws.sh deploy
```

1. **Builds** `deploy/local/lambda/bootstrap` with the build command from [backend/README.md](../backend/README.md#hosts-and-deploys). Terraform zips it with `archive_file`.
2. **Plans** the Terraform configuration with `deploy/local/aws.tfvars` and `deploy/local/lambda.env`.
3. **Checks the plan** with `deploy/check_deploy_plan.py`, which allows only creating or updating resources outside Lightsail. A plan that changes the VM, its firewall or its static IP, or that deletes or replaces anything, such as the Function URL, stops the deploy before the migrations. Review those changes with `scripts/aws.sh apply` instead.
4. **Migrates the database** from your machine: `go run ./backend/cmd/migrate`, with `ISLAND_BATTLES_DATABASE_URL` from `lambda.env`, straight to Postgres over TLS. The old code serves requests against the new schema until the next step finishes.
5. **Applies the checked plan:** the new code and settings. Then it prints the Function URL.

It doesn't ask for confirmation, never connects to the VM and never changes a Lightsail resource. The plan file holds the settings, so the script deletes it afterwards.

**Settings:** every `KEY=value` line of `lambda.env` becomes one of the function's environment variables, so a new setting that the VM deploy writes needs no Terraform change. Values are taken literally, without quotes. Plans show the environment only as `(sensitive value)`.

- Run `scripts/aws.sh deploy` again whenever the VM deploy rewrites `lambda.env`, for example after new secrets.
- `ISLAND_BATTLES_DATABASE_MAX_OPEN_CONNECTIONS` comes from the `database_max_open_connections` variable instead, and the log and trace limits from `log_lines_per_minute`, `log_line_bytes`, `trace_interval_seconds` and the concurrency cap. Terraform refuses them in `lambda.env` and `extra_lambda_environment` ([The concurrency cap](#the-concurrency-cap), [The budget](#the-budget)). It also sets the collector's settings and `OTEL_EXPORTER_OTLP_ENDPOINT`.
- Non-secret settings of your own, such as the pool's idle time, go in `extra_lambda_environment` in `aws.tfvars`.
- The Lambda needs no game retention setting: games are deleted 7 days after their last turn by default. To change the period, set `ISLAND_BATTLES_GAME_RETENTION_PERIOD` there as a Go duration, such as `336h` for 14 days ([Game retention](../backend/README.md#game-retention)).

**Why Terraform publishes the code,** rather than the AWS CLI with Terraform only creating the function:

- **One owner.** If the CLI published the code and settings, Terraform would have to ignore both. The pool size that Terraform checks against the reserved concurrency would then be set by another tool, and nothing would check the two together.
- **One tool.** A deploy needs Terraform, Go and Python, which the rest of the deployment already uses, and no AWS CLI.
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

- **Read the plan.** Changing an instance's zone, image or plan replaces it, and replacing an instance deletes its disk and its automatic snapshots. Add a new instance and [move to it](#moving-to-a-new-vm) instead.
- **Destroying needs the name.** When the plan deletes or replaces an instance, the static IP or a disk, the apply lists them and asks you to type each one's name before the usual `yes`. A wrong answer applies nothing. If you didn't mean to destroy it, `aws.tfvars` probably doesn't match the instances that run. Without a terminal, list the names in `ISLAND_BATTLES_ALLOW_DESTROY`, separated by commas.
- **It may publish the backend too.** When the plan changes the function, the apply first runs the migrations and then publishes the backend built from your checkout, so run it from the commit you last deployed or a newer one. A plan that leaves the function alone, such as moving the static IP, runs no migrations: in the middle of a move, the old VM refuses the backend.

## Smoke test

```sh
scripts/vm.sh smoke
```

It reads the Function URL from your settings and checks the whole path:

1. `/healthz` on the Function URL returns 204.
2. A guest credential works, and the backend issues a connection token for it, which needs Postgres.
3. Centrifugo accepts the token at `wss://vm.<domain>/connection/websocket`, with a valid certificate and the Function URL's origin.
4. Centrifugo subscribes the guest to its personal channel through the backend's subscribe proxy, which needs the proxy secret and the Function URL.

The guest's credential is kept in `deploy/local/smoke-session.json`, and later runs reuse it. After a move to a new VM, that confirms the data came along. Guest credentials expire after 24 hours, and the test replaces one that expires within two hours. If the saved credential is rejected for another reason, delete the file and run the test again.

## Backups

Backups are Lightsail's automatic snapshots (#33). Nothing is uploaded anywhere else.

- **What:** Terraform turns on a daily snapshot of the whole instance at `snapshot_time`, 01:00 UTC by default. Lightsail keeps the last seven. Postgres in a snapshot is crash-consistent, and recovers from it as from a power loss.
- **Automatic snapshots are deleted with their instance,** unless you copy them to manual snapshots first. In the Lightsail console, open the instance's **Snapshots** tab and choose **Keep snapshot** on an automatic snapshot to copy it. Manual snapshots stay until you delete them, and are billed for storage like the automatic ones. Keep the latest one before you delete an instance, as in step 7 of [Moving to a new VM](#moving-to-a-new-vm).
- **What they don't cover:** they live in the same account and region as the instance, and a restore loses everything written since the snapshot, up to a day.
- **Database dumps** are separate, for moving to a new VM: see [Dumps on their own](#dumps-on-their-own).

### Restoring from a snapshot

1. In the Lightsail console, create a new instance from the snapshot, with a new name such as `island-battles-2` and the same plan or a bigger one.
2. Add it to `vm_instances` in `deploy/local/aws.tfvars`, with the zone and plan you chose, and import it:

   ```sh
   terraform -chdir=deploy/terraform import -var-file=../local/aws.tfvars 'aws_lightsail_instance.vm["island-battles-2"]' island-battles-2
   ```

3. Run `scripts/aws.sh apply`. It gives the instance its firewall rules and turns on its automatic snapshots. If the plan wants to replace the imported instance, answer no, and change its `vm_instances` entry until it doesn't.
4. Continue with steps 5 and 6 of [Moving to a new VM](#moving-to-a-new-vm).

## Moving to a new VM

Moving copies the database with `pg_dump` and restores it on the new VM (#31). It takes a few minutes of downtime: from the moment the data is copied until the static IP points at the new VM, the backend can't reach a database. Backups are separate: they're Lightsail's [automatic snapshots](#backups) (#33).

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

   - On the old VM, it locks the backend's role out (`NOLOGIN`) and ends its connections, so nothing is written after the dump. The Lambda's requests fail from here until step 5 is done.
   - The lock is sticky. It leaves the marker file `/opt/island-battles/moved-away` on the old VM, and a deploy or restore there stops while the marker exists. A redeploy can't let the backend write to the old VM again, which would lose those writes.
   - It dumps the old database to `deploy/local/dumps/` and restores the dump into the new VM's empty database, in one transaction.
   - The restore refuses a dump without tables, and a database that already has tables. To overwrite one, pass `-e app_restore_replace_existing=true`: the restore first saves the database it replaces to `deploy/local/dumps/`.
   - **If the restore fails,** the old VM stays locked and the new database stays empty. Fix the cause and run the same `move-data` command again. To give up on the move instead, unlock the old VM: `scripts/vm.sh deploy <static IP> -e app_unlock_after_move=true`.
5. **Move the static IP** to the new instance: set `static_ip_instance = "island-battles-2"` in `aws.tfvars` and run `scripts/aws.sh apply`. The plan replaces only the static IP's attachment. The DNS record and the Lambda's settings don't change, because both use `vm.<domain>`. The Lambda's next connections reach the new VM.
6. **Check the new VM:**

   ```sh
   ssh-keygen -R <static IP>    # the new VM has a new host key
   scripts/vm.sh deploy <static IP>
   scripts/vm.sh smoke
   ```

   The deploy changes nothing, but it now runs the TLS check, which also gets the new VM's certificate. The smoke test should report that the saved credential still works. The restored schema is the old VM's. If the backend deploy brings newer migrations, its migrate step updates it.
7. **Delete the old instance** once you're satisfied. First keep its latest automatic snapshot as a manual one, because deleting the instance deletes its automatic snapshots ([Backups](#backups)). Then remove it from `vm_instances` and run `scripts/aws.sh apply`. The plan deletes the instance and its firewall rules, and the apply asks you to type the instance's name. To go back to the old instance instead, set `static_ip_instance` back, run `scripts/aws.sh apply`, and run `scripts/vm.sh deploy <static IP> -e app_unlock_after_move=true`. That lets the backend's role log in again and removes the marker. Anything written to the new VM in the meantime isn't copied back.

If the old VM is gone, there's nothing to dump. [Restore the latest snapshot](#restoring-from-a-snapshot) instead, then run steps 5 and 6.

### Dumps on their own

```sh
scripts/vm.sh dump <IP>                  # writes deploy/local/dumps/island_battles-<time>.dump
scripts/vm.sh restore <IP> <dump file>   # into an empty database
```

Dumps use `pg_dump`'s custom format, without owners or grants. The restore makes the backend's role the owner of everything. Dumps hold player data, so keep them in `deploy/local/`.

## Logs, alerts and traces

The backend Lambda's logs, error alarms and traces stay inside AWS's free monthly allowances, worst case included (#46). AWS has no hard spending cap, so every source is bounded by the backend's code and by Terraform, for the function at its concurrency cap for a whole month.

- **Logs** go to `/aws/lambda/<name>`, kept for 7 days. Lambda filters application and system logs at WARN. The backend writes nothing below WARN anyway, and each instance writes at most N lines a minute of at most L bytes ([backend/README.md](../backend/README.md#logs-and-traces)).
- **Errors:** every 5xx answer and every recovered panic is one ERROR line with the operation, the error chain and the IDs. One metric filter counts ERROR lines. Two alarms, on that metric and on the function's `Errors`, email `alert_email` through an SNS topic when they enter ALARM.
- **Traces:** the backend traces each request, database query and Centrifugo API call with the OpenTelemetry Go SDK. Each instance starts at most one trace every T seconds. It hands the spans over localhost to the collector layer, which sends them to X-Ray. Lambda's own Active tracing stays off.
- **Tripwire:** a zero-spend AWS Budgets cost budget of $0.01 emails `alert_email` once anything costs money. Budgets refreshes its data about three times a day, so a charge is reported within a day. It stops nothing; the limits do.
- **The VM's logs:** Docker rotates the `db` and `centrifugo` logs at 10 MB, five files each (#27). Read them over SSH with `sudo docker compose logs` in `/opt/island-battles`. They never leave the VM and cost nothing.

### Never turn these on

Each is billed outside the limits below, and nothing in `deploy/terraform/` turns them on:

- Lambda Active tracing (`tracing_config` stays `PassThrough`, which the Terraform tests check)
- Lambda Insights (its layer is a second layer; the tests check the function has only the collector)
- CloudWatch Transaction Search, an account-wide X-Ray setting
- Application Signals
- CloudWatch RUM and Synthetics
- Contributor Insights
- Detailed monitoring

### Free allowances

Checked on 2026-10-29. Accounts created after 15 July 2025 get credits instead of the old 12-month free tier, but the "always free" allowances below apply to every account, on the free and the paid plan ([AWS Free Tier](https://docs.aws.amazon.com/awsaccountbilling/latest/aboutv2/free-tier.html)).

| Service | Free each month | Source |
|---|---|---|
| CloudWatch Logs | 5 GB of ingestion, archive storage and Logs Insights scans together. 1,800 minutes of Live Tail. | [CloudWatch pricing](https://aws.amazon.com/cloudwatch/pricing/) |
| CloudWatch metrics | 10 custom metrics (custom and detailed monitoring together). Lambda's own metrics, such as `Errors`, are free. | [CloudWatch pricing](https://aws.amazon.com/cloudwatch/pricing/) |
| CloudWatch alarms | 10 standard-resolution alarm metrics. | [CloudWatch pricing](https://aws.amazon.com/cloudwatch/pricing/) |
| X-Ray | 100,000 traces recorded, and 1,000,000 traces retrieved or scanned. | [CloudWatch pricing](https://aws.amazon.com/cloudwatch/pricing/), which [X-Ray pricing](https://aws.amazon.com/xray/pricing/) now points to |
| SNS | 1,000 email notifications, 1 million requests. | [SNS FAQ](https://aws.amazon.com/sns/faqs/) |
| AWS Budgets | Budgets and their notifications are free. Only action-enabled budgets past the first two, and emailed reports, cost money. | [Budgets pricing](https://aws.amazon.com/aws-cost-management/aws-budgets/pricing/) |

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
| The collector layer's own lines | It logs errors only (`service.telemetry.logs.level: error` and `OPENTELEMETRY_EXTENSION_LOG_LEVEL=error`), and only exports when a sampled trace ends. Counted at 2 lines of 1 KB per trace. | 74,400 × 2 KB = 0.15 GB |
| Storage | 7 days' retention keeps at most 7/31 of a month's ingestion | 0.35 GB |
| **Total of the bounded sources** | | **1.87 GB of 5 GB** |
| Logs Insights scans and failed invocations | The other 3.1 GB. A query over the whole log group scans at most its 7 days, about 0.35 GB. A crash loop of one failed invocation a second all month comes to 2.7 GB. The `Errors` alarm emails within the hour of the first. | 3.1 GB left |
| Custom metrics | The error-line metric filter, without dimensions. The SDK's and GORM plugin's metrics aren't exported, and the collector has no metrics pipeline. | 1 of 10 |
| Alarms | The two alarms | 2 of 10 |
| Alarm emails | Each alarm looks at one hour and notifies only on entering ALARM, after at least an hour in OK, so at most once every 2 hours: 372 each | 744 of 1,000 |
| Budget emails | Sent by Budgets, not SNS | free |
| Traces recorded | Each instance starts at most one every T, and its first T after it starts: **C × 2,678,400 / T ≤ 80,000**, checked by Terraform and at startup | 74,400 of 100,000 |
| Traces retrieved or scanned | Opening traces in the console, by hand | far under 1,000,000 |

Two assumptions sit under the per-instance limits. Each Lambda instance is one process with its own limits, and the concurrency cap bounds how many run at once. An instance that replaces another, such as after a crash, starts with a fresh minute of N lines but no trace until it has run for T. The 3.1 GB of headroom also covers that.

Lambda's requests and compute are outside this budget. The developer accepted the cost risk of the public Function URL (#26).

### The collector layer

The function runs the [AWS Distro for OpenTelemetry (ADOT)](https://aws-otel.github.io/docs/getting-started/lambda/lambda-go) collector layer, researched on 2026-10-29:

- **Why ADOT:** its build has the `awsxray` exporter, which sends spans to X-Ray's `PutTraceSegments` API, billed per trace recorded. The upstream [opentelemetry-lambda](https://github.com/open-telemetry/opentelemetry-lambda) collector layer (0.24.0 of 2026-10-22, `arn:aws:lambda:<region>:184161586896:layer:opentelemetry-collector-arm64-0_24_0:1`) has no `awsxray` exporter. It could only reach X-Ray through X-Ray's OTLP endpoint, which needs Transaction Search on. AWS's docs now call collector layers the legacy approach and recommend Application Signals layers, which have no Go version and need Application Signals.
- **ARN pattern:** `arn:aws:lambda:<region>:901920570463:layer:aws-otel-collector-arm64-ver-<collector version, dashed>:<layer version>`. Terraform uses `aws-otel-collector-arm64-ver-0-156-0:4`: ADOT Collector for Lambda v0.49.0, built on OpenTelemetry Collector v0.156.0 ([CHANGELOG](https://github.com/aws-observability/aws-otel-lambda/blob/main/CHANGELOG.md), 2026-08-25). Override it with `collector_layer_arn`.
- **Configuration:** Terraform passes it in the environment, `OPENTELEMETRY_COLLECTOR_CONFIG_URI=env:ISLAND_BATTLES_COLLECTOR_CONFIG`, so the zip holds only `bootstrap`: an OTLP/HTTP receiver on `localhost:4318`, the `awsxray` exporter in the function's region, and error-only logs.
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

1. Follow the SNS confirmation link after the first apply.
2. **An email from a failing request:** on the VM, stop Postgres for a minute: `cd /opt/island-battles && sudo docker compose stop db`. Call `POST /lobby/v1/sessions` on the Function URL, which then answers 5xx, and start Postgres again with `sudo docker compose start db`. The `<name>-error-lines` alarm emails within about an hour.
3. **A trace:** stop Centrifugo instead, `sudo docker compose stop centrifugo`. Create a guest session with `POST /lobby/v1/sessions`, wait 3 minutes (T) without other requests, and then create a lobby with `POST /lobby/v1/lobbies` (see `/docs`). The instance samples it, its push to Centrifugo fails, and its `Centrifugo push failed` error line carries `xray_trace_id`. The trace in X-Ray holds the request's span, its database spans and its Centrifugo API spans. If the line has no trace ID, the request reached an instance younger than T: wait 3 minutes and create another lobby. Start Centrifugo again.
4. Check in the CloudWatch console that Transaction Search is off (Settings → X-Ray traces), and in the Billing console that the zero-spend budget exists.

The collector reads its configuration through the `env:` config provider, which the ADOT build includes but which hasn't run on a real function yet. If the function fails to start because the collector extension does, set `collector_layer_arn = ""` in `aws.tfvars` and apply: the function then runs without the layer and without tracing, until the configuration is fixed.

## Troubleshooting

- **`Missing secrets: ...`:** copy `deploy/local/secrets/` from your backup. See [Secrets](#secrets).
- **`<IP> was moved away from`:** you're deploying the VM a move left behind. Check the IP. See step 7 of [Moving to a new VM](#moving-to-a-new-vm) to really go back to it.
- **`vm.<domain> resolves to [...], not <IP>`:** fix the A record and wait for it to resolve, or pass `-e vm_static_ip=<static IP>` when preparing a new VM.
- **The TLS check fails:**
  - Check that the Lightsail firewall opens 443 and that CAA records allow `letsencrypt.org`.
  - Read Centrifugo's log on the VM: `cd /opt/island-battles && sudo docker compose logs centrifugo`.
  - After repeated failures, Let's Encrypt blocks new attempts for about an hour.
- **`REMOTE HOST IDENTIFICATION HAS CHANGED`:** the static IP now points at a different instance. Run `ssh-keygen -R <static IP>`.
- **`This plan changes more than the backend`:** `scripts/aws.sh deploy` found changes to Lightsail resources, or a deletion, in the plan, and stopped before migrating. Run `scripts/aws.sh apply`, read the plan, and deploy again once it's applied.
- **The migrations or the apply fail during `scripts/aws.sh deploy`:** fix the cause, for example the database being unreachable, and run `scripts/aws.sh deploy` again. Migrations that already ran are skipped. If the apply failed after the migrations, the old code keeps serving against the migrated schema until a deploy succeeds.
- **`lambda.env is missing`** from Terraform or `scripts/aws.sh`: copy `deploy/local/` from your backup, or run the VM deploy, which writes it. Only the very first apply passes `-var allow_missing_lambda_settings=true`.
- **`decreases account's UnreservedConcurrentExecution below its minimum value`:** the account's concurrency limit is too low. See [The concurrency cap](#the-concurrency-cap).
- **The Function URL answers `Internal Server Error`:** read the function's log, the CloudWatch log group `/aws/lambda/<name>`, `/aws/lambda/island-battles` by default, as [Reading the logs](#reading-the-logs) describes. Right after the first apply, the function has no settings yet: run `scripts/aws.sh deploy`. A `Forbidden` means the Function URL's public permissions are missing.
- **The Function URL answers `the backend failed to start` (503):** the log's `Backend failed to start` line says why, such as missing settings, an unreachable database or limits over the budget. The instance tries again every 10 seconds.
- **`... over the free-tier budget`** from Terraform: the concurrency cap and the log or trace limits together could leave the free tier. Lower one of them ([The budget](#the-budget)).
- **`Terraform sets ... itself`:** `lambda.env` or `extra_lambda_environment` sets the pool size, a log or trace limit, or a collector setting. Use the Terraform variables instead.
- **No alarm emails:** follow the link in SNS's confirmation email, or resend it from the SNS console's subscription page.
- **Postgres from your machine:** `psql "host=vm.<domain> dbname=island_battles user=island_battles sslmode=require"`, with the password from `deploy/local/secrets/postgres_app_password`.

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
python3 -I -m unittest discover -s deploy -p 'test_*.py'   # the plan checks, on synthetic plans
```
