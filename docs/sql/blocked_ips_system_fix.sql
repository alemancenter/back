-- Blocked IPs dashboard support indexes and expiry field
ALTER TABLE blocked_ips MODIFY ip_address VARCHAR(45) NOT NULL;
ALTER TABLE blocked_ips MODIFY reason TEXT NULL;
ALTER TABLE blocked_ips ADD COLUMN IF NOT EXISTS expires_at DATETIME NULL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_blocked_ips_ip_address ON blocked_ips(ip_address);
CREATE INDEX IF NOT EXISTS idx_blocked_ips_expires_at ON blocked_ips(expires_at);
CREATE INDEX IF NOT EXISTS idx_blocked_ips_created_at ON blocked_ips(created_at);
