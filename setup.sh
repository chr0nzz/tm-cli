#!/bin/sh

set -eu

print_header() {
  echo "--------------------------------------------------" >&2
  echo "  Traefik Manager Installer" >&2
  echo "  --------------------------------------------------" >&2
  echo "  Documentation: https://traefik-manager.xyzlab.dev/traefik-stack.html" >&2
  echo "  Source Code:   https://github.com/chr0nzz/traefik-manager" >&2
  echo "" >&2
  echo "  Running this script will configure the service" >&2
  echo "  on your host. Ensure you have root/sudo access." >&2
  echo "" >&2
  echo "  Usage: curl -fsSL https://get-traefik.xyzlab.dev | bash" >&2
  echo "--------------------------------------------------" >&2
}

die() {
  echo "error: $1" >&2
  exit 1
}

note() {
  echo "  $1" >&2
}

detect_arch() {
  case "$(uname -m)" in
    x86_64) echo amd64 ;;
    aarch64) echo arm64 ;;
    armv7l) echo armv7 ;;
    *) return 1 ;;
  esac
}

resolve_version() {
  case "${TM_VERSION:-latest}" in
    "" | latest) echo latest ;;
    v*) echo "${TM_VERSION}" ;;
    *) echo "v${TM_VERSION}" ;;
  esac
}

asset_url() {
  if [ "$1" = latest ]; then
    echo "https://github.com/${REPO}/releases/latest/download/$2"
  else
    echo "https://github.com/${REPO}/releases/download/$1/$2"
  fi
}

fetch() {
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL "$1" -o "$2"
  elif command -v wget >/dev/null 2>&1; then
    wget -q "$1" -O "$2"
  else
    die "curl or wget is required"
  fi
}

verify_checksum() {
  dir="$1"
  asset="$2"
  grep "[[:space:]]\\*\\{0,1\\}${asset}\$" "${dir}/SHA256SUMS" >"${dir}/${asset}.sha256" || die "no checksum for ${asset} in SHA256SUMS"
  if command -v sha256sum >/dev/null 2>&1; then
    (cd "$dir" && sha256sum -c "${asset}.sha256" >/dev/null 2>&1) || die "checksum mismatch for ${asset}"
  elif command -v shasum >/dev/null 2>&1; then
    (cd "$dir" && shasum -a 256 -c "${asset}.sha256" >/dev/null 2>&1) || die "checksum mismatch for ${asset}"
  else
    die "sha256sum or shasum is required"
  fi
}

install_binary() {
  src="$1"
  if [ "$(id -u)" -eq 0 ]; then
    install -D -m 755 "$src" /usr/local/bin/tm || die "could not install tm to /usr/local/bin"
    echo /usr/local/bin/tm
  elif command -v sudo >/dev/null 2>&1; then
    note "installing tm to /usr/local/bin (sudo may ask for your password)"
    sudo install -D -m 755 "$src" /usr/local/bin/tm || die "could not install tm to /usr/local/bin"
    echo /usr/local/bin/tm
  else
    install -D -m 755 "$src" "${HOME}/.local/bin/tm" || die "could not install tm to ${HOME}/.local/bin"
    note "sudo not found, installed tm to ${HOME}/.local/bin; add it to your PATH to run tm later"
    echo "${HOME}/.local/bin/tm"
  fi
}

run_tm() {
  bin="$1"
  shift
  if ( : </dev/tty ) 2>/dev/null; then
    exec "$bin" install "$@" </dev/tty
  fi
  exec "$bin" install "$@"
}

main() {
  REPO="chr0nzz/traefik-stack"
  [ "$(uname -s)" = Linux ] || die "tm supports linux only"
  arch="$(detect_arch)" || die "unsupported architecture: $(uname -m)"
  version="$(resolve_version)"
  asset="tm-linux-${arch}"
  url="$(asset_url "$version" "$asset")"
  if [ "${TM_BOOTSTRAP_TEST:-}" = 1 ]; then
    echo "$url"
    exit 0
  fi
  print_header
  tmp="$(mktemp -d 2>/dev/null || mktemp -d -t tm)" || die "could not create a temp directory"
  note "downloading tm (${version}, ${arch})"
  fetch "$url" "${tmp}/${asset}" || die "download failed: ${url}"
  fetch "$(asset_url "$version" SHA256SUMS)" "${tmp}/SHA256SUMS" || die "download failed: $(asset_url "$version" SHA256SUMS)"
  verify_checksum "$tmp" "$asset"
  bin="$(install_binary "${tmp}/${asset}")"
  rm -rf "$tmp"
  note "installed ${bin}"
  if [ "${TM_INSTALL_ONLY:-}" = 1 ]; then
    exit 0
  fi
  run_tm "$bin" "$@"
}

main "$@"
