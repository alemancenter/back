-- Teacher Subscription Semester MVP schema
-- This SQL is documentation / fallback only.
-- The Go backend runs AutoMigrate and EnsureTeacherSubscriptionDatabase automatically.

CREATE TABLE IF NOT EXISTS subscription_plans (
  id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  code VARCHAR(80) NOT NULL UNIQUE,
  name VARCHAR(255) NOT NULL,
  description TEXT,
  target_audience VARCHAR(80) DEFAULT 'teacher',
  price_jod DECIMAL(10,3) NOT NULL DEFAULT 25.000,
  currency VARCHAR(10) NOT NULL DEFAULT 'JOD',
  duration_days BIGINT NOT NULL DEFAULT 150,
  device_limit BIGINT NOT NULL DEFAULT 2,
  download_limit BIGINT NOT NULL DEFAULT 300,
  ai_generation_limit BIGINT NOT NULL DEFAULT 100,
  export_limit BIGINT NOT NULL DEFAULT 100,
  features_json JSON,
  permissions_json JSON,
  limits_json JSON,
  sort_order BIGINT NOT NULL DEFAULT 10,
  is_active BOOLEAN NOT NULL DEFAULT TRUE,
  created_at DATETIME(3) NULL,
  updated_at DATETIME(3) NULL,
  INDEX idx_subscription_plans_target_audience (target_audience),
  INDEX idx_subscription_plans_sort_order (sort_order),
  INDEX idx_subscription_plans_is_active (is_active)
);

CREATE TABLE IF NOT EXISTS teacher_profiles (
  id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  user_id BIGINT UNSIGNED NOT NULL UNIQUE,
  subject VARCHAR(255),
  school VARCHAR(255),
  phone VARCHAR(50),
  city VARCHAR(120),
  created_at DATETIME(3) NULL,
  updated_at DATETIME(3) NULL
);

CREATE TABLE IF NOT EXISTS teacher_subscriptions (
  id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  user_id BIGINT UNSIGNED NOT NULL,
  plan_id BIGINT UNSIGNED NOT NULL,
  status VARCHAR(30) NOT NULL DEFAULT 'active',
  starts_at DATETIME(3) NOT NULL,
  ends_at DATETIME(3) NOT NULL,
  price_jod DECIMAL(10,3) NOT NULL DEFAULT 25.000,
  device_limit BIGINT NOT NULL DEFAULT 2,
  download_limit BIGINT NOT NULL DEFAULT 300,
  ai_generation_limit BIGINT NOT NULL DEFAULT 100,
  export_limit BIGINT NOT NULL DEFAULT 100,
  activated_by BIGINT UNSIGNED NULL,
  cancelled_at DATETIME(3) NULL,
  admin_note TEXT,
  created_at DATETIME(3) NULL,
  updated_at DATETIME(3) NULL,
  INDEX idx_teacher_subscriptions_user_id (user_id),
  INDEX idx_teacher_subscriptions_plan_id (plan_id),
  INDEX idx_teacher_subscriptions_status (status),
  INDEX idx_teacher_subscriptions_starts_at (starts_at),
  INDEX idx_teacher_subscriptions_ends_at (ends_at),
  INDEX idx_teacher_subscriptions_activated_by (activated_by)
);

CREATE TABLE IF NOT EXISTS subscription_orders (
  id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  user_id BIGINT UNSIGNED NOT NULL,
  plan_id BIGINT UNSIGNED NOT NULL,
  status VARCHAR(30) NOT NULL DEFAULT 'pending',
  amount_jod DECIMAL(10,3) NOT NULL DEFAULT 25.000,
  currency VARCHAR(10) NOT NULL DEFAULT 'JOD',
  payment_method VARCHAR(80) NOT NULL,
  payer_name VARCHAR(255),
  phone VARCHAR(50),
  payment_reference VARCHAR(255),
  payment_proof_url VARCHAR(500),
  admin_note TEXT,
  reviewed_by BIGINT UNSIGNED NULL,
  reviewed_at DATETIME(3) NULL,
  created_at DATETIME(3) NULL,
  updated_at DATETIME(3) NULL,
  INDEX idx_subscription_orders_user_id (user_id),
  INDEX idx_subscription_orders_plan_id (plan_id),
  INDEX idx_subscription_orders_status (status),
  INDEX idx_subscription_orders_reviewed_by (reviewed_by)
);

