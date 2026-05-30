-- =============================================================================
-- Migration: fix_adsense_and_canonical_url.sql
-- Purpose  : Unify adsense_client across all country databases and populate
--            canonical_url / site_url where missing.
--
-- Run on   : ALL country databases (jo, sa, eg, ps)
-- Safe to  : re-run (uses INSERT … ON DUPLICATE KEY UPDATE)
-- =============================================================================

-- ── 1. Unify AdSense publisher ID ──────────────────────────────────────────
-- AdSense policy: only ONE ca-pub-* per domain.
-- Replace the wrong ID (ca-pub-5143966486037953) with the correct one.
INSERT INTO settings (`key`, `value`, created_at, updated_at)
VALUES ('adsense_client', 'ca-pub-2187451543840210', NOW(), NOW())
ON DUPLICATE KEY UPDATE
    `value`     = 'ca-pub-2187451543840210',
    updated_at  = NOW();

-- ── 2. Populate canonical_url if empty ─────────────────────────────────────
-- Prevents the frontend from rendering broken links like [](<>) in policy pages.
INSERT INTO settings (`key`, `value`, created_at, updated_at)
VALUES ('canonical_url', 'https://alemancenter.com', NOW(), NOW())
ON DUPLICATE KEY UPDATE
    `value`     = IF(`value` = '' OR `value` IS NULL, 'https://alemancenter.com', `value`),
    updated_at  = IF(`value` = '' OR `value` IS NULL, NOW(), updated_at);

-- ── 3. Populate site_url if empty ──────────────────────────────────────────
INSERT INTO settings (`key`, `value`, created_at, updated_at)
VALUES ('site_url', 'https://alemancenter.com', NOW(), NOW())
ON DUPLICATE KEY UPDATE
    `value`     = IF(`value` = '' OR `value` IS NULL, 'https://alemancenter.com', `value`),
    updated_at  = IF(`value` = '' OR `value` IS NULL, NOW(), updated_at);

-- ── 4. Ensure contact_email is set (needed for contact form delivery) ───────
-- Only inserts if the row does not exist yet; does NOT overwrite existing value.
INSERT IGNORE INTO settings (`key`, `value`, created_at, updated_at)
VALUES ('contact_email', 'info@alemancenter.com', NOW(), NOW());

-- ── 5. Ensure site_name is set ──────────────────────────────────────────────
INSERT IGNORE INTO settings (`key`, `value`, created_at, updated_at)
VALUES ('site_name', 'موقع الأيمان', NOW(), NOW());

-- ── Verify ──────────────────────────────────────────────────────────────────
SELECT `key`, `value`, updated_at
FROM settings
WHERE `key` IN (
    'adsense_client',
    'canonical_url',
    'site_url',
    'contact_email',
    'site_name'
)
ORDER BY `key`;
