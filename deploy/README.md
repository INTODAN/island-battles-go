# Deployment

Island Battles runs in one AWS region:

- **Backend:** the Go backend runs on AWS Lambda behind its Function URL. Players open the lobby UI at the Function URL itself. There is no custom domain, CloudFront or ACM certificate.
- **VM:** a Lightsail instance runs Postgres and Centrifugo in Docker Compose. Nothing else runs on it.
- **Network:** there is no VPC or peering. The Lambda reaches Postgres and Centrifugo's HTTP API over the internet:
  - Postgres accepts only TLS (`hostssl`) logins, with scram and a generated password.
  - Centrifugo's API needs TLS and its API key.
- **DNS:** one record, `vm.<domain>`, points at the VM's static IP. Centrifugo gets a Let's Encrypt certificate for it, and browsers connect to `wss://vm.<domain>/connection/websocket`.

This document covers the VM: setting it up with Ansible, redeploying it and moving to a new VM. #26 describes the architecture.

## Files

| Path | What it is |
|---|---|
| `scripts/vm.sh` | The entry point for everything below. |
| `deploy/vm.example.yml` | The settings template. |
| `deploy/ansible/` | The playbooks: `site.yml` deploys, `dump.yml` and `restore.yml` copy the database. The `base` role prepares Ubuntu, and the `app` role deploys the Compose project. |
| `deploy/smoke.py` | The smoke test that `scripts/vm.sh smoke` runs. |
| `deploy/local/` | Gitignored. Your settings, the generated secrets, the Lambda's settings, dumps and the smoke test's guest credential. |

## Prerequisites

