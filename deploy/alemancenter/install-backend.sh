#!/usr/bin/env bash
set -euo pipefail

APP_DIR="/var/www/vhosts/api.alemancenter.com/httpdocs"
ENV_DIR="/etc/alemancenter"

mkdir -p "$APP_DIR/storage/uploads" "$APP_DIR/storage/logs" "$ENV_DIR"

# Copy binary after building it locally or on server:
#   go build -o fiber-api ./cmd/server
if [ -f ./fiber-api ]; then
  cp ./fiber-api "$APP_DIR/fiber-api"
  chmod +x "$APP_DIR/fiber-api"
fi

if [ ! -f "$ENV_DIR/api.env" ]; then
  cp deploy/alemancenter/api.env.example "$ENV_DIR/api.env"
  chmod 600 "$ENV_DIR/api.env"
  echo "Edit $ENV_DIR/api.env before starting the service."
fi

cp deploy/alemancenter/alemancenter-api.service /etc/systemd/system/alemancenter-api.service

# Install shared Nginx rate-limit zones (http-context — cannot go in Plesk Additional Directives)
cp deploy/alemancenter/nginx-rate-limit-zones.conf /etc/nginx/conf.d/alemancenter-rate-limit.conf
nginx -t && nginx -s reload

systemctl daemon-reload
systemctl enable alemancenter-api
systemctl restart alemancenter-api
systemctl status alemancenter-api --no-pager -l
