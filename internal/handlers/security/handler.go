package security

import (
	"net"
	"strconv"
	"strings"
	"time"

	_ "github.com/alemancenter/fiber-api/internal/models"
	"github.com/alemancenter/fiber-api/internal/services"
	"github.com/alemancenter/fiber-api/internal/utils"
	"github.com/gofiber/fiber/v2"
)

// Handler contains security route handlers
type Handler struct {
	svc services.SecurityService
}

// New creates a new security Handler
func New(svc services.SecurityService) *Handler {
	return &Handler{svc: svc}
}

// Stats returns security statistics
// @Summary Security Statistics
// @Description Get overall security statistics including total logs, critical logs, and blocked IPs
// @Tags Security
// @Produce json
// @Security BearerAuth
// @Security FrontendKeyAuth
// @Success 200 {object} utils.APIResponse{data=services.SecurityStatsResponse}
// @Failure 500 {object} utils.APIResponse
// @Router /dashboard/security/stats [get]
func (h *Handler) Stats(c *fiber.Ctx) error {
	totalLogs, criticalLogs, resolvedLogs, blockedIPs, trustedIPs, err := h.svc.GetStats()
	if err != nil {
		return utils.InternalError(c)
	}

	return utils.Success(c, "success", services.SecurityStatsResponse{
		TotalLogs:    totalLogs,
		CriticalLogs: criticalLogs,
		ResolvedLogs: resolvedLogs,
		BlockedIPs:   blockedIPs,
		TrustedIPs:   trustedIPs,
	})
}

// Logs returns paginated security logs
// @Summary List Security Logs
// @Description Returns paginated security logs with optional filters
// @Tags Security
// @Produce json
// @Security BearerAuth
// @Security FrontendKeyAuth
// @Param severity query string false "Filter by severity (low, medium, high, critical)"
// @Param event_type query string false "Filter by event type"
// @Param ip query string false "Filter by IP address"
// @Param resolved query string false "Filter by resolution status (true/false)"
// @Param page query int false "Page number"
// @Param limit query int false "Items per page"
// @Success 200 {object} utils.APIResponse{data=[]models.SecurityLog}
// @Failure 500 {object} utils.APIResponse
// @Router /dashboard/security/logs [get]
func (h *Handler) Logs(c *fiber.Ctx) error {
	pag := utils.GetPagination(c)

	severity := c.Query("severity")
	eventType := c.Query("event_type")
	ip := strings.TrimSpace(c.Query("ip"))
	if ip == "" {
		ip = strings.TrimSpace(c.Query("q"))
	}
	resolved := c.Query("resolved")
	if resolved == "" {
		resolved = c.Query("is_resolved")
	}

	logs, total, err := h.svc.GetLogs(severity, eventType, ip, resolved, pag.PerPage, pag.Offset)
	if err != nil {
		return utils.InternalError(c)
	}

	return utils.Paginated(c, "success", logs, pag.BuildMeta(total))
}

// ResolveLog marks a security log as resolved
// @Summary Resolve Security Log
// @Description Marks a specific security log entry as resolved
// @Tags Security
// @Produce json
// @Security BearerAuth
// @Security FrontendKeyAuth
// @Param id path int true "Log ID"
// @Success 200 {object} utils.APIResponse
// @Failure 400 {object} utils.APIResponse
// @Failure 404 {object} utils.APIResponse
// @Router /dashboard/security/logs/{id}/resolve [post]
func (h *Handler) ResolveLog(c *fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return utils.BadRequest(c, "معرف غير صحيح")
	}

	if err := h.svc.ResolveLog(id); err != nil {
		return utils.NotFound(c)
	}

	return utils.Success(c, "تم حل السجل بنجاح", nil)
}

// DeleteLog deletes a security log
// @Summary Delete Security Log
// @Description Delete a specific security log entry by ID
// @Tags Security
// @Produce json
// @Security BearerAuth
// @Security FrontendKeyAuth
// @Param id path int true "Log ID"
// @Success 200 {object} utils.APIResponse
// @Failure 400 {object} utils.APIResponse
// @Failure 500 {object} utils.APIResponse
// @Router /dashboard/security/logs/{id} [delete]
func (h *Handler) DeleteLog(c *fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return utils.BadRequest(c, "معرف غير صحيح")
	}

	if err := h.svc.DeleteLog(id); err != nil {
		return utils.InternalError(c)
	}
	return utils.Success(c, "تم حذف السجل بنجاح", nil)
}

