package services

import (
	"time"

	"github.com/alemancenter/fiber-api/internal/models"
	"github.com/alemancenter/fiber-api/internal/repositories"
	"github.com/alemancenter/fiber-api/pkg/logger"
	"go.uber.org/zap"
)

type SecurityService interface {
	LogEvent(ip, eventType, description, severity string, opts ...SecurityLogOption)
	BlockIP(ip, reason string, days int, blockedBy *uint) error
	UnblockIP(ip string) error
	TrustIP(ip, note string, addedBy *uint) error
	UntrustIP(ip string) error
	IsBlocked(ip string) bool
	IsTrusted(ip string) bool
	BlockUserIPs(userIDs []uint, reason string, blockedBy *uint)

	GetStats() (totalLogs, criticalLogs, resolvedLogs, blockedIPs, trustedIPs int64, err error)
	GetLogs(severity, eventType, ip, resolved string, limit, offset int) ([]models.SecurityLog, int64, error)
	ResolveLog(id uint64) error
	DeleteLog(id uint64) error
	DeleteAllLogs() error
	GetOverviewStats(last24h, last7d time.Time) (last24hCount, last7dCount, totalAttacks int64, err error)
	GetTopAttackingIPs(limit int) ([]repositories.TopAttackingIP, error)
	GetIPLogs(ip string, limit int) ([]models.SecurityLog, int64, error)
	GetBlockedIPs(search, status string, limit, offset int) ([]models.BlockedIP, int64, error)
	GetTrustedIPs(limit, offset int) ([]models.TrustedIP, int64, error)
	GetAnalyticsBySeverity() ([]repositories.AnalyticRow, error)
	GetAnalyticsByEventType(limit int) ([]repositories.AnalyticRow, error)
	GetTopRoutes(limit int) ([]struct {
		Route string `json:"route"`
		Count int64  `json:"count"`
	}, error)
	GetGeoDistribution(limit int) ([]struct {
		CountryCode string `json:"country_code"`
		Count       int64  `json:"count"`
	}, error)
}

type SecurityStatsResponse struct {
	TotalLogs    int64 `json:"total_logs"`
	CriticalLogs int64 `json:"critical_logs"`
	ResolvedLogs int64 `json:"resolved_logs"`
	BlockedIPs   int64 `json:"blocked_ips"`
	TrustedIPs   int64 `json:"trusted_ips"`
}

type SecurityOverviewResponse struct {
	Last24hEvents int64                         `json:"last_24h_events"`
	Last7dEvents  int64                         `json:"last_7d_events"`
	TotalAttacks  int64                         `json:"total_attacks"`
	TopIPs        []repositories.TopAttackingIP `json:"top_ips"`
}

type IPDetailsResponse struct {
	IP          string               `json:"ip"`
	IsBlocked   bool                 `json:"is_blocked"`
	IsTrusted   bool                 `json:"is_trusted"`
	TotalEvents int64                `json:"total_events"`
	RecentLogs  []models.SecurityLog `json:"recent_logs"`
}

type SecurityAnalyticsResponse struct {
	BySeverity  []repositories.AnalyticRow `json:"by_severity"`
	ByEventType []repositories.AnalyticRow `json:"by_event_type"`
}

// securityService handles security logging and IP management
type securityService struct {
	repo repositories.SecurityRepository
}

// NewSecurityService creates a new SecurityService
func NewSecurityService(repo repositories.SecurityRepository) SecurityService {
	return &securityService{repo: repo}
}

// LogEvent records a security event
func (s *securityService) LogEvent(ip, eventType, description, severity string, opts ...SecurityLogOption) {
	log := &models.SecurityLog{
		IPAddress:   ip,
		EventType:   eventType,
		Description: description,
		Severity:    severity,
	}

	for _, opt := range opts {
		opt(log)
	}

	go func() {
		if err := s.repo.CreateSecurityLog(log); err != nil {
			logger.Error("failed to create security log", zap.Error(err))
		}
	}()
}

// SecurityLogOption is a functional option for SecurityLog
type SecurityLogOption func(*models.SecurityLog)

// WithUserAgent sets the user agent
func WithUserAgent(ua string) SecurityLogOption {
	return func(l *models.SecurityLog) { l.UserAgent = &ua }
}

// WithRoute sets the route
func WithRoute(route string) SecurityLogOption {
	return func(l *models.SecurityLog) { l.Route = &route }
}

// WithMethod sets the HTTP method
func WithMethod(method string) SecurityLogOption {
	return func(l *models.SecurityLog) { l.Method = &method }
}

// WithRiskScore sets the risk score
func WithRiskScore(score int) SecurityLogOption {
	return func(l *models.SecurityLog) { l.RiskScore = score }
}

// WithUserID sets the user ID
func WithUserID(userID uint) SecurityLogOption {
	return func(l *models.SecurityLog) { l.UserID = &userID }
}

// WithAttackType sets the attack type
func WithAttackType(t string) SecurityLogOption {
	return func(l *models.SecurityLog) { l.AttackType = &t }
}

