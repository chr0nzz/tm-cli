#!/usr/bin/env bash

# --- Installer Information ---
cat <<EOF >&2
--------------------------------------------------
  Traefik Manager Installer
  --------------------------------------------------
  Documentation: https://traefik-manager.xyzlab.dev/traefik-stack.html
  Source Code:   https://github.com/chr0nzz/traefik-manager
  
  Running this script will configure the service
  on your host. Ensure you have root/sudo access.
  
  Usage: curl -fsSL https://get-traefik.xyzlab.dev | bash
--------------------------------------------------
EOF

set -euo pipefail

SCRIPT_VERSION="1.7.0"

BOLD="\033[1m"
DIM="\033[2m"
GREEN="\033[32m"
CYAN="\033[36m"
YELLOW="\033[33m"
RED="\033[31m"
RESET="\033[0m"

INSTALL_DIR="${HOME}/traefik-stack"
COMPOSE_CMD=""
INSTALL_MODE=""
DEPLOY_METHOD=""
DOMAIN=""
TRAEFIK_DASHBOARD_HOST=""
TM_HOST=""
RESTART_METHOD=""
TRAEFIK_CONTAINER="traefik"
TRAEFIK_SYSTEMD="false"
TRAEFIK_SERVICE_NAME="traefik"
MOUNT_STATIC_CONFIG="false"
TRAEFIK_YML_HOST_PATH=""
SIGNAL_FILE_PATH="/signals/restart.sig"
ADD_CROWDSEC="false"
CROWDSEC_MODE=""
CS_LAPI_URL=""
CS_API_KEY=""
CS_KEY=""
CS_MACHINE_ID=""
CS_MACHINE_PW=""

# ─── Helpers ──────────────────────────────────────────────────────────────────

print_banner() {
  echo ""
  echo -e "${CYAN}${BOLD}"
  echo "  ████████╗██████╗  █████╗ ███████╗███████╗██╗██╗  ██╗"
  echo "     ██╔══╝██╔══██╗██╔══██╗██╔════╝██╔════╝██║██║ ██╔╝"
  echo "     ██║   ██████╔╝███████║█████╗  █████╗  ██║█████╔╝ "
  echo "     ██║   ██╔══██╗██╔══██║██╔══╝  ██╔══╝  ██║██╔═██╗ "
  echo "     ██║   ██║  ██║██║  ██║███████╗██║     ██║██║  ██╗"
  echo "     ╚═╝   ╚═╝  ╚═╝╚═╝  ╚═╝╚══════╝╚═╝     ╚═╝╚═╝  ╚═╝"
  echo ""
  echo "                       ◉"
  echo "                       │"
  echo "                   ╔═════╗"
  echo "               ◉ ─── ╠     ╣ ─── ◉"
  echo "                   ╚═════╝"
  echo "                       │"
  echo "                       ◉"
  echo -e "${RESET}"
  echo -e "  ${DIM}+ Traefik Manager - Interactive Setup${RESET}"
  echo ""
}

step()  { echo -e "\n${CYAN}${BOLD}▸ $1${RESET}"; }
ok()    { echo -e "  ${GREEN}✔${RESET}  $1"; }
warn()  { echo -e "  ${YELLOW}⚠${RESET}  $1"; }
info()  { echo -e "  ${DIM}ℹ  $1${RESET}"; }
die()   { echo -e "\n  ${RED}✖  Error: $1${RESET}\n"; exit 1; }
sep()   { echo -e "\n  ${DIM}────────────────────────────────────────${RESET}"; }

ask() {
  local prompt="$1" default="${2:-}" var_name="$3"
  if [[ -n "$default" ]]; then
    echo -ne "  ${BOLD}${prompt}${RESET} ${DIM}[${default}]${RESET}: "
  else
    echo -ne "  ${BOLD}${prompt}${RESET}: "
  fi
  read -r input </dev/tty
  if [[ -z "$input" && -n "$default" ]]; then
    printf -v "$var_name" '%s' "$default"
  else
    printf -v "$var_name" '%s' "$input"
  fi
}

ask_secret() {
  local prompt="$1" var_name="$2"
  echo -ne "  ${BOLD}${prompt}${RESET}: "
  local input=''
  read -rs input </dev/tty
  echo ""
  printf -v "$var_name" '%s' "$input"
}

ask_yn() {
  local prompt="$1" default="${2:-y}" var_name="$3"
  echo -ne "  ${BOLD}${prompt}${RESET} ${DIM}(y/n) [${default}]${RESET}: "
  read -r input </dev/tty
  input="${input:-$default}"
  if [[ "$input" =~ ^[Yy] ]]; then printf -v "$var_name" 'true'
  else printf -v "$var_name" 'false'; fi
}

ask_choice() {
  local prompt="$1" var_name="$2"; shift 2
  local -a _ac_opts=("$@")
  local _ac_sel=0 _ac_count=${#_ac_opts[@]}

  echo -e "  ${BOLD}${prompt}${RESET}"

  __ac_draw() {
    local _i
    for _i in "${!_ac_opts[@]}"; do
      if [[ $_i -eq $_ac_sel ]]; then
        printf "  \033[1;36m▸ \033[0m\033[1m%s\033[0m\n" "${_ac_opts[$_i]}"
      else
        printf "    \033[2m%s\033[0m\n" "${_ac_opts[$_i]}"
      fi
    done
  }

  if [[ -t 0 ]] && command -v tput &>/dev/null; then
    tput civis 2>/dev/null || true
    __ac_draw
    local _key _seq
    while true; do
      IFS= read -rsn1 _key </dev/tty || _key=''
      local _done=0
      case "$_key" in
        $'\x1b')
          IFS= read -rsn2 -t 0.05 _seq </dev/tty 2>/dev/null || _seq=''
          [[ "$_seq" == '[A' ]] && (( _ac_sel > 0 )) && (( _ac_sel-- )) || true
          [[ "$_seq" == '[B' ]] && (( _ac_sel < _ac_count-1 )) && (( _ac_sel++ )) || true
          ;;
        '') _done=1 ;;
        [1-9])
          local _n=$(( _key - 1 ))
          if (( _n >= 0 && _n < _ac_count )); then _ac_sel=$_n; _done=1; fi
          ;;
      esac
      printf "\033[%dA\033[J" "$_ac_count"
      __ac_draw
      (( _done )) && break || true
    done
    tput cnorm 2>/dev/null || true
    echo ""
  else
    local _i
    for _i in "${!_ac_opts[@]}"; do
      printf "    \033[2m%d)\033[0m %s\n" "$((_i+1))" "${_ac_opts[$_i]}"
    done
    printf "  Choice [1]: "
    local _input
    read -r _input </dev/tty
    _input="${_input:-1}"
    local _idx=$(( _input - 1 ))
    (( _idx < 0 || _idx >= _ac_count )) && _idx=0
    _ac_sel=$_idx
    echo ""
  fi

  printf -v "$var_name" '%s' "${_ac_opts[$_ac_sel]}"
  ok "${_ac_opts[$_ac_sel]}"
}

# ─── Docker ───────────────────────────────────────────────────────────────────

detect_os() {
  if [[ -f /etc/os-release ]]; then
    . /etc/os-release
    OS_ID="${ID:-unknown}"
    OS_LIKE="${ID_LIKE:-}"
  else
    OS_ID="unknown"
    OS_LIKE=""
  fi
}

install_docker() {
  step "Installing Docker"
  detect_os

  if [[ "$OS_ID" == "ubuntu" || "$OS_ID" == "debian" || "$OS_LIKE" == *"debian"* || "$OS_LIKE" == *"ubuntu"* ]]; then
    info "Detected Debian/Ubuntu"
    sudo apt-get update -qq
    sudo apt-get install -y -qq ca-certificates curl gnupg lsb-release
    sudo install -m 0755 -d /etc/apt/keyrings
    curl -fsSL https://download.docker.com/linux/${OS_ID}/gpg \
      | sudo gpg --dearmor -o /etc/apt/keyrings/docker.gpg
    sudo chmod a+r /etc/apt/keyrings/docker.gpg
    echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.gpg] \
      https://download.docker.com/linux/${OS_ID} $(lsb_release -cs) stable" \
      | sudo tee /etc/apt/sources.list.d/docker.list > /dev/null
    sudo apt-get update -qq
    sudo apt-get install -y -qq docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
    sudo systemctl enable --now docker
    sudo usermod -aG docker "${USER}" || true
    ok "Docker installed"

  elif [[ "$OS_ID" == "fedora" || "$OS_ID" == "rhel" || "$OS_ID" == "centos" || \
          "$OS_ID" == "rocky" || "$OS_ID" == "almalinux" || \
          "$OS_LIKE" == *"rhel"* || "$OS_LIKE" == *"fedora"* ]]; then
    info "Detected RHEL/Fedora"
    sudo dnf -y install dnf-plugins-core
    sudo dnf config-manager --add-repo https://download.docker.com/linux/fedora/docker-ce.repo 2>/dev/null \
      || sudo dnf config-manager --add-repo https://download.docker.com/linux/rhel/docker-ce.repo
    sudo dnf install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
    sudo systemctl enable --now docker
    sudo usermod -aG docker "${USER}" || true
    ok "Docker installed"

  elif [[ "$OS_ID" == "arch" || "$OS_LIKE" == *"arch"* ]]; then
    info "Detected Arch"
    sudo pacman -Sy --noconfirm docker docker-compose
    sudo systemctl enable --now docker
    sudo usermod -aG docker "${USER}" || true
    ok "Docker installed"

  else
    warn "Using Docker convenience script"
    curl -fsSL https://get.docker.com | sudo sh
    sudo usermod -aG docker "${USER}" || true
    ok "Docker installed"
  fi

  if ! docker info &>/dev/null 2>&1; then
    echo ""
    warn "Docker was installed but the current shell does not have the docker group yet."
    warn "Please log out and back in, then re-run:"
    echo ""
    echo -e "  ${CYAN}curl -fsSL https://get-traefik.xyzlab.dev | bash${RESET}"
    echo ""
    exit 0
  fi
}

check_docker() {
  step "Checking Docker"

  command -v curl &>/dev/null && ok "curl found" || die "curl is required."

  if command -v docker &>/dev/null && docker info &>/dev/null 2>&1; then
    ok "Docker found and running"
  else
    warn "Docker is not installed or the daemon is not running."
    ask_yn "Install Docker now?" "y" INSTALL_DOCKER_NOW
    if [[ "$INSTALL_DOCKER_NOW" == "true" ]]; then
      install_docker
    else
      die "Docker is required. Aborting."
    fi
  fi

  if docker compose version &>/dev/null 2>&1; then
    ok "docker compose (v2) found"
    COMPOSE_CMD="docker compose"
  elif command -v docker-compose &>/dev/null; then
    ok "docker-compose (v1) found"
    COMPOSE_CMD="docker-compose"
  else
    die "Docker Compose is required. Install the Docker Compose plugin and re-run."
  fi
}

# ─── Native deps ──────────────────────────────────────────────────────────────

check_native_deps() {
  step "Checking dependencies"

  command -v curl &>/dev/null && ok "curl found" || die "curl is required."
  command -v git  &>/dev/null && ok "git found"  || die "git is required. Install it and re-run."

  if ! command -v python3 &>/dev/null; then
    die "Python 3.11 or newer is required. Install it and re-run."
  fi

  local py_ok
  py_ok=$(python3 -c "import sys; print('ok' if sys.version_info >= (3, 11) else 'old')")
  if [[ "$py_ok" != "ok" ]]; then
    die "Python 3.11 or newer is required. Found: $(python3 --version)"
  fi
  ok "Python $(python3 --version | cut -d' ' -f2) found"

  command -v systemctl &>/dev/null && ok "systemd found" || die "systemd is required for the Linux service install."
}

# ─── Mode selection ───────────────────────────────────────────────────────────

gather_mode() {
  step "What would you like to install?"
  ask_choice "Choose an option" INSTALL_MODE \
    "Traefik + Traefik Manager (full stack)" \
    "Traefik Manager only" \
    "Traefik Manager Agent"

  if [[ "$INSTALL_MODE" == "Traefik Manager only" ]]; then
    sep
    echo ""
    ask_choice "Deployment method" DEPLOY_METHOD \
      "Docker" \
      "Linux service (systemd)"
  elif [[ "$INSTALL_MODE" == "Traefik Manager Agent" ]]; then
    DEPLOY_METHOD="Agent"
  else
    DEPLOY_METHOD="Docker"
  fi
}

# ─── Restart method gathering (Docker) ────────────────────────────────────────

gather_restart_method_docker() {
  local ask_container="${1:-false}"
  sep
  echo ""
  echo -e "  ${BOLD}-- Static Config Editor --${RESET}"
  info "TM can restart Traefik automatically when you save static config changes."
  local choice
  ask_choice "How should TM restart Traefik?" choice \
    "Docker socket proxy (recommended - one extra container, minimal socket exposure)" \
    "Poison pill (no extra container - adds a healthcheck to Traefik compose)" \
    "Direct Docker socket (simplest - full Docker access, higher risk)"
  case "$choice" in
    "Docker socket proxy"*)  RESTART_METHOD="proxy" ;;
    "Poison pill"*)          RESTART_METHOD="poison-pill" ;;
    "Direct Docker socket"*) RESTART_METHOD="socket" ;;
  esac
  if [[ "$ask_container" == "true" ]]; then
    ask "Traefik container name" "traefik" TRAEFIK_CONTAINER
  else
    TRAEFIK_CONTAINER="traefik"
  fi
}

# ─── Full stack config ────────────────────────────────────────────────────────

_full_sec_general() {
  sep; echo ""
  echo -e "  ${BOLD}-- General --${RESET}"
  ask "Install directory" "$INSTALL_DIR" INSTALL_DIR
}

_full_sec_deploy() {
  sep; echo ""
  echo -e "  ${BOLD}-- Deployment type --${RESET}"
  info "Internal = LAN / VPN / Tailscale only.  External = reachable from the internet."
  ask_choice "Where will this be accessed from?" DEPLOYMENT_TYPE \
    "External (internet-facing)" \
    "Internal only (LAN / VPN / Tailscale)"
  if [[ "$DEPLOYMENT_TYPE" == "External"* ]]; then EXTERNAL=true
  else EXTERNAL=false; fi
}

_full_sec_domain() {
  sep; echo ""
  echo -e "  ${BOLD}-- Domain --${RESET}"
  while true; do
    ask "Your domain (e.g. example.com)" "$DOMAIN" DOMAIN
    [[ -n "$DOMAIN" ]] && break
    warn "A domain is required."
  done
  ask "Traefik dashboard subdomain" "${TRAEFIK_DASHBOARD_HOST:-traefik.$DOMAIN}" TRAEFIK_DASHBOARD_HOST
  ask "Traefik Manager subdomain"   "${TM_HOST:-manager.$DOMAIN}" TM_HOST
  ask_yn "Enable Traefik API dashboard UI?" "y" ENABLE_DASHBOARD
}

_full_sec_tls() {
  sep; echo ""
  echo -e "  ${BOLD}-- TLS / Certificates --${RESET}"
  gather_tls_method
}

