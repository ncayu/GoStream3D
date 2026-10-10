#!/usr/bin/env bash
set -euo pipefail
script_directory="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
test_root="$(mktemp -d /tmp/gostream3d-start-test.XXXXXX)"
trap 'rm -rf -- "$test_root"' EXIT
mkdir -p "$test_root/bin"
cat > "$test_root/bin/docker" <<'SH'
#!/usr/bin/env bash
case "$1" in
  version) printf '1.56\n' ;;
  info)
    case "$*" in
      *CgroupVersion*) printf '1\n' ;;
      *CgroupDriver*) printf 'cgroupfs\n' ;;
      *Runtimes*) printf '{}\n' ;;
      *) printf 'unexpected docker info query: %s\n' "$*" >&2; exit 1 ;;
    esac ;;
  image|run|load) exit 0 ;;
  inspect) [[ "${TEST_GATEWAY_OWNS_PORT:-}" != 1 ]] || printf 'owned'; exit 0 ;;
  *) printf 'unexpected docker command: %s\n' "$*" >&2; exit 1 ;;
esac
SH
cat > "$test_root/bin/docker-compose" <<'SH'
#!/usr/bin/env bash
if [[ "$1" == version ]]; then printf '2.32.4\n'; exit 0; fi
if [[ "$*" == *' config '* ]]; then
  grep -Eq '^GOSTREAM3D_ICE_PUBLIC_IP=[^[:space:]]+' .env || { printf 'missing ICE IP\n' >&2; exit 1; }
elif [[ "$*" == *' up '* ]]; then
  printf 'COMPOSE_STARTED\n'
elif [[ "$*" == *' ps '* ]]; then
  [[ "${TEST_GATEWAY_EXISTS:-}" != 1 ]] || printf 'test-gateway\n'
  exit 0
else exit 1; fi
SH
cat > "$test_root/bin/ip" <<'SH'
#!/usr/bin/env bash
printf '1.1.1.1 via 192.168.1.1 dev eth0 src 192.168.1.100 uid 0\n'
SH
cat > "$test_root/bin/ss" <<'SH'
#!/usr/bin/env bash
if [[ "${TEST_PORT_BUSY:-}" == 1 ]]; then printf 'LISTEN 0 4096 127.0.0.1:8888 0.0.0.0:*\n'; fi
SH
chmod +x "$test_root/bin/"*
export PATH="$test_root/bin:$PATH" GOSTREAM3D_DOCKER_SOCKET=/var/run/docker.sock
unset GOSTREAM3D_ICE_PUBLIC_IP GOSTREAM3D_HTTP_PORT
new_case() {
  mkdir -p "$test_root/$1"
  cp "$script_directory/start-docker.sh" "$test_root/$1/start.sh"
  sed -i 's/\r$//' "$test_root/$1/start.sh"
  printf 'services: {}\n' > "$test_root/$1/docker-compose.yml"
  cd "$test_root/$1"
}
new_case fresh
bash start.sh > result.log 2>&1 || { cat result.log; exit 1; }
grep -q '^GOSTREAM3D_ICE_PUBLIC_IP=192.168.1.100$' .env
grep -q 'http://192.168.1.100:8888/admin' result.log
[[ $(stat -c %a .env) == 600 ]]
new_case missing
printf 'GOSTREAM3D_ICE_PUBLIC_IP=\nGOSTREAM3D_HTTP_PORT=10088\n# retained\nGOSTREAM3D_DATA_DIR=/existing/data\n' > .env
bash start.sh > result.log 2>&1 || { cat result.log; exit 1; }
grep -q '^GOSTREAM3D_ICE_PUBLIC_IP=192.168.1.100$' .env
grep -q '^GOSTREAM3D_HTTP_PORT=10088$' .env
grep -q '^GOSTREAM3D_DATA_DIR=/existing/data$' .env
compgen -G '.env.before-*' > /dev/null
new_case preserve
printf 'GOSTREAM3D_ICE_PUBLIC_IP="203.0.113.20"\nGOSTREAM3D_HTTP_PORT=18888\nGOSTREAM3D_DOCKER_SOCKET=%s\n' "$GOSTREAM3D_DOCKER_SOCKET" > .env
cp .env original.env
bash start.sh > result.log 2>&1 || { cat result.log; exit 1; }
cmp .env original.env
grep -q 'http://203.0.113.20:18888/admin' result.log
new_case explicit
bash start.sh --public-ip 192.168.1.100 --http-port 10088 > result.log 2>&1 || { cat result.log; exit 1; }
grep -q '^GOSTREAM3D_HTTP_PORT=10088$' .env
new_case busy
if TEST_PORT_BUSY=1 bash start.sh > result.log 2>&1; then printf 'busy port was accepted\n'; exit 1; fi
grep -q 'http-port' result.log
new_case busy_changed_port
if TEST_PORT_BUSY=1 TEST_GATEWAY_EXISTS=1 bash start.sh > result.log 2>&1; then printf 'another service port was accepted for an existing gateway\n'; exit 1; fi
new_case running_gateway
TEST_PORT_BUSY=1 TEST_GATEWAY_EXISTS=1 TEST_GATEWAY_OWNS_PORT=1 bash start.sh > result.log 2>&1 || { cat result.log; exit 1; }
grep -q 'COMPOSE_STARTED' result.log
new_case invalid
if bash start.sh --http-port 70000 > result.log 2>&1; then printf 'invalid port was accepted\n'; exit 1; fi
[[ ! -e .env ]]
printf 'PASS: automatic LAN IP, missing settings, backups, preserved values, explicit port, occupied port and invalid port.\n'
