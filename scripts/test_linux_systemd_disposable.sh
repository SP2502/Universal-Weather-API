#!/usr/bin/env bash
set -euo pipefail

if [[ ${EUID} -ne 0 ]]; then echo 'Run as root in a disposable systemd-enabled Linux environment.' >&2; exit 1; fi
root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
base="$root/.uwa-testing/gateway-linux-systemd-acceptance"
update="$root/.uwa-testing/gateway-linux-systemd-update"
installer="$root/deployments/linux/install.sh"
updater="$root/deployments/linux/update.sh"
unit=universal-weather-api.service
target=/usr/local/bin/uwa-gateway
backup=/usr/local/bin/uwa-gateway.previous
config_dir=/etc/universal-weather-api
state_dir=/var/lib/universal-weather-api
unit_file=/etc/systemd/system/universal-weather-api.service
port=18598

for file in "$base" "$update"; do [[ -f "$file" ]] || { echo "Missing test binary: $file" >&2; exit 1; }; done
base_hash="$(sha256sum "$base" | cut -d' ' -f1)"
update_hash="$(sha256sum "$update" | cut -d' ' -f1)"
[[ "$base_hash" != "$update_hash" ]] || { echo 'Update binary must differ from base.' >&2; exit 1; }
if id uwa >/dev/null 2>&1 || systemctl list-unit-files "$unit" --no-legend 2>/dev/null | grep -q '^universal-weather-api.service' || \
  [[ -e "$target" || -e "$backup" || -e "$config_dir" || -e "$state_dir" || -e "$unit_file" ]]; then
  echo 'Existing UWA Linux installation or account found; refusing to touch it.' >&2
  exit 1
fi
if curl -fsS --max-time 1 "http://127.0.0.1:$port/liveness" >/dev/null 2>&1; then
  echo "Test port $port already serves liveness; refusing to run." >&2
  exit 1
fi

started=false
invalid=''
cleanup() {
  status=$?
  trap - EXIT
  set +e
  if [[ "$started" == true ]]; then
    systemctl disable --now "$unit" >/dev/null 2>&1
    rm -f -- "$unit_file" "$target" "$backup" "$config_dir/gateway.env"
    systemctl daemon-reload >/dev/null 2>&1
    rmdir -- "$config_dir" "$state_dir" >/dev/null 2>&1
    userdel uwa >/dev/null 2>&1
  fi
  [[ -z "$invalid" ]] || rm -f -- "$invalid"
  if systemctl list-unit-files "$unit" --no-legend 2>/dev/null | grep -q '^universal-weather-api.service' ||
     [[ -e "$unit_file" || -e "$target" || -e "$backup" ]] || id uwa >/dev/null 2>&1; then
    echo 'Cleanup incomplete; inspect disposable Linux installation.' >&2
    status=1
  fi
  exit "$status"
}
trap cleanup EXIT
started=true

bash "$installer" "$base"
cat > "$config_dir/gateway.env" <<EOF
APP_ENV=development
APP_DEPLOYMENT_USE=noncommercial
APP_DB_DRIVER=memory
APP_CACHE_DRIVER=memory
APP_BIND_HOST=127.0.0.1
APP_PORT=$port
APP_LOG_LEVEL=INFO
EOF
chown root:root "$config_dir/gateway.env"
chmod 0600 "$config_dir/gateway.env"
systemctl start "$unit"

wait_liveness() {
  for ((i=0; i<40; i++)); do
    if [[ "$(curl -sS --max-time 2 -o /dev/null -w '%{http_code}' "http://127.0.0.1:$port/liveness" 2>/dev/null || true)" == 200 ]]; then return 0; fi
    sleep 0.25
  done
  systemctl status "$unit" --no-pager -l >&2 || true
  echo 'Liveness 200 not reached.' >&2
  return 1
}
wait_liveness
[[ "$(systemctl show "$unit" -p User --value)" == uwa ]] || { echo 'Wrong systemd user.' >&2; exit 1; }
echo 'INSTALL_PASS'

bash "$updater" "$update"
wait_liveness
[[ "$(sha256sum "$target" | cut -d' ' -f1)" == "$update_hash" ]] || { echo 'Update hash mismatch.' >&2; exit 1; }
echo 'UPDATE_PASS'

invalid="$(mktemp /tmp/uwa-invalid.XXXXXX)"
printf 'not a Linux executable\n' > "$invalid"
chmod 0755 "$invalid"
if bash "$updater" "$invalid"; then echo 'Invalid update unexpectedly succeeded.' >&2; exit 1; fi
wait_liveness
[[ "$(sha256sum "$target" | cut -d' ' -f1)" == "$update_hash" ]] || { echo 'Rollback hash mismatch.' >&2; exit 1; }
echo 'ROLLBACK_PASS'
echo 'LINUX_SYSTEMD_ACCEPTANCE_PASS'