_full_sec_config() {
  sep; echo ""
  echo -e "  ${BOLD}-- Dynamic Config --${RESET}"
  info "Single file is simpler. Directory (one .yml per service) is easier at scale."
  ask_choice "Dynamic config layout" CONFIG_LAYOUT \
    "Single file (dynamic.yml)" \
    "Directory - one .yml file per service"
}

_full_sec_mounts() {
  sep; echo ""
  echo -e "  ${BOLD}-- Optional Mounts --${RESET}"
  info "Expose extra Traefik data to Traefik Manager for richer visibility."
  ask_yn "Mount access logs?"                      "y" MOUNT_ACCESS_LOGS
  ask_yn "Mount SSL certs (acme.json)?"              "y" MOUNT_CERTS
  ask_yn "Mount Traefik static config (traefik.yml)?" "n" MOUNT_STATIC_CONFIG
  if [[ "$MOUNT_STATIC_CONFIG" == "true" ]]; then
    gather_restart_method_docker "false"
  else
    RESTART_METHOD=""
  fi
}

_full_sec_crowdsec() {
  sep; echo ""
  echo -e "  ${BOLD}-- CrowdSec IDS --${RESET}"
  info "CrowdSec detects intrusions and bans malicious IPs. Visible in the CrowdSec tab in Traefik Manager."
  ask_yn "Add CrowdSec?" "n" ADD_CROWDSEC
  if [[ "$ADD_CROWDSEC" == "true" ]]; then
    ask_choice "CrowdSec setup" CROWDSEC_MODE \
      "Install as part of this stack" \
      "Connect to existing instance"
    if [[ "$CROWDSEC_MODE" == "Connect"* ]]; then
      ask "CrowdSec LAPI URL" "http://crowdsec:8080" CS_LAPI_URL
      while true; do
        ask_secret "CrowdSec API key (bouncer, for decisions)" CS_API_KEY
        [[ -n "$CS_API_KEY" ]] && break
        warn "A CrowdSec API key is required."
      done
      info "Machine credentials are needed to view alerts and unban (the bouncer key cannot). Create one with: cscli machines add traefik-manager --auto"
      ask "CrowdSec machine ID (optional, for alerts)" "" CS_MACHINE_ID
      [[ -n "$CS_MACHINE_ID" ]] && ask_secret "CrowdSec machine password" CS_MACHINE_PW
    fi
    if [[ "$CROWDSEC_MODE" == "Install"* && "$MOUNT_ACCESS_LOGS" != "true" ]]; then
      warn "CrowdSec reads Traefik access logs - enabling access log mount."
      MOUNT_ACCESS_LOGS="true"
    fi
  else
    CROWDSEC_MODE=""
  fi
}

_full_sec_network() {
  sep; echo ""
  echo -e "  ${BOLD}-- Docker Network --${RESET}"
  ask "Docker network name"       "traefik-net" DOCKER_NETWORK
  ask "Traefik internal API port" "8080"        TRAEFIK_API_PORT
}

_full_build_section_list() {
  FULL_SEC_LABELS=("General" "Deployment type" "Domain" "TLS / Certificates" "Dynamic config" "Optional mounts" "CrowdSec" "Docker network")
  FULL_SEC_FNS=(_full_sec_general _full_sec_deploy _full_sec_domain _full_sec_tls _full_sec_config _full_sec_mounts _full_sec_crowdsec _full_sec_network)
}

_full_show_review() {
  echo ""
  echo -e "  ${BOLD}Review configuration${RESET}"
  echo -e "  ${DIM}────────────────────────────────────────────────────────${RESET}"
  local _i=1
  for _lbl in "${FULL_SEC_LABELS[@]}"; do
    local _val=""
    case "$_lbl" in
      "General") _val="$INSTALL_DIR" ;;
      "Deployment type") [[ "$EXTERNAL" == "true" ]] && _val="external (internet-facing)" || _val="internal (LAN / VPN)" ;;
      "Domain")
        _val="$DOMAIN  dash:${TRAEFIK_DASHBOARD_HOST}  tm:${TM_HOST}"
        [[ "$ENABLE_DASHBOARD" != "true" ]] && _val+="  dashboard:off"
        ;;
      "TLS / Certificates")
        if [[ "$TLS_TYPE" == "none" ]]; then _val="none (HTTP only)"
        elif [[ "$TLS_TYPE" == "dns" ]]; then _val="Let's Encrypt DNS (${DNS_PROVIDER})  ${ACME_EMAIL}"
        else _val="Let's Encrypt HTTP  ${ACME_EMAIL}"; fi
        ;;
      "Dynamic config") _val="$(echo "$CONFIG_LAYOUT" | grep -o 'Single file\|Directory')" ;;
      "Optional mounts")
        local _m=""
        [[ "$MOUNT_ACCESS_LOGS" == "true" ]]  && _m+="logs "
        [[ "$MOUNT_CERTS" == "true" ]]         && _m+="certs "
        [[ "$MOUNT_STATIC_CONFIG" == "true" ]] && _m+="static(restart:${RESTART_METHOD})"
        _val="${_m:-(none)}"
        ;;
      "CrowdSec")
        case "$CROWDSEC_MODE" in
          Install*) _val="install alongside" ;;
          Connect*) _val="connect  ${CS_LAPI_URL}" ;;
          *)        _val="disabled" ;;
        esac
        ;;
      "Docker network") _val="${DOCKER_NETWORK}  api:${TRAEFIK_API_PORT}" ;;
    esac
    printf "  \033[1;36m%2d\033[0m  \033[2m%-20s\033[0m  %s\n" "$_i" "$_lbl" "$_val"
    (( _i++ ))
  done
  echo -e "  ${DIM}────────────────────────────────────────────────────────${RESET}"
}

gather_full_stack() {
  step "Traefik + Traefik Manager Setup"
  echo -e "  ${DIM}Press Enter to accept defaults shown in brackets.${RESET}"

  _full_build_section_list
  for _fn in "${FULL_SEC_FNS[@]}"; do "$_fn"; done

  while true; do
    _full_show_review
    echo ""
    echo -ne "  ${BOLD}Edit a section (1-${#FULL_SEC_FNS[@]}) or Enter to install:${RESET} "
    local _choice
    read -r _choice </dev/tty
    [[ -z "$_choice" ]] && break
    if [[ "$_choice" =~ ^[0-9]+$ ]] && (( _choice >= 1 && _choice <= ${#FULL_SEC_FNS[@]} )); then
      "${FULL_SEC_FNS[$((_choice-1))]}"
    fi
  done

  if [[ "$EXTERNAL" == "true" ]]; then
    sep
    echo ""
    echo -e "  ${YELLOW}${BOLD}Firewall / Port Requirements${RESET}"
    echo -e "  ${DIM}The following ports must be open on this server's firewall:${RESET}\n"
    if [[ "$TLS_TYPE" != "none" ]]; then
      echo -e "    ${CYAN}80/tcp${RESET}   HTTP (redirects to HTTPS + ACME HTTP-01 challenge)"
      echo -e "    ${CYAN}443/tcp${RESET}  HTTPS"
    else
      echo -e "    ${CYAN}80/tcp${RESET}   HTTP"
    fi
    echo ""
    echo -e "  ${DIM}  sudo ufw allow 80/tcp${RESET}"
    if [[ "$TLS_TYPE" != "none" ]]; then
      echo -e "  ${DIM}  sudo ufw allow 443/tcp${RESET}"
    fi
    echo -e "  ${DIM}  sudo ufw reload${RESET}"
    echo ""
    echo -ne "  ${BOLD}Press Enter when ports are open to continue...${RESET}"
    read -r </dev/tty
  fi
}

# ─── TLS method (shared) ──────────────────────────────────────────────────────

gather_tls_method() {
  ask_choice "Certificate method" CERT_METHOD \
    "Let's Encrypt - HTTP challenge (port 80 must be open)" \
    "Let's Encrypt - DNS challenge: Cloudflare" \
    "Let's Encrypt - DNS challenge: Route 53 (AWS)" \
    "Let's Encrypt - DNS challenge: DigitalOcean" \
    "Let's Encrypt - DNS challenge: Namecheap" \
    "Let's Encrypt - DNS challenge: DuckDNS" \
    "Let's Encrypt - DNS challenge: deSEC" \
    "No TLS (HTTP only)"

  DNS_ENV_BLOCK=""
  DNS_PROVIDER=""

  if [[ "$CERT_METHOD" != "No TLS"* ]]; then
    while true; do
      ask "Email for Let's Encrypt" "${ACME_EMAIL:-}" ACME_EMAIL
      [[ -n "$ACME_EMAIL" ]] && break
      warn "An email is required for Let's Encrypt."
    done
  fi

  case "$CERT_METHOD" in
    "Let's Encrypt - HTTP challenge"*)
      TLS_TYPE="http"; CERT_RESOLVER="letsencrypt"; TRAEFIK_ENTRYPOINT="websecure"
      ;;
    *"Cloudflare"*)
      TLS_TYPE="dns"; CERT_RESOLVER="letsencrypt"; TRAEFIK_ENTRYPOINT="websecure"
      DNS_PROVIDER="cloudflare"
      ask_secret "Cloudflare API Token (DNS-scoped token)" CF_DNS_API_TOKEN
      DNS_ENV_BLOCK="      - CF_DNS_API_TOKEN=${CF_DNS_API_TOKEN}"
      ;;
    *"Route 53"*)
      TLS_TYPE="dns"; CERT_RESOLVER="letsencrypt"; TRAEFIK_ENTRYPOINT="websecure"
      DNS_PROVIDER="route53"
      ask "AWS Access Key ID"     "" AWS_ACCESS_KEY_ID
      ask_secret "AWS Secret Access Key" AWS_SECRET_ACCESS_KEY
      ask "AWS Region"            "us-east-1" AWS_REGION
      DNS_ENV_BLOCK="      - AWS_ACCESS_KEY_ID=${AWS_ACCESS_KEY_ID}
      - AWS_SECRET_ACCESS_KEY=${AWS_SECRET_ACCESS_KEY}
      - AWS_REGION=${AWS_REGION}"
      ;;
    *"DigitalOcean"*)
      TLS_TYPE="dns"; CERT_RESOLVER="letsencrypt"; TRAEFIK_ENTRYPOINT="websecure"
      DNS_PROVIDER="digitalocean"
      ask_secret "DigitalOcean API Token" DO_AUTH_TOKEN
      DNS_ENV_BLOCK="      - DO_AUTH_TOKEN=${DO_AUTH_TOKEN}"
      ;;
    *"Namecheap"*)
      TLS_TYPE="dns"; CERT_RESOLVER="letsencrypt"; TRAEFIK_ENTRYPOINT="websecure"
      DNS_PROVIDER="namecheap"
      ask "Namecheap API User" "" NAMECHEAP_API_USER
      ask_secret "Namecheap API Key"  NAMECHEAP_API_KEY
      DNS_ENV_BLOCK="      - NAMECHEAP_API_USER=${NAMECHEAP_API_USER}
      - NAMECHEAP_API_KEY=${NAMECHEAP_API_KEY}"
      ;;
    *"DuckDNS"*)
      TLS_TYPE="dns"; CERT_RESOLVER="letsencrypt"; TRAEFIK_ENTRYPOINT="websecure"
      DNS_PROVIDER="duckdns"
      ask_secret "DuckDNS Token" DUCKDNS_TOKEN
      DNS_ENV_BLOCK="      - DUCKDNS_TOKEN=${DUCKDNS_TOKEN}"
      ;;
    *"deSEC"*)
      TLS_TYPE="dns"; CERT_RESOLVER="letsencrypt"; TRAEFIK_ENTRYPOINT="websecure"
      DNS_PROVIDER="desec"
      ask_secret "deSEC Token" DESEC_TOKEN
      DNS_ENV_BLOCK="      - DESEC_TOKEN=${DESEC_TOKEN}"
      ;;
    "No TLS"*)
      TLS_TYPE="none"; CERT_RESOLVER=""; TRAEFIK_ENTRYPOINT="web"
      warn "Running without TLS. HTTP only."
      ;;
  esac
}

# ─── TM-only Docker config ────────────────────────────────────────────────────

_tmd_sec_general() {
  sep; echo ""
  echo -e "  ${BOLD}-- General --${RESET}"
  ask "Install directory" "${HOME}/traefik-manager" INSTALL_DIR
}

_tmd_sec_network() {
  sep; echo ""
  echo -e "  ${BOLD}-- Network --${RESET}"
  ask_yn "Connect to an existing Traefik Docker network?" "y" USE_TRAEFIK_NETWORK
  if [[ "$USE_TRAEFIK_NETWORK" == "true" ]]; then
    ask "Traefik network name" "traefik-net" DOCKER_NETWORK
    NETWORK_EXTERNAL="true"
  else
    ask "Docker network name" "traefik-manager-net" DOCKER_NETWORK
    NETWORK_EXTERNAL="false"
  fi
}

_tmd_sec_access() {
  sep; echo ""
  echo -e "  ${BOLD}-- Access --${RESET}"
  ask_yn "Expose via Traefik labels (requires Traefik on same network)?" "y" USE_TRAEFIK_LABELS
  if [[ "$USE_TRAEFIK_LABELS" == "true" ]]; then
    while true; do
      ask "Traefik Manager domain (e.g. manager.example.com)" "$TM_HOST" TM_HOST
      [[ -n "$TM_HOST" ]] && break
      warn "A domain is required for Traefik labels."
    done
    gather_tls_method
    TM_PORT=""
  else
    ask "Port to expose on host" "5000" TM_PORT
    TLS_TYPE="none"
    CERT_RESOLVER=""
    TRAEFIK_ENTRYPOINT="web"
  fi
}

_tmd_sec_config() {
  sep; echo ""
  echo -e "  ${BOLD}-- Dynamic Config --${RESET}"
  info "Single file is simpler. Directory (one .yml per service) is easier at scale."
  ask_choice "Dynamic config layout" CONFIG_LAYOUT \
    "Single file (dynamic.yml)" \
    "Directory - one .yml file per service"
}

_tmd_sec_mounts() {
  sep; echo ""
  echo -e "  ${BOLD}-- Optional Mounts --${RESET}"
  info "Expose extra Traefik data to Traefik Manager for richer visibility."
  ask_yn "Mount access logs?"           "y" MOUNT_ACCESS_LOGS
  if [[ "$MOUNT_ACCESS_LOGS" == "true" ]]; then
    ask "Path to Traefik access log" "/var/log/traefik/access.log" ACCESS_LOG_PATH
  fi
  ask_yn "Mount SSL certs (acme.json)?" "y" MOUNT_CERTS
  if [[ "$MOUNT_CERTS" == "true" ]]; then
    ask "Path to acme.json" "/etc/traefik/acme.json" ACME_JSON_HOST_PATH
  fi
  ask_yn "Mount Traefik static config (traefik.yml)?" "n" MOUNT_STATIC_CONFIG
  if [[ "$MOUNT_STATIC_CONFIG" == "true" ]]; then
    ask "Path to traefik.yml" "/etc/traefik/traefik.yml" TRAEFIK_YML_HOST_PATH
    gather_restart_method_docker "true"
  else
    RESTART_METHOD=""
  fi
}

