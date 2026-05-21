-- MySQL/MariaDB compatible version if ADD COLUMN IF NOT EXISTS is not supported.
-- Run each block manually only if the column does not already exist.
-- Check columns:
-- SHOW COLUMNS FROM files LIKE 'view_count';
-- SHOW COLUMNS FROM files LIKE 'views_count';
-- SHOW COLUMNS FROM files LIKE 'download_count';

ALTER TABLE files ADD COLUMN view_count INT NOT NULL DEFAULT 0;
ALTER TABLE files ADD COLUMN views_count INT NOT NULL DEFAULT 0;
ALTER TABLE files ADD COLUMN download_count INT NOT NULL DEFAULT 0;

UPDATE files
SET view_count = GREATEST(COALESCE(view_count, 0), COALESCE(views_count, 0)),
    views_count = GREATEST(COALESCE(view_count, 0), COALESCE(views_count, 0));

CREATE INDEX idx_files_view_count ON files(view_count);
CREATE INDEX idx_files_views_count ON files(views_count);
CREATE INDEX idx_files_download_count ON files(download_count);
