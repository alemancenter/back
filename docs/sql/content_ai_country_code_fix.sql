ALTER TABLE content_ai_decisions
  MODIFY COLUMN country_code VARCHAR(20) NOT NULL DEFAULT 'jo';

ALTER TABLE content_ai_fix_previews
  MODIFY COLUMN country_code VARCHAR(20) NOT NULL DEFAULT 'jo';

UPDATE content_ai_decisions
SET country_code = 'jo',
    content_id = CONCAT('jo:', SUBSTRING_INDEX(content_id, ':', -1))
WHERE country_code IN ('alhurani_jo', 'jordan', 'Jordan')
   OR country_code LIKE '%_jo';

UPDATE content_ai_fix_previews
SET country_code = 'jo',
    content_id = CONCAT('jo:', SUBSTRING_INDEX(content_id, ':', -1))
WHERE country_code IN ('alhurani_jo', 'jordan', 'Jordan')
   OR country_code LIKE '%_jo';