_tmd_build_section_list() {
  TMD_SEC_LABELS=("General" "Network" "Access" "Dynamic config" "Optional mounts")
  TMD_SEC_FNS=(_tmd_sec_general _tmd_sec_network _tmd_sec_access _tmd_sec_config _tmd_sec_mounts)
}

_tmd_show_review() {
  echo ""
  echo -e "  ${BOLD}Review configuration${RESET}"
  echo -e "  ${DIM}────────────────────────────────────────────────────────${RESET}"
  local _i=1
  for _lbl in "${TMD_SEC_LABELS[@]}"; do
    local _val=""
    case "$_lbl" in
      "General") _val="$INSTALL_DIR" ;;
      "Network")
        [[ "$NETWORK_EXTERNAL" == "true" ]] && _val="${DOCKER_NETWORK} (existing)" || _val="${DOCKER_NETWORK} (new)"
        ;;
      "Access")
        if [[ "$USE_TRAEFIK_LABELS" == "true" ]]; then
          if [[ "$TLS_TYPE" == "none" ]]; then _val="via Traefik  ${TM_HOST}  no TLS"
          elif [[ "$TLS_TYPE" == "dns" ]]; then _val="via Traefik  ${TM_HOST}  TLS:dns(${DNS_PROVIDER})"
          else _val="via Traefik  ${TM_HOST}  TLS:http"; fi
        else
          _val="host port :${TM_PORT}"
        fi
        ;;
      "Dynamic config") _val="$(echo "$CONFIG_LAYOUT" | grep -o 'Single file\|Directory')" ;;
      "Optional mounts")
        local _m=""
        [[ "$MOUNT_ACCESS_LOGS" == "true" ]]  && _m+="logs "
        [[ "$MOUNT_CERTS" == "true" ]]         && _m+="certs "
        [[ "$MOUNT_STATIC_CONFIG" == "true" ]] && _m+="static(restart:${RESTART_METHOD})"
        _val="${_m:-(none)}"
        ;;
    esac
    printf "  \033[1;36m%2d\033[0m  \033[2m%-20s\033[0m  %s\n" "$_i" "$_lbl" "$_val"
    (( _i++ ))
  done
  echo -e "  ${DIM}────────────────────────────────────────────────────────${RESET}"
}

gather_tm_docker() {
  step "Traefik Manager - Docker Setup"
  echo -e "  ${DIM}Press Enter to accept defaults shown in brackets.${RESET}"

  _tmd_build_section_list
  for _fn in "${TMD_SEC_FNS[@]}"; do "$_fn"; done

  while true; do
    _tmd_show_review
    echo ""
    echo -ne "  ${BOLD}Edit a section (1-${#TMD_SEC_FNS[@]}) or Enter to install:${RESET} "
    local _choice
    read -r _choice </dev/tty
    [[ -z "$_choice" ]] && break
    if [[ "$_choice" =~ ^[0-9]+$ ]] && (( _choice >= 1 && _choice <= ${#TMD_SEC_FNS[@]} )); then
      "${TMD_SEC_FNS[$((_choice-1))]}"
    fi
  done
}

# ─── TM-only native config ────────────────────────────────────────────────────

_tmn_sec_general() {
  sep; echo ""
  echo -e "  ${BOLD}-- General --${RESET}"
  ask "Install directory" "/opt/traefik-manager" NATIVE_INSTALL_DIR
  ask "Data directory"    "/var/lib/traefik-manager" NATIVE_DATA_DIR
  ask "Port"              "5000" TM_PORT
}

_tmn_sec_user() {
  sep; echo ""
  echo -e "  ${BOLD}-- Service User --${RESET}"
  ask_yn "Create a dedicated system user (traefik-manager)?" "y" CREATE_SVC_USER
}

_tmn_sec_config() {
  sep; echo ""
  echo -e "  ${BOLD}-- Dynamic Config --${RESET}"
  info "Single file is simpler. Directory (one .yml per service) is easier at scale."
  ask_choice "Dynamic config layout" CONFIG_LAYOUT \
    "Single file (dynamic.yml)" \
    "Directory - one .yml file per service"
  if [[ "$CONFIG_LAYOUT" == "Single file"* ]]; then
    ask "Path to Traefik dynamic config file" "/etc/traefik/dynamic.yml" NATIVE_CONFIG_PATH
  else
    ask "Path to Traefik dynamic config directory" "/etc/traefik/conf.d" NATIVE_CONFIG_DIR
  fi
}

_tmn_sec_mounts() {
  sep; echo ""
  echo -e "  ${BOLD}-- Optional Mounts --${RESET}"
  info "Expose extra Traefik data to Traefik Manager for richer visibility."
  ask_yn "Mount SSL certs (acme.json)?" "y" MOUNT_CERTS
  if [[ "$MOUNT_CERTS" == "true" ]]; then
    ask "Path to acme.json" "/etc/traefik/acme.json" ACME_JSON_HOST_PATH
  fi
  ask_yn "Mount access logs?" "y" MOUNT_ACCESS_LOGS
  if [[ "$MOUNT_ACCESS_LOGS" == "true" ]]; then
    ask "Path to Traefik access log" "/var/log/traefik/access.log" ACCESS_LOG_PATH
  fi
  ask_yn "Mount Traefik static config (traefik.yml)?" "n" MOUNT_STATIC_CONFIG
  if [[ "$MOUNT_STATIC_CONFIG" == "true" ]]; then
    ask "Path to traefik.yml" "/etc/traefik/traefik.yml" TRAEFIK_YML_HOST_PATH
    sep
    echo ""
    echo -e "  ${BOLD}-- Static Config Editor --${RESET}"
    local traefik_deploy_choice
    ask_choice "How is Traefik running on this server?" traefik_deploy_choice \
      "Docker" \
      "Linux service (systemd)"
    if [[ "$traefik_deploy_choice" == "Linux service"* ]]; then
      TRAEFIK_SYSTEMD="true"
      RESTART_METHOD="poison-pill"
      ask "Traefik service name" "traefik" TRAEFIK_SERVICE_NAME
      ask "Signal file path" "/var/lib/traefik-manager/signals/restart.sig" SIGNAL_FILE_PATH
    else
      TRAEFIK_SYSTEMD="false"
      info "Choose how TM should restart Traefik after saving static config changes."
      local choice
      ask_choice "Restart method" choice \
        "Poison pill (recommended - signal file, no Docker socket needed)" \
        "Direct Docker socket (requires TM user to have Docker group access)"
      case "$choice" in
        "Poison pill"*)          RESTART_METHOD="poison-pill" ;;
        "Direct Docker socket"*) RESTART_METHOD="socket" ;;
      esac
      if [[ "$RESTART_METHOD" == "socket" ]]; then
        ask "Traefik container name" "traefik" TRAEFIK_CONTAINER
      fi
      if [[ "$RESTART_METHOD" == "poison-pill" ]]; then
        ask "Signal file path" "/var/lib/traefik-manager/signals/restart.sig" SIGNAL_FILE_PATH
      fi
    fi
  else
    RESTART_METHOD=""
    TRAEFIK_SYSTEMD="false"
  fi
}

_tmn_build_section_list() {
  TMN_SEC_LABELS=("General" "Service user" "Dynamic config" "Optional mounts")
  TMN_SEC_FNS=(_tmn_sec_general _tmn_sec_user _tmn_sec_config _tmn_sec_mounts)
}

_tmn_show_review() {
  echo ""
  echo -e "  ${BOLD}Review configuration${RESET}"
  echo -e "  ${DIM}────────────────────────────────────────────────────────${RESET}"
  local _i=1
  for _lbl in "${TMN_SEC_LABELS[@]}"; do
    local _val=""
    case "$_lbl" in
      "General") _val="${NATIVE_INSTALL_DIR}  data:${NATIVE_DATA_DIR}  :${TM_PORT}" ;;
      "Service user")
        [[ "$CREATE_SVC_USER" == "true" ]] && _val="dedicated (traefik-manager)" || _val="current user"
        ;;
      "Dynamic config")
        if [[ "$CONFIG_LAYOUT" == "Single file"* ]]; then _val="Single file  ${NATIVE_CONFIG_PATH}"
        else _val="Directory  ${NATIVE_CONFIG_DIR}"; fi
        ;;
      "Optional mounts")
        local _m=""
        [[ "$MOUNT_CERTS" == "true" ]]         && _m+="certs "
        [[ "$MOUNT_ACCESS_LOGS" == "true" ]]  && _m+="logs "
        [[ "$MOUNT_STATIC_CONFIG" == "true" ]] && _m+="static(restart:${RESTART_METHOD})"
        _val="${_m:-(none)}"
        ;;
    esac
    printf "  \033[1;36m%2d\033[0m  \033[2m%-20s\033[0m  %s\n" "$_i" "$_lbl" "$_val"
    (( _i++ ))
  done
  echo -e "  ${DIM}────────────────────────────────────────────────────────${RESET}"
}

