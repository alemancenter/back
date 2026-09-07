#!/usr/bin/env bash
set -euo pipefail
APP_DIR="${IMANJO_APP_DIR:-/var/www/vhosts/api.imanjo.com/httpdocs}"
ENV_DIR="/etc/imanjo"
if [ "$(id -u)" -ne 0 ]; then echo "Run this installation preparation as root." >&2; exit 1; fi
if ! id imanjo >/dev/null 2>&1; then useradd --system --no-create-home --shell /sbin/nologin imanjo; fi
install -d -o imanjo -g imanjo -m 0750 "$APP_DIR/storage" "$APP_DIR/storage/logs"
install -d -m 0750 "$ENV_DIR"
if [ ! -f "$ENV_DIR/api.env" ]; then
  install -m 0600 deploy/imanjo/api.env.example "$ENV_DIR/api.env"
  echo "Configure $ENV_DIR/api.env and storage permissions before starting."
fi
if [ -f ./fiber-api ]; then install -o root -g imanjo -m 0750 ./fiber-api "$APP_DIR/fiber-api"; fi
# Keep paths aligned with the selected APP_DIR; environment remains a systemd EnvironmentFile.
sed "s|/var/www/vhosts/api.imanjo.com/httpdocs|$APP_DIR|g" deploy/imanjo/imanjo-api.service > /etc/systemd/system/imanjo-api.service
sed "s|/var/www/vhosts/api.imanjo.com/httpdocs|$APP_DIR|g" deploy/imanjo/imanjo-api-migrate.service > /etc/systemd/system/imanjo-api-migrate.service
systemctl daemon-reload
echo "Preparation complete. Follow RELEASE-NOTES.md: backup, validate staging, migrate, then activate."
echo "No service was restarted and no Nginx configuration was activated."
