# Redis TTL / Persistent Keys Admin Fix

Implemented fixes:

- Redis key listing now returns real TTL from Redis using TTL command.
- Redis key listing returns key type and memory usage.
- Persistent keys are explicitly detected by TTL = -1.
- New cache keys default to 1 hour TTL when TTL is not provided.
- Persistent cache keys require explicit `persist: true`.
- Added endpoint to assign TTL to an existing key.
- Added endpoint to assign TTL to legacy Laravel IP location keys matching `*_cache_ip_location_*`.
- Added endpoint to delete legacy Laravel IP location keys matching `*_cache_ip_location_*`.

Endpoints:

- GET `/api/dashboard/redis/keys?ttl_filter=all|persistent|volatile`
- POST `/api/dashboard/redis/:key/expire`
- POST `/api/dashboard/redis/legacy-ip-location/expire`
- DELETE `/api/dashboard/redis/legacy-ip-location/clean`

Recommended default TTLs:

- IP location cache: 7 days
- Dashboard cache: 30-120 seconds
- Auth permissions: 10-15 minutes
- AI results: 6-24 hours

Notes:

- Redis automatically deletes expired keys.
- Persistent keys should be rare and created only intentionally.