gather_tm_native() {
  step "Traefik Manager - Linux Service Setup"
  echo -e "  ${DIM}Press Enter to accept defaults shown in brackets.${RESET}"

  _tmn_build_section_list
  for _fn in "${TMN_SEC_FNS[@]}"; do "$_fn"; done

  while true; do
    _tmn_show_review
    echo ""
    echo -ne "  ${BOLD}Edit a section (1-${#TMN_SEC_FNS[@]}) or Enter to install:${RESET} "
    local _choice
    read -r _choice </dev/tty
    [[ -z "$_choice" ]] && break
    if [[ "$_choice" =~ ^[0-9]+$ ]] && (( _choice >= 1 && _choice <= ${#TMN_SEC_FNS[@]} )); then
      "${TMN_SEC_FNS[$((_choice-1))]}"
    fi
  done
}

# ─── Full stack scaffold ──────────────────────────────────────────────────────

scaffold_full() {
  step "Creating directory structure at ${INSTALL_DIR}"
  mkdir -p "${INSTALL_DIR}/traefik/"{config,logs}
  mkdir -p "${INSTALL_DIR}/traefik-manager/"{config,backups}
  touch "${INSTALL_DIR}/traefik/acme.json"
  chmod 600 "${INSTALL_DIR}/traefik/acme.json"
  touch "${INSTALL_DIR}/traefik/logs/access.log"
  ok "Directories and seed files created"

  if [[ "$ADD_CROWDSEC" == "true" && "$CROWDSEC_MODE" == "Install"* ]]; then
    mkdir -p "${INSTALL_DIR}/crowdsec"
    cat > "${INSTALL_DIR}/crowdsec/acquis.yaml" <<'EOF'
filenames:
  - /var/log/traefik/access.log
labels:
  type: traefik
EOF
    ok "crowdsec/acquis.yaml created"
  fi
}

build_traefik_static() {
  local resolver_block=""
  if [[ "$TLS_TYPE" == "http" ]]; then
    resolver_block="
certificatesResolvers:
  ${CERT_RESOLVER}:
    acme:
      email: ${ACME_EMAIL}
      storage: /acme.json
      httpChallenge:
        entryPoint: web"
  elif [[ "$TLS_TYPE" == "dns" ]]; then
    resolver_block="
certificatesResolvers:
  ${CERT_RESOLVER}:
    acme:
      email: ${ACME_EMAIL}
      storage: /acme.json
      dnsChallenge:
        provider: ${DNS_PROVIDER}
        resolvers:
          - \"1.1.1.1:53\"
          - \"8.8.8.8:53\""
  fi

  local file_provider=""
  if [[ "$CONFIG_LAYOUT" == "Single file"* ]]; then
    file_provider="  file:
    filename: /etc/traefik/config/dynamic.yml
    watch: true"
  else
    file_provider="  file:
    directory: /etc/traefik/config
    watch: true"
  fi

  local entrypoints_block=""
  if [[ "$TLS_TYPE" != "none" ]]; then
    entrypoints_block="  web:
    address: \":80\"
    http:
      redirections:
        entryPoint:
          to: websecure
          scheme: https
  websecure:
    address: \":443\""
  else
    entrypoints_block="  web:
    address: \":80\""
  fi

  cat > "${INSTALL_DIR}/traefik/traefik.yml" <<EOF
api:
  dashboard: ${ENABLE_DASHBOARD}
  insecure: true

entryPoints:
${entrypoints_block}

providers:
  docker:
    exposedByDefault: false
    network: ${DOCKER_NETWORK}
${file_provider}
${resolver_block}

log:
  level: INFO

accessLog:
  filePath: /logs/access.log
  bufferingSize: 100
EOF
  ok "traefik/traefik.yml written"
}

build_dynamic_config() {
  if [[ "$CONFIG_LAYOUT" == "Single file"* ]]; then
    cat > "${INSTALL_DIR}/traefik/config/dynamic.yml" <<'EOF'
http:
  routers: {}
  services: {}
  middlewares: {}
EOF
    ok "traefik/config/dynamic.yml created"
  else
    mkdir -p "${INSTALL_DIR}/traefik/config"
    cat > "${INSTALL_DIR}/traefik/config/example-app.yml.disabled" <<'EOF'
http:
  routers:
    my-app:
      rule: "Host(`app.example.com`)"
      entryPoints:
        - websecure
      service: my-app
      tls:
        certResolver: ${CERT_RESOLVER}

  services:
    my-app:
      loadBalancer:
        servers:
          - url: "http://my-app-container:3000"
EOF
    ok "traefik/config/ directory created"
  fi
}

build_compose_full() {
  if [[ "$ADD_CROWDSEC" == "true" && "$CROWDSEC_MODE" == "Install"* ]]; then
    CS_KEY=$(openssl rand -hex 32)
    CS_LAPI_URL="http://crowdsec:8080"
    CS_API_KEY="$CS_KEY"
    CS_MACHINE_ID="traefik-manager"
    CS_MACHINE_PW=$(openssl rand -hex 24)
  fi

  local tls_label_traefik="" tls_label_tm=""
  if [[ "$TLS_TYPE" != "none" ]]; then
    tls_label_traefik='      - "traefik.http.routers.dashboard.tls.certresolver='"${CERT_RESOLVER}"'"'
    tls_label_tm='      - "traefik.http.routers.traefik-manager.tls.certresolver='"${CERT_RESOLVER}"'"'
  fi

  local traefik_env=""
  if [[ -n "$DNS_ENV_BLOCK" ]]; then
    traefik_env="    environment:
${DNS_ENV_BLOCK}"
  fi

  local traefik_vols="      - /var/run/docker.sock:/var/run/docker.sock:ro
      - ./traefik/traefik.yml:/traefik.yml:ro
      - ./traefik/acme.json:/acme.json
      - ./traefik/logs:/logs"
  if [[ "$CONFIG_LAYOUT" == "Single file"* ]]; then
    traefik_vols+="
      - ./traefik/config/dynamic.yml:/etc/traefik/config/dynamic.yml:ro"
  else
    traefik_vols+="
      - ./traefik/config:/etc/traefik/config:ro"
  fi

  local traefik_healthcheck=""
  local traefik_static_labels=""
  if [[ "$MOUNT_STATIC_CONFIG" == "true" ]]; then
    traefik_static_labels='      - "traefik-manager.role=traefik"
      - "traefik-manager.static-config=/app/traefik.yml"
      - "traefik-manager.restart-method='"${RESTART_METHOD}"'"'
    if [[ "$RESTART_METHOD" == "poison-pill" ]]; then
      traefik_vols+="
      - tm-signals:/signals"
      traefik_healthcheck='    healthcheck:
      test: ["CMD-SHELL", "[ ! -f /signals/restart.sig ] || (rm /signals/restart.sig && kill -TERM 1)"]
      interval: 5s
      timeout: 3s
      retries: 1'
    fi
  fi

  local tm_vols="      - ./traefik-manager/config:/app/config
      - ./traefik-manager/backups:/app/backups"
  if [[ "$RESTART_METHOD" == "socket" ]]; then
    tm_vols="      - /var/run/docker.sock:/var/run/docker.sock:ro
${tm_vols}"
  fi
  if [[ "$MOUNT_ACCESS_LOGS" == "true" ]]; then
    tm_vols+="
      - ./traefik/logs:/app/logs:ro"
  fi
  if [[ "$MOUNT_CERTS" == "true" ]]; then
    tm_vols+="
      - ./traefik/acme.json:/app/acme.json:ro"
  fi
  if [[ "$MOUNT_STATIC_CONFIG" == "true" ]]; then
    tm_vols+="
      - ./traefik/traefik.yml:/app/traefik.yml"
    if [[ "$RESTART_METHOD" == "poison-pill" ]]; then
      tm_vols+="
      - tm-signals:/signals"
    fi
  fi
  if [[ "$CONFIG_LAYOUT" == "Single file"* ]]; then
    tm_vols+="
      - ./traefik/config/dynamic.yml:/app/config/dynamic.yml"
  else
    tm_vols+="
      - ./traefik/config:/app/config/dynamic"
  fi

  local tm_networks="      - ${DOCKER_NETWORK}"
  if [[ "$MOUNT_STATIC_CONFIG" == "true" && "$RESTART_METHOD" == "proxy" ]]; then
    tm_networks+="
      - socket-proxy-net"
  fi

  local static_env=""
  if [[ "$MOUNT_STATIC_CONFIG" == "true" ]]; then
    static_env="      - STATIC_CONFIG_PATH=/app/traefik.yml
      - RESTART_METHOD=${RESTART_METHOD}
      - TRAEFIK_CONTAINER=${TRAEFIK_CONTAINER}"
    if [[ "$RESTART_METHOD" == "proxy" ]]; then
      static_env+="
      - DOCKER_HOST=tcp://socket-proxy:2375"
    elif [[ "$RESTART_METHOD" == "poison-pill" ]]; then
      static_env+="
      - SIGNAL_FILE_PATH=/signals/restart.sig"
    fi
  fi

  local cookie_secure="false"
  [[ "$TLS_TYPE" != "none" ]] && cookie_secure="true"

  local port_443=""
  [[ "$TLS_TYPE" != "none" ]] && port_443='      - "443:443"'

  local socket_proxy_service=""
  if [[ "$MOUNT_STATIC_CONFIG" == "true" && "$RESTART_METHOD" == "proxy" ]]; then
    socket_proxy_service="
  socket-proxy:
    image: tecnativa/docker-socket-proxy
    container_name: socket-proxy
    restart: unless-stopped
    networks:
      - socket-proxy-net
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
    environment:
      CONTAINERS: 1
      POST: 1"
  fi

  local extra_networks=""
  if [[ "$MOUNT_STATIC_CONFIG" == "true" && "$RESTART_METHOD" == "proxy" ]]; then
    extra_networks="  socket-proxy-net:
    internal: true"
  fi

  local volumes_section=""
  if [[ "$MOUNT_STATIC_CONFIG" == "true" && "$RESTART_METHOD" == "poison-pill" ]] || \
     [[ "$ADD_CROWDSEC" == "true" && "$CROWDSEC_MODE" == "Install"* ]]; then
    volumes_section="
volumes:"
    [[ "$MOUNT_STATIC_CONFIG" == "true" && "$RESTART_METHOD" == "poison-pill" ]] && \
      volumes_section+="
  tm-signals:"
    [[ "$ADD_CROWDSEC" == "true" && "$CROWDSEC_MODE" == "Install"* ]] && \
      volumes_section+="
  crowdsec_data:"
  fi

  local crowdsec_env=""
  if [[ "$ADD_CROWDSEC" == "true" ]]; then
    crowdsec_env="      - CROWDSEC_LAPI_URL=${CS_LAPI_URL}
      - CROWDSEC_API_KEY=${CS_API_KEY}"
    if [[ -n "$CS_MACHINE_ID" && -n "$CS_MACHINE_PW" ]]; then
      crowdsec_env="${crowdsec_env}
      - CROWDSEC_MACHINE_ID=${CS_MACHINE_ID}
      - CROWDSEC_MACHINE_PASSWORD=${CS_MACHINE_PW}"
    fi
  fi

  local crowdsec_service=""
  if [[ "$ADD_CROWDSEC" == "true" && "$CROWDSEC_MODE" == "Install"* ]]; then
    crowdsec_service="
  crowdsec:
    image: crowdsecurity/crowdsec:latest
    container_name: crowdsec
    restart: unless-stopped
    networks:
      - ${DOCKER_NETWORK}
    environment:
      - BOUNCER_KEY_traefik-manager=${CS_KEY}
      - COLLECTIONS=crowdsecurity/traefik
    volumes:
      - crowdsec_data:/var/lib/crowdsec/data
      - ./crowdsec/acquis.yaml:/etc/crowdsec/acquis.yaml:ro
      - ./traefik/logs/access.log:/var/log/traefik/access.log:ro"
  fi

  cat > "${INSTALL_DIR}/docker-compose.yml" <<EOF
networks:
  ${DOCKER_NETWORK}:
    external: false
    name: ${DOCKER_NETWORK}
$(if [[ -n "$extra_networks" ]]; then echo "$extra_networks"; fi)
$(if [[ -n "$volumes_section" ]]; then echo "$volumes_section"; fi)
services:

  traefik:
    image: traefik:latest
    container_name: traefik
    restart: unless-stopped
    networks:
      - ${DOCKER_NETWORK}
    ports:
      - "80:80"
$(if [[ -n "$port_443" ]]; then echo "$port_443"; fi)
      - "${TRAEFIK_API_PORT}:8080"
    volumes:
${traefik_vols}
$(if [[ -n "$traefik_env" ]]; then echo "$traefik_env"; fi)
$(if [[ -n "$traefik_healthcheck" ]]; then echo "$traefik_healthcheck"; fi)
    labels:
      - "traefik.enable=true"
      - "traefik.http.routers.dashboard.rule=Host(\`${TRAEFIK_DASHBOARD_HOST}\`)"
      - "traefik.http.routers.dashboard.entrypoints=${TRAEFIK_ENTRYPOINT}"
      - "traefik.http.routers.dashboard.service=api@internal"
$(if [[ -n "$tls_label_traefik" ]]; then echo "$tls_label_traefik"; fi)
$(if [[ -n "$traefik_static_labels" ]]; then echo "$traefik_static_labels"; fi)

  traefik-manager:
    image: ghcr.io/chr0nzz/traefik-manager:latest
    container_name: traefik-manager
    restart: unless-stopped
    networks:
${tm_networks}
    volumes:
${tm_vols}
    environment:
      - COOKIE_SECURE=${cookie_secure}
$(if [[ "$CONFIG_LAYOUT" == "Directory"* ]]; then echo "      - CONFIG_DIR=/app/config/dynamic"; fi)
$(if [[ -n "$static_env" ]]; then echo "$static_env"; fi)
$(if [[ -n "$crowdsec_env" ]]; then echo "$crowdsec_env"; fi)
    labels:
      - "traefik.enable=true"
      - "traefik.http.routers.traefik-manager.rule=Host(\`${TM_HOST}\`)"
      - "traefik.http.routers.traefik-manager.entrypoints=${TRAEFIK_ENTRYPOINT}"
      - "traefik.http.services.traefik-manager.loadbalancer.server.port=5000"
$(if [[ -n "$tls_label_tm" ]]; then echo "$tls_label_tm"; fi)
    depends_on:
      - traefik
$(if [[ -n "$socket_proxy_service" ]]; then echo "$socket_proxy_service"; fi)
$(if [[ -n "$crowdsec_service" ]]; then echo "$crowdsec_service"; fi)
EOF
  ok "docker-compose.yml written"
}

# ─── TM-only Docker scaffold ──────────────────────────────────────────────────

scaffold_tm_docker() {
  step "Creating directory structure at ${INSTALL_DIR}"
  mkdir -p "${INSTALL_DIR}/config"
  mkdir -p "${INSTALL_DIR}/backups"
  if [[ "$CONFIG_LAYOUT" == "Single file"* ]]; then
    touch "${INSTALL_DIR}/config/dynamic.yml"
  fi
  ok "Directories created"
}

build_compose_tm() {
  local cookie_secure="false"
  [[ "${TLS_TYPE:-none}" != "none" ]] && cookie_secure="true"

  local tm_vols="      - ./config:/app/config
      - ./backups:/app/backups"
  if [[ "$RESTART_METHOD" == "socket" ]]; then
    tm_vols="      - /var/run/docker.sock:/var/run/docker.sock:ro
${tm_vols}"
  fi
  if [[ "${MOUNT_ACCESS_LOGS:-false}" == "true" ]]; then
    tm_vols+="
      - ${ACCESS_LOG_PATH}:/app/logs/access.log:ro"
  fi
  if [[ "${MOUNT_CERTS:-false}" == "true" ]]; then
    tm_vols+="
      - ${ACME_JSON_HOST_PATH}:/app/acme.json:ro"
  fi
  if [[ "$MOUNT_STATIC_CONFIG" == "true" ]]; then
    tm_vols+="
      - ${TRAEFIK_YML_HOST_PATH}:/app/traefik.yml"
    if [[ "$RESTART_METHOD" == "poison-pill" ]]; then
      tm_vols+="
      - tm-signals:/signals"
    fi
  fi
  if [[ "$CONFIG_LAYOUT" == "Single file"* ]]; then
    tm_vols+="
      - ./config/dynamic.yml:/app/config/dynamic.yml"
  else
    tm_vols+="
      - ./config:/app/config/dynamic"
  fi

  local tm_networks="      - ${DOCKER_NETWORK}"
  if [[ "$MOUNT_STATIC_CONFIG" == "true" && "$RESTART_METHOD" == "proxy" ]]; then
    tm_networks+="
      - socket-proxy-net"
  fi

  local static_env=""
  if [[ "$MOUNT_STATIC_CONFIG" == "true" ]]; then
    static_env="      - STATIC_CONFIG_PATH=/app/traefik.yml
      - RESTART_METHOD=${RESTART_METHOD}
      - TRAEFIK_CONTAINER=${TRAEFIK_CONTAINER}"
    if [[ "$RESTART_METHOD" == "proxy" ]]; then
      static_env+="
      - DOCKER_HOST=tcp://socket-proxy:2375"
    elif [[ "$RESTART_METHOD" == "poison-pill" ]]; then
      static_env+="
      - SIGNAL_FILE_PATH=/signals/restart.sig"
    fi
  fi

  local ports_block=""
  if [[ -n "${TM_PORT:-}" ]]; then
    ports_block="    ports:
      - \"${TM_PORT}:5000\""
  fi

  local labels_block=""
  if [[ "${USE_TRAEFIK_LABELS:-false}" == "true" ]]; then
    local tls_label=""
    [[ "${TLS_TYPE:-none}" != "none" ]] && tls_label='      - "traefik.http.routers.traefik-manager.tls.certresolver='"${CERT_RESOLVER}"'"'
    labels_block="    labels:
      - \"traefik.enable=true\"
      - \"traefik.http.routers.traefik-manager.rule=Host(\`${TM_HOST}\`)\"
      - \"traefik.http.routers.traefik-manager.entrypoints=${TRAEFIK_ENTRYPOINT}\"
      - \"traefik.http.services.traefik-manager.loadbalancer.server.port=5000\"
$(if [[ -n "$tls_label" ]]; then echo "$tls_label"; fi)"
  fi

  local network_def=""
  if [[ "${NETWORK_EXTERNAL:-false}" == "true" ]]; then
    network_def="networks:
  ${DOCKER_NETWORK}:
    external: true
    name: ${DOCKER_NETWORK}"
  else
    network_def="networks:
  ${DOCKER_NETWORK}:
    external: false
    name: ${DOCKER_NETWORK}"
  fi
  if [[ "$MOUNT_STATIC_CONFIG" == "true" && "$RESTART_METHOD" == "proxy" ]]; then
    network_def+="
  socket-proxy-net:
    internal: true"
  fi

  local volumes_section=""
  if [[ "$MOUNT_STATIC_CONFIG" == "true" && "$RESTART_METHOD" == "poison-pill" ]]; then
    volumes_section="
volumes:
  tm-signals:"
  fi

  local socket_proxy_service=""
  if [[ "$MOUNT_STATIC_CONFIG" == "true" && "$RESTART_METHOD" == "proxy" ]]; then
    socket_proxy_service="
  socket-proxy:
    image: tecnativa/docker-socket-proxy
    container_name: socket-proxy
    restart: unless-stopped
    networks:
      - socket-proxy-net
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
    environment:
      CONTAINERS: 1
      POST: 1"
  fi

  local config_dir_env=""
  [[ "$CONFIG_LAYOUT" == "Directory"* ]] && config_dir_env="      - CONFIG_DIR=/app/config/dynamic"

  cat > "${INSTALL_DIR}/docker-compose.yml" <<EOF
${network_def}
$(if [[ -n "$volumes_section" ]]; then echo "$volumes_section"; fi)
services:
  traefik-manager:
    image: ghcr.io/chr0nzz/traefik-manager:latest
    container_name: traefik-manager
    restart: unless-stopped
    networks:
${tm_networks}
${ports_block}
    volumes:
${tm_vols}
    environment:
      - COOKIE_SECURE=${cookie_secure}
${config_dir_env}
$(if [[ -n "$static_env" ]]; then echo "$static_env"; fi)
${labels_block}
$(if [[ -n "$socket_proxy_service" ]]; then echo "$socket_proxy_service"; fi)
EOF
  ok "docker-compose.yml written"
}

# ─── Native install ───────────────────────────────────────────────────────────

install_tm_native() {
  step "Installing Traefik Manager"

  if [[ -d "${NATIVE_INSTALL_DIR}" ]]; then
    warn "${NATIVE_INSTALL_DIR} already exists. Pulling latest changes."
    git -C "${NATIVE_INSTALL_DIR}" pull
  else
    git clone --branch main https://github.com/chr0nzz/traefik-manager.git "${NATIVE_INSTALL_DIR}"
  fi
  ok "Repository cloned to ${NATIVE_INSTALL_DIR}"

  python3 -m venv "${NATIVE_INSTALL_DIR}/venv"
  "${NATIVE_INSTALL_DIR}/venv/bin/pip" install -q -r "${NATIVE_INSTALL_DIR}/requirements.txt" gunicorn
  ok "Python dependencies installed"

  bash "${NATIVE_INSTALL_DIR}/scripts/setup-assets.sh"
  ok "Vendor assets and Tailwind CSS built"

  mkdir -p "${NATIVE_DATA_DIR}/backups"
  ok "Data directories created at ${NATIVE_DATA_DIR}"

  if [[ "$MOUNT_STATIC_CONFIG" == "true" && "$RESTART_METHOD" == "poison-pill" ]]; then
    local signal_dir="${SIGNAL_FILE_PATH%/*}"
    mkdir -p "$signal_dir"
    ok "Signal directory created at ${signal_dir}"
  fi

  if [[ "$CREATE_SVC_USER" == "true" ]]; then
    if ! id traefik-manager &>/dev/null; then
      sudo useradd --system --no-create-home --shell /usr/sbin/nologin traefik-manager
      ok "System user traefik-manager created"
    else
      ok "System user traefik-manager already exists"
    fi
    sudo chown -R traefik-manager: "${NATIVE_INSTALL_DIR}"
    sudo chown -R traefik-manager: "${NATIVE_DATA_DIR}"
    if [[ "$MOUNT_STATIC_CONFIG" == "true" && "$RESTART_METHOD" == "poison-pill" ]]; then
      sudo chown -R traefik-manager: "${SIGNAL_FILE_PATH%/*}"
    fi
    if [[ "$MOUNT_STATIC_CONFIG" == "true" && "$RESTART_METHOD" == "socket" ]]; then
      sudo usermod -aG docker traefik-manager || true
    fi
    SVC_USER="traefik-manager"
  else
    SVC_USER="${USER}"
  fi

  local config_env=""
  if [[ "$CONFIG_LAYOUT" == "Single file"* ]]; then
    config_env="Environment=CONFIG_PATH=${NATIVE_CONFIG_PATH}"
  else
    config_env="Environment=CONFIG_DIR=${NATIVE_CONFIG_DIR}"
  fi

  local optional_env=""
  if [[ "${MOUNT_CERTS:-false}" == "true" ]]; then
    optional_env+="Environment=ACME_JSON_PATH=${ACME_JSON_HOST_PATH}
"
  fi
  if [[ "${MOUNT_ACCESS_LOGS:-false}" == "true" ]]; then
    optional_env+="Environment=ACCESS_LOG_PATH=${ACCESS_LOG_PATH}
"
  fi
  if [[ "$MOUNT_STATIC_CONFIG" == "true" ]]; then
    optional_env+="Environment=STATIC_CONFIG_PATH=${TRAEFIK_YML_HOST_PATH}
Environment=RESTART_METHOD=${RESTART_METHOD}
"
    if [[ "$RESTART_METHOD" == "socket" ]]; then
      optional_env+="Environment=TRAEFIK_CONTAINER=${TRAEFIK_CONTAINER}
"
    fi
    if [[ "$RESTART_METHOD" == "poison-pill" ]]; then
      optional_env+="Environment=SIGNAL_FILE_PATH=${SIGNAL_FILE_PATH}
"
    fi
  fi

  if [[ "$MOUNT_STATIC_CONFIG" == "true" && "$TRAEFIK_SYSTEMD" == "true" ]]; then
    sudo tee /etc/systemd/system/traefik-restart.path > /dev/null <<EOF
[Unit]
Description=Watch for Traefik Manager restart signal

[Path]
PathExists=${SIGNAL_FILE_PATH}

[Install]
WantedBy=multi-user.target
EOF
    sudo tee /etc/systemd/system/traefik-restart.service > /dev/null <<EOF
[Unit]
Description=Restart Traefik on signal from Traefik Manager

[Service]
Type=oneshot
ExecStart=/bin/sh -c 'rm -f ${SIGNAL_FILE_PATH} && systemctl restart ${TRAEFIK_SERVICE_NAME}'
EOF
    sudo systemctl daemon-reload
    sudo systemctl enable --now traefik-restart.path
    ok "Traefik restart watcher enabled (traefik-restart.path)"
  fi

  sudo tee /etc/systemd/system/traefik-manager.service > /dev/null <<EOF
[Unit]
Description=Traefik Manager
After=network.target

[Service]
Type=simple
User=${SVC_USER}
WorkingDirectory=${NATIVE_INSTALL_DIR}
Environment=HOME=${NATIVE_INSTALL_DIR}
ExecStart=${NATIVE_INSTALL_DIR}/venv/bin/gunicorn \\
    --bind 0.0.0.0:${TM_PORT} \\
    --workers 1 \\
    --log-level info \\
    app:app
${config_env}
Environment=BACKUP_DIR=${NATIVE_DATA_DIR}/backups
Environment=SETTINGS_PATH=${NATIVE_DATA_DIR}/manager.yml
Environment=COOKIE_SECURE=false
${optional_env}
Restart=on-failure
RestartSec=5

[Install]
WantedBy=multi-user.target
EOF
  ok "systemd unit written to /etc/systemd/system/traefik-manager.service"

  sudo systemctl daemon-reload
  sudo systemctl enable --now traefik-manager
  ok "Service enabled and started"
}

# ─── Start Docker services ────────────────────────────────────────────────────

start_docker() {
  step "Pulling images"
  cd "${INSTALL_DIR}"
  $COMPOSE_CMD pull

  step "Starting services"
  $COMPOSE_CMD up -d
  ok "Services started"

  if [[ "$ADD_CROWDSEC" == "true" && "$CROWDSEC_MODE" == "Install"* && -n "$CS_MACHINE_ID" && -n "$CS_MACHINE_PW" ]]; then
    step "Registering CrowdSec machine for alerts"
    local i
    for i in $(seq 1 30); do
      if docker exec crowdsec cscli lapi status >/dev/null 2>&1; then break; fi
      sleep 2
    done
    if docker exec crowdsec cscli machines add "$CS_MACHINE_ID" --password "$CS_MACHINE_PW" --force >/dev/null 2>&1; then
      ok "CrowdSec machine '${CS_MACHINE_ID}' registered (enables the Alerts view)"
    else
      warn "Could not auto-register the CrowdSec machine. Run manually once CrowdSec is up:"
      echo -e "  ${DIM}docker exec crowdsec cscli machines add ${CS_MACHINE_ID} --password '${CS_MACHINE_PW}' --force${RESET}"
    fi
  fi
}

# ─── Fetch temp password ──────────────────────────────────────────────────────

fetch_password_docker() {
  step "Waiting for Traefik Manager to generate temporary password"
  TEMP_PASSWORD=""
  local attempts=0

  while [[ $attempts -lt 20 ]]; do
    local log_line
    log_line=$(docker logs traefik-manager 2>&1 | grep -A3 "AUTO-GENERATED" | grep "Password:" | grep -oP '(?<=Password: )\S+' || true)
    if [[ -n "$log_line" ]]; then
      TEMP_PASSWORD="$log_line"
      ok "Temporary password retrieved"
      break
    fi
    sleep 1.5
    (( attempts++ )) || true
  done

  if [[ -z "$TEMP_PASSWORD" ]]; then
    warn "Could not retrieve temporary password. Check: docker logs traefik-manager"
  fi
}

fetch_password_native() {
  step "Waiting for Traefik Manager to generate temporary password"
  TEMP_PASSWORD=""
  local attempts=0

  while [[ $attempts -lt 20 ]]; do
    local log_line
    log_line=$(sudo journalctl -u traefik-manager --no-pager -n 50 2>/dev/null | grep -A3 "AUTO-GENERATED" | grep "Password:" | grep -oP '(?<=Password: )\S+' || true)
    if [[ -n "$log_line" ]]; then
      TEMP_PASSWORD="$log_line"
      ok "Temporary password retrieved"
      break
    fi
    sleep 1.5
    (( attempts++ )) || true
  done

  if [[ -z "$TEMP_PASSWORD" ]]; then
    warn "Could not retrieve temporary password. Check: sudo journalctl -u traefik-manager"
  fi
}

# ─── Summaries ────────────────────────────────────────────────────────────────

print_static_config_summary() {
  if [[ "$MOUNT_STATIC_CONFIG" != "true" ]]; then return; fi
  echo ""
  echo -e "  ${CYAN}${BOLD}Static Config Editor${RESET}"
  case "$RESTART_METHOD" in
    proxy)
      echo -e "  ${DIM}Restart method  socket proxy (tecnativa/docker-socket-proxy)${RESET}"
      echo -e "  ${DIM}The socket-proxy service is running alongside TM with minimal permissions.${RESET}"
      ;;
    poison-pill)
      echo -e "  ${DIM}Restart method  poison pill (signal file)${RESET}"
      if [[ "$TRAEFIK_SYSTEMD" == "true" ]]; then
        echo -e "  ${DIM}Traefik running as systemd service: ${TRAEFIK_SERVICE_NAME}${RESET}"
        echo -e "  ${DIM}traefik-restart.path watcher is active - restarts ${TRAEFIK_SERVICE_NAME} when TM writes the signal file.${RESET}"
      else
        echo -e "  ${YELLOW}⚠${RESET}  ${DIM}Add this healthcheck to your Traefik service if not already set:${RESET}"
        echo ""
        echo -e "    ${DIM}healthcheck:${RESET}"
        echo -e "    ${DIM}  test: [\"CMD-SHELL\", \"[ ! -f /signals/restart.sig ] || (rm /signals/restart.sig && kill -TERM 1)\"]${RESET}"
        echo -e "    ${DIM}  interval: 5s${RESET}"
        echo -e "    ${DIM}  timeout: 3s${RESET}"
        echo -e "    ${DIM}  retries: 1${RESET}"
        echo ""
      fi
      ;;
    socket)
      echo -e "  ${DIM}Restart method  direct Docker socket${RESET}"
      warn "Full Docker socket is mounted in TM. Keep TM behind authentication."
      ;;
  esac
}

