# Redis Layer + Monitoring Implementation

## Backend additions

### Redis Layer
- Added JSON helpers to `internal/database/redis.go`:
  - `SetJSON`
  - `GetJSON`
  - `RememberJSON`
  - `WithLock`
  - `Stats`
- Added public response cache middleware for safe GET endpoints:
  - `/api/front/settings`
  - `/api/home`
  - `/api/articles`
  - `/api/posts`
  - `/api/categories`
  - `/api/keywords`
  - academic public lookups
- Dashboard, auth, notifications, messages, comments and private APIs are explicitly excluded from HTTP caching.

### Monitoring
- Added in-process metrics collector in `internal/monitoring`.
- Added middleware `middleware.Metrics()` for route counters, error counters, and latency.
- Added Prometheus-compatible endpoint:
  - `GET /metrics`
- Added dashboard JSON endpoint:
  - `GET /api/dashboard/performance/metrics`
- Extended `/api/health` with:
  - Redis stats
  - app metrics snapshot
  - memory and goroutine stats

### Permissions
- Added `middleware.CanAny()` so performance dashboard can be accessed by either `manage monitoring` or `manage performance`.

## Frontend additions

- Added `PERFORMANCE.METRICS` endpoint.
- Extended performance service with `getMetrics()`.
- Updated dashboard performance page to show:
  - total requests
  - total 5xx errors
  - application average latency

## Deployment files

- Added `deploy/monitoring/docker-compose.monitoring.yml`
- Added `deploy/monitoring/prometheus/prometheus.yml`
- Added starter Grafana dashboard JSON.

## Required environment

```env
REDIS_HOST=127.0.0.1
REDIS_PORT=6379
REDIS_PASSWORD=
REDIS_DB=0
REDIS_CACHE_DB=1
REDIS_QUEUE_DB=2
REDIS_PREFIX=alemancenter
```

## Local monitoring start

```bash
cd deploy/monitoring
docker compose -f docker-compose.monitoring.yml up -d
```

Prometheus:

```text
http://localhost:9090
```

Grafana:

```text
http://localhost:3001
```

Prometheus scrape endpoint:

```text
http://localhost:8080/metrics
```