// DeleteAllLogs deletes all security logs
// @Summary Clear All Logs
// @Description Delete all security logs
// @Tags Security
// @Produce json
// @Security BearerAuth
// @Security FrontendKeyAuth
// @Success 200 {object} utils.APIResponse
// @Failure 500 {object} utils.APIResponse
// @Router /dashboard/security/logs [delete]
func (h *Handler) DeleteAllLogs(c *fiber.Ctx) error {
	if err := h.svc.DeleteAllLogs(); err != nil {
		return utils.InternalError(c)
	}
	return utils.Success(c, "تم حذف جميع السجلات بنجاح", nil)
}

// Overview returns a security overview
// @Summary Security Overview
// @Description Returns an overview of security metrics (last 24h, 7d, total attacks, and top IPs)
// @Tags Security
// @Produce json
// @Security BearerAuth
// @Security FrontendKeyAuth
// @Success 200 {object} utils.APIResponse{data=services.SecurityOverviewResponse}
// @Failure 500 {object} utils.APIResponse
// @Router /dashboard/security/overview [get]
func (h *Handler) Overview(c *fiber.Ctx) error {
	last24h := time.Now().Add(-24 * time.Hour)
	last7d := time.Now().Add(-7 * 24 * time.Hour)

	last24hCount, last7dCount, totalAttacks, err := h.svc.GetOverviewStats(last24h, last7d)
	if err != nil {
		return utils.InternalError(c)
	}

	topIPs, err := h.svc.GetTopAttackingIPs(10)
	if err != nil {
		return utils.InternalError(c)
	}

	return utils.Success(c, "success", services.SecurityOverviewResponse{
		Last24hEvents: last24hCount,
		Last7dEvents:  last7dCount,
		TotalAttacks:  totalAttacks,
		TopIPs:        topIPs,
	})
}

// MonitorDashboard returns the frontend monitor payload.
// @Summary Security Monitor Dashboard
// @Description Returns a comprehensive payload (stats, recent logs) for the frontend monitor view
// @Tags Security
// @Produce json
// @Security BearerAuth
// @Security FrontendKeyAuth
// @Success 200 {object} utils.APIResponse{data=map[string]interface{}}
// @Failure 500 {object} utils.APIResponse
// @Router /dashboard/security/monitor/dashboard [get]
func (h *Handler) MonitorDashboard(c *fiber.Ctx) error {
	totalLogs, criticalLogs, resolvedLogs, blockedIPs, trustedIPs, err := h.svc.GetStats()
	if err != nil {
		return utils.InternalError(c)
	}

	recentLogs, _, err := h.svc.GetLogs("", "", "", "", 10, 0)
	if err != nil {
		return utils.InternalError(c)
	}

	unresolved := totalLogs - resolvedLogs
	if unresolved < 0 {
		unresolved = 0
	}

	return utils.Success(c, "success", fiber.Map{
		"stats": fiber.Map{
			"total_events":      totalLogs,
			"unresolved_events": unresolved,
			"high_risk_events":  criticalLogs,
			"blocked_ips":       blockedIPs,
			"trusted_ips":       trustedIPs,
			"alerts_count":      unresolved,
			"blocked_ips_count": blockedIPs,
			"total_requests":    totalLogs,
			"attack_attempts":   criticalLogs,
			"blocked_attacks":   blockedIPs,
			"resolved_events":   resolvedLogs,
			"critical_events":   criticalLogs,
		},
		"recent_events": recentLogs,
		"recent_logs":   recentLogs,
	})
}

// IPDetails returns details about a specific IP
// @Summary Get IP Details
// @Description Returns details, status, and recent logs for a specific IP address
// @Tags Security
// @Produce json
// @Security BearerAuth
// @Security FrontendKeyAuth
// @Param ip path string true "IP Address"
// @Success 200 {object} utils.APIResponse{data=services.IPDetailsResponse}
// @Failure 500 {object} utils.APIResponse
// @Router /dashboard/security/ip/{ip} [get]
func (h *Handler) IPDetails(c *fiber.Ctx) error {
	ip := c.Params("ip")

	logs, count, err := h.svc.GetIPLogs(ip, 20)
	if err != nil {
		return utils.InternalError(c)
	}

	isBlocked := h.svc.IsBlocked(ip)
	isTrusted := h.svc.IsTrusted(ip)

	return utils.Success(c, "success", services.IPDetailsResponse{
		IP:          ip,
		IsBlocked:   isBlocked,
		IsTrusted:   isTrusted,
		TotalEvents: count,
		RecentLogs:  logs,
	})
}

