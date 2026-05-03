#!/usr/bin/env bash
set -euo pipefail

SCRIPT_VERSION="1.0.0-beta1.1"

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
RESTART_METHOD=""
TRAEFIK_CONTAINER="traefik"
TRAEFIK_SYSTEMD="false"
TRAEFIK_SERVICE_NAME="traefik"
MOUNT_STATIC_CONFIG="false"
TRAEFIK_YML_HOST_PATH=""
SIGNAL_FILE_PATH="/signals/restart.sig"

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
  echo "                    ╔═════╗"
  echo "              ◉ ─── ╠     ╣ ─── ◉"
  echo "                    ╚═════╝"
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
  local options=("$@")
  echo -e "  ${BOLD}${prompt}${RESET}"
  for i in "${!options[@]}"; do
    echo -e "    ${DIM}$((i+1)))${RESET} ${options[$i]}"
  done
  echo -ne "  Choice [1]: "
  read -r input </dev/tty
  input="${input:-1}"
  local idx=$(( input - 1 ))
  if [[ $idx -lt 0 || $idx -ge ${#options[@]} ]]; then idx=0; fi
  printf -v "$var_name" '%s' "${options[$idx]}"
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
    "Traefik Manager only"

  if [[ "$INSTALL_MODE" == "Traefik Manager only" ]]; then
    sep
    echo ""
    ask_choice "Deployment method" DEPLOY_METHOD \
      "Docker" \
      "Linux service (systemd)"
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

gather_full_stack() {
  step "General"
  echo -e "  ${DIM}Press Enter to accept defaults shown in brackets.${RESET}\n"

  ask "Install directory" "$INSTALL_DIR" INSTALL_DIR

  sep
  echo ""
  echo -e "  ${BOLD}Deployment type${RESET}"
  info "Internal = LAN / VPN / Tailscale only.  External = reachable from the internet."
  ask_choice "Where will this be accessed from?" DEPLOYMENT_TYPE \
    "External (internet-facing)" \
    "Internal only (LAN / VPN / Tailscale)"

  if [[ "$DEPLOYMENT_TYPE" == "External"* ]]; then EXTERNAL=true
  else EXTERNAL=false; fi

  sep
  echo ""
  echo -e "  ${BOLD}-- Domain --${RESET}"
  ask "Your domain (e.g. example.com)" "" DOMAIN
  [[ -z "$DOMAIN" ]] && die "A domain is required."
  ask "Traefik dashboard subdomain" "traefik.$DOMAIN" TRAEFIK_DASHBOARD_HOST
  ask "Traefik Manager subdomain"   "manager.$DOMAIN" TM_HOST
  ask_yn "Enable Traefik API dashboard UI?" "y" ENABLE_DASHBOARD

  sep
  echo ""
  echo -e "  ${BOLD}-- TLS / Certificates --${RESET}"
  gather_tls_method

  sep
  echo ""
  echo -e "  ${BOLD}-- Dynamic Config --${RESET}"
  info "Single file is simpler. Directory (one .yml per service) is easier at scale."
  ask_choice "Dynamic config layout" CONFIG_LAYOUT \
    "Single file (dynamic.yml)" \
    "Directory - one .yml file per service"

  sep
  echo ""
  echo -e "  ${BOLD}-- Optional Mounts --${RESET}"
  info "Expose extra Traefik data to Traefik Manager for richer visibility."
  ask_yn "Mount access logs?"                        "y" MOUNT_ACCESS_LOGS
  ask_yn "Mount SSL certs (acme.json)?"              "y" MOUNT_CERTS
  ask_yn "Mount Traefik static config (traefik.yml)?" "n" MOUNT_STATIC_CONFIG

  if [[ "$MOUNT_STATIC_CONFIG" == "true" ]]; then
    gather_restart_method_docker "false"
  fi

  sep
  echo ""
  echo -e "  ${BOLD}-- Docker Network --${RESET}"
  ask "Docker network name"       "traefik-net" DOCKER_NETWORK
  ask "Traefik internal API port" "8080"        TRAEFIK_API_PORT

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
    ask "Email for Let's Encrypt" "" ACME_EMAIL
    [[ -z "$ACME_EMAIL" ]] && die "An email is required for Let's Encrypt."
  fi

  case "$CERT_METHOD" in
    "Let's Encrypt - HTTP challenge"*)
      TLS_TYPE="http"; CERT_RESOLVER="letsencrypt"; TRAEFIK_ENTRYPOINT="websecure"
      ;;
    *"Cloudflare"*)
      TLS_TYPE="dns"; CERT_RESOLVER="letsencrypt"; TRAEFIK_ENTRYPOINT="websecure"
      DNS_PROVIDER="cloudflare"
      ask "Cloudflare API Token (DNS-scoped token)" "" CF_DNS_API_TOKEN
      DNS_ENV_BLOCK="      - CF_DNS_API_TOKEN=${CF_DNS_API_TOKEN}"
      ;;
    *"Route 53"*)
      TLS_TYPE="dns"; CERT_RESOLVER="letsencrypt"; TRAEFIK_ENTRYPOINT="websecure"
      DNS_PROVIDER="route53"
      ask "AWS Access Key ID"     "" AWS_ACCESS_KEY_ID
      ask "AWS Secret Access Key" "" AWS_SECRET_ACCESS_KEY
      ask "AWS Region"            "us-east-1" AWS_REGION
      DNS_ENV_BLOCK="      - AWS_ACCESS_KEY_ID=${AWS_ACCESS_KEY_ID}
      - AWS_SECRET_ACCESS_KEY=${AWS_SECRET_ACCESS_KEY}
      - AWS_REGION=${AWS_REGION}"
      ;;
    *"DigitalOcean"*)
      TLS_TYPE="dns"; CERT_RESOLVER="letsencrypt"; TRAEFIK_ENTRYPOINT="websecure"
      DNS_PROVIDER="digitalocean"
      ask "DigitalOcean API Token" "" DO_AUTH_TOKEN
      DNS_ENV_BLOCK="      - DO_AUTH_TOKEN=${DO_AUTH_TOKEN}"
      ;;
    *"Namecheap"*)
      TLS_TYPE="dns"; CERT_RESOLVER="letsencrypt"; TRAEFIK_ENTRYPOINT="websecure"
      DNS_PROVIDER="namecheap"
      ask "Namecheap API User" "" NAMECHEAP_API_USER
      ask "Namecheap API Key"  "" NAMECHEAP_API_KEY
      DNS_ENV_BLOCK="      - NAMECHEAP_API_USER=${NAMECHEAP_API_USER}
      - NAMECHEAP_API_KEY=${NAMECHEAP_API_KEY}"
      ;;
    *"DuckDNS"*)
      TLS_TYPE="dns"; CERT_RESOLVER="letsencrypt"; TRAEFIK_ENTRYPOINT="websecure"
      DNS_PROVIDER="duckdns"
      ask "DuckDNS Token" "" DUCKDNS_TOKEN
      DNS_ENV_BLOCK="      - DUCKDNS_TOKEN=${DUCKDNS_TOKEN}"
      ;;
    *"deSEC"*)
      TLS_TYPE="dns"; CERT_RESOLVER="letsencrypt"; TRAEFIK_ENTRYPOINT="websecure"
      DNS_PROVIDER="desec"
      ask "deSEC Token" "" DESEC_TOKEN
      DNS_ENV_BLOCK="      - DESEC_TOKEN=${DESEC_TOKEN}"
      ;;
    "No TLS"*)
      TLS_TYPE="none"; CERT_RESOLVER=""; TRAEFIK_ENTRYPOINT="web"
      warn "Running without TLS. HTTP only."
      ;;
  esac
}