CREATE TABLE IF NOT EXISTS teacher_devices (
  id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  user_id BIGINT UNSIGNED NOT NULL,
  device_hash VARCHAR(128) NOT NULL,
  label VARCHAR(255),
  ip_hash VARCHAR(128),
  user_agent VARCHAR(500),
  is_active BOOLEAN NOT NULL DEFAULT TRUE,
  last_seen_at DATETIME(3) NULL,
  created_at DATETIME(3) NULL,
  updated_at DATETIME(3) NULL,
  INDEX idx_teacher_devices_user_id (user_id),
  INDEX idx_teacher_devices_device_hash (device_hash),
  INDEX idx_teacher_devices_ip_hash (ip_hash),
  INDEX idx_teacher_devices_is_active (is_active)
);

CREATE TABLE IF NOT EXISTS teacher_premium_downloads (
  id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  user_id BIGINT UNSIGNED NOT NULL,
  subscription_id BIGINT UNSIGNED NOT NULL,
  file_id BIGINT UNSIGNED NULL,
  download_code VARCHAR(120),
  ip_hash VARCHAR(128),
  created_at DATETIME(3) NULL,
  INDEX idx_teacher_premium_downloads_user_id (user_id),
  INDEX idx_teacher_premium_downloads_subscription_id (subscription_id),
  INDEX idx_teacher_premium_downloads_file_id (file_id),
  INDEX idx_teacher_premium_downloads_download_code (download_code),
  INDEX idx_teacher_premium_downloads_ip_hash (ip_hash)
);

CREATE TABLE IF NOT EXISTS teacher_ai_generations (
  id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  user_id BIGINT UNSIGNED NOT NULL,
  subscription_id BIGINT UNSIGNED NOT NULL,
  tool_type VARCHAR(80) NOT NULL,
  title VARCHAR(255),
  model VARCHAR(120),
  created_at DATETIME(3) NULL,
  INDEX idx_teacher_ai_generations_user_id (user_id),
  INDEX idx_teacher_ai_generations_subscription_id (subscription_id),
  INDEX idx_teacher_ai_generations_tool_type (tool_type)
);


CREATE TABLE IF NOT EXISTS teacher_library_items (
  id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  user_id BIGINT UNSIGNED NOT NULL,
  item_type VARCHAR(50) NOT NULL,
  item_id BIGINT UNSIGNED NULL,
  title VARCHAR(500) NOT NULL,
  source_type VARCHAR(80) DEFAULT 'premium_file',
  category VARCHAR(80) DEFAULT '',
  country VARCHAR(10) DEFAULT 'jo',
  created_at DATETIME(3) NULL,
  updated_at DATETIME(3) NULL,
  INDEX idx_teacher_library_items_user_id (user_id),
  INDEX idx_teacher_library_items_item_type (item_type),
  INDEX idx_teacher_library_items_item_id (item_id),
  INDEX idx_teacher_library_items_source_type (source_type),
  INDEX idx_teacher_library_items_category (category),
  INDEX idx_teacher_library_items_country (country)
);

ALTER TABLE files
  ADD COLUMN IF NOT EXISTS is_premium BOOLEAN DEFAULT FALSE,
  ADD COLUMN IF NOT EXISTS premium_audience VARCHAR(50) DEFAULT '',
  ADD COLUMN IF NOT EXISTS premium_category VARCHAR(80) DEFAULT '',
  ADD COLUMN IF NOT EXISTS premium_requires_subscription BOOLEAN DEFAULT FALSE,
  ADD COLUMN IF NOT EXISTS premium_subject VARCHAR(255) DEFAULT '',
  ADD COLUMN IF NOT EXISTS premium_download_count BIGINT DEFAULT 0;
