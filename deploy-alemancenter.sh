#!/bin/bash
# =============================================================================
# Deploy alemancenter.com — Backend (port 8082) + Frontend (port 3001)
# Same codebase as alemedu.com with different environment
# Run as root on: /var/www/vhosts/alemancenter.com
# =============================================================================

set -e

DOMAIN="alemancenter.com"
API_DOMAIN="api.alemancenter.com"
FRONT_DIR="/var/www/vhosts/${DOMAIN}/httpdocs"
FIBER_DIR="/var/www/vhosts/${DOMAIN}/fiber"
SYSTEM_DIR="/var/www/vhosts/system"
LOG_DIR="/var/www/vhosts/${DOMAIN}/logs"

# ─────────────────────────────────────────────────────────────────────────────
echo "=== [1/7] Creating server directories ==="
mkdir -p "${FIBER_DIR}"
mkdir -p "${FRONT_DIR}"
mkdir -p "${FRONT_DIR}/storage/app/public"
mkdir -p "${LOG_DIR}"
mkdir -p "${SYSTEM_DIR}/${DOMAIN}/conf"
mkdir -p "${SYSTEM_DIR}/${API_DOMAIN}/conf"

# ─────────────────────────────────────────────────────────────────────────────
echo "=== [2/7] Backend — copy binary + .env ==="
# Copy the compiled Go binary
cp ./fiber-api "${FIBER_DIR}/fiber-api"
chmod +x "${FIBER_DIR}/fiber-api"

# Copy .env if not already present
if [ ! -f "${FIBER_DIR}/.env" ]; then
    cp ./fiber/.env.alemancenter "${FIBER_DIR}/.env"
    echo ""
    echo "  ⚠  IMPORTANT: Edit ${FIBER_DIR}/.env"
    echo "     Fill in: DB_PASS_*, JWT_SECRET, FRONTEND_API_KEY, MAIL_*"
    echo ""
fi

# ─────────────────────────────────────────────────────────────────────────────
echo "=== [3/7] Backend — install systemd service ==="
cp ./fiber/alemancenter-api.service /etc/systemd/system/alemancenter-api.service
systemctl daemon-reload
systemctl enable alemancenter-api
systemctl restart alemancenter-api
systemctl status alemancenter-api --no-pager

# ─────────────────────────────────────────────────────────────────────────────
echo "=== [4/7] Frontend — build for alemancenter.com ==="
cd ./front

# .env.production.local has highest priority in Next.js and overrides .env.production
cp .env.alemancenter .env.production.local

npm ci --prefer-offline
npm run build

# Remove the temporary override so alemedu.com builds are not affected
rm .env.production.local

# ─────────────────────────────────────────────────────────────────────────────
echo "=== [5/7] Frontend — deploy built files ==="
rsync -a --delete \
    .next \
    public \
    package.json \
    package-lock.json \
    ecosystem.alemancenter.config.js \
    "${FRONT_DIR}/"

# Rename PM2 config to standard name in deployment dir
mv "${FRONT_DIR}/ecosystem.alemancenter.config.js" "${FRONT_DIR}/ecosystem.config.js"

# Install production deps only
cd "${FRONT_DIR}"
npm ci --omit=dev --prefer-offline

# ─────────────────────────────────────────────────────────────────────────────
echo "=== [6/7] Nginx vhost configurations ==="
cat > "${SYSTEM_DIR}/${DOMAIN}/conf/vhost_nginx.conf" << 'NGINX_FRONTEND'
# ============================================================
# alemancenter.com → Next.js (port 3001) + Go Fiber (port 8082)
# ============================================================

# ── AI generation — 90s timeout ─────────────────────────────
location ^~ /backend-api/ai/ {
    proxy_pass http://127.0.0.1:8082/api/ai/;
    proxy_http_version 1.1;
    proxy_set_header Host api.alemancenter.com;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto https;
    proxy_set_header X-Forwarded-Host $host;
    proxy_set_header Origin "https://alemancenter.com";
    proxy_set_header Referer "https://alemancenter.com/";
    proxy_set_header Accept "application/json";
    proxy_set_header X-Requested-With "XMLHttpRequest";
    proxy_set_header Accept-Encoding "";
    gzip off;
    brotli off;
    proxy_connect_timeout 10s;
    proxy_send_timeout    90s;
    proxy_read_timeout    90s;
}

# ── All other backend-api calls ──────────────────────────────
location ^~ /backend-api/ {
    proxy_pass http://127.0.0.1:8082/api/;
    proxy_http_version 1.1;
    proxy_set_header Host api.alemancenter.com;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto https;
    proxy_set_header X-Forwarded-Host $host;
    proxy_set_header Origin "https://alemancenter.com";
    proxy_set_header Referer "https://alemancenter.com/";
    proxy_set_header Accept "application/json";
    proxy_set_header X-Requested-With "XMLHttpRequest";
    proxy_set_header Accept-Encoding "";
    gzip off;
    brotli off;
    proxy_connect_timeout 10s;
    proxy_send_timeout    60s;
    proxy_read_timeout    60s;
}

