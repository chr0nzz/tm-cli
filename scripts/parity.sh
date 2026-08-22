#!/usr/bin/env bash
set -euo pipefail

REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
GOLDEN="$REPO/testdata/golden"
LEGACY_REV="${LEGACY_REV:-305ec13}"
PARITY_USER="alice"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

legacy="$(git -C "$REPO" show "$LEGACY_REV:setup.sh" | sed '$d')"

cat > "$tmp/answers.py" <<'PY'
import shlex
import sys
import yaml

with open(sys.argv[1]) as f:
    data = yaml.safe_load(f)


def emit(prefix, value):
    if isinstance(value, dict):
        for k, v in value.items():
            emit(prefix + "_" + str(k), v)
    elif isinstance(value, list):
        print("ans%s=%s" % (prefix, shlex.quote(" ".join(str(v) for v in value))))
    else:
        if isinstance(value, bool):
            value = "true" if value else "false"
        elif value is None:
            value = ""
        print("ans%s=%s" % (prefix, shlex.quote(str(value))))


emit("", data)
PY

mkdir -p "$tmp/bin"
cat > "$tmp/bin/openssl" <<'STUB'
#!/usr/bin/env bash
case "$*" in
  "rand -hex 32") printf '%s\n' "$PARITY_CROWDSEC_API_KEY" ;;
  "rand -hex 24") printf '%s\n' "$PARITY_CROWDSEC_MACHINE_PASSWORD" ;;
  *) exit 1 ;;
esac
STUB
cat > "$tmp/bin/sudo" <<'STUB'
#!/usr/bin/env bash
if [[ "${1:-}" == tee ]]; then
  mkdir -p "$(dirname "$PARITY_ROOT$2")"
  exec tee "$PARITY_ROOT$2"
