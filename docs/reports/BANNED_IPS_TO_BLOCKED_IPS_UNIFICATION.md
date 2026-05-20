# Banned IPs / Blocked IPs Unification

## Canonical table
`blocked_ips` is now the official table used by the dashboard and IPGuard.

## Backward compatibility
If an old Laravel table named `banned_ips` exists, the backend now:

- Copies old rows from `banned_ips` into `blocked_ips` when listing blocked IPs.
- Checks `banned_ips` as a fallback inside IPGuard.
- Mirrors new manual blocks into `banned_ips` when that table exists.
- Deletes from both tables when unblocking by IP or ID.

## Field mapping

| banned_ips | blocked_ips |
|---|---|
| ip | ip_address |
| reason | reason |
| banned_by | blocked_by |
| banned_until | expires_at |

## Manual migration
See `docs/sql/unify_banned_ips_to_blocked_ips.sql`.

## Recommendation
Keep `blocked_ips` as the only official table. After verifying production behavior, rename the legacy table:

```sql
RENAME TABLE banned_ips TO banned_ips_backup;
```