print_summary_full() {
  local scheme="http"
  [[ "$TLS_TYPE" != "none" ]] && scheme="https"

  echo ""
  echo -e "${GREEN}${BOLD}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${RESET}"
  echo -e "${GREEN}${BOLD}  Setup complete!${RESET}"
  echo -e "${GREEN}${BOLD}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${RESET}"
  echo ""
  echo -e "  Traefik dashboard   ${CYAN}${scheme}://${TRAEFIK_DASHBOARD_HOST}${RESET}"
  echo -e "  Traefik Manager     ${CYAN}${scheme}://${TM_HOST}${RESET}"
  echo ""
  if [[ -n "$TEMP_PASSWORD" ]]; then
    echo -e "  ${YELLOW}${BOLD}Temporary password  ${TEMP_PASSWORD}${RESET}"
  else
    echo -e "  ${YELLOW}Temporary password  run: docker logs traefik-manager${RESET}"
  fi
  echo -e "  Install dir         ${DIM}${INSTALL_DIR}${RESET}"
  echo ""
  if [[ "$CONFIG_LAYOUT" == "Single file"* ]]; then
    echo -e "  ${DIM}Dynamic config  ${INSTALL_DIR}/traefik/config/dynamic.yml${RESET}"
  else
    echo -e "  ${DIM}Dynamic config  ${INSTALL_DIR}/traefik/config/*.yml${RESET}"
  fi
  print_static_config_summary
  if [[ "$ADD_CROWDSEC" == "true" ]]; then
    echo ""
    echo -e "  ${CYAN}${BOLD}CrowdSec${RESET}"
    if [[ "$CROWDSEC_MODE" == "Install"* ]]; then
      echo -e "  ${DIM}Mode            installed as part of this stack${RESET}"
      echo -e "  ${DIM}LAPI URL        http://crowdsec:8080${RESET}"
      echo -e "  ${DIM}Bouncer key     ${CS_KEY}${RESET}"
      echo -e "  ${DIM}Machine ID      ${CS_MACHINE_ID}${RESET}"
      echo -e "  ${DIM}Machine pass    ${CS_MACHINE_PW}${RESET}"
      info "Enable the CrowdSec tab in Traefik Manager under Settings to view decisions and alerts."
    else
      echo -e "  ${DIM}Mode            connected to existing instance${RESET}"
      echo -e "  ${DIM}LAPI URL        ${CS_LAPI_URL}${RESET}"
      [[ -n "$CS_MACHINE_ID" ]] && echo -e "  ${DIM}Machine ID      ${CS_MACHINE_ID}${RESET}"
      [[ -z "$CS_MACHINE_ID" ]] && info "Alerts need machine credentials - add CROWDSEC_MACHINE_ID / CROWDSEC_MACHINE_PASSWORD later in Settings or env."
      info "Enable the CrowdSec tab in Traefik Manager under Settings to view decisions and alerts."
    fi
  fi
  echo ""
  echo -e "  ${DIM}cd ${INSTALL_DIR}${RESET}"
  echo -e "  ${DIM}${COMPOSE_CMD} logs -f traefik-manager${RESET}"
  echo ""
  echo -e "  ${CYAN}${BOLD}Updating${RESET}"
  echo -e "  ${DIM}  cd ${INSTALL_DIR} && ${COMPOSE_CMD} pull && ${COMPOSE_CMD} up -d${RESET}"
  echo ""
  if [[ "$EXTERNAL" == "true" ]]; then
    warn "DNS A records for ${TRAEFIK_DASHBOARD_HOST} and ${TM_HOST} must point to this server's IP."
  fi
  if [[ "$TLS_TYPE" == "none" ]]; then
    warn "TLS is disabled. Consider enabling it before exposing this publicly."
  fi
  echo ""
}

print_summary_tm_docker() {
  local scheme="http"
  local access_url=""
  if [[ "${USE_TRAEFIK_LABELS:-false}" == "true" ]]; then
    [[ "${TLS_TYPE:-none}" != "none" ]] && scheme="https"
    access_url="${scheme}://${TM_HOST}"
  else
    access_url="http://$(hostname -I | awk '{print $1}'):${TM_PORT}"
  fi

  echo ""
  echo -e "${GREEN}${BOLD}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${RESET}"
  echo -e "${GREEN}${BOLD}  Setup complete!${RESET}"
  echo -e "${GREEN}${BOLD}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${RESET}"
  echo ""
  echo -e "  Traefik Manager     ${CYAN}${access_url}${RESET}"
  echo ""
  if [[ -n "$TEMP_PASSWORD" ]]; then
    echo -e "  ${YELLOW}${BOLD}Temporary password  ${TEMP_PASSWORD}${RESET}"
  else
    echo -e "  ${YELLOW}Temporary password  run: docker logs traefik-manager${RESET}"
  fi
  echo -e "  Install dir         ${DIM}${INSTALL_DIR}${RESET}"
  print_static_config_summary
  echo ""
  echo -e "  ${DIM}cd ${INSTALL_DIR}${RESET}"
  echo -e "  ${DIM}${COMPOSE_CMD} logs -f traefik-manager${RESET}"
  echo ""
  echo -e "  ${CYAN}${BOLD}Updating${RESET}"
  echo -e "  ${DIM}  cd ${INSTALL_DIR} && ${COMPOSE_CMD} pull && ${COMPOSE_CMD} up -d${RESET}"
  echo ""
}

