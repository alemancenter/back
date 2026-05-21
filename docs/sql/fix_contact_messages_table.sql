-- Ensures the dashboard contact-message inbox has the table and columns it expects.
-- Run on the primary/Jordan database if AutoMigrate is disabled in production.

CREATE TABLE IF NOT EXISTS contact_messages (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  name VARCHAR(255) NOT NULL,
  email VARCHAR(255) NOT NULL,
  phone VARCHAR(100) NULL,
  subject VARCHAR(500) NOT NULL,
  message TEXT NOT NULL,
  page_url VARCHAR(1000) NULL,
  `read` TINYINT(1) NOT NULL DEFAULT 0,
  created_at DATETIME(3) NULL,
  updated_at DATETIME(3) NULL,
  PRIMARY KEY (id),
  INDEX idx_contact_messages_read (`read`),
  INDEX idx_contact_messages_created_at (created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

ALTER TABLE contact_messages ADD COLUMN IF NOT EXISTS phone VARCHAR(100) NULL;
ALTER TABLE contact_messages ADD COLUMN IF NOT EXISTS page_url VARCHAR(1000) NULL;
ALTER TABLE contact_messages ADD COLUMN IF NOT EXISTS `read` TINYINT(1) NOT NULL DEFAULT 0;
ALTER TABLE contact_messages ADD COLUMN IF NOT EXISTS created_at DATETIME(3) NULL;
ALTER TABLE contact_messages ADD COLUMN IF NOT EXISTS updated_at DATETIME(3) NULL;
