#!/usr/bin/env bash
set -euo pipefail
if [[ ${EUID} -ne 0 || $# -ne 1 || ! -f "$1" ]]; then
 echo 'Usage: sudo update.sh /path/to/new/linux-gateway-binary' >&2; exit 2
fi
newbin="$(realpath "$1")"
target=/usr/local/bin/uwa-gateway
backup=/usr/local/bin/uwa-gateway.previous
unit=universal-weather-api.service
[[ -f "$target" ]] || { echo 'Install service first.' >&2; exit 1; }
was_active=false
if systemctl is-active --quiet "$unit"; then was_active=true; fi
staged="$(mktemp /usr/local/bin/.uwa-update.XXXXXX)"
prior="$(mktemp /usr/local/bin/.uwa-prior.XXXXXX)"
stopped=false
rollback() {
 status=$?
 trap - EXIT HUP INT TERM
 if [[ $status -ne 0 && "$stopped" == true ]]; then
  echo 'Update failed; restoring previous executable and running state.' >&2
  systemctl stop "$unit" || true
  mv -f -- "$prior" "$target"
  if [[ "$was_active" == true ]]; then systemctl start "$unit" || true; fi
 fi
 rm -f -- "$staged" "$prior"
 exit "$status"
}
trap rollback EXIT
trap 'exit 1' HUP INT TERM
# Stage and validate before stopping; quoted paths are never executed as shell text.
install -m 0755 -- "$newbin" "$staged"
cp -p -- "$target" "$prior"
cp -p -- "$target" "$backup"
stopped=true
systemctl stop "$unit"
mv -f -- "$staged" "$target"
if [[ "$was_active" == true ]]; then systemctl start "$unit"; fi
stopped=false
echo 'Gateway updated. Previous executable retained at /usr/local/bin/uwa-gateway.previous'
