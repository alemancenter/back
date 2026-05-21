-- Fix file counters for legacy Alemancenter databases.
-- Run this once on each country database if AutoMigrate is not adding columns automatically.

ALTER TABLE files ADD COLUMN IF NOT EXISTS view_count INT NOT NULL DEFAULT 0;
ALTER TABLE files ADD COLUMN IF NOT EXISTS views_count INT NOT NULL DEFAULT 0;
ALTER TABLE files ADD COLUMN IF NOT EXISTS download_count INT NOT NULL DEFAULT 0;

UPDATE files
SET view_count = GREATEST(COALESCE(view_count, 0), COALESCE(views_count, 0)),
    views_count = GREATEST(COALESCE(view_count, 0), COALESCE(views_count, 0));

CREATE INDEX IF NOT EXISTS idx_files_view_count ON files(view_count);
CREATE INDEX IF NOT EXISTS idx_files_views_count ON files(views_count);
CREATE INDEX IF NOT EXISTS idx_files_download_count ON files(download_count);
