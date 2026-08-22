# traefik-stack

One-command installer for [Traefik](https://github.com/traefik/traefik) and [Traefik Manager](https://github.com/chr0nzz/traefik-manager).

`tm` is a CLI that asks what you want to install and how, generates all required config files, starts the services, and manages the install afterwards.

---

## Quick start

```bash
curl -fsSL https://get-traefik.xyzlab.dev | bash
```

Or if you prefer to review the bootstrap before running:

```bash
curl -fsSL https://get-traefik.xyzlab.dev -o setup.sh
chmod +x setup.sh
./setup.sh
```

The bootstrap is a 120-line POSIX sh script that:

- Checks the OS is Linux and maps the architecture (amd64, arm64, armv7)
- Downloads `tm-linux-<arch>` and `SHA256SUMS` from the GitHub release
- Verifies the checksum
- Installs `tm` to `/usr/local/bin` (sudo if needed, `~/.local/bin` without sudo)
- Runs `tm install`, passing through any arguments

| Variable | Effect |
|---|---|
| `TM_VERSION=1.12.0` | Install that release instead of the latest |
| `TM_INSTALL_ONLY=1` | Install `tm` and stop, without running `tm install` |

```bash
curl -fsSL https://get-traefik.xyzlab.dev | TM_VERSION=1.12.0 bash
curl -fsSL https://get-traefik.xyzlab.dev | bash -s -- --mode agent
```

---

## Commands

| Command | What it does |
|---|---|
| `tm install` | Runs the wizard and installs. On an existing install, offers update or reconfigure |
| `tm status` | Mode, directory, services, URLs, health |
| `tm update` | Pulls images, `git pull`, or downloads the agent binary, then restarts |
| `tm logs [service]` | Follows the logs (`--no-follow`, `-n <lines>`) |
| `tm restart`, `tm start`, `tm stop` | Whole install, or one service |
| `tm password` | Prints the temporary password from the logs |
| `tm reconfigure [--section <id>]` | Re-runs the wizard pre-filled, regenerates tm-owned files, restarts (`--list` shows the sections) |
| `tm add crowdsec` | Adds CrowdSec to an existing install |
| `tm doctor` | Checks Docker, ports, DNS, `acme.json`, health endpoints, CrowdSec |
| `tm uninstall` | Stops the services and removes the files tm wrote, keeping any you changed. `--purge` also removes configs, data and volumes |
| `tm version` | Prints the version |
| `tm self-update` | Updates `tm` itself (`--version` picks a release) |
| `tm completion bash\|zsh\|fish` | Shell completion |

Commands find the install from `--dir` or `TM_DIR`, then the current directory, then the installs `tm` already knows about.

---

## Install modes

| Mode | Menu | Installs |
|---|---|---|
| `full` | Traefik + Traefik Manager (full stack) | Traefik and Traefik Manager via Docker Compose |
| `tm-docker` | Traefik Manager only, Docker | Traefik Manager container next to an existing Traefik |
| `tm-native` | Traefik Manager only, Linux service (systemd) | Traefik Manager as a systemd service, no Docker |
| `agent-docker` | Traefik Manager Agent, Docker - Agent only | Agent container next to an existing Traefik |
| `agent-docker-traefik` | Traefik Manager Agent, Docker - Agent + Traefik | Traefik and the agent via Docker Compose |
| `agent-binary` | Traefik Manager Agent, Binary - Agent only | `tma` binary as a systemd service |

`tm install --mode <mode>` skips the menu. `--mode agent` (or `TMA_INSTALL=1`) asks only which agent method. The wizard keeps the same sections and review screen as before and writes nothing until you confirm. Full details per mode: [traefik-manager.xyzlab.dev/traefik-stack](https://traefik-manager.xyzlab.dev/traefik-stack)

---

## Non-interactive

```bash
tm install --answers answers.yml --yes
```

`answers.yml` for the full stack with Let's Encrypt DNS on Cloudflare and CrowdSec. Keys you leave out take their defaults:

```yaml
mode: full
dir: /srv/traefik-stack
domain: example.com
tls:
  method: dns
  provider: cloudflare
  email: admin@example.com
config:
  layout: directory
mounts:
  static_config: true
restart:
  method: proxy
crowdsec:
  mode: install
```

`tm install --dump-answers answers.yml` writes the answers of a wizard run to a file (no secrets) to start from. DNS providers: `cloudflare`, `route53`, `digitalocean`, `namecheap`, `duckdns`, `desec`, or `other` with `lego_provider`, `vars` and `secret_vars` for any other lego provider.

Secrets come from environment variables of the same name (`CF_DNS_API_TOKEN=... tm install --answers answers.yml --yes`) or a `secrets:` map in the answers file. A missing required secret is asked for on a terminal; without one the install stops and lists the names.

| Key | Used by |
|---|---|
| `TMA_API_KEY`, `TRAEFIK_API_PASSWORD`, `GIT_BACKUP_TOKEN` | Agent: API key, Traefik basic auth password, git backup token |
| `CROWDSEC_API_KEY`, `CROWDSEC_MACHINE_PASSWORD` | CrowdSec (generated when installed as part of the stack) |
| `CF_DNS_API_TOKEN`, `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, `DO_AUTH_TOKEN`, `NAMECHEAP_API_KEY`, `DUCKDNS_TOKEN`, `DESEC_TOKEN` | DNS challenge, per provider |

Flags on `tm install`:

| Flag | Effect |
|---|---|
| `--mode` | `full`, `tm-docker`, `tm-native`, `agent-docker`, `agent-docker-traefik`, `agent-binary`, `agent` |
| `--dir` | Install directory (Docker modes) |
| `--yes` | Assumes yes for confirmations, including installing Docker, and skips the firewall pause |
| `--api-key` | Agent API key (`TMA_API_KEY`) |
| `--traefik-url` | Agent: Traefik API URL |
| `--allow-unverified` | Install the `tma` binary from a release that publishes no `SHA256SUMS` |

```bash
tm install --mode agent-docker --api-key <key> --traefik-url http://traefik:8080
```

`--dry-run` renders every file and starts nothing; `--output` picks the directory (default: the install directory):

```bash
tm install --dry-run --answers answers.yml --output ./out
```

---

## Existing installs

Installs made by the old `setup.sh` are adopted automatically:

| Install | Adopted when |
|---|---|
| Docker modes | Any `tm` command run in the install directory, or with `--dir` |
| Linux service, agent binary | Any `tm` command when no Docker install is found, or `tm install` with the same mode, which then offers update or reconfigure |

Secrets stay in the compose file or unit until the first `tm reconfigure`, which moves them to `.env` (or `/etc/traefik-manager-agent/env` for the binary agent).

---

## What gets created

`.env` holds the secrets (mode 600) and is only written when there are any. `.tm/state.yml` is tm's record of the install, without secrets. Configs, backups, `acme.json`, `traefik.yml` and logs are not tm-owned: `reconfigure` leaves them alone and `uninstall` keeps them unless you `--purge`. A file you edited by hand is never overwritten or removed without being offered first.

```
~/traefik-stack/                          full
- docker-compose.yml
- .env
- .tm/state.yml
- traefik/
  - traefik.yml
  - acme.json
  - logs/access.log
  - config/
    - dynamic.yml                         (single file layout)
    - *.yml                               (directory layout)
- traefik-manager/
  - config/
  - backups/
- crowdsec/acquis.yaml                    (CrowdSec install only)

~/traefik-manager/                        tm-docker
- docker-compose.yml
- .env
- .tm/state.yml
- config/dynamic.yml                      (or config directory)
- backups/

/opt/traefik-manager-agent/               agent-docker
- docker-compose.yml
- .env
- .tm/state.yml
- backups/
- crowdsec/acquis.yaml                    (CrowdSec install only)

/opt/traefik-manager-agent/               agent-docker-traefik
- docker-compose.yml
- .env
- .tm/state.yml
- backups/
- traefik/
  - traefik.yml
  - acme.json                             (TLS enabled)
  - logs/access.log
  - config/dynamic.yml
- crowdsec/acquis.yaml                    (CrowdSec install only)
```

The Linux service keeps its record in `/etc/traefik-manager/tm-state.yml`, the binary agent in `/etc/traefik-manager-agent/tm-state.yml`.

---

## Requirements

| Mode | Needs |
|---|---|
| All | Linux, amd64, arm64 or armv7 |
| Docker modes | Docker and Compose. `tm` offers to install them with Docker's official script, and uses `pacman` on Arch, which that script does not support |
| Linux service | Python 3.11+, `git`, `systemd` |
| Agent binary | `systemd` |

**Docker group:** a shell that was open before Docker was installed is not in the `docker` group yet, so `tm` runs docker through `sudo` for the rest of the run and says so. Log out and back in to stop needing it. Every other command detects the same situation on its own.

---

## Documentation

Full setup guide and configuration reference: [traefik-manager.xyzlab.dev/traefik-stack](https://traefik-manager.xyzlab.dev/traefik-stack)

---

## Related

- [Traefik](https://github.com/traefik/traefik) - the reverse proxy
- [Traefik Manager](https://github.com/chr0nzz/traefik-manager) - web UI for managing Traefik
- [Traefik Manager Mobile](https://github.com/chr0nzz/traefik-manager-mobile) - Android companion app

---

## License

GPL-3.0