print_summary_native() {
  echo ""
  echo -e "${GREEN}${BOLD}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${RESET}"
  echo -e "${GREEN}${BOLD}  Setup complete!${RESET}"
  echo -e "${GREEN}${BOLD}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${RESET}"
  echo ""
  echo -e "  Traefik Manager     ${CYAN}http://$(hostname -I | awk '{print $1}'):${TM_PORT}${RESET}"
  echo ""
  if [[ -n "$TEMP_PASSWORD" ]]; then
    echo -e "  ${YELLOW}${BOLD}Temporary password  ${TEMP_PASSWORD}${RESET}"
  else
    echo -e "  ${YELLOW}Temporary password  run: sudo journalctl -u traefik-manager${RESET}"
  fi
  echo -e "  Install dir         ${DIM}${NATIVE_INSTALL_DIR}${RESET}"
  echo -e "  Data dir            ${DIM}${NATIVE_DATA_DIR}${RESET}"
  print_static_config_summary
  echo ""
  echo -e "  ${DIM}sudo systemctl status traefik-manager${RESET}"
  echo -e "  ${DIM}sudo journalctl -u traefik-manager -f${RESET}"
  echo ""
  echo -e "  ${CYAN}${BOLD}Updating${RESET}"
  echo -e "  ${DIM}  cd ${NATIVE_INSTALL_DIR} && git pull${RESET}"
  echo -e "  ${DIM}  venv/bin/pip install -q -r requirements.txt gunicorn${RESET}"
  echo -e "  ${DIM}  sudo systemctl restart traefik-manager${RESET}"
  echo ""
}

# ─── Agent installer ──────────────────────────────────────────────────────────

AGENT_INSTALL_METHOD=""
AGENT_API_KEY=""
AGENT_TRAEFIK_URL="http://traefik:8080"
AGENT_CONFIG_PATH="/app/config"
AGENT_STATIC_PATH=""
AGENT_INSECURE_TLS="false"
AGENT_ACME_PATH=""
AGENT_LOG_PATH=""
AGENT_PLUGINS_DIR=""
AGENT_RESTART_METHOD=""
AGENT_CONTAINER="traefik"
AGENT_DOCKER_HOST=""
AGENT_SIGNAL_FILE=""
AGENT_CS_MODE="none"
AGENT_CS_URL=""
AGENT_CS_KEY=""
AGENT_CS_INSTALL_KEY=""
AGENT_GIT_ENABLED="false"
AGENT_GIT_REPO=""
AGENT_GIT_BRANCH="main"
AGENT_GIT_USER=""
AGENT_GIT_TOKEN=""
AGENT_GIT_AUTO="true"
AGENT_INSTALL_DIR="${INSTALL_DIR:-/opt/traefik-manager-agent}"
AGENT_PORT="8090"
AGENT_NETWORK="traefik-net"
AGENT_HAS_WEBSECURE="true"
AGENT_TLS_TYPE="none"
AGENT_ACME_EMAIL=""
AGENT_CERT_RESOLVER="letsencrypt"
AGENT_TRAEFIK_DASHBOARD="true"
AGENT_DASHBOARD_HOST=""
AGENT_EXTERNAL="false"
AGENT_CONFIG_LAYOUT=""
AGENT_TRAEFIK_API_PORT="8080"
AGENT_HOST=""
AGENT_SEC_LABELS=()
AGENT_SEC_FNS=()

_agent_sec_method() {
  sep; echo ""
  echo -e "  ${BOLD}-- Install method --${RESET}"
  info "TMA runs alongside Traefik and lets a central TM manage this server remotely."
  ask_choice "Install method" AGENT_INSTALL_METHOD \
    "Docker - Agent only (alongside existing Traefik)" \
    "Docker - Agent + Traefik (deploy both together)" \
    "Binary - Agent only (systemd service, no Docker)"
}

_agent_sec_apikey() {
  sep; echo ""
  echo -e "  ${BOLD}-- API key --${RESET}"
  info "Generate this in TM Settings -> Agents before running the script."
  while true; do
    echo -ne "  ${BOLD}API key (TMA_API_KEY):${RESET} "
    read -rs AGENT_API_KEY </dev/tty; echo ""
    [[ -n "$AGENT_API_KEY" ]] && break
    warn "API key is required."
  done
  ok "API key set"
}

_agent_sec_traefik() {
  sep; echo ""
  echo -e "  ${BOLD}-- Traefik connection --${RESET}"
  ask "Traefik API URL" "http://traefik:8080" AGENT_TRAEFIK_URL
  ask "Dynamic config path" "/app/config" AGENT_CONFIG_PATH
  if [[ "$AGENT_TRAEFIK_URL" == https://* ]]; then
    ask_yn "Skip TLS verification? (needed for self-signed / Cloudflare Origin certs)" "n" AGENT_INSECURE_TLS
  else
    AGENT_INSECURE_TLS="false"
  fi
  local _mount_static
  ask_yn "Mount static config (traefik.yml)?" "n" _mount_static
  if [[ "$_mount_static" == "true" ]]; then
    ask "Static config path" "/etc/traefik/traefik.yml" AGENT_STATIC_PATH
  else
    AGENT_STATIC_PATH=""
  fi
}

_agent_sec_traefik_install() {
  sep; echo ""
  echo -e "  ${BOLD}-- Traefik install --${RESET}"
  info "Traefik will be deployed on the same server alongside the agent."

  local _deploy_type
  ask_choice "Where will this be accessed from?" _deploy_type \
    "External (internet-facing)" \
    "Internal only (LAN / VPN / Tailscale)"
  [[ "$_deploy_type" == "External"* ]] && AGENT_EXTERNAL="true" || AGENT_EXTERNAL="false"

  ask_yn "Enable Traefik dashboard?" "y" AGENT_TRAEFIK_DASHBOARD
  if [[ "$AGENT_TRAEFIK_DASHBOARD" == "true" ]]; then
    ask "Dashboard hostname (e.g. traefik.example.com)" "" AGENT_DASHBOARD_HOST
  else
    AGENT_DASHBOARD_HOST=""
  fi

  sep; echo ""
  echo -e "  ${BOLD}-- TLS / Certificates --${RESET}"
  gather_tls_method
  AGENT_TLS_TYPE="$TLS_TYPE"
  AGENT_CERT_RESOLVER="$CERT_RESOLVER"
  AGENT_ACME_EMAIL="${ACME_EMAIL:-}"
  [[ "$TLS_TYPE" != "none" ]] && AGENT_HAS_WEBSECURE="true" || AGENT_HAS_WEBSECURE="false"

  sep; echo ""
  echo -e "  ${BOLD}-- Dynamic Config --${RESET}"
  info "Single file is simpler. Directory (one .yml per service) is easier at scale."
  ask_choice "Dynamic config layout" AGENT_CONFIG_LAYOUT \
    "Single file (dynamic.yml)" \
    "Directory - one .yml file per service"

  ask "Docker network name" "traefik-net" AGENT_NETWORK
  ask "Traefik internal API port" "8080" AGENT_TRAEFIK_API_PORT

  AGENT_TRAEFIK_URL="http://traefik:${AGENT_TRAEFIK_API_PORT}"
  AGENT_CONFIG_PATH="/etc/traefik/config"

  sep; echo ""
  echo -e "  ${BOLD}-- Agent access --${RESET}"
  info "The agent port is always bound. A Traefik label adds a hostname + TLS route on top."
  local _use_label
  ask_yn "Expose agent via Traefik label (recommended for HTTPS)?" "y" _use_label
  if [[ "$_use_label" == "true" ]]; then
    ask "Agent hostname (e.g. agent.example.com)" "" AGENT_HOST
  else
    AGENT_HOST=""
  fi

  AGENT_LOG_PATH="/var/log/traefik/access.log"
  [[ "$AGENT_TLS_TYPE" != "none" ]] && AGENT_ACME_PATH="/etc/traefik/acme.json" || AGENT_ACME_PATH=""

  if [[ "$AGENT_EXTERNAL" == "true" ]]; then
    sep; echo ""
    echo -e "  ${YELLOW}${BOLD}Firewall / Port Requirements${RESET}"
    echo -e "  ${DIM}The following ports must be open on this server's firewall:${RESET}\n"
    if [[ "$TLS_TYPE" != "none" ]]; then
      echo -e "    ${CYAN}80/tcp${RESET}   HTTP (redirects to HTTPS + ACME challenge)"
      echo -e "    ${CYAN}443/tcp${RESET}  HTTPS"
    else
      echo -e "    ${CYAN}80/tcp${RESET}   HTTP"
    fi
    echo ""
    echo -e "  ${DIM}  sudo ufw allow 80/tcp${RESET}"
    [[ "$TLS_TYPE" != "none" ]] && echo -e "  ${DIM}  sudo ufw allow 443/tcp${RESET}"
    echo -e "  ${DIM}  sudo ufw reload${RESET}"
    echo ""
    echo -ne "  ${BOLD}Press Enter when ports are open to continue...${RESET}"
    read -r </dev/tty
  fi
}

_agent_sec_paths() {
  sep; echo ""
  echo -e "  ${BOLD}-- Optional paths --${RESET}"
  info "Expose extra Traefik data to the agent for richer visibility."
  local _has
  ask_yn "Mount ACME / certs (acme.json)?" "n" _has
  [[ "$_has" == "true" ]] && ask "ACME path" "/etc/traefik/acme.json" AGENT_ACME_PATH || AGENT_ACME_PATH=""
  ask_yn "Mount access logs?" "n" _has
  [[ "$_has" == "true" ]] && ask "Access log path" "/var/log/traefik/access.log" AGENT_LOG_PATH || AGENT_LOG_PATH=""
  ask_yn "Mount plugins directory?" "n" _has
  [[ "$_has" == "true" ]] && ask "Plugins dir" "/etc/traefik/plugins" AGENT_PLUGINS_DIR || AGENT_PLUGINS_DIR=""
}

_agent_sec_restart() {
  sep; echo ""
  echo -e "  ${BOLD}-- Traefik restart --${RESET}"
  info "Allows the agent to restart Traefik after static config changes."
  local _choice
  ask_choice "Restart method" _choice \
    "None" \
    "Socket proxy (recommended - minimal socket exposure)" \
    "Poison pill (signal file, no Docker socket)" \
    "Direct Docker socket"
  case "$_choice" in
    "Socket proxy"*)
      AGENT_RESTART_METHOD="proxy"
      ask "Docker host" "tcp://socket-proxy:2375" AGENT_DOCKER_HOST
      ask "Traefik container name" "traefik" AGENT_CONTAINER
      ;;
    "Poison pill"*)
      AGENT_RESTART_METHOD="poison-pill"
      ask "Signal file path" "/signals/restart.sig" AGENT_SIGNAL_FILE
      ;;
    "Direct Docker"*)
      AGENT_RESTART_METHOD="socket"
      ask "Traefik container name" "traefik" AGENT_CONTAINER
      ;;
    *)
      AGENT_RESTART_METHOD=""
      AGENT_DOCKER_HOST=""
      AGENT_SIGNAL_FILE=""
      ;;
  esac
}

_agent_sec_crowdsec() {
  sep; echo ""
  echo -e "  ${BOLD}-- CrowdSec (optional) --${RESET}"
  local _cs_choice
  if [[ "$AGENT_INSTALL_METHOD" == "Binary"* ]]; then
    ask_choice "CrowdSec integration" _cs_choice "None" "Connect to existing instance"
  else
    ask_choice "CrowdSec integration" _cs_choice \
      "None" \
      "Install alongside agent" \
      "Connect to existing instance"
  fi
  case "$_cs_choice" in
    "Install alongside"*)
      AGENT_CS_MODE="install"
      AGENT_CS_INSTALL_KEY=$(openssl rand -hex 32 2>/dev/null || od -A n -t x -N 32 /dev/urandom | tr -d ' \n')
      if [[ -z "$AGENT_LOG_PATH" ]]; then
        warn "CrowdSec reads Traefik access logs - enabling access log mount."
        ask "Access log path" "/var/log/traefik/access.log" AGENT_LOG_PATH
      fi
      AGENT_CS_URL="http://crowdsec:8080"
      AGENT_CS_KEY="$AGENT_CS_INSTALL_KEY"
      ok "CrowdSec will be installed alongside the agent"
      ;;
    "Connect"*)
      AGENT_CS_MODE="connect"
      AGENT_CS_INSTALL_KEY=""
      ask "LAPI URL" "http://crowdsec:8080" AGENT_CS_URL
      echo -ne "  ${BOLD}API key:${RESET} "
      read -rs AGENT_CS_KEY </dev/tty; echo ""
      ;;
    *)
      AGENT_CS_MODE="none"
      AGENT_CS_URL=""
      AGENT_CS_KEY=""
      AGENT_CS_INSTALL_KEY=""
      ;;
  esac
}

_agent_sec_git() {
  sep; echo ""
  echo -e "  ${BOLD}-- Git backup (optional) --${RESET}"
  local _add_git
  ask_choice "Enable git backup?" _add_git "No" "Yes"
  if [[ "$_add_git" == "Yes" ]]; then
    AGENT_GIT_ENABLED="true"
    ask "Repository URL" "" AGENT_GIT_REPO
    ask "Branch" "main" AGENT_GIT_BRANCH
    ask "Username" "" AGENT_GIT_USER
    echo -ne "  ${BOLD}Access token:${RESET} "
    read -rs AGENT_GIT_TOKEN </dev/tty; echo ""
    local _auto
    ask_choice "Auto-push on config change?" _auto "Yes" "No"
    [[ "$_auto" == "No" ]] && AGENT_GIT_AUTO="false" || AGENT_GIT_AUTO="true"
  else
    AGENT_GIT_ENABLED="false"
    AGENT_GIT_REPO=""
    AGENT_GIT_TOKEN=""
  fi
}

_agent_sec_location() {
  sep; echo ""
  echo -e "  ${BOLD}-- Install location --${RESET}"
  ask "Install directory" "/opt/traefik-manager-agent" AGENT_INSTALL_DIR
  ask "Agent port" "8090" AGENT_PORT
}

_agent_build_section_list() {
  AGENT_SEC_LABELS=()
  AGENT_SEC_FNS=()
  AGENT_SEC_LABELS+=("Install method"); AGENT_SEC_FNS+=("_agent_sec_method")
  AGENT_SEC_LABELS+=("API key");        AGENT_SEC_FNS+=("_agent_sec_apikey")
  if [[ "$AGENT_INSTALL_METHOD" == *"+ Traefik"* ]]; then
    AGENT_SEC_LABELS+=("Traefik install");    AGENT_SEC_FNS+=("_agent_sec_traefik_install")
  else
    AGENT_SEC_LABELS+=("Traefik connection"); AGENT_SEC_FNS+=("_agent_sec_traefik")
  fi
  AGENT_SEC_LABELS+=("Optional paths");  AGENT_SEC_FNS+=("_agent_sec_paths")
  AGENT_SEC_LABELS+=("Restart method");  AGENT_SEC_FNS+=("_agent_sec_restart")
  AGENT_SEC_LABELS+=("CrowdSec");        AGENT_SEC_FNS+=("_agent_sec_crowdsec")
  AGENT_SEC_LABELS+=("Git backup");      AGENT_SEC_FNS+=("_agent_sec_git")
  if [[ "$AGENT_INSTALL_METHOD" != "Binary"* ]]; then
    AGENT_SEC_LABELS+=("Install location"); AGENT_SEC_FNS+=("_agent_sec_location")
  fi
}