- **On your machine:**
  - Ansible: `uv tool install ansible-core`. `scripts/vm.sh` installs the collections it needs into `deploy/ansible/.collections/` (gitignored) on first use.
  - [uv](https://docs.astral.sh/uv/), for the smoke test.
  - The instance's SSH key, through `ssh-agent`, `~/.ssh/config` or `ansible_ssh_private_key_file` in the settings.
- **Lightsail** (#32 creates these with Terraform):
  - An Ubuntu 24.04 LTS instance. The 1 GB plan is enough. Both images publish amd64 and arm64 builds.
  - A static IP attached to it.
  - Firewall rules for 22, 443 and 5432. Limit 22 to your own IP if you can. Port 80 stays closed, because Centrifugo answers Let's Encrypt's challenge on 443.
  - Ansible doesn't set up a host firewall: ports that Docker publishes bypass ufw, so the Lightsail firewall is the one that counts.
- **The Lambda's Function URL** (#38). Centrifugo calls the backend's subscribe proxy through it, and accepts browser connections only from its origin.
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
| `lambda_function_url` | The backend Lambda's Function URL, e.g. `https://<id>.lambda-url.<region>.on.aws/`. |
| `ansible_ssh_private_key_file` | Optional. The instance's SSH key. |
| `app_letsencrypt_email` | Optional. The contact address for the Let's Encrypt account. |

Everything else derives from these two: the autocert host whitelist, `ISLAND_BATTLES_CENTRIFUGO_PUBLIC_URL`, the API URL, and Centrifugo's allowed origin and proxy endpoints. Other variables are in `deploy/ansible/group_vars/all.yml` and the roles' `defaults/main.yml`, and any of them can be overridden with `-e name=value`.

## First deploy

```sh
scripts/vm.sh deploy <static IP> -e app_generate_secrets=true
```

`app_generate_secrets=true` lets this first deploy create the [secrets](#secrets). Later deploys don't need it.

The deploy logs in as `ubuntu`, which Lightsail's Ubuntu images use, and:

1. **Checks** the settings, that the secrets exist, the Ubuntu version and the DNS record. It also refuses a VM that a move left behind (see [Moving to a new VM](#moving-to-a-new-vm)).
2. **Base role:** installs Docker and the Compose plugin from Ubuntu's archive, enables unattended security upgrades, keeps containers running while the Docker daemon restarts, adds a 1 GB swap file, and creates the `deploy` user that owns the Compose project. Your SSH keys log in as `deploy` too.
3. **App role:** writes the Compose project to `/opt/island-battles` and starts it with `docker compose up --wait`:
   - `db` is `postgres:18-alpine` with a named volume. It publishes 5432 with TLS, using a self-signed certificate, and its `pg_hba.conf` allows only `hostssl` scram logins by the backend's role to its own database. The `postgres` superuser only connects through the container's local socket.
   - `centrifugo` is `centrifugo/centrifugo:v6.9.6`. Host port 443 maps to its public port, which serves TLS with an autocert certificate for `vm.<domain>`, the client endpoints and the HTTP API (`http_api.external`). Its health endpoint listens only inside the container, for the healthcheck. The admin UI is off. The `lobby` and `player` namespaces keep history and force recovery, so reconnecting clients receive what they missed. The proxy timeout is 10 seconds, to allow for a Lambda cold start on top of the backend's push retry. The certificate cache is a named volume.
   - Both services rotate their Docker logs at 10 MB, keeping five files.
4. **Creates** the backend's role and its database. The role owns the database but isn't a superuser.
   - Owning the database gives the role `CREATE` on it. The migration runner (#23) needs that: on a database without migration history, it creates a scratch schema in a transaction to check migration 1, then rolls it back.
   - The deploy creates the database with the role as its owner, and a restore recreates it the same way.
   - The VM doesn't create the schema. The migrate step in the Lambda deploy does (#29, #38), connecting as this role.
5. **Checks** that the role logs in over TLS and is refused without it. It also checks that `https://vm.<domain>/api/info` answers with a valid certificate when called with the API key, and refuses calls without it. That first TLS connection also makes Centrifugo request its certificate.
6. **Writes** `deploy/local/lambda.env`: the settings the backend Lambda needs to reach the VM.

Then deploy the Lambda (#38) and run the [smoke test](#smoke-test).

### Secrets

The first deploy generates these into `deploy/local/secrets/`. Every later deploy and every new VM reuses them:

- The two Postgres passwords.
- Centrifugo's API key, the connection token secret and the lobby proxy secret. Reusing them keeps issued tokens valid when you move to a new VM.
- Postgres's TLS key and its self-signed certificate.

Back up `deploy/local/secrets/` somewhere safe, such as a password manager, and copy it into every checkout or worktree you deploy from.

A deploy stops if any of these files is missing, because new secrets would replace the live VM's password and keys. Pass `-e app_generate_secrets=true` only for a first deploy, or if the secrets are lost for good. In that case the deploy resets the backend's password and Centrifugo's keys, and you redeploy the Lambda with the new `lambda.env`. Players then need new connection tokens, which clients fetch on their own.

Nothing secret is committed. Never copy anything from `deploy/local/` into the repo.

### The Lambda's settings

`deploy/local/lambda.env` holds the six VM-related settings the backend reads (see `backend/dependencies/config.go`). The Lambda deploy (#38) reads it:

| Variable | Value |
|---|---|
| `ISLAND_BATTLES_DATABASE_URL` | `postgres://island_battles:<password>@vm.<domain>:5432/island_battles?sslmode=require` |
| `ISLAND_BATTLES_CENTRIFUGO_PUBLIC_URL` | `wss://vm.<domain>/connection/websocket` |
| `ISLAND_BATTLES_CENTRIFUGO_API_URL` | `https://vm.<domain>/api` |
| `ISLAND_BATTLES_CENTRIFUGO_API_KEY` | Centrifugo's API key |
| `ISLAND_BATTLES_CONNECTION_TOKEN_HMAC_SECRET` | The connection token secret |
| `ISLAND_BATTLES_LOBBY_PROXY_SECRET` | The secret Centrifugo sends to the subscribe proxy |

All of them use the hostname, not an IP, so they stay the same when you move to a new VM.

`sslmode=require` encrypts the connection but doesn't check the server's certificate. To check it, ship `deploy/local/secrets/postgres-tls/server.crt` with the Lambda and use `sslmode=verify-full&sslrootcert=<path>`. The certificate is issued for `vm.<domain>` and is reused on new VMs.

## Redeploy

Run the same command again. It's idempotent: when nothing changed, nothing restarts. A changed configuration file recreates only its container. Redeploying the Lambda doesn't touch the VM.

```sh
scripts/vm.sh deploy <static IP>
scripts/vm.sh deploy <static IP> -e app_pull=always   # also pull newer image patch releases
```

A Centrifugo restart drops every WebSocket connection together with its in-memory history. Clients reconnect on their own and re-read their state over HTTP.

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

## Moving to a new VM

Moving copies the database with `pg_dump` and restores it on the new VM (#31). It takes a few minutes of downtime: from the moment the data is copied until the static IP points at the new VM, the backend can't reach a database. Backups are separate: they're Lightsail's automatic snapshots (#33).

1. **Create the new instance** (#32), without moving the static IP yet. Note its temporary public IP. Lightsail's firewall rules belong to each instance, so the new one needs 22, 443 and 5432 too.
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
5. **Move the static IP** to the new instance (#32). The DNS record and the Lambda's settings don't change, because both use `vm.<domain>`. The Lambda's next connections reach the new VM.
6. **Check the new VM:**

   ```sh
   ssh-keygen -R <static IP>    # the new VM has a new host key
   scripts/vm.sh deploy <static IP>
   scripts/vm.sh smoke
   ```

   The deploy changes nothing, but it now runs the TLS check, which also gets the new VM's certificate. The smoke test should report that the saved credential still works. The restored schema is the old VM's. If the Lambda deploy (#38) brings newer migrations, its migrate step updates it.
7. **Delete the old instance** once you're satisfied. To go back to it instead, move the static IP back and run `scripts/vm.sh deploy <static IP> -e app_unlock_after_move=true`. That lets the backend's role log in again and removes the marker. Anything written to the new VM in the meantime isn't copied back.

If the old VM is gone, there's nothing to dump. Create the new instance from its latest Lightsail snapshot instead (#33), then run steps 5 and 6.

### Dumps on their own

```sh
scripts/vm.sh dump <IP>                  # writes deploy/local/dumps/island_battles-<time>.dump
scripts/vm.sh restore <IP> <dump file>   # into an empty database
```

Dumps use `pg_dump`'s custom format, without owners or grants. The restore makes the backend's role the owner of everything. Dumps hold player data, so keep them in `deploy/local/`.

## Troubleshooting

- **`Missing secrets: ...`:** copy `deploy/local/secrets/` from your backup. See [Secrets](#secrets).
- **`<IP> was moved away from`:** you're deploying the VM a move left behind. Check the IP. See step 7 of [Moving to a new VM](#moving-to-a-new-vm) to really go back to it.
- **`vm.<domain> resolves to [...], not <IP>`:** fix the A record and wait for it to resolve, or pass `-e vm_static_ip=<static IP>` when preparing a new VM.
- **The TLS check fails:**
  - Check that the Lightsail firewall opens 443 and that CAA records allow `letsencrypt.org`.
  - Read Centrifugo's log on the VM: `cd /opt/island-battles && sudo docker compose logs centrifugo`.
  - After repeated failures, Let's Encrypt blocks new attempts for about an hour.
- **`REMOTE HOST IDENTIFICATION HAS CHANGED`:** the static IP now points at a different instance. Run `ssh-keygen -R <static IP>`.
- **Postgres from your machine:** `psql "host=vm.<domain> dbname=island_battles user=island_battles sslmode=require"`, with the password from `deploy/local/secrets/postgres_app_password`.

## Validating changes

Never run the playbooks against a host you don't own. To check changes, run these from `deploy/ansible`:

```sh
ansible-lint
ansible-playbook -i 203.0.113.10, --syntax-check site.yml dump.yml restore.yml
```
