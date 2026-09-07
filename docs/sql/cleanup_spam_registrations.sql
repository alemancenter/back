-- Cleanup for the promo-spam registration wave (Cyrillic "Поздравляем! … tinyurl.com" names).
-- Run the SELECT first, eyeball the rows, then run the DELETE block.
-- `users` lives in the MAIN database (not the per-country shards).

-- ── 1. Inspect the scope ───────────────────────────────────────────────────────
SELECT id, name, email, status, email_verified_at, created_at
FROM users
WHERE email_verified_at IS NULL
  AND google_id IS NULL
  AND facebook_id IS NULL
  AND (
        name REGEXP 'https?://|www\\.|tinyurl|t\\.me|bit\\.ly|cutt\\.ly'   -- links in the name
     OR name REGEXP '[А-Яа-яЁёЂ-џ]'                                        -- Cyrillic letters
     OR CHAR_LENGTH(name) > 60                                            -- sentence, not a name
     OR name LIKE '%@%'
  )
ORDER BY created_at DESC;

-- Optional: also flag the burst window if you know it (adjust the timestamps).
-- AND created_at BETWEEN '2026-09-07 16:00:00' AND '2026-09-07 18:00:00'

-- ── 2. Delete (run inside a transaction) ───────────────────────────────────────
-- Adjust the WHERE to match exactly what step 1 returned. Keep the guards
-- (email_verified_at IS NULL, no OAuth ids) so a real account is never caught.
START TRANSACTION;

CREATE TEMPORARY TABLE _spam_ids AS
SELECT id FROM users
WHERE email_verified_at IS NULL
  AND google_id IS NULL
  AND facebook_id IS NULL
  AND (
        name REGEXP 'https?://|www\\.|tinyurl|t\\.me|bit\\.ly|cutt\\.ly'
     OR name REGEXP '[А-Яа-яЁёЂ-џ]'
     OR CHAR_LENGTH(name) > 60
     OR name LIKE '%@%'
  );

-- child rows first (ignore any table that does not exist in your schema)
DELETE FROM model_has_roles       WHERE model_id IN (SELECT id FROM _spam_ids);
DELETE FROM model_has_permissions WHERE model_id IN (SELECT id FROM _spam_ids);
DELETE FROM personal_access_tokens WHERE tokenable_id IN (SELECT id FROM _spam_ids);
DELETE FROM notifications          WHERE notifiable_id IN (SELECT id FROM _spam_ids);
UPDATE visitors_tracking SET user_id = NULL WHERE user_id IN (SELECT id FROM _spam_ids);

DELETE FROM users WHERE id IN (SELECT id FROM _spam_ids);

DROP TEMPORARY TABLE _spam_ids;

-- verify the count looks right, then:
COMMIT;
-- or: ROLLBACK;