// BlockIP blocks an IP address
// @Summary Block IP
// @Description Block a specific IP address
// @Tags Security
// @Accept json
// @Produce json
// @Security BearerAuth
// @Security FrontendKeyAuth
// @Param ip path string true "IP Address"
// @Param request body ipPayload false "Optional payload (reason, note)"
// @Success 200 {object} utils.APIResponse
// @Failure 400 {object} utils.APIResponse
// @Failure 500 {object} utils.APIResponse
// @Router /dashboard/security/ip/{ip}/block [post]
func (h *Handler) BlockIP(c *fiber.Ctx) error {
	req, err := parseIPPayload(c)
	if err != nil {
		return utils.BadRequest(c, "invalid payload")
	}

	ip := firstNonEmpty(c.Params("ip"), req.IPAddress, req.IP)
	if ip == "" {
		return utils.BadRequest(c, "ip address is required")
	}
	if parsedIP := net.ParseIP(ip); parsedIP == nil {
		return utils.BadRequest(c, "invalid ip address")
	}

	userID := currentUserID(c)
	var blockedBy *uint
	if userID != 0 {
		blockedBy = &userID
	}

	if err := h.svc.BlockIP(ip, req.Reason, req.Days, blockedBy); err != nil {
		return utils.InternalError(c)
	}

	return utils.Success(c, "تم حظر عنوان IP بنجاح", nil)
}

// UnblockIP unblocks an IP address
// @Summary Unblock IP
// @Description Unblock a previously blocked IP address
// @Tags Security
// @Produce json
// @Security BearerAuth
// @Security FrontendKeyAuth
// @Param ip path string true "IP Address"
// @Success 200 {object} utils.APIResponse
// @Failure 400 {object} utils.APIResponse
// @Failure 500 {object} utils.APIResponse
// @Router /dashboard/security/ip/{ip}/unblock [post]
func (h *Handler) UnblockIP(c *fiber.Ctx) error {
	req, _ := parseIPPayload(c)
	ip := firstNonEmpty(c.Params("ip"), req.IPAddress, req.IP)
	if ip == "" {
		return utils.BadRequest(c, "ip address is required")
	}

	if err := h.svc.UnblockIP(ip); err != nil {
		return utils.InternalError(c)
	}

	return utils.Success(c, "تم إلغاء حظر عنوان IP بنجاح", nil)
}

// TrustIP marks an IP as trusted
// @Summary Trust IP
// @Description Mark a specific IP address as trusted (bypassing certain limits)
// @Tags Security
// @Accept json
// @Produce json
// @Security BearerAuth
// @Security FrontendKeyAuth
// @Param ip path string true "IP Address"
// @Param request body ipPayload false "Optional payload (note)"
// @Success 200 {object} utils.APIResponse
// @Failure 400 {object} utils.APIResponse
// @Failure 500 {object} utils.APIResponse
// @Router /dashboard/security/ip/{ip}/trust [post]
func (h *Handler) TrustIP(c *fiber.Ctx) error {
	req, err := parseIPPayload(c)
	if err != nil {
		return utils.BadRequest(c, "invalid payload")
	}

	ip := firstNonEmpty(c.Params("ip"), req.IPAddress, req.IP)
	if ip == "" {
		return utils.BadRequest(c, "ip address is required")
	}

	userID := currentUserID(c)
	var addedBy *uint
	if userID != 0 {
		addedBy = &userID
	}

	note := firstNonEmpty(req.Note, req.Reason)
	if err := h.svc.TrustIP(ip, note, addedBy); err != nil {
		return utils.InternalError(c)
	}

	return utils.Success(c, "تم إضافة عنوان IP للموثوقين بنجاح", nil)
}

