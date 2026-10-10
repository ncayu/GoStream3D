#!/usr/bin/env bash
set -euo pipefail
umask 077

usage() {
  cat <<'HELP'
Usage: sudo bash install-linux.sh [--public-ip IP] [--image-archive FILE] [--mirror-ecr] [--no-start]
Ubuntu 22.04 / openEuler 22.03, x86_64. Installs to /opt/gostream3d.
Existing configuration and project data are preserved. Existing Docker is never replaced.
If Docker is missing, a private Docker service is installed from Docker's official binaries.
The first online install builds the CPU worker image; an offline image archive can replace this.
HELP
}
fail() { printf 'ERROR: %s\n' "$*" >&2; exit 1; }

public_ip=''
image_archive=''
use_ecr=0
start_service=1
while (($#)); do
  case "$1" in
    --public-ip) (($# >= 2)) || fail '--public-ip requires an IP'; public_ip="$2"; shift 2 ;;
    --image-archive) (($# >= 2)) || fail '--image-archive requires a file'; image_archive="$2"; shift 2 ;;
    --no-start) start_service=0; shift ;;
    --mirror-ecr) use_ecr=1; shift ;;
    --help|-h) usage; exit 0 ;;
    *) fail "Unknown argument: $1" ;;
  esac
done

[[ $(uname -s) == Linux ]] || fail 'This installer requires Linux.'
[[ $(uname -m) == x86_64 ]] || fail 'This release requires x86_64/amd64; ARM needs its own release.'
((EUID == 0)) || fail 'Run this installer with sudo.'
[[ -d /run/systemd/system ]] || fail 'A systemd host is required; do not run this installer inside a container.'
source_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
root=/opt/gostream3d
[[ -x "$source_dir/gostream3d" || -f "$source_dir/gostream3d" ]] || fail 'Unpack the linux-deploy release and run its install-linux.sh.'
chmod 0755 "$source_dir/gostream3d"
"$source_dir/gostream3d" --version
[[ -r /etc/os-release ]] || fail 'Cannot detect the distribution.'
# os-release is maintained by the OS, not supplied by the release.
. /etc/os-release
case "${ID:-}:${VERSION_ID:-}" in
  ubuntu:22.04|openEuler:22.03*|openeuler:22.03*) ;;
  *) fail "Supported targets: Ubuntu 22.04 / openEuler 22.03; detected ${ID:-unknown} ${VERSION_ID:-unknown}" ;;
esac

temp_dir="$(mktemp -d)"
trap 'rm -rf -- "$temp_dir"' EXIT
"$source_dir/gostream3d" --config "$temp_dir/config.env" --init-config --setup-runtime docker --public-ip "$public_ip" >/dev/null
install -d -m 0750 "$root"
docker_socket=unix:///var/run/docker.sock
docker_dependency=docker.service
docker_cli=''

if [[ -x "$root/docker/docker" && -f /etc/systemd/system/gostream3d-docker.service ]]; then
  docker_cli="$root/docker/docker"
  docker_socket=unix:///run/gostream3d-docker.sock
  docker_dependency=gostream3d-docker.service
elif command -v docker >/dev/null 2>&1; then
  docker_cli="$(command -v docker)"
