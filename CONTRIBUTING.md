# Contributing to tm

Thanks for your interest in contributing. This guide covers everything you need to get started.

---

## Table of Contents

- [Reporting bugs](#reporting-bugs)
- [Suggesting features](#suggesting-features)
- [Submitting a pull request](#submitting-a-pull-request)
- [Running locally](#running-locally)
- [Tests](#tests)
- [Project structure](#project-structure)
- [Code style](#code-style)
- [Branch guide](#branch-guide)

---

## Reporting bugs

Issues for `tm` are filed in the main repo, using the [Traefik Manager issue form](https://github.com/chr0nzz/traefik-manager/issues/new/choose). Include:

- The `tm` version and how you installed (full stack, TM only or agent, Docker or native)
- The output of `tm doctor` and `tm status`, with secrets removed
- Steps to reproduce
- What you expected vs what happened

For **security vulnerabilities**, do not open a public issue - see [SECURITY.md](.github/SECURITY.md).

---

## Suggesting features

Start an [Idea discussion](https://github.com/chr0nzz/traefik-manager/discussions/new?category=ideas) in the main repo before writing any code, and pick `Other` as the area. This lets us discuss it first and avoids wasted effort if it doesn't fit the project's direction.

---

## Submitting a pull request

> [!IMPORTANT]
> **All pull requests target `dev`.** Never open a PR against `main` - it only moves when a release is cut. PRs against `main` will be asked to retarget.

1. Fork the repo and create your branch from `dev`.
2. Keep PRs focused - one fix or feature per PR.
3. For anything beyond a small bug fix, open an issue first so we can align on approach.
4. Test your changes on a real install if possible (see [Running locally](#running-locally)).
5. If you change the files `tm` generates, regenerate the golden files and check the diff (see [Tests](#tests)).
6. If your change affects user-facing behaviour, open a matching PR for [`docs/tm-cli.md`](https://github.com/chr0nzz/traefik-manager/blob/dev/docs/tm-cli.md) in the main repo. The `tm` docs live there, and that PR targets its `dev` branch too.
7. Open the PR against `dev` (see [Branch guide](#branch-guide)). GitHub preselects `main` - change the base branch dropdown when creating the PR.

---

## Running locally

### Requirements

- Go, at the version in `go.mod`
- `make`
- `shellcheck` for `make lint`
- Linux to run `tm` itself, on amd64, arm64 or armv7

### Build

```bash
git clone https://github.com/chr0nzz/tm-cli.git
cd tm-cli
git checkout dev

make build
./bin/tm --help
```

> [!WARNING]
> **`tm` changes the machine it runs on.** It can install packages, start containers, write systemd units and use `sudo`. Try `tm install` in a VM or on a throwaway host, not on a machine you care about.

---

## Tests

```bash
make test         # go test ./...
make vet          # go vet ./...
gofmt -l .        # lists files that need formatting, should print nothing
make lint         # shellcheck on setup.sh and scripts/
```

Please run these before opening a pull request. CI runs them too, along with a smoke test of `setup.sh`:

```bash
TM_BOOTSTRAP_TEST=1 sh setup.sh
```

A separate workflow fails on a dependency or Go version with a known vulnerability. If you update `go.mod`, run the same check:

```bash
go run golang.org/x/vuln/cmd/govulncheck@v1.7.0 ./...
```

The files `tm` generates are compared against the scenarios in `testdata/golden`. If you change a template or what gets rendered, regenerate them and read the diff to confirm that only what you meant to change has changed:

```bash
go test ./internal/render -update
```

---

## Project structure

```
cmd/tm/                       # Entry point
internal/
    answers/                  # The answers behind an install: load, finalize, validate
    cli/                      # The tm commands and their flags
    ghrelease/                # GitHub release lookups and downloads
    host/                     # Docker, systemd, packages, users and files on the host
    installer/                # Install, update, reconfigure, uninstall and doctor
    render/                   # Generates config files from templates/
    state/                    # Finds, records and adopts existing installs
    ui/                       # Terminal output
    wizard/                   # The install and reconfigure questions
testdata/golden/              # Expected output for each install scenario
scripts/parity.sh             # Compares generated files with the old bash installer
setup.sh                      # The bootstrap behind the one-line install
.github/
    workflows/
        audit.yml             # govulncheck on dev, main, PRs and daily
        ci.yml                # Build, vet, tests, bootstrap smoke test and shellcheck
        pr-base-check.yml     # Fails a PR opened against main instead of dev
        release.yml           # On a version tag: audit, tests, then build and publish the release
```

---

## Code style

- **Go:** run `gofmt`, CI fails on anything it would change.
- **Shell:** `setup.sh` stays POSIX `sh` and passes `shellcheck`.
- **No comments:** don't add explanatory comments to code - use clear names instead. The existing codebase follows this convention.
- **No dead code:** don't leave commented-out blocks or unused variables.
- **Commit messages:** one imperative subject line with a conventional prefix (`fix:`, `feat:`, `docs:`, `build:`, `ci:`, `chore:`, `test:`), with the issue or discussion in brackets when there is one. No body - the reasoning belongs in the release notes.

---

## Branch guide

| Branch | Purpose | Accepts PRs? |
|--------|---------|--------------|
| `dev` | Active development - all features, bug fixes, and docs changes for the next release | **Yes - every PR targets `dev`** |
| `main` | Release branch - only moves when a version is tagged | **No - never open PRs here** |

Everything lands on `dev` first - features, bug fixes, and docs alike. When `dev` is ready for release it is merged to `main` and tagged. The tag runs the dependency audit and the tests, and only if both pass does it build the `tm` binaries and publish the release. Nothing else touches `main`: a PR merged there would sit unreleased, drift out of sync with `dev`, and risk being clobbered by the next release merge.