_agent_show_review() {
  local _mask="(not set)"
  [[ -n "$AGENT_API_KEY" ]] && _mask="${AGENT_API_KEY:0:4}••••••••"
  echo ""
  echo -e "  ${BOLD}Review configuration${RESET}"
  echo -e "  ${DIM}────────────────────────────────────────────────────────${RESET}"
  local _i=1
  for _lbl in "${AGENT_SEC_LABELS[@]}"; do
    local _val=""
    case "$_lbl" in
      "Install method")
        _val="${AGENT_INSTALL_METHOD//Docker - /}"
        _val="${_val//Binary - /}"
        ;;
      "API key") _val="$_mask" ;;
      "Traefik connection")
        _val="$AGENT_TRAEFIK_URL"
        [[ "$AGENT_INSECURE_TLS" == "true" ]] && _val+="  insecure-tls"
        [[ -n "$AGENT_STATIC_PATH" ]] && _val+="  static:$(basename "$AGENT_STATIC_PATH")"
        ;;
      "Traefik install")
        _val="${AGENT_TLS_TYPE:-none}"
        [[ "$AGENT_EXTERNAL" == "true" ]] && _val+="  external" || _val+="  internal"
        [[ -n "$AGENT_DASHBOARD_HOST" ]] && _val+="  dash:${AGENT_DASHBOARD_HOST}"
        [[ -n "$AGENT_HOST" ]] && _val+="  tma:${AGENT_HOST}"
        _val+="  net:${AGENT_NETWORK}"
        [[ -n "$AGENT_CONFIG_LAYOUT" ]] && _val+="  $(echo "$AGENT_CONFIG_LAYOUT" | grep -o 'Single\|Directory')"
        ;;
      "Optional paths")
        local _p=""
        [[ -n "$AGENT_ACME_PATH" ]]   && _p+="acme "
        [[ -n "$AGENT_LOG_PATH" ]]    && _p+="logs "
        [[ -n "$AGENT_PLUGINS_DIR" ]] && _p+="plugins"
        _val="${_p:-(none)}"
        ;;
      "Restart method") _val="${AGENT_RESTART_METHOD:-(none)}" ;;
      "CrowdSec")
        case "$AGENT_CS_MODE" in
          install) _val="install alongside" ;;
          connect) _val="connect  ${AGENT_CS_URL}" ;;
          *)       _val="disabled" ;;
        esac
        ;;
      "Git backup")
        [[ "$AGENT_GIT_ENABLED" == "true" ]] && _val="${AGENT_GIT_REPO:-(no repo set)}" || _val="disabled"
        ;;
      "Install location") _val="${AGENT_INSTALL_DIR}  :${AGENT_PORT}" ;;
    esac
    printf "  \033[1;36m%2d\033[0m  \033[2m%-20s\033[0m  %s\n" "$_i" "$_lbl" "$_val"
    (( _i++ ))
  done
  echo -e "  ${DIM}────────────────────────────────────────────────────────${RESET}"
}

gather_agent() {
  step "Traefik Manager Agent Setup"

  _agent_sec_method
  _agent_build_section_list
  local _prev_method="$AGENT_INSTALL_METHOD"

  for _fn in "${AGENT_SEC_FNS[@]:1}"; do "$_fn"; done

  while true; do
    _agent_show_review
    echo ""
    echo -ne "  ${BOLD}Edit a section (1-${#AGENT_SEC_FNS[@]}) or Enter to install:${RESET} "
    local _choice
    read -r _choice </dev/tty
    [[ -z "$_choice" ]] && break
    if [[ "$_choice" =~ ^[0-9]+$ ]] && (( _choice >= 1 && _choice <= ${#AGENT_SEC_FNS[@]} )); then
      "${AGENT_SEC_FNS[$((_choice-1))]}"
      if (( _choice == 1 )) && [[ "$AGENT_INSTALL_METHOD" != "$_prev_method" ]]; then
        _agent_build_section_list
        _prev_method="$AGENT_INSTALL_METHOD"
      fi
    fi
  done
}

build_agent_env() {
  local lines=""
  lines+="      - TMA_API_KEY=${AGENT_API_KEY}\n"
  lines+="      - TRAEFIK_API_URL=${AGENT_TRAEFIK_URL}\n"
  lines+="      - TMA_RATE_LIMIT=300\n"
  lines+="      - CONFIG_PATH=${AGENT_CONFIG_PATH}\n"
  [[ "$AGENT_INSECURE_TLS" == "true" ]]     && lines+="      - TRAEFIK_INSECURE_SKIP_VERIFY=true\n"
  [[ -n "$AGENT_STATIC_PATH" ]]             && lines+="      - STATIC_CONFIG_PATH=${AGENT_STATIC_PATH}\n"
  [[ -n "$AGENT_RESTART_METHOD" ]]          && lines+="      - RESTART_METHOD=${AGENT_RESTART_METHOD}\n"
  [[ -n "$AGENT_RESTART_METHOD" && -n "$AGENT_CONTAINER" ]] && lines+="      - TRAEFIK_CONTAINER=${AGENT_CONTAINER}\n"
  [[ "$AGENT_RESTART_METHOD" == "proxy" && -n "$AGENT_DOCKER_HOST" ]]       && lines+="      - DOCKER_HOST=${AGENT_DOCKER_HOST}\n"
  [[ "$AGENT_RESTART_METHOD" == "poison-pill" && -n "$AGENT_SIGNAL_FILE" ]] && lines+="      - SIGNAL_FILE_PATH=${AGENT_SIGNAL_FILE}\n"
  [[ -n "$AGENT_ACME_PATH" ]]               && lines+="      - ACME_JSON_PATH=${AGENT_ACME_PATH}\n"
  [[ -n "$AGENT_LOG_PATH" ]]                && lines+="      - ACCESS_LOG_PATH=${AGENT_LOG_PATH}\n"
  [[ -n "$AGENT_PLUGINS_DIR" ]]             && lines+="      - PLUGINS_DIR=${AGENT_PLUGINS_DIR}\n"
  if [[ "$AGENT_CS_MODE" == "install" ]]; then
    lines+="      - CROWDSEC_LAPI_URL=http://crowdsec:8080\n"
    lines+="      - CROWDSEC_API_KEY=${AGENT_CS_INSTALL_KEY}\n"
  elif [[ "$AGENT_CS_MODE" == "connect" ]]; then
    [[ -n "$AGENT_CS_URL" ]] && lines+="      - CROWDSEC_LAPI_URL=${AGENT_CS_URL}\n"
    [[ -n "$AGENT_CS_KEY" ]] && lines+="      - CROWDSEC_API_KEY=${AGENT_CS_KEY}\n"
  fi
  if [[ "$AGENT_GIT_ENABLED" == "true" ]]; then
    lines+="      - GIT_BACKUP_ENABLED=true\n"
    [[ -n "$AGENT_GIT_REPO" ]]  && lines+="      - GIT_BACKUP_REPO=${AGENT_GIT_REPO}\n"
    lines+="      - GIT_BACKUP_BRANCH=${AGENT_GIT_BRANCH}\n"
    [[ -n "$AGENT_GIT_USER" ]]  && lines+="      - GIT_BACKUP_USERNAME=${AGENT_GIT_USER}\n"
    [[ -n "$AGENT_GIT_TOKEN" ]] && lines+="      - GIT_BACKUP_TOKEN=${AGENT_GIT_TOKEN}\n"
    lines+="      - GIT_BACKUP_AUTO_PUSH=${AGENT_GIT_AUTO}\n"
  fi
  printf "%b" "$lines"
}

build_agent_vols() {
  local lines=""
  lines+="      - ${AGENT_CONFIG_PATH}:${AGENT_CONFIG_PATH}\n"
  lines+="      - ./backups:/app/backups\n"
  [[ -n "$AGENT_STATIC_PATH" ]]  && lines+="      - ${AGENT_STATIC_PATH}:${AGENT_STATIC_PATH}\n"
  [[ -n "$AGENT_ACME_PATH" ]]    && lines+="      - ${AGENT_ACME_PATH}:${AGENT_ACME_PATH}:ro\n"
  [[ -n "$AGENT_LOG_PATH" ]]     && lines+="      - ${AGENT_LOG_PATH}:${AGENT_LOG_PATH}:ro\n"
  [[ -n "$AGENT_PLUGINS_DIR" ]]  && lines+="      - ${AGENT_PLUGINS_DIR}:${AGENT_PLUGINS_DIR}:ro\n"
  [[ "$AGENT_RESTART_METHOD" == "socket" ]]      && lines+="      - /var/run/docker.sock:/var/run/docker.sock:ro\n"
  [[ "$AGENT_RESTART_METHOD" == "poison-pill" ]] && lines+="      - traefik-signals:/signals\n"
  printf "%b" "$lines"
}

scaffold_agent() {
  step "Creating install directory"
  mkdir -p "${AGENT_INSTALL_DIR}"
  mkdir -p "${AGENT_INSTALL_DIR}/backups"
  if [[ "$AGENT_CS_MODE" == "install" ]]; then
    mkdir -p "${AGENT_INSTALL_DIR}/crowdsec"
    cat > "${AGENT_INSTALL_DIR}/crowdsec/acquis.yaml" <<'ACQUIS'
filenames:
  - /var/log/traefik/access.log
labels:
  type: traefik
ACQUIS
    ok "crowdsec/acquis.yaml created"
  fi
  ok "Directory ready at ${AGENT_INSTALL_DIR}"
}

scaffold_agent_with_traefik() {
  step "Creating directory structure at ${AGENT_INSTALL_DIR}"
  mkdir -p "${AGENT_INSTALL_DIR}/traefik/config"
  mkdir -p "${AGENT_INSTALL_DIR}/traefik/logs"
  mkdir -p "${AGENT_INSTALL_DIR}/backups"
  touch "${AGENT_INSTALL_DIR}/traefik/logs/access.log"
  if [[ "$AGENT_TLS_TYPE" != "none" ]]; then
    touch "${AGENT_INSTALL_DIR}/traefik/acme.json"
    chmod 600 "${AGENT_INSTALL_DIR}/traefik/acme.json"
    ok "traefik/acme.json created (chmod 600)"
  fi
  if [[ "$AGENT_CONFIG_LAYOUT" == "Single file"* ]]; then
    cat > "${AGENT_INSTALL_DIR}/traefik/config/dynamic.yml" <<'EOF'
http:
  routers: {}
  services: {}
  middlewares: {}
EOF
    ok "traefik/config/dynamic.yml created"
  else
    ok "traefik/config/ directory ready"
  fi
  if [[ "$AGENT_CS_MODE" == "install" ]]; then
    mkdir -p "${AGENT_INSTALL_DIR}/crowdsec"
    cat > "${AGENT_INSTALL_DIR}/crowdsec/acquis.yaml" <<'ACQUIS'
filenames:
  - /var/log/traefik/access.log
labels:
  type: traefik
ACQUIS
    ok "crowdsec/acquis.yaml created"
  fi
  ok "Directory structure created"
}

build_agent_traefik_static() {
  local resolver_block=""
  if [[ "$AGENT_TLS_TYPE" == "http" ]]; then
    resolver_block="
certificatesResolvers:
  ${AGENT_CERT_RESOLVER}:
    acme:
      email: ${AGENT_ACME_EMAIL}
      storage: /acme.json
      httpChallenge:
        entryPoint: web"
  elif [[ "$AGENT_TLS_TYPE" == "dns" ]]; then
    resolver_block="
certificatesResolvers:
  ${AGENT_CERT_RESOLVER}:
    acme:
      email: ${AGENT_ACME_EMAIL}
      storage: /acme.json
      dnsChallenge:
        provider: ${DNS_PROVIDER}
        resolvers:
          - \"1.1.1.1:53\"
          - \"8.8.8.8:53\""
  fi

  local file_provider=""
  if [[ "$AGENT_CONFIG_LAYOUT" == "Single file"* ]]; then
    file_provider="  file:
    filename: /etc/traefik/config/dynamic.yml
    watch: true"
  else
    file_provider="  file:
    directory: /etc/traefik/config
    watch: true"
  fi

  local entrypoints_block
  if [[ "$AGENT_HAS_WEBSECURE" == "true" ]]; then
    entrypoints_block="  web:
    address: \":80\"
    http:
      redirections:
        entryPoint:
          to: websecure
          scheme: https
  websecure:
    address: \":443\""
  else
    entrypoints_block="  web:
    address: \":80\""
  fi

  cat > "${AGENT_INSTALL_DIR}/traefik/traefik.yml" <<EOF
api:
  dashboard: ${AGENT_TRAEFIK_DASHBOARD}
  insecure: true

entryPoints:
${entrypoints_block}

providers:
  docker:
    exposedByDefault: false
    network: ${AGENT_NETWORK}
${file_provider}
${resolver_block}

log:
  level: INFO

accessLog:
  filePath: /logs/access.log
  bufferingSize: 100
EOF
  ok "traefik/traefik.yml written"
}

build_agent_compose() {
  local env_block vol_block
  env_block="$(build_agent_env)"
  vol_block="$(build_agent_vols)"

  local volumes_section=""
  if [[ "$AGENT_RESTART_METHOD" == "poison-pill" ]] || [[ "$AGENT_CS_MODE" == "install" ]]; then
    volumes_section="\nvolumes:"
    [[ "$AGENT_RESTART_METHOD" == "poison-pill" ]] && volumes_section+="\n  traefik-signals:"
    [[ "$AGENT_CS_MODE" == "install" ]]            && volumes_section+="\n  crowdsec_data:"
  fi

  local cs_service=""
  if [[ "$AGENT_CS_MODE" == "install" ]]; then
    cs_service="\n  crowdsec:\n    image: crowdsecurity/crowdsec:latest\n    container_name: crowdsec\n    restart: unless-stopped\n    environment:\n      - BOUNCER_KEY_tma=${AGENT_CS_INSTALL_KEY}\n      - COLLECTIONS=crowdsecurity/traefik\n    volumes:\n      - crowdsec_data:/var/lib/crowdsec/data\n      - ./crowdsec/acquis.yaml:/etc/crowdsec/acquis.yaml:ro"
    [[ -n "$AGENT_LOG_PATH" ]] && cs_service+="\n      - ${AGENT_LOG_PATH}:/var/log/traefik/access.log:ro"
  fi

  cat > "${AGENT_INSTALL_DIR}/docker-compose.yml" <<COMPOSE
services:

  traefik-manager-agent:
    image: ghcr.io/chr0nzz/traefik-manager-agent:latest
    container_name: traefik-manager-agent
    restart: unless-stopped
    ports:
      - "${AGENT_PORT}:8090"
    environment:
${env_block}
    volumes:
${vol_block}
$(printf "%b" "$cs_service")
$(printf "%b" "$volumes_section")
COMPOSE
  ok "docker-compose.yml written"
}

build_agent_compose_with_traefik() {
  local env_block
  env_block="$(build_agent_env)"

  local traefik_config_vol=""
  local agent_config_vol=""
  if [[ "$AGENT_CONFIG_LAYOUT" == "Single file"* ]]; then
    traefik_config_vol="      - ./traefik/config/dynamic.yml:/etc/traefik/config/dynamic.yml:ro"
    agent_config_vol="      - ./traefik/config/dynamic.yml:/etc/traefik/config/dynamic.yml\n"
  else
    traefik_config_vol="      - ./traefik/config:/etc/traefik/config:ro"
    agent_config_vol="      - ./traefik/config:/etc/traefik/config\n"
  fi

  local agent_vols=""
  agent_vols+="$agent_config_vol"
  agent_vols+="      - ./backups:/app/backups\n"
  agent_vols+="      - ./traefik/logs/access.log:/var/log/traefik/access.log:ro\n"
  [[ "$AGENT_TLS_TYPE" != "none" ]] && agent_vols+="      - ./traefik/acme.json:/etc/traefik/acme.json:ro\n"
  [[ -n "$AGENT_PLUGINS_DIR" ]] && agent_vols+="      - ${AGENT_PLUGINS_DIR}:${AGENT_PLUGINS_DIR}:ro\n"
  [[ "$AGENT_RESTART_METHOD" == "socket" ]]      && agent_vols+="      - /var/run/docker.sock:/var/run/docker.sock:ro\n"
  [[ "$AGENT_RESTART_METHOD" == "poison-pill" ]] && agent_vols+="      - traefik-signals:/signals\n"

  local agent_label_block=""
  if [[ -n "$AGENT_HOST" ]]; then
    local _ep="${TRAEFIK_ENTRYPOINT:-web}"
    agent_label_block="    labels:
      - \"traefik.enable=true\"
      - \"traefik.http.routers.tma.rule=Host(\`${AGENT_HOST}\`)\"
      - \"traefik.http.routers.tma.entrypoints=${_ep}\"
      - \"traefik.http.services.tma.loadbalancer.server.port=8090\""
    [[ "$AGENT_TLS_TYPE" != "none" ]] && \
      agent_label_block+="
      - \"traefik.http.routers.tma.tls.certresolver=${AGENT_CERT_RESOLVER}\""
    agent_label_block+="
"
  fi

  local traefik_ports='"80:80"'
  [[ "$AGENT_HAS_WEBSECURE" == "true" ]] && traefik_ports+="
      - \"443:443\""

  local traefik_env_block=""
  if [[ -n "${DNS_ENV_BLOCK:-}" ]]; then
    traefik_env_block="    environment:
${DNS_ENV_BLOCK}
"
  fi

  local traefik_acme_vol=""
  [[ "$AGENT_TLS_TYPE" != "none" ]] && traefik_acme_vol="
      - ./traefik/acme.json:/acme.json"

  local dashboard_block=""
  if [[ "$AGENT_TRAEFIK_DASHBOARD" == "true" && -n "$AGENT_DASHBOARD_HOST" ]]; then
    local _ep="${TRAEFIK_ENTRYPOINT:-web}"
    dashboard_block="    labels:
      - \"traefik.enable=true\"
      - \"traefik.http.routers.dashboard.rule=Host(\`${AGENT_DASHBOARD_HOST}\`)\"
      - \"traefik.http.routers.dashboard.entrypoints=${_ep}\"
      - \"traefik.http.routers.dashboard.service=api@internal\""
    [[ "$AGENT_TLS_TYPE" != "none" ]] && \
      dashboard_block+="
      - \"traefik.http.routers.dashboard.tls.certresolver=${AGENT_CERT_RESOLVER}\""
    dashboard_block+="
"
  fi

  local cs_service=""
  if [[ "$AGENT_CS_MODE" == "install" ]]; then
    cs_service="
  crowdsec:
    image: crowdsecurity/crowdsec:latest
    container_name: crowdsec
    restart: unless-stopped
    networks:
      - ${AGENT_NETWORK}
    environment:
      - BOUNCER_KEY_tma=${AGENT_CS_INSTALL_KEY}
      - COLLECTIONS=crowdsecurity/traefik
    volumes:
      - crowdsec_data:/var/lib/crowdsec/data
      - ./crowdsec/acquis.yaml:/etc/crowdsec/acquis.yaml:ro
      - ./traefik/logs/access.log:/var/log/traefik/access.log:ro
"
  fi

  local volumes_section=""
  [[ "$AGENT_RESTART_METHOD" == "poison-pill" ]] && volumes_section+="  traefik-signals:
"
  [[ "$AGENT_CS_MODE" == "install" ]] && volumes_section+="  crowdsec_data:
"
  [[ -n "$volumes_section" ]] && volumes_section="
volumes:
${volumes_section}"

  cat > "${AGENT_INSTALL_DIR}/docker-compose.yml" <<COMPOSE
networks:
  ${AGENT_NETWORK}:
    external: false
    name: ${AGENT_NETWORK}

services:

  traefik:
    image: traefik:latest
    container_name: traefik
    restart: unless-stopped
    networks:
      - ${AGENT_NETWORK}
    ports:
      - ${traefik_ports}
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
      - ./traefik/traefik.yml:/traefik.yml:ro
      - ./traefik/logs:/logs${traefik_acme_vol}
${traefik_config_vol}
${traefik_env_block}${dashboard_block}
  traefik-manager-agent:
    image: ghcr.io/chr0nzz/traefik-manager-agent:latest
    container_name: traefik-manager-agent
    restart: unless-stopped
    networks:
      - ${AGENT_NETWORK}
    ports:
      - "${AGENT_PORT}:8090"
    environment:
${env_block}
    volumes:
$(printf "%b" "$agent_vols")
${agent_label_block}
    depends_on:
      - traefik
${cs_service}
${volumes_section}
COMPOSE
  ok "docker-compose.yml written"
}

install_agent_docker() {
  if [[ "$AGENT_INSTALL_METHOD" == *"+ Traefik"* ]]; then
    scaffold_agent_with_traefik
    build_agent_traefik_static
    build_agent_compose_with_traefik
  else
    scaffold_agent
    build_agent_compose
  fi
  step "Starting services…"
  cd "$AGENT_INSTALL_DIR" && $COMPOSE_CMD pull && $COMPOSE_CMD up -d
}

install_agent_binary() {
  local arch
  case "$(uname -m)" in
    x86_64)  arch="amd64" ;;
    aarch64) arch="arm64" ;;
    armv7l)  arch="armv7" ;;
    *) die "Unsupported architecture: $(uname -m)" ;;
  esac
  local os
  os=$(uname -s | tr '[:upper:]' '[:lower:]')
  local bin_url="https://github.com/chr0nzz/traefik-manager/releases/latest/download/tma-${os}-${arch}"
  step "Downloading TMA binary…"
  curl -fsSL "$bin_url" -o /usr/local/bin/tma && chmod +x /usr/local/bin/tma

  local env_lines=""
  env_lines+="Environment=TMA_API_KEY=${AGENT_API_KEY}\n"
  env_lines+="Environment=TRAEFIK_API_URL=${AGENT_TRAEFIK_URL}\n"
  env_lines+="Environment=CONFIG_PATH=${AGENT_CONFIG_PATH}\n"
  [[ "$AGENT_INSECURE_TLS" == "true" ]]     && env_lines+="Environment=TRAEFIK_INSECURE_SKIP_VERIFY=true\n"
  [[ -n "$AGENT_STATIC_PATH" ]]             && env_lines+="Environment=STATIC_CONFIG_PATH=${AGENT_STATIC_PATH}\n"
  [[ -n "$AGENT_RESTART_METHOD" ]]          && env_lines+="Environment=RESTART_METHOD=${AGENT_RESTART_METHOD}\n"
  [[ -n "$AGENT_RESTART_METHOD" && -n "$AGENT_CONTAINER" ]] && env_lines+="Environment=TRAEFIK_CONTAINER=${AGENT_CONTAINER}\n"
  [[ "$AGENT_RESTART_METHOD" == "proxy" && -n "$AGENT_DOCKER_HOST" ]]       && env_lines+="Environment=DOCKER_HOST=${AGENT_DOCKER_HOST}\n"
  [[ "$AGENT_RESTART_METHOD" == "poison-pill" && -n "$AGENT_SIGNAL_FILE" ]] && env_lines+="Environment=SIGNAL_FILE_PATH=${AGENT_SIGNAL_FILE}\n"
  [[ -n "$AGENT_ACME_PATH" ]]               && env_lines+="Environment=ACME_JSON_PATH=${AGENT_ACME_PATH}\n"
  [[ -n "$AGENT_LOG_PATH" ]]                && env_lines+="Environment=ACCESS_LOG_PATH=${AGENT_LOG_PATH}\n"
  [[ -n "$AGENT_PLUGINS_DIR" ]]             && env_lines+="Environment=PLUGINS_DIR=${AGENT_PLUGINS_DIR}\n"
  if [[ "$AGENT_CS_MODE" == "connect" ]]; then
    [[ -n "$AGENT_CS_URL" ]] && env_lines+="Environment=CROWDSEC_LAPI_URL=${AGENT_CS_URL}\n"
    [[ -n "$AGENT_CS_KEY" ]] && env_lines+="Environment=CROWDSEC_API_KEY=${AGENT_CS_KEY}\n"
  fi
  if [[ "$AGENT_GIT_ENABLED" == "true" ]]; then
    env_lines+="Environment=GIT_BACKUP_ENABLED=true\n"
    [[ -n "$AGENT_GIT_REPO" ]]  && env_lines+="Environment=GIT_BACKUP_REPO=${AGENT_GIT_REPO}\n"
    env_lines+="Environment=GIT_BACKUP_BRANCH=${AGENT_GIT_BRANCH}\n"
    [[ -n "$AGENT_GIT_USER" ]]  && env_lines+="Environment=GIT_BACKUP_USERNAME=${AGENT_GIT_USER}\n"
    [[ -n "$AGENT_GIT_TOKEN" ]] && env_lines+="Environment=GIT_BACKUP_TOKEN=${AGENT_GIT_TOKEN}\n"
    env_lines+="Environment=GIT_BACKUP_AUTO_PUSH=${AGENT_GIT_AUTO}\n"
  fi
  env_lines+="Environment=TMA_PORT=${AGENT_PORT}\n"

  step "Installing systemd service…"
  cat > /etc/systemd/system/tma.service <<UNIT