fi
exit 0
STUB
cat > "$tmp/bin/curl" <<'STUB'
#!/usr/bin/env bash
while [[ $# -gt 0 ]]; do
  if [[ "$1" == -o ]]; then
    mkdir -p "$(dirname "$2")"
    : > "$2"
    exit 0
  fi
  shift
done
STUB
for stub in git python3 systemctl; do
  printf '#!/usr/bin/env bash\nexit 0\n' > "$tmp/bin/$stub"
done
chmod +x "$tmp"/bin/*

dns_vars_for() {
  case "$1" in
    cloudflare) echo "CF_DNS_API_TOKEN" ;;
    route53) echo "AWS_ACCESS_KEY_ID AWS_SECRET_ACCESS_KEY AWS_REGION" ;;
    digitalocean) echo "DO_AUTH_TOKEN" ;;
    namecheap) echo "NAMECHEAP_API_USER NAMECHEAP_API_KEY" ;;
    duckdns) echo "DUCKDNS_TOKEN" ;;
    desec) echo "DESEC_TOKEN" ;;
    other)
      local names
      names="$(compgen -v ans_tls_vars_ || true)"
      echo "${ans_tls_secret_vars:-} $(printf '%s\n' "$names" | sed 's/^ans_tls_vars_//' | sort | tr '\n' ' ')"
      ;;
    *) echo "" ;;
  esac
}

legacy_tls() {
  export TLS_TYPE="${ans_tls_method:-http}"
  export ACME_EMAIL="${ans_tls_email:-}"
  export DNS_PROVIDER="" DNS_ENV_BLOCK="" CERT_RESOLVER="letsencrypt" TRAEFIK_ENTRYPOINT="websecure"
  if [[ "$TLS_TYPE" == none ]]; then
    CERT_RESOLVER=""
    TRAEFIK_ENTRYPOINT="web"
    return 0
  fi
  if [[ "$TLS_TYPE" != dns ]]; then
    return 0
  fi
  DNS_PROVIDER="${ans_tls_provider:-}"
  local name value secret_var vars_var names
  local nl=$'\n'
  read -ra names <<< "$(dns_vars_for "$DNS_PROVIDER")"
  if [[ "$DNS_PROVIDER" == other ]]; then
    DNS_PROVIDER="${ans_tls_lego_provider:-}"
  fi
  for name in "${names[@]}"; do
    secret_var="ans_secrets_$name"
    vars_var="ans_tls_vars_$name"
    value="${!secret_var:-${!vars_var:-}}"
    if [[ -z "$value" && "$name" == AWS_REGION ]]; then
      value="us-east-1"
    fi
    DNS_ENV_BLOCK+="${DNS_ENV_BLOCK:+$nl}      - ${name}=${value}"
  done
}

legacy_layout() {
  if [[ "${ans_config_layout:-single}" == single ]]; then
    export CONFIG_LAYOUT="Single file (dynamic.yml)"
  else
    export CONFIG_LAYOUT="Directory - one .yml file per service"
  fi
}

legacy_mounts() {
  export MOUNT_ACCESS_LOGS="${ans_mounts_access_logs:-true}"
  export MOUNT_CERTS="${ans_mounts_certs:-true}"
  export MOUNT_STATIC_CONFIG="${ans_mounts_static_config:-false}"
  export ACCESS_LOG_PATH="${ans_mounts_access_log_path:-/var/log/traefik/access.log}"
  export ACME_JSON_HOST_PATH="${ans_mounts_acme_path:-/etc/traefik/acme.json}"
  export TRAEFIK_YML_HOST_PATH="${ans_mounts_static_config_path:-/etc/traefik/traefik.yml}"
}

legacy_restart_docker() {
  export RESTART_METHOD="" TRAEFIK_CONTAINER="${ans_restart_container:-traefik}"
  if [[ "$MOUNT_STATIC_CONFIG" == true && "${ans_restart_method:-none}" != none ]]; then
    RESTART_METHOD="${ans_restart_method}"
  fi
}

legacy_crowdsec_full() {
  export ADD_CROWDSEC="false" CROWDSEC_MODE="" CS_LAPI_URL="" CS_API_KEY="" CS_KEY="" CS_MACHINE_ID="" CS_MACHINE_PW=""
  case "${ans_crowdsec_mode:-none}" in
    install)
      ADD_CROWDSEC="true"
      CROWDSEC_MODE="Install as part of this stack"
      MOUNT_ACCESS_LOGS="true"
      ;;
    connect)
      ADD_CROWDSEC="true"
      CROWDSEC_MODE="Connect to existing instance"
      CS_LAPI_URL="${ans_crowdsec_lapi_url:-http://crowdsec:8080}"
      CS_API_KEY="${ans_secrets_CROWDSEC_API_KEY:-}"
      CS_MACHINE_ID="${ans_crowdsec_machine_id:-}"
      if [[ -n "$CS_MACHINE_ID" ]]; then
        CS_MACHINE_PW="${ans_secrets_CROWDSEC_MACHINE_PASSWORD:-}"
      fi
      ;;
  esac
}

render_full() {
  export INSTALL_DIR="$out"
  export DOMAIN="${ans_domain:-}"
  export TRAEFIK_DASHBOARD_HOST="${ans_hosts_dashboard:-traefik.$DOMAIN}"
  export TM_HOST="${ans_hosts_manager:-manager.$DOMAIN}"
  export ENABLE_DASHBOARD="${ans_dashboard:-true}"
  legacy_tls
  legacy_layout
  legacy_mounts
  legacy_restart_docker
  TRAEFIK_CONTAINER="traefik"
  legacy_crowdsec_full
  export DOCKER_NETWORK="${ans_network_name:-traefik-net}"
  export TRAEFIK_API_PORT="${ans_network_traefik_api_port:-8080}"
  scaffold_full
  build_traefik_static
  build_dynamic_config
  build_compose_full
}

render_tm_docker() {
  export INSTALL_DIR="$out"
  export NETWORK_EXTERNAL="${ans_network_external:-true}"
  export USE_TRAEFIK_NETWORK="$NETWORK_EXTERNAL"
  local default_net="traefik-manager-net"
  if [[ "$NETWORK_EXTERNAL" == true ]]; then
    default_net="traefik-net"
  fi
  export DOCKER_NETWORK="${ans_network_name:-$default_net}"
  export USE_TRAEFIK_LABELS="${ans_access_via_traefik:-true}"
  export TM_HOST="${ans_hosts_manager:-}" TM_PORT=""
  if [[ "$USE_TRAEFIK_LABELS" == true ]]; then
    legacy_tls
  else
    TM_PORT="${ans_access_port:-5000}"
    export TLS_TYPE="none" CERT_RESOLVER="" TRAEFIK_ENTRYPOINT="web" DNS_ENV_BLOCK="" ACME_EMAIL=""
  fi
  legacy_layout
  legacy_mounts
  legacy_restart_docker
  scaffold_tm_docker
  build_compose_tm
}

render_tm_native() {
  export NATIVE_INSTALL_DIR="$root${ans_native_install_dir:-/opt/traefik-manager}"
  export NATIVE_DATA_DIR="$root${ans_native_data_dir:-/var/lib/traefik-manager}"
  export TM_PORT="${ans_native_port:-5000}"
  export CREATE_SVC_USER="${ans_native_service_user:-true}"
  export USER="$PARITY_USER"
  legacy_layout
  export NATIVE_CONFIG_PATH="${ans_config_path:-/etc/traefik/dynamic.yml}"
  export NATIVE_CONFIG_DIR="${ans_config_dir:-/etc/traefik/conf.d}"
  legacy_mounts
  export RESTART_METHOD="" TRAEFIK_SYSTEMD="false"
  export TRAEFIK_SERVICE_NAME="${ans_restart_traefik_service:-traefik}"
  export TRAEFIK_CONTAINER="${ans_restart_container:-traefik}"
  export SIGNAL_FILE_PATH="$root${ans_restart_signal_file:-/var/lib/traefik-manager/signals/restart.sig}"
  if [[ "$MOUNT_STATIC_CONFIG" == true ]]; then
    TRAEFIK_SYSTEMD="${ans_restart_traefik_systemd:-false}"
    if [[ "$TRAEFIK_SYSTEMD" == true ]]; then
      RESTART_METHOD="poison-pill"
    elif [[ "${ans_restart_method:-none}" != none ]]; then
      RESTART_METHOD="${ans_restart_method}"
    fi
  fi
  mkdir -p "$NATIVE_INSTALL_DIR/venv/bin" "$NATIVE_INSTALL_DIR/scripts"
  printf '#!/usr/bin/env bash\nexit 0\n' > "$NATIVE_INSTALL_DIR/venv/bin/pip"
  chmod +x "$NATIVE_INSTALL_DIR/venv/bin/pip"
  printf 'exit 0\n' > "$NATIVE_INSTALL_DIR/scripts/setup-assets.sh"
  install_tm_native
}

legacy_agent() {
  export AGENT_INSTALL_DIR="$out"
  export AGENT_API_KEY="${ans_secrets_TMA_API_KEY:-}"
  export AGENT_TRAEFIK_URL="${ans_agent_traefik_url:-http://traefik:8080}"
  export AGENT_CONFIG_PATH="${ans_agent_config_path:-/app/config}"
  export AGENT_INSECURE_TLS="false"
  if [[ "$AGENT_TRAEFIK_URL" == https://* ]]; then
    AGENT_INSECURE_TLS="${ans_agent_insecure_tls:-false}"
  fi
  export AGENT_TRAEFIK_USER="" AGENT_TRAEFIK_PASS=""
  if [[ "${ans_agent_basic_auth:-false}" == true ]]; then
    AGENT_TRAEFIK_USER="${ans_agent_basic_auth_user:-}"
    AGENT_TRAEFIK_PASS="${ans_secrets_TRAEFIK_API_PASSWORD:-}"
  fi
  export AGENT_STATIC_PATH="" AGENT_ACME_PATH="" AGENT_LOG_PATH="" AGENT_PLUGINS_DIR=""
  if [[ "${ans_mounts_static_config:-false}" == true ]]; then
    AGENT_STATIC_PATH="${ans_mounts_static_config_path:-/etc/traefik/traefik.yml}"
  fi
  if [[ "${ans_mounts_certs:-false}" == true ]]; then
    AGENT_ACME_PATH="${ans_mounts_acme_path:-/etc/traefik/acme.json}"
  fi
  if [[ "${ans_mounts_access_logs:-false}" == true ]]; then
    AGENT_LOG_PATH="${ans_mounts_access_log_path:-/var/log/traefik/access.log}"
  fi
  if [[ "${ans_mounts_plugins:-false}" == true ]]; then
    AGENT_PLUGINS_DIR="${ans_mounts_plugins_dir:-/etc/traefik/plugins}"
  fi
  export AGENT_RESTART_METHOD="" AGENT_CONTAINER="${ans_restart_container:-traefik}" AGENT_DOCKER_HOST="" AGENT_SIGNAL_FILE=""
  case "${ans_restart_method:-none}" in
    proxy)
      AGENT_RESTART_METHOD="proxy"
      AGENT_DOCKER_HOST="${ans_restart_docker_host:-tcp://socket-proxy:2375}"
      ;;
    poison-pill)
      AGENT_RESTART_METHOD="poison-pill"
      AGENT_SIGNAL_FILE="${ans_restart_signal_file:-/signals/restart.sig}"
      ;;
    socket)
      AGENT_RESTART_METHOD="socket"
      ;;
  esac
  export AGENT_CS_MODE="${ans_crowdsec_mode:-none}" AGENT_CS_URL="" AGENT_CS_KEY="" AGENT_CS_INSTALL_KEY=""
  case "$AGENT_CS_MODE" in
    install)
      AGENT_CS_INSTALL_KEY="${ans_secrets_CROWDSEC_API_KEY:-}"
      if [[ -z "$AGENT_LOG_PATH" ]]; then
        AGENT_LOG_PATH="${ans_mounts_access_log_path:-/var/log/traefik/access.log}"
      fi
      AGENT_CS_URL="http://crowdsec:8080"
      AGENT_CS_KEY="$AGENT_CS_INSTALL_KEY"
      ;;
    connect)
      AGENT_CS_URL="${ans_crowdsec_lapi_url:-http://crowdsec:8080}"
      AGENT_CS_KEY="${ans_secrets_CROWDSEC_API_KEY:-}"
      ;;
  esac
  export AGENT_GIT_ENABLED="${ans_agent_git_enabled:-false}" AGENT_GIT_REPO="" AGENT_GIT_USER="" AGENT_GIT_TOKEN=""
  export AGENT_GIT_BRANCH="${ans_agent_git_branch:-main}" AGENT_GIT_AUTO="${ans_agent_git_auto_push:-true}"
  if [[ "$AGENT_GIT_ENABLED" == true ]]; then
    AGENT_GIT_REPO="${ans_agent_git_repo:-}"
    AGENT_GIT_USER="${ans_agent_git_user:-}"
    AGENT_GIT_TOKEN="${ans_secrets_GIT_BACKUP_TOKEN:-}"
  fi
  export AGENT_PORT="${ans_agent_port:-8090}"
}

render_agent_docker() {
  legacy_agent
  scaffold_agent
  build_agent_compose
}

render_agent_docker_traefik() {
  legacy_agent
  export AGENT_TRAEFIK_DASHBOARD="${ans_dashboard:-true}" AGENT_DASHBOARD_HOST=""
  if [[ "$AGENT_TRAEFIK_DASHBOARD" == true ]]; then
    AGENT_DASHBOARD_HOST="${ans_hosts_dashboard:-}"
  fi
  legacy_tls
  export AGENT_TLS_TYPE="$TLS_TYPE" AGENT_CERT_RESOLVER="$CERT_RESOLVER" AGENT_ACME_EMAIL="$ACME_EMAIL" AGENT_HAS_WEBSECURE="true"
  if [[ "$TLS_TYPE" == none ]]; then
    AGENT_HAS_WEBSECURE="false"
  fi
  legacy_layout
  export AGENT_CONFIG_LAYOUT="$CONFIG_LAYOUT"
  export AGENT_NETWORK="${ans_network_name:-traefik-net}"
  export AGENT_TRAEFIK_API_PORT="${ans_network_traefik_api_port:-8080}"
  AGENT_TRAEFIK_URL="http://traefik:${AGENT_TRAEFIK_API_PORT}"
  AGENT_CONFIG_PATH="/etc/traefik/config"
  export AGENT_HOST=""
  if [[ "${ans_access_via_traefik:-true}" == true ]]; then
    AGENT_HOST="${ans_hosts_agent:-}"
  fi
  AGENT_LOG_PATH="/var/log/traefik/access.log"
  AGENT_ACME_PATH=""
  if [[ "$TLS_TYPE" != none ]]; then
    AGENT_ACME_PATH="/etc/traefik/acme.json"
  fi
  AGENT_TRAEFIK_USER=""
  AGENT_TRAEFIK_PASS=""
  AGENT_STATIC_PATH=""
  AGENT_INSECURE_TLS="false"
  scaffold_agent_with_traefik
  build_agent_traefik_static
  build_agent_compose_with_traefik
}

render_agent_binary() {
  legacy_agent
  eval "$(declare -f install_agent_binary | sed "s|/etc/systemd/system/tma.service|$root&|; s|/usr/local/bin/tma|$root&|g")"
  mkdir -p "$root/etc/systemd/system"
  install_agent_binary
}

golden_paths() {
  awk '{print $1}' "$dir/files.txt"
}

normalise() {
  mkdir -p "$(dirname "$2")"
  sed -e "$secret_sed" -e "s|$root||g" -e 's/[[:space:]]*$//' "$1" > "$2"
}

note_dev() {
  echo "$1" >> "$tmp/deviations"
}

empty_seed() {
  local body
  body="$(grep -vE '^[[:space:]]*(#|$)' "$1" || true)"
  [[ -z "$body" || "$body" == "$(printf 'http:\n  routers: {}\n  services: {}\n  middlewares: {}')" ]]
}

quote_unit_values() {
  sed -e 's/^Environment=\(.*\)$/Environment="\1"/' \
      -e 's|^ExecStart=\(.*\)/venv/bin/gunicorn \\$|ExecStart="\1/venv/bin/gunicorn" \\|' \
      -e "s|^ExecStart=/bin/sh -c 'rm -f \\(.*\\) \&\& systemctl restart \\(.*\\)'$|ExecStart=-/bin/rm -f \"\\1\"\nExecStart=/bin/systemctl restart \"\\2\"|" \
      "$1" > "$1.q"
  if cmp -s "$1" "$1.q"; then
    rm -f "$1.q"
    return 1
  fi
  mv "$1.q" "$1"
}

canonical_agt_compose() {
  local api="${ans_network_traefik_api_port:-8080}"
  sed -e "/^      - \"$api:8080\"$/d" \
      -e "s|TRAEFIK_API_URL=http://traefik:8080|TRAEFIK_API_URL=http://traefik:$api|" \
      "$1" > "$1.c" && mv "$1.c" "$1"
}

ancestors() {
  local p
  while IFS= read -r p; do
    while [[ -n "$p" ]]; do
      echo "$p"
      if [[ "$p" == */* ]]; then
        p="${p%/*}"
      else
        p=""
      fi
    done
  done
}

