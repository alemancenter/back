package routes

import (
	"github.com/alemancenter/fiber-api/internal/middleware"
	"github.com/alemancenter/fiber-api/internal/utils"
	"github.com/gofiber/fiber/v2"
)

// registerSystemRoutes handles configuration, security settings,
// robots/sitemap, redis management, legal pages, and localization.
func registerSystemRoutes(api, _, dash fiber.Router, h *Handlers) {
	// =====================
	// PUBLIC ROUTES
	// =====================

	// Front Page Settings
	front := api.Group("/front", middleware.OptionalAuth())
	front.Get("/settings", h.Settings.GetPublic)
	front.Post("/contact", h.Settings.Contact)

	// Legal Pages
	legal := api.Group("/legal")
	legal.Get("/privacy-policy", func(c *fiber.Ctx) error {
		return utils.Success(c, "success", "privacy-policy")
	})
	legal.Get("/terms-of-service", func(c *fiber.Ctx) error {
		return utils.Success(c, "success", "terms-of-service")
	})
	legal.Get("/cookie-policy", func(c *fiber.Ctx) error {
		return utils.Success(c, "success", "cookie-policy")
	})
	legal.Get("/disclaimer", func(c *fiber.Ctx) error {
		return utils.Success(c, "success", "disclaimer")
	})

	// Language settings
	langGroup := api.Group("/lang")
	langGroup.Post("/change", func(c *fiber.Ctx) error {
		type LangRequest struct {
			Locale string `json:"locale"`
		}
		var req LangRequest
		c.BodyParser(&req)
		return utils.Success(c, "success", req.Locale)
	})
	langGroup.Get("/current", func(c *fiber.Ctx) error {
		lang := c.Get("Accept-Language", "ar")
		if len(lang) >= 2 {
			lang = lang[:2]
		}
		return utils.Success(c, "success", lang)
	})

	// =====================
	// ADMIN DASHBOARD ROUTES
	// =====================

	// Settings
	dashSettings := dash.Group("/settings", middleware.Can("manage settings"))
	dashSettings.Post("/smtp/test", h.Settings.TestSMTP)
	dashSettings.Post("/smtp/send-test", h.Settings.SendTestEmail)
	dashSettings.Post("/robots", h.Settings.UpdateRobots)
	dashSettings.Get("/email-verification/users", h.EmailVerify.List)
	dashSettings.Get("/email-verification/stats", h.EmailVerify.Stats)
	dashSettings.Post("/email-verification/send-reminders", h.EmailVerify.SendReminders)
	dashSettings.Post("/email-verification/mark-invalid", h.EmailVerify.MarkInvalid)
	dashSettings.Post("/email-verification/clear-status", h.EmailVerify.ClearStatus)
	dashSettings.Post("/email-verification/delete-filtered", h.EmailVerify.DeleteFiltered)
	dashSettings.Post("/email-verification/delete-users", h.EmailVerify.DeleteUsers)
	dashSettings.Get("/email-bounce/events", h.EmailBounce.ListEvents)
	dashSettings.Get("/email-bounce/stats", h.EmailBounce.Stats)
	dashSettings.Post("/email-bounce/mark", h.EmailBounce.MarkStatus)
	dashSettings.Post("/email-bounce/reset", h.EmailBounce.ResetStatus)
	dashSettings.Post("/email-bounce/process-now", h.EmailBounce.ProcessNow)
	dashSettings.Get("", h.Settings.GetAll)
	dashSettings.Post("", h.Settings.Update)
	dashSettings.Post("/update", h.Settings.Update)

	// Sitemap
	dashSitemap := dash.Group("/sitemap", middleware.Can("manage sitemap"))
	dashSitemap.Get("/status", h.Sitemap.Status)
	dashSitemap.Post("/generate", h.Sitemap.GenerateAll)
	dashSitemap.Delete("/delete/:type/:database", h.Sitemap.Delete)

	// Content policy audit
	dashContentAudit := dash.Group("/content-audit", middleware.Can("manage content audit"))
	dashContentAudit.Post("/run", h.ContentAudit.Start)
	dashContentAudit.Get("/runs", h.ContentAudit.ListRuns)
	dashContentAudit.Get("/runs/:id", h.ContentAudit.ShowRun)
	dashContentAudit.Get("/runs/:id/findings", h.ContentAudit.ListFindings)
	dashContentAudit.Get("/runs/:id/export", h.ContentAudit.ExportCSV)
	dashContentAudit.Get("/adsense-readiness", h.ContentAudit.AdsenseReadiness)
	dashContentAudit.Post("/ai/analyze", h.ContentAudit.AnalyzeWithAI)
	dashContentAudit.Get("/ai/decisions/:id", h.ContentAudit.ShowAIDecision)
	dashContentAudit.Get("/ai/decision/:type/:content_id", h.ContentAudit.LatestAIDecision)
	dashContentAudit.Post("/ai/fix-preview", h.ContentAudit.CreateFixPreview)
	dashContentAudit.Get("/ai/fix-preview/:id", h.ContentAudit.ShowFixPreview)
	dashContentAudit.Post("/ai/apply-fix", h.ContentAudit.ApplyFix)
	dashContentAudit.Post("/ai/reject-fix", h.ContentAudit.RejectFix)

	// Security
	dashSecurity := dash.Group("/security", middleware.Can("manage security"))
	dashSecurity.Get("/stats", h.Security.Stats)
	dashSecurity.Get("/logs", h.Security.Logs)
	dashSecurity.Delete("/logs", h.Security.DeleteAllLogs)
	dashSecurity.Get("/analytics/routes", h.Security.TopRoutes)
	dashSecurity.Get("/analytics/geo", h.Security.GeoDistribution)
	dashSecurity.Get("/analytics", h.Security.Analytics)
	dashSecurity.Get("/overview", h.Security.Overview)
	dashSecurity.Get("/monitor/dashboard", h.Security.MonitorDashboard)
	dashSecurity.Post("/logs/:id/resolve", h.Security.ResolveLog)
	dashSecurity.Delete("/logs/:id", h.Security.DeleteLog)
	dashSecurity.Get("/blocked-ips", h.Security.BlockedIPs)
	dashSecurity.Delete("/blocked-ips/:ip", h.Security.UnblockIP)
	dashSecurity.Get("/trusted-ips", h.Security.TrustedIPs)
	dashSecurity.Delete("/trusted-ips/:ip", h.Security.UntrustIP)

	// IPs Management (Inside Security)
	dashIPs := dashSecurity.Group("/ip")
	dashIPs.Get("/:ip", h.Security.IPDetails)
	dashIPs.Post("/block", h.Security.BlockIP)
	dashIPs.Post("/unblock", h.Security.UnblockIP)
	dashIPs.Post("/trust", h.Security.TrustIP)
	dashIPs.Post("/untrust", h.Security.UntrustIP)
	dashIPs.Post("/:ip/block", h.Security.BlockIP)
	dashIPs.Post("/:ip/unblock", h.Security.UnblockIP)
	dashIPs.Post("/:ip/trust", h.Security.TrustIP)
	dashIPs.Post("/:ip/untrust", h.Security.UntrustIP)

	// Blocked IPs shortcut
	dashBlockedIPs := dash.Group("/blocked-ips", middleware.Can("manage security"))
	dashBlockedIPs.Delete("/:ip", h.Security.UnblockIP)

	// Trusted IPs shortcut
	dashTrustedIPs := dash.Group("/trusted-ips", middleware.Can("manage security"))
	dashTrustedIPs.Delete("/:ip", h.Security.UntrustIP)

	// Redis management (admin only)
	dashRedis := dash.Group("/redis", middleware.AdminOnly())
	dashRedis.Get("/keys", h.Redis.ListKeys)
	dashRedis.Post("", h.Redis.SetKey)
	dashRedis.Delete("/expired/clean", h.Redis.CleanExpired)
	dashRedis.Post("/legacy-ip-location/expire", h.Redis.ExpireLegacyIPLocation)
	dashRedis.Delete("/legacy-ip-location/clean", h.Redis.CleanLegacyIPLocation)
	dashRedis.Get("/test", h.Redis.TestConnection)
	dashRedis.Get("/info", h.Redis.GetInfo)
	dashRedis.Post("/env", h.Redis.UpdateEnv)
	dashRedis.Post("/:key/expire", h.Redis.ExpireKey)
	dashRedis.Delete("/:key", h.Redis.DeleteKey)
}