// UntrustIP removes an IP from trusted list
// @Summary Untrust IP
// @Description Remove a specific IP address from the trusted list
// @Tags Security
// @Produce json
// @Security BearerAuth
// @Security FrontendKeyAuth
// @Param ip path string true "IP Address"
// @Success 200 {object} utils.APIResponse
// @Failure 400 {object} utils.APIResponse
// @Failure 500 {object} utils.APIResponse
// @Router /dashboard/security/ip/{ip}/untrust [post]
func (h *Handler) UntrustIP(c *fiber.Ctx) error {
	req, _ := parseIPPayload(c)
	ip := firstNonEmpty(c.Params("ip"), req.IPAddress, req.IP)
	if ip == "" {
		return utils.BadRequest(c, "ip address is required")
	}

	if err := h.svc.UntrustIP(ip); err != nil {
		return utils.InternalError(c)
	}

	return utils.Success(c, "تم إزالة عنوان IP من الموثوقين بنجاح", nil)
}

// BlockedIPs lists all blocked IPs
// GET /api/dashboard/security/blocked-ips
func (h *Handler) BlockedIPs(c *fiber.Ctx) error {
	pag := utils.GetPagination(c)

	search := strings.TrimSpace(firstNonEmpty(c.Query("search"), c.Query("q"), c.Query("ip")))
	status := strings.TrimSpace(c.Query("status"))
	if status != "active" && status != "expired" {
		status = ""
	}

	blocked, total, err := h.svc.GetBlockedIPs(search, status, pag.PerPage, pag.Offset)
	if err != nil {
		return utils.InternalError(c)
	}

	return utils.Paginated(c, "success", blocked, pag.BuildMeta(total))
}

// TrustedIPs lists all trusted IPs
// GET /api/dashboard/security/trusted-ips
func (h *Handler) TrustedIPs(c *fiber.Ctx) error {
	pag := utils.GetPagination(c)

	trusted, total, err := h.svc.GetTrustedIPs(pag.PerPage, pag.Offset)
	if err != nil {
		return utils.InternalError(c)
	}

	return utils.Paginated(c, "success", trusted, pag.BuildMeta(total))
}

// Analytics returns security analytics
// GET /api/dashboard/security/analytics
func (h *Handler) Analytics(c *fiber.Ctx) error {
	bySeverity, err := h.svc.GetAnalyticsBySeverity()
	if err != nil {
		return utils.InternalError(c)
	}

	byEventType, err := h.svc.GetAnalyticsByEventType(10)
	if err != nil {
		return utils.InternalError(c)
	}

	return utils.Success(c, "success", services.SecurityAnalyticsResponse{
		BySeverity:  bySeverity,
		ByEventType: byEventType,
	})
}

// TopRoutes returns the most targeted routes
// GET /api/dashboard/security/analytics/routes
func (h *Handler) TopRoutes(c *fiber.Ctx) error {
	routes, err := h.svc.GetTopRoutes(20)
	if err != nil {
		return utils.InternalError(c)
	}

	return utils.Success(c, "success", routes)
}

// GeoDistribution returns geographic distribution of events
// GET /api/dashboard/security/analytics/geo
func (h *Handler) GeoDistribution(c *fiber.Ctx) error {
	geo, err := h.svc.GetGeoDistribution(20)
	if err != nil {
		return utils.InternalError(c)
	}

	return utils.Success(c, "success", geo)
}

type ipPayload struct {
	IP        string `json:"ip"`
	IPAddress string `json:"ip_address"`
	Reason    string `json:"reason"`
	Note      string `json:"note"`
	Days      int    `json:"days"`
}

func parseIPPayload(c *fiber.Ctx) (ipPayload, error) {
	var req ipPayload
	if len(c.Body()) == 0 {
		return req, nil
	}
	err := c.BodyParser(&req)
	req.IP = strings.TrimSpace(req.IP)
	req.IPAddress = strings.TrimSpace(req.IPAddress)
	req.Reason = strings.TrimSpace(req.Reason)
	req.Note = strings.TrimSpace(req.Note)
	return req, err
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func currentUserID(c *fiber.Ctx) uint {
	switch v := c.Locals("user_id").(type) {
	case uint:
		return v
	case int:
		if v > 0 {
			return uint(v)
		}
	case float64:
		if v > 0 {
			return uint(v)
		}
	}
	return 0
}
