-- Alemancenter Chatbot Core tables
-- AutoMigrate creates these tables on app start. Use this file only for manual review/deployment if needed.

CREATE TABLE IF NOT EXISTS chat_sessions (
  id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  user_id BIGINT UNSIGNED NULL,
  guest_id VARCHAR(128) NOT NULL DEFAULT '',
  country_code VARCHAR(10) DEFAULT 'jo',
  status VARCHAR(30) DEFAULT 'open',
  last_intent VARCHAR(80) DEFAULT '',
  created_at DATETIME(3) NULL,
  updated_at DATETIME(3) NULL,
  closed_at DATETIME(3) NULL,
  INDEX idx_chat_sessions_user_id (user_id),
  INDEX idx_chat_sessions_guest_id (guest_id),
  INDEX idx_chat_sessions_country_code (country_code),
  INDEX idx_chat_sessions_status (status)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS chat_messages (
  id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  session_id BIGINT UNSIGNED NOT NULL,
  role VARCHAR(30) NOT NULL,
  message TEXT NOT NULL,
  intent VARCHAR(80) DEFAULT '',
  confidence DECIMAL(5,2) DEFAULT 0,
  source_type VARCHAR(60) DEFAULT '',
  metadata JSON NULL,
  ip_address VARCHAR(80) DEFAULT '',
  user_agent VARCHAR(500) DEFAULT '',
  created_at DATETIME(3) NULL,
  INDEX idx_chat_messages_session_id (session_id),
  INDEX idx_chat_messages_role (role),
  INDEX idx_chat_messages_intent (intent),
  INDEX idx_chat_messages_ip_address (ip_address)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS chat_knowledge_base (
  id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  title VARCHAR(255) NOT NULL,
  question VARCHAR(700) NOT NULL,
  answer TEXT NOT NULL,
  category VARCHAR(80) DEFAULT '',
  keywords VARCHAR(1000) DEFAULT '',
  country_code VARCHAR(10) DEFAULT 'jo',
  is_active TINYINT(1) DEFAULT 1,
  priority INT DEFAULT 10,
  created_by BIGINT UNSIGNED NULL,
  updated_by BIGINT UNSIGNED NULL,
  created_at DATETIME(3) NULL,
  updated_at DATETIME(3) NULL,
  INDEX idx_chat_knowledge_base_category (category),
  INDEX idx_chat_knowledge_base_keywords (keywords),
  INDEX idx_chat_knowledge_base_country_code (country_code),
  INDEX idx_chat_knowledge_base_is_active (is_active),
  INDEX idx_chat_knowledge_base_priority (priority)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS chat_feedback (
  id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  message_id BIGINT UNSIGNED NOT NULL,
  rating VARCHAR(30) NOT NULL,
  comment VARCHAR(1000) DEFAULT '',
  created_at DATETIME(3) NULL,
  INDEX idx_chat_feedback_message_id (message_id),
  INDEX idx_chat_feedback_rating (rating)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
