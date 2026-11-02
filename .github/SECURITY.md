# Security Policy

## Supported Versions

Only the latest release receives security fixes.

| Version | Supported |
|---------|-----------|
| Latest  | Yes |
| Older   | No |

## Reporting a Vulnerability

Please **do not** open a public GitHub issue for security vulnerabilities.

**Preferred:** Use [GitHub private vulnerability reporting](https://github.com/chr0nzz/tm-cli/security/advisories/new) - this keeps the report confidential until a fix is released.

**Alternative:** Email [187675356+chr0nzz@users.noreply.github.com](mailto:187675356+chr0nzz@users.noreply.github.com) with a description of the issue and steps to reproduce.

Please include:
- A description of the vulnerability and its potential impact
- Steps to reproduce or proof-of-concept
- The version of `tm` affected

You can expect an acknowledgement within **48 hours** and a fix or mitigation plan within **14 days** depending on severity.

## Scope

In scope:
- A way around the checksum check in `setup.sh` or `tm self-update`
- Privilege escalation through `tm` or the services and files it installs
- Secrets exposed in logs, command output or generated files
- Generated files with permissions that are too open
- Path traversal or unintended file changes during install, update or uninstall

Out of scope:
- Vulnerabilities requiring physical access to the host
- Issues in Traefik itself (report those to the [Traefik project](https://github.com/traefik/traefik/security))
- Issues in Traefik Manager itself (report those to [Traefik Manager](https://github.com/chr0nzz/traefik-manager/security/advisories/new))
- Issues that require the attacker to already have root on the host
