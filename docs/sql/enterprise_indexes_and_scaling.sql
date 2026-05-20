-- Enterprise prelaunch indexes and schema hardening for 500k+ daily visits.
-- Run manually if AutoMigrate boot logs show skipped duplicate-index warnings.

ALTER TABLE content_ai_decisions MODIFY COLUMN country_code VARCHAR(20) NOT NULL DEFAULT 'jo';
ALTER TABLE content_ai_fix_previews MODIFY COLUMN country_code VARCHAR(20) NOT NULL DEFAULT 'jo';
ALTER TABLE content_ai_decisions MODIFY COLUMN report_json LONGTEXT;
ALTER TABLE content_ai_fix_previews MODIFY COLUMN original_content LONGTEXT;
ALTER TABLE content_ai_fix_previews MODIFY COLUMN fixed_content LONGTEXT;

UPDATE content_ai_decisions
SET country_code = 'jo', content_id = CONCAT('jo:', SUBSTRING_INDEX(content_id, ':', -1))
WHERE country_code IN ('alhurani_jo', 'jordan', 'Jordan') OR country_code LIKE '%_jo';

UPDATE content_ai_fix_previews
SET country_code = 'jo', content_id = CONCAT('jo:', SUBSTRING_INDEX(content_id, ':', -1))
WHERE country_code IN ('alhurani_jo', 'jordan', 'Jordan') OR country_code LIKE '%_jo';

CREATE INDEX idx_ai_decisions_lookup ON content_ai_decisions(content_type, content_id, country_code);
CREATE INDEX idx_ai_decisions_created ON content_ai_decisions(created_at);
CREATE INDEX idx_ai_fix_decision_status ON content_ai_fix_previews(decision_id, status, created_at);
CREATE INDEX idx_policy_findings_run_risk ON policy_audit_findings(run_id, risk);
CREATE INDEX idx_policy_findings_run_type ON policy_audit_findings(run_id, content_type);
CREATE INDEX idx_visitors_tracking_created ON visitors_tracking(created_at);
CREATE INDEX idx_visitors_tracking_url_created ON visitors_tracking(url(191), created_at);