compare_docker_tree() {
  local status=0 rel
  while IFS= read -r rel; do
    if [[ "$rel" == .env ]]; then
      continue
    fi
    if [[ ! -f "$out/$rel" ]]; then
      echo "  legacy did not write $rel"
      status=1
      continue
    fi
    normalise "$out/$rel" "$norm/$rel"
    cp "$dir/files/$rel" "$norm/$rel.tm"
    if [[ "$rel" == */dynamic.yml || "$rel" == dynamic.yml ]]; then
      if empty_seed "$norm/$rel" && empty_seed "$norm/$rel.tm"; then
        note_dev "$name: $rel seed is a comment instead of the empty http skeleton traefik rejects"
        cp "$norm/$rel.tm" "$norm/$rel"
      fi
    fi
    if [[ "$rel" == docker-compose.yml && "${ans_mode:-}" == agent-docker-traefik ]]; then
      case "${ans_restart_method:-none}" in
        proxy|poison-pill)
          note_dev "$name: $rel not diffed, tm adds the socket proxy or poison pill wiring the legacy script left out"
          cp "$norm/$rel" "$norm/$rel.tm"
          ;;
        *)
          note_dev "$name: $rel traefik api port is published and the agent points at the container port"
          canonical_agt_compose "$norm/$rel.tm"
          ;;
      esac
    fi
    if ! diff -B -u "$norm/$rel.tm" "$norm/$rel"; then
      status=1
    fi
  done < <(golden_paths)
  while IFS= read -r rel; do
    if ! golden_paths | grep -qx -- "$rel"; then
      echo "  legacy wrote $rel but tm does not"
      status=1
    fi
  done < <(find "$out" -type f -printf '%P\n')
  local want have
  want="$({ cat "$dir/dirs.txt"; golden_paths | grep / | sed 's|/[^/]*$||'; } | ancestors | sort -u)"
  have="$(find "$out" -mindepth 1 -type d -printf '%P\n' | sort -u)"
  if [[ "$want" != "$have" ]]; then
    echo "  directory sets differ (tm vs legacy)"
    diff <(echo "$want") <(echo "$have") || true
    status=1
  fi
  return $status
}

