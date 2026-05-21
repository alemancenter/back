-- Ensure dashboard view counters use the same columns as the Go models and services.
-- Run per country database if AutoMigrate is disabled.
ALTER TABLE files ADD COLUMN IF NOT EXISTS view_count INT NOT NULL DEFAULT 0;

-- Optional legacy migration: if an older database has views_count values, copy them once.
-- MySQL versions without IF EXISTS in UPDATE expressions may need manual adjustment.
UPDATE files
SET view_count = GREATEST(COALESCE(view_count, 0), COALESCE(views_count, 0))
WHERE EXISTS (
  SELECT 1
  FROM information_schema.columns
  WHERE table_schema = DATABASE()
    AND table_name = 'files'
    AND column_name = 'views_count'
);