// BlockIP adds an IP to the blocked list
func (s *securityService) BlockIP(ip, reason string, days int, blockedBy *uint) error {
	blocked := models.BlockedIP{
		IPAddress:   ip,
		IsAutoBlock: blockedBy == nil,
		BlockedBy:   blockedBy,
	}
	if reason != "" {
		blocked.Reason = &reason
	}
	if days > 0 {
		expiresAt := time.Now().AddDate(0, 0, days)
		blocked.ExpiresAt = &expiresAt
	}
	return s.repo.BlockIP(ip, blocked)
}

// BlockUserIPs fetches the last known IP for each user ID and blocks them all.
// The IPGuard Redis cache will expire naturally (5 min TTL) and pick up the new block.
func (s *securityService) BlockUserIPs(userIDs []uint, reason string, blockedBy *uint) {
	ipMap := s.repo.GetLastIPsByUserIDs(userIDs)
	for _, ip := range ipMap {
		_ = s.BlockIP(ip, reason, 0, blockedBy)
	}
}

// UnblockIP removes an IP from the blocked list
func (s *securityService) UnblockIP(ip string) error {
	return s.repo.UnblockIP(ip)
}

// TrustIP adds an IP to the trusted list
func (s *securityService) TrustIP(ip, note string, addedBy *uint) error {
	trusted := models.TrustedIP{
		IPAddress: ip,
		AddedBy:   addedBy,
	}
	if note != "" {
		trusted.Note = &note
	}
	return s.repo.TrustIP(ip, trusted)
}

// UntrustIP removes an IP from the trusted list
func (s *securityService) UntrustIP(ip string) error {
	return s.repo.UntrustIP(ip)
}

// IsBlocked checks if an IP is blocked
func (s *securityService) IsBlocked(ip string) bool {
	blocked, err := s.repo.GetBlockedIP(ip)
	if err != nil {
		return false
	}
	return !blocked.HasExpired()
}

// IsTrusted checks if an IP is trusted
func (s *securityService) IsTrusted(ip string) bool {
	_, err := s.repo.GetTrustedIP(ip)
	return MapError(err) == nil
}

func (s *securityService) GetStats() (totalLogs, criticalLogs, resolvedLogs, blockedIPs, trustedIPs int64, err error) {
	return s.repo.GetStats()
}

func (s *securityService) GetLogs(severity, eventType, ip, resolved string, limit, offset int) ([]models.SecurityLog, int64, error) {
	return s.repo.GetLogs(severity, eventType, ip, resolved, limit, offset)
}

func (s *securityService) ResolveLog(id uint64) error {
	return s.repo.ResolveLog(id)
}

func (s *securityService) DeleteLog(id uint64) error {
	return s.repo.DeleteLog(id)
}

func (s *securityService) DeleteAllLogs() error {
	return s.repo.DeleteAllLogs()
}

func (s *securityService) GetOverviewStats(last24h, last7d time.Time) (last24hCount, last7dCount, totalAttacks int64, err error) {
	return s.repo.GetOverviewStats(last24h, last7d)
}

func (s *securityService) GetTopAttackingIPs(limit int) ([]repositories.TopAttackingIP, error) {
	return s.repo.GetTopAttackingIPs(limit)
}

func (s *securityService) GetIPLogs(ip string, limit int) ([]models.SecurityLog, int64, error) {
	return s.repo.GetIPLogs(ip, limit)
}

func (s *securityService) GetBlockedIPs(search, status string, limit, offset int) ([]models.BlockedIP, int64, error) {
	blocked, total, err := s.repo.GetBlockedIPs(search, status, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	now := time.Now()
	for i := range blocked {
		blocked[i].EnrichStatus(now)
	}
	return blocked, total, nil
}

func (s *securityService) GetTrustedIPs(limit, offset int) ([]models.TrustedIP, int64, error) {
	return s.repo.GetTrustedIPs(limit, offset)
}

func (s *securityService) GetAnalyticsBySeverity() ([]repositories.AnalyticRow, error) {
	return s.repo.GetAnalyticsBySeverity()
}

func (s *securityService) GetAnalyticsByEventType(limit int) ([]repositories.AnalyticRow, error) {
	return s.repo.GetAnalyticsByEventType(limit)
}

func (s *securityService) GetTopRoutes(limit int) ([]struct {
	Route string `json:"route"`
	Count int64  `json:"count"`
}, error) {
	return s.repo.GetTopRoutes(limit)
}

func (s *securityService) GetGeoDistribution(limit int) ([]struct {
	CountryCode string `json:"country_code"`
	Count       int64  `json:"count"`
}, error) {
	return s.repo.GetGeoDistribution(limit)
}

// LogActivity records a user activity
var LogActivity = func(description string, subjectType string, subjectID uint, causerID uint) {
	log := &models.ActivityLog{
		Description: description,
		SubjectType: &subjectType,
		SubjectID:   &subjectID,
		CauserType:  strPtr("App\\Models\\User"),
		CauserID:    &causerID,
	}

	go func() {
		// Use repository for loose coupling
		repo := repositories.NewSecurityRepository()
		if err := repo.CreateActivityLog(log); err != nil {
			logger.Error("failed to create activity log", zap.Error(err))
		}
	}()
}

func strPtr(s string) *string {
	return &s
}