# ─── TM-only Docker config ────────────────────────────────────────────────────

gather_tm_docker() {
  step "Traefik Manager - Docker Setup"
  echo -e "  ${DIM}Press Enter to accept defaults shown in brackets.${RESET}\n"

  ask "Install directory" "${HOME}/traefik-manager" INSTALL_DIR

  sep
  echo ""
  echo -e "  ${BOLD}-- Network --${RESET}"
  ask_yn "Connect to an existing Traefik Docker network?" "y" USE_TRAEFIK_NETWORK
  if [[ "$USE_TRAEFIK_NETWORK" == "true" ]]; then
    ask "Traefik network name" "traefik-net" DOCKER_NETWORK
    NETWORK_EXTERNAL="true"
  else
    ask "Docker network name" "traefik-manager-net" DOCKER_NETWORK
    NETWORK_EXTERNAL="false"
  fi

  sep
  echo ""
  echo -e "  ${BOLD}-- Access --${RESET}"
  ask_yn "Expose via Traefik labels (requires Traefik on same network)?" "y" USE_TRAEFIK_LABELS
  if [[ "$USE_TRAEFIK_LABELS" == "true" ]]; then
    ask "Traefik Manager domain (e.g. manager.example.com)" "" TM_HOST
    [[ -z "$TM_HOST" ]] && die "A domain is required for Traefik labels."
    gather_tls_method
    TM_PORT=""
  else
    ask "Port to expose on host" "5000" TM_PORT
    TLS_TYPE="none"
    CERT_RESOLVER=""
    TRAEFIK_ENTRYPOINT="web"
  fi

  sep
  echo ""
  echo -e "  ${BOLD}-- Dynamic Config --${RESET}"
  info "Single file is simpler. Directory (one .yml per service) is easier at scale."
  ask_choice "Dynamic config layout" CONFIG_LAYOUT \
    "Single file (dynamic.yml)" \
    "Directory - one .yml file per service"

  sep
  echo ""
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
  fi
}