[Unit]
Description=Traefik Manager Agent
After=network.target

[Service]
Type=simple
$(printf "%b" "$env_lines")
ExecStart=/usr/local/bin/tma
Restart=on-failure
RestartSec=5

[Install]
WantedBy=multi-user.target
UNIT
  systemctl daemon-reload
  systemctl enable --now tma
}

print_summary_agent() {
  local ip
  ip=$(hostname -I | awk '{print $1}')
  echo ""
  echo -e "${GREEN}${BOLD}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${RESET}"
  echo -e "${GREEN}${BOLD}  Agent setup complete!${RESET}"
  echo -e "${GREEN}${BOLD}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${RESET}"
  echo ""
  echo -e "  Agent URL     ${CYAN}http://${ip}:${AGENT_PORT}${RESET}"
  echo -e "  Health check  ${DIM}curl http://${ip}:${AGENT_PORT}/health${RESET}"
  if [[ "$AGENT_INSTALL_METHOD" == *"+ Traefik"* ]]; then
    echo ""
    local _ports="80"
    [[ "$AGENT_HAS_WEBSECURE" == "true" ]] && _ports="80 + 443"
    echo -e "  Traefik       ${DIM}running on ports ${_ports}${RESET}"
    if [[ -n "$AGENT_HOST" ]]; then
      local _agent_scheme="http"
      [[ "$AGENT_HAS_WEBSECURE" == "true" ]] && _agent_scheme="https"
      echo -e "  Agent (label) ${CYAN}${_agent_scheme}://${AGENT_HOST}${RESET}"
    fi
    if [[ -n "$AGENT_DASHBOARD_HOST" ]]; then
      local _dash_scheme="http"
      [[ "$AGENT_HAS_WEBSECURE" == "true" ]] && _dash_scheme="https"
      echo -e "  Dashboard     ${CYAN}${_dash_scheme}://${AGENT_DASHBOARD_HOST}${RESET}"
    fi
    if [[ "$AGENT_CONFIG_LAYOUT" == "Single file"* ]]; then
      echo -e "  Dynamic config  ${DIM}${AGENT_INSTALL_DIR}/traefik/config/dynamic.yml${RESET}"
    else
      echo -e "  Dynamic config  ${DIM}${AGENT_INSTALL_DIR}/traefik/config/*.yml${RESET}"
    fi
    [[ "$AGENT_TLS_TYPE" == "none" ]] && \
      warn "TLS is disabled. Consider enabling it before exposing this publicly."
    if [[ "$AGENT_EXTERNAL" == "true" ]]; then
      [[ -n "$AGENT_HOST" ]] && \
        warn "DNS A record for ${AGENT_HOST} must point to this server's IP."
      [[ -n "$AGENT_DASHBOARD_HOST" ]] && \
        warn "DNS A record for ${AGENT_DASHBOARD_HOST} must point to this server's IP."
    fi
  fi
  echo ""
  echo -e "  ${BOLD}Next steps:${RESET}"
  echo -e "  ${DIM}1. In TM Settings -> Agents, click Add Agent${RESET}"
  echo -e "  ${DIM}2. Enter the Agent URL above and the API key you configured${RESET}"
  echo -e "  ${DIM}3. Use the server switcher in the TM nav bar to switch to this agent${RESET}"
  echo ""
  if [[ "$AGENT_INSTALL_METHOD" == "Binary"* ]]; then
    echo -e "  ${CYAN}${BOLD}Updating${RESET}"
    echo -e "  ${DIM}  curl -fsSL https://get-traefik.xyzlab.dev/agent | bash${RESET}"
    echo ""
  elif [[ -n "$AGENT_INSTALL_DIR" ]]; then
    echo -e "  ${CYAN}${BOLD}Updating${RESET}"
    echo -e "  ${DIM}  cd ${AGENT_INSTALL_DIR} && ${COMPOSE_CMD} pull && ${COMPOSE_CMD} up -d${RESET}"
    echo ""
  fi
}

# ─── Main ─────────────────────────────────────────────────────────────────────

main() {
  print_banner

  if [[ "${TMA_INSTALL:-}" == "1" ]]; then
    INSTALL_MODE="Traefik Manager Agent"
    DEPLOY_METHOD="Agent"
  else
    gather_mode
  fi

  if [[ "$INSTALL_MODE" == "Traefik Manager Agent" ]]; then
    gather_agent
    if [[ "$AGENT_INSTALL_METHOD" == "Binary"* ]]; then
      check_native_deps
      install_agent_binary
    else
      check_docker
      install_agent_docker
    fi
    print_summary_agent

  elif [[ "$INSTALL_MODE" == "Traefik + Traefik Manager"* ]]; then
    check_docker
    gather_full_stack
    scaffold_full
    build_traefik_static
    build_dynamic_config
    build_compose_full
    start_docker
    fetch_password_docker
    print_summary_full

  elif [[ "$DEPLOY_METHOD" == "Docker" ]]; then
    check_docker
    gather_tm_docker
    scaffold_tm_docker
    build_compose_tm
    start_docker
    fetch_password_docker
    print_summary_tm_docker

  else
    check_native_deps
    gather_tm_native
    install_tm_native
    fetch_password_native
    print_summary_native
  fi
}

main "$@"