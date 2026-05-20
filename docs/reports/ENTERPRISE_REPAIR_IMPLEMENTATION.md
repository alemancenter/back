# Enterprise Repair Implementation — 500k Daily Visits

## Backend changes

- Increased authenticated user/permission cache TTL from 15 seconds to 15 minutes to reduce permission-query flooding.
- Added Redis-backed prefix rate limiting for expensive paths:
  - `/api/dashboard/content-audit/ai/`
  - `/api/ai/`
  - `/api/dashboard/files`
- Activated existing auth-sensitive rate limits globally.
- Added distributed Redis locks for AI analyze and fix-preview operations to prevent duplicate AI calls across browser double-clicks and multiple API workers.
- Preserved the existing in-process mutex as a secondary local guard.
- Hardened Content AI schema normalization and added enterprise indexes for AI lookups, audit findings, and visitor tracking.
- Kept visitor tracking in non-blocking batch mode to avoid per-request DB writes.

## Frontend changes

- Added in-memory auth user cache and singleflight for `/auth/user`.
- Throttled `useUserRefresh` to avoid repeated auth/user calls during dashboard hydration.
- Increased AI client timeouts to match backend behavior:
  - analyze: 60s
  - fix preview: 155s
- Kept in-flight de-duplication for AI analyze and fix-preview calls.

## Remaining production recommendations

For true 500k+ daily visits, the next step should be a real asynchronous AI worker queue using Redis/Asynq or RabbitMQ. The current implementation is much safer and more stable, but AI still runs in request lifecycle for compatibility with the existing UI.

## Local verification commands

```bash
go run ./cmd/server/main.go
npm install
npm run build
```

## Note

The sandbox could not run Go tests because the project requires Go 1.25 and the environment cannot download the toolchain.
