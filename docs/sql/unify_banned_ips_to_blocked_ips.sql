-- One-time compatibility migration: copy old Laravel banned_ips into canonical blocked_ips.
INSERT INTO blocked_ips (ip_address, reason, blocked_by, expires_at, is_auto_block, created_at, updated_at)
SELECT ip, reason, banned_by, banned_until, 0, COALESCE(created_at, NOW(3)), COALESCE(updated_at, NOW(3))
FROM banned_ips
WHERE ip IS NOT NULL AND ip <> ''
ON DUPLICATE KEY UPDATE
    reason = VALUES(reason),
    blocked_by = VALUES(blocked_by),
    expires_at = VALUES(expires_at),
    updated_at = VALUES(updated_at);

-- After verifying the dashboard uses blocked_ips correctly for at least a week:
-- RENAME TABLE banned_ips TO banned_ips_backup;
