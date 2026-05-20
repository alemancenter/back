-- Content AI Decision Center schema helper.
-- AutoMigrate creates missing tables/columns on server start. Use these statements if your existing MySQL tables were created before the latest AI integration.

ALTER TABLE content_ai_decisions
  ADD COLUMN prompt_version VARCHAR(80) NOT NULL DEFAULT 'content-intelligence-v1' AFTER model,
  ADD COLUMN ai_tokens INT NOT NULL DEFAULT 0 AFTER prompt_version,
  ADD COLUMN processing_time_ms BIGINT NOT NULL DEFAULT 0 AFTER ai_tokens;

ALTER TABLE content_ai_decisions MODIFY country_code VARCHAR(10) NOT NULL DEFAULT 'jo';
ALTER TABLE content_ai_fix_previews MODIFY country_code VARCHAR(10) NOT NULL DEFAULT 'jo';

ALTER TABLE content_ai_issues AUTO_INCREMENT = 1000;
ALTER TABLE content_ai_suggestions AUTO_INCREMENT = 1000;