check_docker_env() {
  local env="$dir/files/.env" compose="$dir/files/docker-compose.yml" status=0 key value var
  if [[ ! -f "$env" ]]; then
    if grep -q '[$][{]' "$compose"; then
      echo "  compose references secrets but there is no .env"
      return 1
    fi
    return 0
  fi
  while IFS='=' read -r key value; do
    value="${value#\'}"
    value="${value%\'}"
    var="ans_secrets_$key"
    if [[ "${!var:-}" != "$value" ]]; then
      echo "  .env carries $key with a value that is not the scenario secret"
      status=1
    fi
  done < "$env"
  while IFS= read -r key; do
    if ! grep -q "^$key=" "$env"; then
      echo "  compose references \${$key} but .env lacks it"
      status=1
    fi
  done < <(grep -o '[$][{][A-Z_]*}' "$compose" | tr -d '$}{' | sort -u)
  return $status
}

compare_systemd_tree() {
  local status=0 path unit_keys env_keys key value var
  while IFS= read -r path; do
    if [[ "$path" == /etc/traefik-manager-agent/env ]]; then
      continue
    fi
    if [[ ! -f "$root$path" ]]; then
      echo "  legacy did not write $path"
      status=1
      continue
    fi
    normalise "$root$path" "$norm$path"
    if quote_unit_values "$norm$path"; then
      note_dev "$name: $path values are quoted for systemd"
    fi
    if [[ "$path" == /etc/systemd/system/tma.service ]]; then
      unit_keys="$(grep -o '^Environment="[A-Z_]*=[$][{][A-Z_]*}"$' "$norm$path" | sed 's/^Environment="//; s/=.*//' | sort)"
      env_keys="$(cut -d= -f1 "$dir/files/etc/traefik-manager-agent/env" | sort)"
      if [[ "$unit_keys" != "$env_keys" ]]; then
        echo "  secret keys moved to the env file differ from the legacy inline secrets"
        diff <(echo "$unit_keys") <(echo "$env_keys") || true
        status=1
      fi
      while IFS='=' read -r key value; do
        value="${value#\'}"
        value="${value%\'}"
        var="ans_secrets_$key"
        if [[ "${!var:-}" != "$value" ]]; then
          echo "  env file carries $key with a value that is not the scenario secret"
          status=1
        fi
      done < "$dir/files/etc/traefik-manager-agent/env"
      grep -v '^Environment="[A-Z_]*=[$][{][A-Z_]*}"$' "$norm$path" | sed '/^Type=simple$/a EnvironmentFile=/etc/traefik-manager-agent/env' > "$norm$path.tm"
      mv "$norm$path.tm" "$norm$path"
    fi
    if ! diff -B -u "$dir/files$path" "$norm$path"; then
      status=1
    fi
  done < <(golden_paths)
  while IFS= read -r path; do
    if ! golden_paths | grep -qx -- "/etc/systemd/system/$path"; then
      echo "  legacy wrote /etc/systemd/system/$path but tm does not"
      status=1
    fi
  done < <(find "$root/etc/systemd/system" -type f -printf '%P\n')
  while IFS= read -r path; do
    if [[ "$path" == /etc/traefik-manager-agent ]]; then
      continue
    fi
    if [[ ! -d "$root$path" ]]; then
      echo "  legacy did not create $path"
      status=1
    fi
  done < "$dir/dirs.txt"
  return $status
}

