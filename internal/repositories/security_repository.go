package repositories

import (
	"errors"
	"strconv"
	"time"

	"github.com/alemancenter/fiber-api/internal/database"
	"github.com/alemancenter/fiber-api/internal/models"
	"gorm.io/gorm"
)

type TopAttackingIP struct {
	IPAddress string `json:"ip_address"`
	Count     int64  `json:"count"`
}

type AnalyticRow struct {
	Severity  string `json:"severity,omitempty"`
	EventType string `json:"event_type,omitempty"`
	Count     int64  `json:"count"`
}

type SecurityRepository interface {
	GetStats() (totalLogs, criticalLogs, resolvedLogs, blockedIPs, trustedIPs int64, err error)
	GetLogs(severity, eventType, ip, resolved string, limit, offset int) ([]models.SecurityLog, int64, error)
	ResolveLog(id uint64) error
	DeleteLog(id uint64) error
	DeleteAllLogs() error
	GetOverviewStats(last24h, last7d time.Time) (last24hCount, last7dCount, totalAttacks int64, err error)
	GetTopAttackingIPs(limit int) ([]TopAttackingIP, error)
	GetIPLogs(ip string, limit int) ([]models.SecurityLog, int64, error)
	GetBlockedIPs(search, status string, limit, offset int) ([]models.BlockedIP, int64, error)
	GetTrustedIPs(limit, offset int) ([]models.TrustedIP, int64, error)
	GetAnalyticsBySeverity() ([]AnalyticRow, error)
	GetAnalyticsByEventType(limit int) ([]AnalyticRow, error)
	GetTopRoutes(limit int) ([]struct {
		Route string `json:"route"`
		Count int64  `json:"count"`
	}, error)
	GetGeoDistribution(limit int) ([]struct {
		CountryCode string `json:"country_code"`
		Count       int64  `json:"count"`
	}, error)
	CreateSecurityLog(log *models.SecurityLog) error
	BlockIP(ip string, blocked models.BlockedIP) error
	UnblockIP(ip string) error
	TrustIP(ip string, trusted models.TrustedIP) error
	UntrustIP(ip string) error
	GetBlockedIP(ip string) (*models.BlockedIP, error)
	GetTrustedIP(ip string) (*models.TrustedIP, error)
	CreateActivityLog(log *models.ActivityLog) error
	GetLastIPsByUserIDs(ids []uint) map[uint]string
}

func legacyBannedIPsAvailable(db *gorm.DB) bool {
	return db != nil && db.Migrator().HasTable("banned_ips")
}

// syncLegacyBannedIPs copies old banned_ips rows into blocked_ips.
// blocked_ips is the canonical table used by the dashboard and IPGuard.
func syncLegacyBannedIPs(db *gorm.DB) error {
	if !legacyBannedIPsAvailable(db) {
		return nil
	}
	return db.Exec(`
		INSERT INTO blocked_ips (ip_address, reason, blocked_by, expires_at, is_auto_block, created_at, updated_at)
		SELECT ip, reason, banned_by, banned_until, 0, COALESCE(created_at, NOW(3)), COALESCE(updated_at, NOW(3))
		FROM banned_ips
		WHERE ip IS NOT NULL AND ip <> ''
		ON DUPLICATE KEY UPDATE
			reason = VALUES(reason),
			blocked_by = VALUES(blocked_by),
			expires_at = VALUES(expires_at),
			updated_at = VALUES(updated_at)
	`).Error
}

func mirrorBlockedIPToLegacy(db *gorm.DB, ip string, blocked models.BlockedIP) error {
	if !legacyBannedIPsAvailable(db) {
		return nil
	}
	return db.Exec(`
		INSERT INTO banned_ips (ip, reason, banned_by, banned_until, created_at, updated_at)
		VALUES (?, ?, ?, ?, NOW(3), NOW(3))
		ON DUPLICATE KEY UPDATE
			reason = VALUES(reason),
			banned_by = VALUES(banned_by),
			banned_until = VALUES(banned_until),
			updated_at = NOW(3)
	`, ip, blocked.Reason, blocked.BlockedBy, blocked.ExpiresAt).Error
}

func deleteLegacyBannedIP(db *gorm.DB, ipOrID string) error {
	if !legacyBannedIPsAvailable(db) {
		return nil
	}
	if id, err := strconv.ParseUint(ipOrID, 10, 64); err == nil {
		return db.Where("id = ?", id).Delete(&models.LegacyBannedIP{}).Error
	}
	return db.Where("ip = ?", ipOrID).Delete(&models.LegacyBannedIP{}).Error
}

type securityRepository struct{}

func NewSecurityRepository() SecurityRepository {
	return &securityRepository{}
}

func (r *securityRepository) GetStats() (totalLogs, criticalLogs, resolvedLogs, blockedIPs, trustedIPs int64, err error) {
	db := database.DB()
	_ = syncLegacyBannedIPs(db)
	db.Model(&models.SecurityLog{}).Count(&totalLogs)
	db.Model(&models.SecurityLog{}).Where("severity = ?", models.SeverityCritical).Count(&criticalLogs)
	db.Model(&models.SecurityLog{}).Where("is_resolved = ?", true).Count(&resolvedLogs)
	db.Model(&models.BlockedIP{}).Count(&blockedIPs)
	db.Model(&models.TrustedIP{}).Count(&trustedIPs)
	return
}

