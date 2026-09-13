package routes

import (
	"github.com/gofiber/fiber/v2"
	"github.com/imanjo/fiber-api/internal/middleware"
	"github.com/imanjo/fiber-api/internal/utils"
)

// registerSystemRoutes handles configuration, security settings,
// robots/sitemap, redis management, legal pages, and localization.
func registerSystemRoutes(api, public, dash fiber.Router, h *Handlers) {
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

	// ImanSEO public presentation and 404 recovery. Specific routes precede the
	// dynamic content route so "authors" can never be parsed as a content type.
	publicSEO := public.Group("/seo")
	publicSEO.Get("/redirect", h.SEO.ResolveRedirect)
	publicSEO.Post("/404", h.SEO.Record404)
	publicSEO.Get("/authors/:id", h.SEO.PublicAuthor)
	publicSEO.Get("/:content_type/:id", h.SEO.Effective)

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

	// Native SEO platform. Content-policy auditing and sitemap generation keep
	// their existing, stricter modules; this group coordinates presentation,
	// analysis, redirects, internal links, authorship and indexing signals.
	// Per-content editor tools authorize against the matching article/post
	// permission inside the handler, while platform-wide controls remain under
	// the dedicated manage-seo permission below.
	dashSEOEditor := dash.Group("/seo")
	dashSEOEditor.Post("/analyze", h.SEO.Analyze)
	// "/optimize" and "/optimize-save" (AI-assisted SEO rewrite) removed along with the rest
	// of the AI/content-audit subsystem — see the removal note above registerContentRoutes.
	dashSEOEditor.Get("/metadata/:content_type/:id", h.SEO.Metadata)
	dashSEOEditor.Put("/metadata/:content_type/:id", h.SEO.SaveMetadata)
	dashSEOEditor.Get("/metadata/:content_type/:id/revisions", h.SEO.Revisions)
	dashSEOEditor.Post("/metadata/:content_type/:id/revisions/:revision_id/restore", h.SEO.RestoreRevision)
	dashSEOEditor.Get("/links/:content_type/:id", h.SEO.LinkSuggestions)

	dashSEO := dash.Group("/seo", middleware.Can("manage seo"))
	dashSEO.Get("/overview", h.SEO.Overview)
	dashSEO.Get("/content", h.SEO.Content)
	dashSEO.Get("/redirects", h.SEO.Redirects)
	dashSEO.Post("/redirects", h.SEO.CreateRedirect)
	dashSEO.Put("/redirects/:id", h.SEO.UpdateRedirect)
	dashSEO.Delete("/redirects/:id", h.SEO.DeleteRedirect)
	dashSEO.Get("/404", h.SEO.NotFoundLogs)
	dashSEO.Post("/404/:id/resolve", h.SEO.Resolve404Log)
	dashSEO.Delete("/404", h.SEO.Clear404)
	dashSEO.Get("/audits", h.SEO.Audits)
	dashSEO.Post("/audits", h.SEO.StartAudit)
	dashSEO.Get("/audits/:id", h.SEO.Audit)
	dashSEO.Get("/audits/:id/issues", h.SEO.AuditIssues)
	dashSEO.Get("/authors", h.SEO.Authors)
	dashSEO.Put("/authors", h.SEO.SaveAuthor)
	dashSEO.Post("/indexnow", h.SEO.IndexNow)

	// The content-audit / content-quality subsystem (AI-assisted analysis, batch fixes,
	// readiness reports, corruption/similarity/inventory scans) has been removed entirely —
	// on the user's explicit decision, after AI-generated drafts repeatedly shipped thin,
	// duplicated content that contributed to a Google AdSense rejection. Google Search
	// Console below is kept as a standalone feature (it reports real Google data, no AI).
	// The article/post save-time uniqueness and minimum-length gates (article_service.go /
	// post_service.go, backed by internal/contentquality) are unaffected — they never
	// depended on this subsystem.

	// Google Search Console integration — separate from the readiness gate
	// above by design: this reports what Google actually shows (index status,
	// clicks/impressions), never what internal readiness thinks it should show.
	// See CONTENT_QUALITY_GOVERNANCE_CENTER_PLAN.md §4.
	dashGSC := dash.Group("/gsc", middleware.CanAny("manage content audit", "manage seo"))
	dashGSC.Get("/properties", h.SearchConsole.ListProperties)
	dashGSC.Post("/properties/:country_code", h.SearchConsole.UpsertProperty)
	dashGSC.Post("/sync", h.SearchConsole.Sync)
	dashGSC.Post("/analytics/sync", h.SearchConsole.SyncAnalytics)
	dashGSC.Get("/analytics", h.SearchConsole.Analytics)
	dashGSC.Get("/keywords", h.SearchConsole.Keywords)
	dashGSC.Get("/status/:content_type/:id", h.SearchConsole.Status)
	dashGSC.Get("/test", h.SearchConsole.TestConnection)

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