else
  [[ ! -S /var/run/docker.sock ]] || fail 'A Docker socket exists but the CLI is missing. Install the matching CLI first.'
  if [[ "$ID" == ubuntu ]]; then
    apt-get update
    apt-get install -y ca-certificates curl tar iptables iproute2
  else
    dnf install -y ca-certificates curl tar iptables iproute
  fi
  printf 'Installing private Docker Engine 29.8.2 (official static distribution).\n'
  curl --fail --location --retry 3 --proto '=https' --tlsv1.2 \
    https://download.docker.com/linux/static/stable/x86_64/docker-29.8.2.tgz \
    -o "$temp_dir/docker.tgz"
  tar -xzf "$temp_dir/docker.tgz" -C "$temp_dir"
  install -d -m 0755 "$root/docker"
  install -m 0755 "$temp_dir"/docker/* "$root/docker/"
  docker_cli="$root/docker/docker"
  docker_socket=unix:///run/gostream3d-docker.sock
  docker_dependency=gostream3d-docker.service
  cat > /etc/systemd/system/gostream3d-docker.service <<'UNIT'
[Unit]
Description=Private Docker Engine for GoStream3D
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
Environment=PATH=/opt/gostream3d/docker:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
ExecStart=/opt/gostream3d/docker/dockerd --host=unix:///run/gostream3d-docker.sock --data-root=/var/lib/gostream3d-docker --exec-root=/run/gostream3d-docker --pidfile=/run/gostream3d-docker.pid
Restart=on-failure
RestartSec=5
Delegate=yes
KillMode=process
LimitNOFILE=infinity
LimitNPROC=infinity
TasksMax=infinity

[Install]
WantedBy=multi-user.target
UNIT
fi
systemctl daemon-reload
systemctl enable --now "$docker_dependency"
docker_command=("$docker_cli" --host "$docker_socket")
docker_ready=0
for ((attempt=0; attempt<30; attempt++)); do
  if "${docker_command[@]}" info >/dev/null 2>&1; then docker_ready=1; break; fi
  sleep 1
done
((docker_ready)) || fail "Docker did not become ready. Run: journalctl -u $docker_dependency -n 100"
api_version="$("${docker_command[@]}" version --format '{{.Server.APIVersion}}')"
api_minor="${api_version#1.}"
[[ "$api_version" == 1.* && "$api_minor" =~ ^[0-9]+$ ]] || fail "Unexpected Docker API: $api_version"
((api_minor >= 44)) || fail "Docker API $api_version is too old (requires 1.44 / Docker 25+). Upgrade your existing Docker; this installer will not replace it."

image=gostream3d/neko/chromium:3.1.5
if [[ -z "$image_archive" && -f "$source_dir/worker.tar" ]]; then image_archive="$source_dir/worker.tar"; fi
if [[ -n "$image_archive" ]]; then
  [[ -f "$image_archive" ]] || fail "Image archive not found: $image_archive"
  "${docker_command[@]}" load -i "$image_archive"
fi
if ! "${docker_command[@]}" image inspect "$image" >/dev/null 2>&1; then
  [[ -f "$source_dir/deploy/Dockerfile.worker" && -d "$source_dir/runtime-sources/server/vendor" ]] \
    || fail 'Worker build sources are missing. Supply the full linux-deploy ZIP or --image-archive.'
  image_arguments=()
  if ((use_ecr)); then
    image_arguments=(--build-arg GO_IMAGE=public.ecr.aws/docker/library/golang:1.25-trixie
      --build-arg RUNTIME_IMAGE=public.ecr.aws/docker/library/debian:trixie-slim)
  fi
  "${docker_command[@]}" build "${image_arguments[@]}" -f "$source_dir/deploy/Dockerfile.worker" -t "$image" "$source_dir"
fi

"$source_dir/gostream3d" --config "$root/config.env" --init-config --setup-runtime docker --public-ip "$public_ip"
chmod 0600 "$root/config.env"
saved_runtime="$(awk -F= '/^[[:space:]]*GOSTREAM3D_RUNTIME[[:space:]]*=/ {value=$2; gsub(/[[:space:]"\047]/,"",value); print value; exit}' "$root/config.env")"
[[ "$saved_runtime" == docker ]] || fail 'Existing config.env uses another runtime. To migrate, set GOSTREAM3D_RUNTIME=docker first; credentials and projects are preserved.'
DOCKER_HOST="$docker_socket" "$source_dir/gostream3d" --config "$root/config.env" --doctor
# Do not stop a working deployment until Docker, the image and configuration are ready.
if systemctl is-active --quiet gostream3d.service; then systemctl stop gostream3d.service; fi
if [[ "$(readlink -f "$source_dir/gostream3d")" != "$root/gostream3d" ]]; then
  install -m 0755 "$source_dir/gostream3d" "$root/gostream3d.new"
  mv -f "$root/gostream3d.new" "$root/gostream3d"
fi
install -d -m 0700 "$root/data"

cat > /etc/systemd/system/gostream3d.service <<UNIT
[Unit]
Description=GoStream3D rendering gateway
After=network-online.target $docker_dependency
Requires=$docker_dependency
Wants=network-online.target

[Service]
Type=simple
# Docker socket grants host control; the gateway needs it to manage its workers.
User=root
WorkingDirectory=$root
Environment=DOCKER_HOST=$docker_socket
ExecStart=$root/gostream3d --config $root/config.env
Restart=on-failure
RestartSec=5
KillMode=control-group
TimeoutStopSec=40
UMask=0077

[Install]
WantedBy=multi-user.target
UNIT
systemd-analyze verify /etc/systemd/system/gostream3d.service
systemctl daemon-reload
systemctl enable gostream3d.service
if ((start_service)); then
  systemctl restart gostream3d.service
  sleep 2
  systemctl is-active --quiet gostream3d.service || fail 'Gateway failed to start. Run: journalctl -u gostream3d -n 100'
fi
printf '\nInstallation ready: %s\nConfiguration: %s/config.env\n' "$root" "$root"
printf 'Use the administrator credentials in config.env. Existing files were preserved.\n'
printf 'Allow TCP 8888 and UDP 52000-52023 in your firewall/security group (or your configured ports).\n'
printf 'Start/stop/status: sudo systemctl {start|stop|status} gostream3d\n'
printf 'Logs: sudo journalctl -u gostream3d -f\n'
