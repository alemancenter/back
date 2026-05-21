-- Optional compatibility migration for older content_ai_decisions tables.
-- Run only if your database is missing these columns.
-- MySQL 8 / MariaDB supporting ADD COLUMN IF NOT EXISTS:
ALTER TABLE content_ai_decisions ADD COLUMN IF NOT EXISTS adsense_risk VARCHAR(30) NOT NULL DEFAULT 'unknown';
ALTER TABLE content_ai_decisions ADD COLUMN IF NOT EXISTS model VARCHAR(150) NULL;
ALTER TABLE content_ai_decisions ADD COLUMN IF NOT EXISTS prompt_version VARCHAR(80) NOT NULL DEFAULT 'content-intelligence-v1';
ALTER TABLE content_ai_decisions ADD COLUMN IF NOT EXISTS processing_time_ms BIGINT NOT NULL DEFAULT 0;
