#!/usr/bin/env bash
set -euo pipefail
umask 077
fail() { printf 'ERROR: %s\n' "$*" >&2; exit 1; }
public_ip=''
http_port=''
while (($#)); do
  case "$1" in
    --public-ip) (($# >= 2)) || fail '--public-ip requires an IP'; public_ip="$2"; shift 2 ;;
    --http-port) (($# >= 2)) || fail '--http-port requires a port'; http_port="$2"; shift 2 ;;
    --help|-h) printf 'Usage: sudo bash start.sh [--public-ip IP] [--http-port PORT]\nRequires local Docker 25+ and Compose 2.20+. Missing IP uses the host route address for LAN viewing. Existing values are preserved unless explicitly changed; .env changes are backed up.\n'; exit 0 ;;
    *) fail "Unknown argument: $1" ;;
  esac
done
[[ $(uname -s) == Linux && $(uname -m) == x86_64 ]] || fail 'This release requires x86_64 Linux.'
cd -- "$(dirname -- "${BASH_SOURCE[0]}")"

# Read only deployment values; never execute .env as shell code.
env_value() {
  [[ -f .env ]] || return 0
  awk -v key="$1" '
    { sub(/\r$/, ""); line=$0; sub(/^[[:space:]]*(export[[:space:]]+)?/, "", line) }
    index(line, "=") {
      name=substr(line,1,index(line,"=")-1); sub(/[[:space:]]+$/, "", name)
      if(name==key) {
        value=substr(line,index(line,"=")+1); sub(/^[[:space:]]+/,"",value); sub(/[[:space:]]+$/,"",value)
        sub(/[[:space:]]+#.*$/, "", value)
        if ((substr(value,1,1)=="\"" && substr(value,length(value),1)=="\"") ||
            (substr(value,1,1)==sprintf("%c",39) && substr(value,length(value),1)==sprintf("%c",39))) value=substr(value,2,length(value)-2)
        result=value
      }
    }
    END { print result }
  ' .env
}
env_backed_up=false
[[ -f .env ]] || env_backed_up=true
save_env_value() {
  local key="$1" value="$2" temporary backup
  if [[ -f .env && "$env_backed_up" == false ]]; then
    backup="$(mktemp ".env.before-$(TZ=UTC-8 date +%Y%m%d-%H%M%S).XXXXXX")"
    cp -- .env "$backup"
    chmod 0600 "$backup"
    printf 'Backed up deployment settings: %s\n' "$backup"
    env_backed_up=true
  fi
  temporary="$(mktemp .env.gostream3d.XXXXXX)"
  if [[ -f .env ]]; then
    awk -v key="$key" -v value="$value" '
      /^[[:space:]]*(export[[:space:]]+)?[A-Za-z_][A-Za-z0-9_]*[[:space:]]*=/ {
        line=$0; sub(/^[[:space:]]*(export[[:space:]]+)?/,"",line)
        name=substr(line,1,index(line,"=")-1); sub(/[[:space:]]+$/, "", name)
        if(name==key) { if(!written) print key "=" value; written=1; next }
      }
      { print }
      END { if(!written) print key "=" value }
    ' .env > "$temporary"
  else printf '%s=%s\n' "$key" "$value" > "$temporary"; fi
  chmod 0600 "$temporary"
  mv -- "$temporary" .env
}
docker_cli="$(command -v docker || true)"
if [[ -z "$docker_cli" && -x /opt/gostream3d/docker/docker ]]; then docker_cli=/opt/gostream3d/docker/docker; fi
[[ -n "$docker_cli" ]] || fail 'Install Docker 25+ first.'
socket="${GOSTREAM3D_DOCKER_SOCKET:-$(env_value GOSTREAM3D_DOCKER_SOCKET)}"
if [[ -z "$socket" ]]; then
  case "${DOCKER_HOST:-}" in
    unix://*) socket="${DOCKER_HOST#unix://}" ;;
    '') if [[ -S /var/run/docker.sock ]]; then socket=/var/run/docker.sock; else socket=/run/gostream3d-docker.sock; fi ;;
    *) fail 'This deployment requires a local Unix Docker socket.' ;;
  esac
fi
[[ -S "$socket" ]] || fail "Docker socket unavailable: $socket"
export DOCKER_HOST="unix://$socket" GOSTREAM3D_DOCKER_SOCKET="$socket"
compose_is_supported() {
  local version="${1#v}"
  [[ "$version" =~ ^([0-9]+)\.([0-9]+)\. ]] \
    && ((BASH_REMATCH[1] > 2 || (BASH_REMATCH[1] == 2 && BASH_REMATCH[2] >= 20)))
}
compose_cmd=()
compose_cli="$(command -v docker-compose || true)"
if [[ -n "$compose_cli" ]]; then
  if compose_version="$("$compose_cli" version --short 2>/dev/null)" && compose_is_supported "$compose_version"; then
    compose_cmd=("$compose_cli")
  fi
fi
if ((${#compose_cmd[@]} == 0)); then
  if compose_version="$("$docker_cli" compose version --short 2>/dev/null)" && compose_is_supported "$compose_version"; then
    compose_cmd=("$docker_cli" compose)
  fi
fi
((${#compose_cmd[@]})) || fail 'Install Compose 2.20+: docker-compose standalone or docker compose plugin. Legacy Python Compose 1.x is unsupported.'
printf 'Using Compose: %s (%s)\n' "${compose_cmd[*]}" "$compose_version"
api_version="$("$docker_cli" version --format '{{.Server.APIVersion}}')"
api_minor="${api_version#1.}"
[[ "$api_version" == 1.* && "$api_minor" =~ ^[0-9]+$ ]] && ((api_minor >= 44)) \
  || fail "Docker API $api_version is too old; Docker 25+ is required."
if [[ -f images.tar ]]; then
  [[ -f images.tar.sha256 ]] || fail 'Image checksum is missing.'
  sha256sum -c images.tar.sha256
  "$docker_cli" load -i images.tar
fi
"$docker_cli" image inspect gostream3d/gateway:latest >/dev/null || fail 'Load images.tar first.'
"$docker_cli" image inspect gostream3d/neko/chromium:3.1.5 >/dev/null || fail 'The CPU worker image is missing.'
stored_ip="$(env_value GOSTREAM3D_ICE_PUBLIC_IP)"
selected_ip="${public_ip:-${GOSTREAM3D_ICE_PUBLIC_IP:-$stored_ip}}"
if [[ -z "$selected_ip" ]]; then
  selected_ip="$(ip -4 route get 1.1.1.1 2>/dev/null | awk '{for(i=1;i<NF;i++) if($i=="src") {print $(i+1); exit}}' || true)"
  [[ -n "$selected_ip" && "$selected_ip" != 127.* && "$selected_ip" != 169.254.* ]] \
    || fail 'Cannot detect a viewer-reachable IP. Run: bash start.sh --public-ip SERVER_IP'
  printf 'Detected host IP: %s (LAN viewing; use --public-ip for public/NAT access).\n' "$selected_ip"
fi
stored_port="$(env_value GOSTREAM3D_HTTP_PORT)"
selected_port="${http_port:-${GOSTREAM3D_HTTP_PORT:-${stored_port:-8888}}}"
[[ "$selected_port" =~ ^[1-9][0-9]{0,4}$ ]] && ((10#$selected_port <= 65535)) \
  || fail 'HTTP port must be 1-65535; use --http-port PORT.'
"$docker_cli" run --rm gostream3d/gateway:latest --config /tmp/config.env \
  --init-config --setup-runtime docker --public-ip "$selected_ip" >/dev/null
if [[ -z "$stored_ip" || ( -n "$public_ip" && "$public_ip" != "$stored_ip" ) ]]; then
  save_env_value GOSTREAM3D_ICE_PUBLIC_IP "$selected_ip"
fi
if [[ -z "$stored_port" || ( -n "$http_port" && "$http_port" != "$stored_port" ) ]]; then
  save_env_value GOSTREAM3D_HTTP_PORT "$selected_port"
fi
if [[ -z "$(env_value GOSTREAM3D_DOCKER_SOCKET)" ]]; then
  save_env_value GOSTREAM3D_DOCKER_SOCKET "$socket"
fi
export GOSTREAM3D_ICE_PUBLIC_IP="$selected_ip" GOSTREAM3D_HTTP_PORT="$selected_port"
# Prepare one shared cgroup before Compose validates its bind mount. The
# persistent slice also exists before Docker restores containers after reboot.
if [[ "$("$docker_cli" info --format '{{.CgroupVersion}}')" == 2 && "$("$docker_cli" info --format '{{.CgroupDriver}}')" == systemd ]]; then
  if [[ $EUID == 0 && -d /run/systemd/system ]]; then
    if [[ ! -f /etc/systemd/system/gostream3d.slice ]]; then
      cat > /etc/systemd/system/gostream3d.slice <<'UNIT'
[Unit]
Description=GoStream3D shared CPU budget
Before=docker.service
[Slice]
CPUAccounting=yes
[Install]
WantedBy=slices.target
UNIT
      systemctl daemon-reload
    fi
    systemctl enable --now gostream3d.slice
    systemctl set-property --runtime gostream3d.slice CPUAccounting=yes
    [[ -w /sys/fs/cgroup/gostream3d.slice/cpu.max ]] || fail 'The GoStream3D slice has no writable CPU controller.'
    save_env_value GOSTREAM3D_CGROUP_PARENT gostream3d.slice
    save_env_value GOSTREAM3D_CPU_CGROUP_DIR /sys/fs/cgroup/gostream3d.slice
    save_env_value GOSTREAM3D_CPU_CGROUP_PATH /host/cpu/cpu.max
  else
    printf 'CPU quota setup requires sudo and systemd; existing manual delegation is preserved.\n'
  fi
else
  printf 'Automatic CPU quota setup requires cgroup v2 with the systemd Docker driver; existing manual delegation is preserved.\n'
fi
# Monitoring needs the utility driver in the gateway as well as the render worker.
smi="$(command -v nvidia-smi || true)"
[[ -n "$smi" ]] || { [[ ! -x /usr/lib/wsl/lib/nvidia-smi ]] || smi=/usr/lib/wsl/lib/nvidia-smi; }
runtimes="$("$docker_cli" info --format '{{json .Runtimes}}')"
if [[ "$runtimes" == *'"nvidia"'* && -n "$smi" ]] && "$smi" -L >/dev/null 2>&1; then
  save_env_value GOSTREAM3D_GATEWAY_RUNTIME nvidia
  if [[ -d /usr/lib/wsl/lib ]]; then save_env_value GOSTREAM3D_GPU_LIBRARY_DIR /usr/lib/wsl; fi
fi
chmod 0600 .env
mkdir -p data
"${compose_cmd[@]}" -f docker-compose.yml config --quiet
if command -v ss >/dev/null && [[ -n "$(ss -H -ltn "sport = :$selected_port")" ]]; then
  existing_gateway="$("${compose_cmd[@]}" -f docker-compose.yml ps -q gateway)"
  owns_port=''
  if [[ -n "$existing_gateway" ]]; then
    port_template="{{range .Config.Env}}{{if eq . \"GOSTREAM3D_HTTP_PORT=$selected_port\"}}owned{{end}}{{end}}"
    owns_port="$("$docker_cli" inspect --format "$port_template" "$existing_gateway" 2>/dev/null || true)"
  fi
  [[ "$owns_port" == owned ]] \
    || fail "HTTP port $selected_port is occupied. Use: bash start.sh --http-port 10088 (or another free port)."
fi
"${compose_cmd[@]}" -f docker-compose.yml up -d --wait --wait-timeout 90 --pull never
admin_host="$selected_ip"
[[ "$admin_host" != *:* ]] || admin_host="[$admin_host]"
printf '\nGateway started. Admin: http://%s:%s/admin\n' "$admin_host" "$selected_port"
printf 'Credentials: sudo cat data/config.env (or your configured data directory).\n'
printf 'Logs: sudo %s logs -f gateway\n' "${compose_cmd[*]}"
printf 'Allow TCP %s and UDP 52000-52023, or your configured UDP ports.\n' "$selected_port"