# ─── TM-only native config ────────────────────────────────────────────────────

gather_tm_native() {
  step "Traefik Manager - Linux Service Setup"
  echo -e "  ${DIM}Press Enter to accept defaults shown in brackets.${RESET}\n"

  ask "Install directory" "/opt/traefik-manager" NATIVE_INSTALL_DIR
  ask "Data directory"    "/var/lib/traefik-manager" NATIVE_DATA_DIR
  ask "Port"              "5000" TM_PORT

  sep
  echo ""
  echo -e "  ${BOLD}-- Service User --${RESET}"
  ask_yn "Create a dedicated system user (traefik-manager)?" "y" CREATE_SVC_USER

  sep
  echo ""
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

  sep
  echo ""
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
  fi
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
}

build_traefik_static() {
  local resolver_block=""
  if [[ "$TLS_TYPE" == "http" ]]; then
    resolver_block="
certificatesResolvers:
  letsencrypt:
    acme:
      email: ${ACME_EMAIL}
      storage: /acme.json
      httpChallenge:
        entryPoint: web"
  elif [[ "$TLS_TYPE" == "dns" ]]; then
    resolver_block="
certificatesResolvers:
  letsencrypt:
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
        certResolver: letsencrypt

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
  if [[ "$MOUNT_STATIC_CONFIG" != "true" || "$RESTART_METHOD" != "proxy" ]]; then
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
  if [[ "$MOUNT_STATIC_CONFIG" == "true" && "$RESTART_METHOD" == "poison-pill" ]]; then
    volumes_section="
volumes:
  tm-signals:"
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
    labels:
      - "traefik.enable=true"
      - "traefik.http.routers.traefik-manager.rule=Host(\`${TM_HOST}\`)"
      - "traefik.http.routers.traefik-manager.entrypoints=${TRAEFIK_ENTRYPOINT}"
      - "traefik.http.services.traefik-manager.loadbalancer.server.port=5000"
$(if [[ -n "$tls_label_tm" ]]; then echo "$tls_label_tm"; fi)
    depends_on:
      - traefik
$(if [[ -n "$socket_proxy_service" ]]; then echo "$socket_proxy_service"; fi)
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
  if [[ "$MOUNT_STATIC_CONFIG" != "true" || "$RESTART_METHOD" != "proxy" ]]; then
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
    git clone --branch v1 https://github.com/chr0nzz/traefik-manager.git "${NATIVE_INSTALL_DIR}"
  fi
  ok "Repository cloned to ${NATIVE_INSTALL_DIR}"

  python3 -m venv "${NATIVE_INSTALL_DIR}/venv"
  "${NATIVE_INSTALL_DIR}/venv/bin/pip" install -q -r "${NATIVE_INSTALL_DIR}/requirements.txt" gunicorn
  ok "Python dependencies installed"

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

# ─── Main ─────────────────────────────────────────────────────────────────────

main() {
  print_banner
  gather_mode

  if [[ "$INSTALL_MODE" == "Traefik + Traefik Manager"* ]]; then
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
