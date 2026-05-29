# traefik-stack

One-command installer for [Traefik](https://github.com/traefik/traefik) and [Traefik Manager](https://github.com/chr0nzz/traefik-manager).

An interactive script that asks what you want to install and how, then generates all required config files and starts the services.

---

## Quick start

```bash
curl -fsSL https://get-traefik.xyzlab.dev | bash
```

Or if you prefer to review the script before running:

```bash
curl -fsSL https://get-traefik.xyzlab.dev -o setup.sh
chmod +x setup.sh
./setup.sh
```

---

## Install modes

The script starts by asking what you want to install:

```
What would you like to install?
  1) Traefik + Traefik Manager (full stack)
  2) Traefik Manager only
```

If you choose **Traefik Manager only**, it then asks how to deploy it:

```
Deployment method
  1) Docker
  2) Linux service (systemd)
```

---

## Mode 1 - Traefik + Traefik Manager (full stack)

Installs both via Docker Compose. Best for a fresh server with nothing running yet.

The script walks you through:

- **Deployment type** - external (internet-facing) or internal (LAN / VPN / Tailscale). If external, it shows which ports to open and waits for confirmation.
- **Domain + subdomains** - hostnames for the Traefik dashboard and Traefik Manager.
- **TLS / certificates** - Let's Encrypt HTTP challenge, DNS challenge (Cloudflare, Route 53, DigitalOcean, Namecheap, DuckDNS, deSEC), or no TLS.
- **Dynamic config layout** - single `dynamic.yml` file or a directory where each service gets its own `.yml`.
- **Optional mounts** - access logs, `acme.json`, and `traefik.yml` for the Logs, Certs, and Plugins tabs in Traefik Manager.
- **Static config editor** - if you enable the `traefik.yml` mount, the script also asks which restart method to use (socket proxy, poison pill, or direct socket) and adds all required compose additions automatically.
- **CrowdSec** - optionally add CrowdSec intrusion detection. Choose to install it as part of the stack (generates a bouncer key, adds the service, writes `acquis.yaml`) or connect to an existing instance (prompts for LAPI URL and API key). Credentials are injected into Traefik Manager automatically so the CrowdSec tab works out of the box.
- **Docker** - if Docker is not installed, the script offers to install it for you.

### What gets created

```
~/traefik-stack/
- docker-compose.yml
- traefik/
  - traefik.yml
  - acme.json
  - logs/
    - access.log
  - config/
    - dynamic.yml        (single file mode)
    - *.yml              (directory mode)
- traefik-manager/
  - config/
  - backups/
- crowdsec/              (only if CrowdSec install mode chosen)
  - acquis.yaml
```

---

## Mode 2 - Traefik Manager only (Docker)

Installs just Traefik Manager as a Docker container. Use this when Traefik is already running.

The script asks:

- Whether to connect to an existing Traefik Docker network
- Whether to expose via Traefik labels (with domain + TLS) or a direct host port
- Dynamic config layout and optional mounts - you provide paths to your existing Traefik files

---

## Mode 3 - Traefik Manager only (Linux service)

Installs Traefik Manager as a native systemd service. No Docker required.

Requirements: Python 3.11+, `git`, `systemd`.

The script handles cloning the repo, creating a Python venv, installing dependencies, writing the systemd unit file, and enabling the service.

---

## Notes

**Docker group:** if the script installs Docker, it exits after installation with a prompt to re-run. The new `docker` group requires a fresh shell to take effect - just run the curl command again.

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
