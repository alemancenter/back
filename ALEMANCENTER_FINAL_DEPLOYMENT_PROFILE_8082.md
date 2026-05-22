# Alemancenter Final Deployment Profile — 8082 Backend

## Domains
- Frontend: `https://alemancenter.com`
- Backend public API: `https://api.alemancenter.com`
- Backend internal Fiber: `http://127.0.0.1:8082`
- Frontend browser API path: `/backend-api`
- Frontend SSR internal API: `http://127.0.0.1:8082/api`

## DNS
```dns
alemancenter.com.       A     152.53.208.71
www.alemancenter.com.   A     152.53.208.71
api.alemancenter.com.   A     152.53.208.71
alemancenter.com.       AAAA  2a0a:4cc0:2000:c6b0::10
www.alemancenter.com.   AAAA  2a0a:4cc0:2000:c6b0::10
api.alemancenter.com.   AAAA  2a0a:4cc0:2000:c6b0::10
```

## Frontend production env
```env
NEXT_PUBLIC_API_URL=/backend-api
API_INTERNAL_URL=http://127.0.0.1:8082/api
API_HOSTNAME=api.alemancenter.com
NEXT_PUBLIC_APP_URL=https://alemancenter.com
NEXT_PUBLIC_SITE_URL=https://alemancenter.com
FRONTEND_API_KEY=<same-as-backend>
NEXT_PUBLIC_FRONTEND_API_KEY=<same-as-backend>
```

## Backend production env
```env
APP_HOST=127.0.0.1
APP_PORT=8082
APP_URL=https://api.alemancenter.com
FRONTEND_URL=https://alemancenter.com
CORS_ALLOWED_ORIGINS=https://alemancenter.com,https://www.alemancenter.com
TRUSTED_PROXIES=127.0.0.1,::1,152.53.208.71,2a0a:4cc0:2000:c6b0::10
SSR_TRUSTED_IPS=127.0.0.1,::1
```

## Smoke tests
```bash
curl -s http://127.0.0.1:8082/api/ping
curl -I https://api.alemancenter.com/api/ping
curl -I https://alemancenter.com/backend-api/ping
curl -I https://alemancenter.com/
```