run_scenario() {
  local name="$1"
  local dir="$GOLDEN/$name" root="$tmp/$name/root" out="$tmp/$name/out" norm="$tmp/$name/norm"
  local var key secret_sed=""
  mkdir -p "$root" "$out" "$norm"
  eval "$(python3 "$tmp/answers.py" "$dir/answers.yml")"
  for var in $(compgen -v ans_secrets_ || true); do
    key="${var#ans_secrets_}"
    secret_sed+="s|${!var}|\${$key}|g;"
  done
  export PARITY_ROOT="$root"
  export PARITY_CROWDSEC_API_KEY="${ans_secrets_CROWDSEC_API_KEY:-}"
  export PARITY_CROWDSEC_MACHINE_PASSWORD="${ans_secrets_CROWDSEC_MACHINE_PASSWORD:-}"
  PATH="$tmp/bin:$PATH"
  eval "$legacy" 2>/dev/null
  case "${ans_mode:-}" in
    full) render_full ;;
    tm-docker) render_tm_docker ;;
    tm-native) render_tm_native ;;
    agent-docker) render_agent_docker ;;
    agent-docker-traefik) render_agent_docker_traefik ;;
    agent-binary) render_agent_binary ;;
    *)
      echo "  unknown mode ${ans_mode:-}"
      return 1
      ;;
  esac > /dev/null
  case "$ans_mode" in
    tm-native|agent-binary) compare_systemd_tree ;;
    *)
      compare_docker_tree
      check_docker_env
      ;;
  esac
}

status=0
passed=0
failed=0
for scenario in "$GOLDEN"/*/; do
  name="$(basename "$scenario")"
  if ( run_scenario "$name" ); then
    echo "ok    $name"
    passed=$((passed + 1))
  else
    echo "FAIL  $name"
    failed=$((failed + 1))
    status=1
  fi
done
echo
if [[ -s "$tmp/deviations" ]]; then
  echo "deliberate deviations from the legacy script, not diffed:"
  sort -u "$tmp/deviations" | sed 's/^/  /'
  echo
fi
echo "parity: $passed passed, $failed failed (legacy setup.sh at $LEGACY_REV)"
exit $status
