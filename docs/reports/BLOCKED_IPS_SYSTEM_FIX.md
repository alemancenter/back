# Blocked IPs System Fix

## Scope
Implemented a full backend fix for `/api/dashboard/security/blocked-ips` and the related manual block/unblock flow.

## Backend changes
- Added filtering by `search`, `q`, or `ip` query parameter.
- Added `status=active|expired` filtering based on `expires_at`.
- Added computed response fields:
  - `is_active`
  - `is_expired`
  - `status`
  - `remaining_days`
- Added support for temporary blocks using `days` in the request body.
- Added IP validation before manual blocking.
- Kept permanent blocks supported when `days` is empty or zero.
- Kept unblock-by-ID and unblock-by-IP support.

## API contract
### List blocked IPs
`GET /api/dashboard/security/blocked-ips?search=127.0.0.1&status=active&page=1&per_page=15`

### Block IP
`POST /api/dashboard/security/ip/block`

```json
{
  "ip_address": "192.168.1.10",
  "reason": "Repeated login attempts",
  "days": 7
}
```

### Unblock IP
`DELETE /api/dashboard/security/blocked-ips/{ip_or_id}`

## Notes
Run the SQL file if your database does not already have the required indexes/fields:
`docs/sql/blocked_ips_system_fix.sql`