func (r *securityRepository) GetLogs(severity, eventType, ip, resolved string, limit, offset int) ([]models.SecurityLog, int64, error) {
	db := database.DB()
	var logs []models.SecurityLog
	var total int64

	query := db.Model(&models.SecurityLog{})

	if severity != "" {
		query = query.Where("severity = ?", severity)
	}
	if eventType != "" {
		query = query.Where("event_type = ?", eventType)
	}
	if ip != "" {
		query = query.Where("ip_address = ?", ip)
	}

	switch resolved {
	case "1", "true":
		query = query.Where("is_resolved = ?", true)
	case "0", "false":
		query = query.Where("is_resolved = ?", false)
	}

	err := query.Count(&total).Error
	if err != nil {
		return nil, 0, err
	}

	err = query.Order("created_at DESC").Limit(limit).Offset(offset).Find(&logs).Error
	return logs, total, err
}

func (r *securityRepository) ResolveLog(id uint64) error {
	db := database.DB()
	var log models.SecurityLog
	if err := db.First(&log, id).Error; err != nil {
		return err
	}

	log.IsResolved = true
	now := time.Now()
	log.ResolvedAt = &now

	return db.Save(&log).Error
}

func (r *securityRepository) DeleteLog(id uint64) error {
	return database.DB().Delete(&models.SecurityLog{}, id).Error
}

func (r *securityRepository) DeleteAllLogs() error {
	return database.DB().Where("1 = 1").Delete(&models.SecurityLog{}).Error
}

func (r *securityRepository) GetOverviewStats(last24h, last7d time.Time) (last24hCount, last7dCount, totalAttacks int64, err error) {
	db := database.DB()
	db.Model(&models.SecurityLog{}).Where("created_at >= ?", last24h).Count(&last24hCount)
	db.Model(&models.SecurityLog{}).Where("created_at >= ?", last7d).Count(&last7dCount)
	db.Model(&models.SecurityLog{}).Where("attack_type IS NOT NULL").Count(&totalAttacks)
	return
}

func (r *securityRepository) GetTopAttackingIPs(limit int) ([]TopAttackingIP, error) {
	var topIPs []TopAttackingIP
	err := database.DB().Model(&models.SecurityLog{}).
		Select("ip_address, COUNT(*) as count").
		Group("ip_address").
		Order("count DESC").
		Limit(limit).
		Scan(&topIPs).Error
	return topIPs, err
}

func (r *securityRepository) GetIPLogs(ip string, limit int) ([]models.SecurityLog, int64, error) {
	db := database.DB()
	var logs []models.SecurityLog
	var count int64
	db.Model(&models.SecurityLog{}).Where("ip_address = ?", ip).Count(&count)
	err := db.Where("ip_address = ?", ip).Order("created_at DESC").Limit(limit).Find(&logs).Error
	return logs, count, err
}

func (r *securityRepository) GetBlockedIPs(search, status string, limit, offset int) ([]models.BlockedIP, int64, error) {
	db := database.DB()
	if err := syncLegacyBannedIPs(db); err != nil {
		return nil, 0, err
	}
	var blocked []models.BlockedIP
	var total int64

	query := db.Model(&models.BlockedIP{})

	if search != "" {
		like := "%" + search + "%"
		query = query.Where("ip_address LIKE ? OR reason LIKE ?", like, like)
	}

	switch status {
	case "active":
		query = query.Where("expires_at IS NULL OR expires_at > ?", time.Now())
	case "expired":
		query = query.Where("expires_at IS NOT NULL AND expires_at <= ?", time.Now())
	}

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	err := query.Order("created_at DESC").Limit(limit).Offset(offset).Find(&blocked).Error
	return blocked, total, err
}

func (r *securityRepository) GetTrustedIPs(limit, offset int) ([]models.TrustedIP, int64, error) {
	db := database.DB()
	var trusted []models.TrustedIP
	var total int64
	db.Model(&models.TrustedIP{}).Count(&total)
	err := db.Order("created_at DESC").Limit(limit).Offset(offset).Find(&trusted).Error
	return trusted, total, err
}

func (r *securityRepository) GetAnalyticsBySeverity() ([]AnalyticRow, error) {
	var bySeverity []AnalyticRow
	err := database.DB().Model(&models.SecurityLog{}).
		Select("severity, COUNT(*) as count").
		Group("severity").
		Scan(&bySeverity).Error
	return bySeverity, err
}

func (r *securityRepository) GetAnalyticsByEventType(limit int) ([]AnalyticRow, error) {
	var byEventType []AnalyticRow
	err := database.DB().Model(&models.SecurityLog{}).
		Select("event_type, COUNT(*) as count").
		Group("event_type").
		Order("count DESC").
		Limit(limit).
		Scan(&byEventType).Error
	return byEventType, err
}

