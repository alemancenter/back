# Alemancenter Final Production Deployment Profile

## Qualified domains

- Frontend public domain: `https://alemancenter.com`
- Backend public API domain: `https://api.alemancenter.com`
- Frontend browser API path: `https://alemancenter.com/backend-api/*`
- SSR/internal API URL: `http://127.0.0.1:8080/api`
- Internal backend listen: `127.0.0.1:8080`
- Internal frontend listen: `127.0.0.1:3000`
- Server IPv4: `152.53.208.71`
- Server IPv6: `2a0a:4cc0:2000:c6b0::10`

## DNS records

```dns
alemancenter.com.       A     152.53.208.71
www.alemancenter.com.   A     152.53.208.71
api.alemancenter.com.   A     152.53.208.71

alemancenter.com.       AAAA  2a0a:4cc0:2000:c6b0::10
www.alemancenter.com.   AAAA  2a0a:4cc0:2000:c6b0::10
api.alemancenter.com.   AAAA  2a0a:4cc0:2000:c6b0::10
```

## Frontend environment

Use `deploy/alemancenter/frontend.env.production` and copy it to:

```bash
/var/www/vhosts/alemancenter.com/httpdocs/.env.production.local
```

Important values:

```env
NEXT_PUBLIC_API_URL=/backend-api
API_INTERNAL_URL=http://127.0.0.1:8080/api
API_HOSTNAME=api.alemancenter.com
NEXT_PUBLIC_APP_URL=https://alemancenter.com
NEXT_PUBLIC_SITE_URL=https://alemancenter.com
```

## Backend environment

Use `deploy/alemancenter/api.env.example` and install it as:

```bash
/etc/alemancenter/api.env
```

Important values:

```env
APP_HOST=127.0.0.1
APP_PORT=8080
APP_URL=https://api.alemancenter.com
FRONTEND_URL=https://alemancenter.com
CORS_ALLOWED_ORIGINS=https://alemancenter.com,https://www.alemancenter.com
TRUSTED_PROXIES=127.0.0.1,::1,152.53.208.71,2a0a:4cc0:2000:c6b0::10
SSR_TRUSTED_IPS=127.0.0.1,::1
```

## Nginx/Plesk

Frontend vhost `alemancenter.com` must proxy `/backend-api/` internally to:

```txt
http://127.0.0.1:8080/api/
```

API vhost `api.alemancenter.com` must proxy public requests to:

```txt
http://127.0.0.1:8080
```

Use:
- `frontend/deploy/alemancenter/nginx-frontend-additional-directives.conf`
- `backend/deploy/alemancenter/nginx-api-additional-directives.conf`

## Build checks

Frontend:

```bash
npm run lint
npm run type-check
NEXT_TELEMETRY_DISABLED=1 npm run build
```

Backend:

```bash
go build -o fiber-api ./cmd/server
go test ./...
```

## Smoke tests

```bash
curl -I https://alemancenter.com/
curl -I https://alemancenter.com/backend-api/health
curl -I https://api.alemancenter.com/api/health
curl -s http://127.0.0.1:8080/api/health
```
