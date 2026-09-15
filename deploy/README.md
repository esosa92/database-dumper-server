# Deploying Database Dumper

Target: one VPS with Docker installed (1 vCPU / 1 GB RAM is enough; disk depends
on how many dumps you keep). The VPS needs outbound SSH access to every Magento
host you want to dump.

## Server preparation

As root on a fresh box:

```bash
apt update && apt upgrade -y
curl -fsSL https://get.docker.com | sh
ufw default deny incoming
ufw default allow outgoing
ufw allow 22
ufw allow 80
ufw allow 443
ufw enable
adduser deploy
usermod -aG sudo,docker deploy
rsync --archive --chown=deploy:deploy ~/.ssh /home/deploy/
```

Set `PermitRootLogin no` and `PasswordAuthentication no` in `sshd_config`, run
`sshd -t && systemctl restart ssh`, and confirm you can log in as `deploy`
before closing the root session.

## First deploy

```bash
git clone <repo> /opt/database-dumper
cd /opt/database-dumper/deploy
cp .env.example .env
mkdir -p data dumps ssh
```

Edit `.env`:

| Variable | Value |
|---|---|
| `DUMPER_HOSTNAME` | Hostname Caddy gets a certificate for. With sslip.io that is the VPS IP with dashes: `203-0-113-10.sslip.io`. A real domain pointing at the VPS works the same way. |
| `DUMPER_ADMIN_PASSWORD` | Password of the initial `admin` user. Only used when the database has no users yet. |

### SSH access to the Magento hosts

The app runs `ssh` and `scp` from inside its container. Put in `deploy/ssh/`
what a user on this VPS would need in `~/.ssh` to reach the Magento hosts:

- `config` with the host aliases used in the server configs
- the private keys those aliases reference, without passphrase
- `known_hosts` with every target host already accepted

To populate `known_hosts`:

```bash
ssh-keyscan -H magento-host.example.com >> ssh/known_hosts
```

The directory is mounted read-only and copied into the container at start, so
after changing anything in it run `docker compose restart app`.

Hosts that only accept password auth work with the `SSH Password` field of the
server config; `sshpass` is in the image.

### Server configs

Server configs live in the SQLite database in `deploy/data/`. To seed it from an
existing `dump.json`, copy it to `deploy/data/dump.json` before the first
start; it is imported once, when the `servers` table is empty. If any entry
uses a file path in `ignore_tables` or `only_tables`, copy those files next to
it with the same relative paths.

`local_path` in each server config must be a path inside the container. Use
`/dumps` or a subdirectory of it; that is `deploy/dumps/` on the VPS.

Then:

```bash
docker compose up -d --build
docker compose logs -f app
```

When Caddy has its certificate, open `https://<DUMPER_HOSTNAME>/` and log in
as `admin`.

## Updating

```bash
cd /opt/database-dumper && git pull
cd deploy && docker compose up -d --build
```

Running jobs are killed by the restart and show up as `interrupted`. Schema
changes are applied at startup.

## Users from the command line

The CLI uses the same binary and database:

```bash
docker compose exec app /database-dumper user list
docker compose exec app /database-dumper user create <name> [--admin]
docker compose exec -it app /database-dumper user passwd <name>
docker compose exec app /database-dumper user admin <name> on|off
docker compose exec app /database-dumper user delete <name>
```

`passwd` prompts for the password; to script it set `DUMPER_ADMIN_PASSWORD`:

```bash
docker compose exec -e DUMPER_ADMIN_PASSWORD=newpass app /database-dumper user passwd admin
```

## Backups

- `deploy/data/dumper.db` (plus `-wal` and `-shm` if present): servers, users,
  sessions, job history.
- `deploy/dumps/`: the downloaded dumps. Old ones are never deleted
  automatically; prune by hand or with a cron.
- `deploy/ssh/`: keys and config. Back up separately and carefully.