func (r *securityRepository) GetTopRoutes(limit int) ([]struct {
	Route string `json:"route"`
	Count int64  `json:"count"`
}, error) {
	var routes []struct {
		Route string `json:"route"`
		Count int64  `json:"count"`
	}
	err := database.DB().Model(&models.SecurityLog{}).
		Select("route, COUNT(*) as count").
		Where("route IS NOT NULL").
		Group("route").
		Order("count DESC").
		Limit(limit).
		Scan(&routes).Error
	return routes, err
}

func (r *securityRepository) GetGeoDistribution(limit int) ([]struct {
	CountryCode string `json:"country_code"`
	Count       int64  `json:"count"`
}, error) {
	var geo []struct {
		CountryCode string `json:"country_code"`
		Count       int64  `json:"count"`
	}
	err := database.DB().Model(&models.SecurityLog{}).
		Select("country_code, COUNT(*) as count").
		Where("country_code IS NOT NULL").
		Group("country_code").
		Order("count DESC").
		Limit(limit).
		Scan(&geo).Error
	return geo, err
}

func (r *securityRepository) CreateSecurityLog(log *models.SecurityLog) error {
	return database.DB().Create(log).Error
}

func (r *securityRepository) BlockIP(ip string, blocked models.BlockedIP) error {
	db := database.DB()
	attrs := map[string]interface{}{
		"ip_address":    ip,
		"reason":        blocked.Reason,
		"blocked_by":    blocked.BlockedBy,
		"expires_at":    blocked.ExpiresAt,
		"is_auto_block": blocked.IsAutoBlock,
	}

	if err := db.Where(models.BlockedIP{IPAddress: ip}).
		Assign(attrs).
		FirstOrCreate(&blocked).Error; err != nil {
		return err
	}

	// Backward compatibility: mirror the canonical block into legacy banned_ips
	// when that old Laravel table still exists.
	return mirrorBlockedIPToLegacy(db, ip, blocked)
}

func (r *securityRepository) UnblockIP(ip string) error {
	db := database.DB()
	legacyKey := ip
	var err error
	if id, parseErr := strconv.ParseUint(ip, 10, 64); parseErr == nil {
		var existing models.BlockedIP
		if findErr := db.Where("id = ?", id).First(&existing).Error; findErr == nil && existing.IPAddress != "" {
			legacyKey = existing.IPAddress
		}
		err = db.Where("id = ?", id).Delete(&models.BlockedIP{}).Error
	} else {
		err = db.Where("ip_address = ?", ip).Delete(&models.BlockedIP{}).Error
	}
	if err != nil {
		return err
	}
	return deleteLegacyBannedIP(db, legacyKey)
}

func (r *securityRepository) TrustIP(ip string, trusted models.TrustedIP) error {
	return database.DB().Where(models.TrustedIP{IPAddress: ip}).
		Assign(trusted).
		FirstOrCreate(&trusted).Error
}

func (r *securityRepository) UntrustIP(ip string) error {
	if id, err := strconv.ParseUint(ip, 10, 64); err == nil {
		return database.DB().Where("id = ?", id).Delete(&models.TrustedIP{}).Error
	}
	return database.DB().Where("ip_address = ?", ip).Delete(&models.TrustedIP{}).Error
}

func (r *securityRepository) GetBlockedIP(ip string) (*models.BlockedIP, error) {
	db := database.DB()
	var blocked models.BlockedIP
	err := db.Where("ip_address = ?", ip).First(&blocked).Error
	if err == nil {
		return &blocked, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) || !legacyBannedIPsAvailable(db) {
		return &blocked, err
	}

	var legacy models.LegacyBannedIP
	if legacyErr := db.Where("ip = ?", ip).First(&legacy).Error; legacyErr != nil {
		return &blocked, err
	}

	mapped := legacy.ToBlockedIP()
	_ = r.BlockIP(mapped.IPAddress, mapped)
	return &mapped, nil
}

func (r *securityRepository) GetTrustedIP(ip string) (*models.TrustedIP, error) {
	var trusted models.TrustedIP
	err := database.DB().Where("ip_address = ?", ip).First(&trusted).Error
	return &trusted, err
}

func (r *securityRepository) CreateActivityLog(log *models.ActivityLog) error {
	return database.DB().Create(log).Error
}

func (r *securityRepository) GetLastIPsByUserIDs(ids []uint) map[uint]string {
	if len(ids) == 0 {
		return nil
	}
	var rows []struct {
		UserID    uint   `gorm:"column:user_id"`
		IPAddress string `gorm:"column:ip_address"`
	}
	database.DB().Raw(`
		SELECT vt.user_id, vt.ip_address
		FROM visitors_tracking vt
		INNER JOIN (
			SELECT user_id, MAX(last_activity) AS max_act
			FROM visitors_tracking
			WHERE user_id IN ?
			GROUP BY user_id
		) latest ON vt.user_id = latest.user_id AND vt.last_activity = latest.max_act
	`, ids).Scan(&rows)

	result := make(map[uint]string, len(rows))
	for _, row := range rows {
		if row.IPAddress != "" {
			result[row.UserID] = row.IPAddress
		}
	}
	return result
}
