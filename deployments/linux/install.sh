#!/usr/bin/env bash
set -euo pipefail
# Usage: sudo ./deployments/linux/install.sh ./gateway-linux-amd64
if [[ ${EUID} -ne 0 ]]; then echo 'Run as root (sudo)' >&2; exit 1; fi
if [[ $# -ne 1 || ! -f "$1" ]]; then echo 'Usage: sudo install.sh /path/to/gateway-binary' >&2; exit 2; fi
src="$(realpath "$1")"
script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
if ! id -u uwa >/dev/null 2>&1; then
  nologin="$(command -v nologin || printf '/bin/false')"
  useradd --system --user-group --home-dir /var/lib/universal-weather-api --shell "$nologin" uwa
fi
install -D -m 0755 "$src" /usr/local/bin/uwa-gateway
install -d -m 0700 /etc/universal-weather-api
install -d -m 0700 -o uwa -g uwa /var/lib/universal-weather-api
if [[ ! -f /etc/universal-weather-api/gateway.env ]]; then
  install -m 0600 "$script_dir/gateway.env.example" /etc/universal-weather-api/gateway.env
fi
chown root:root /etc/universal-weather-api/gateway.env
chmod 0600 /etc/universal-weather-api/gateway.env
install -m 0644 "$script_dir/universal-weather-api.service" /etc/systemd/system/universal-weather-api.service
systemctl daemon-reload
systemctl enable universal-weather-api.service
echo 'Installed and enabled, NOT started.'
echo 'Edit /etc/universal-weather-api/gateway.env and ensure PostgreSQL/provider credentials are correct.'
echo 'Then run: sudo systemctl start universal-weather-api'
echo 'To inspect: sudo journalctl -u universal-weather-api -f'