# ── Next.js Static Assets (1 year immutable cache) ──────────
location ^~ /_next/static/ {
    alias /var/www/vhosts/alemancenter.com/httpdocs/.next/static/;
    access_log off;
    log_not_found off;
    etag on;
    add_header Cache-Control "public, max-age=31536000, immutable" always;
    try_files $uri =404;
}

# ── Favicon + manifest ───────────────────────────────────────
location = /favicon.ico {
    root /var/www/vhosts/alemancenter.com/httpdocs/public;
    access_log off; log_not_found off;
    expires 7d;
    add_header Cache-Control "public, max-age=604800" always;
    try_files $uri =404;
}

location = /robots.txt {
    root /var/www/vhosts/alemancenter.com/httpdocs/public;
    access_log off; log_not_found off;
    expires 1h;
    add_header Cache-Control "public, max-age=3600" always;
    try_files $uri =404;
}

location = /sitemap.xml {
    root /var/www/vhosts/alemancenter.com/httpdocs/public;
    access_log off; log_not_found off;
    expires 1h;
    add_header Cache-Control "public, max-age=3600" always;
    try_files $uri =404;
}

location = /manifest.json {
    root /var/www/vhosts/alemancenter.com/httpdocs/public;
    access_log off; log_not_found off;
    expires 7d;
    add_header Cache-Control "public, max-age=604800" always;
    try_files $uri =404;
}

# ── Public static media ──────────────────────────────────────
location ~* ^/(assets|images|icons|fonts|storage)/.*\.(?:css|js|mjs|map|jpg|jpeg|png|gif|webp|avif|svg|ico|woff|woff2|ttf|eot)$ {
    root /var/www/vhosts/alemancenter.com/httpdocs/public;
    access_log off; log_not_found off;
    expires 30d;
    add_header Cache-Control "public, max-age=2592000" always;
    add_header X-Content-Type-Options "nosniff" always;
    try_files $uri =404;
}
NGINX_FRONTEND

cat > "${SYSTEM_DIR}/${API_DOMAIN}/conf/vhost_nginx.conf" << 'NGINX_BACKEND'
# ============================================================
# api.alemancenter.com → Go Fiber (port 8082)
# ============================================================

# ── AI generation — 90s timeout ─────────────────────────────
location ^~ /api/ai/ {
    proxy_pass http://127.0.0.1:8082;
    proxy_http_version 1.1;
    proxy_set_header Connection "";
    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;
    proxy_set_header Accept-Encoding "";
    gzip off; brotli off;
    proxy_connect_timeout 10s;
    proxy_send_timeout    90s;
    proxy_read_timeout    90s;
}

# ── All API routes ───────────────────────────────────────────
location ^~ /api/ {
    proxy_pass http://127.0.0.1:8082;
    proxy_http_version 1.1;
    proxy_set_header Connection "";
    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;
    proxy_set_header Accept-Encoding "";
    gzip off; brotli off;
    proxy_connect_timeout 10s;
    proxy_send_timeout    60s;
    proxy_read_timeout    300s;
}

# ── Storage (public files) ───────────────────────────────────
location ^~ /storage/ {
    alias /var/www/vhosts/alemancenter.com/httpdocs/storage/app/public/;
    expires 30d;
    add_header Cache-Control "public" always;
    access_log off;
}
NGINX_BACKEND

nginx -t && systemctl reload nginx

# ─────────────────────────────────────────────────────────────────────────────
echo "=== [7/7] Start frontend with PM2 ==="
cd "${FRONT_DIR}"

# Export keys before PM2 reads ecosystem.config.js
export NEXT_PUBLIC_FRONTEND_API_KEY=$(grep NEXT_PUBLIC_FRONTEND_API_KEY /var/www/vhosts/alemancenter.com/fiber/.env | cut -d= -f2)
export FRONTEND_API_KEY=$(grep "^FRONTEND_API_KEY=" /var/www/vhosts/alemancenter.com/fiber/.env | cut -d= -f2)

pm2 delete alemancenter-frontend 2>/dev/null || true
pm2 start ecosystem.config.js
pm2 save

echo ""
echo "═══════════════════════════════════════════════════════════"
echo " ✓ alemancenter.com deployed successfully"
echo "═══════════════════════════════════════════════════════════"
echo " Backend : http://127.0.0.1:8082  (alemancenter-api.service)"
echo " Frontend: http://127.0.0.1:3001  (pm2: alemancenter-frontend)"
echo ""
echo " ⚠  Still required:"
echo "   1. In Plesk: set alemancenter.com Node.js app port → 3001"
echo "   2. Verify: systemctl status alemancenter-api"
echo "   3. Verify: pm2 status"
echo "   4. Test: curl http://127.0.0.1:8082/api/health"
echo "   5. Test: curl http://127.0.0.1:3001"
echo "═══════════════════════════════════════════════════════════"
