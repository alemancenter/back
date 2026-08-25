package routes

import (
	"github.com/gofiber/fiber/v2"
	"github.com/imanjo/fiber-api/internal/middleware"
)

// registerContentRoutes handles all routes related to core content:
// Articles, Posts, Categories, Comments, Files, Keywords, and AI.
func registerContentRoutes(api, public, dash fiber.Router, h *Handlers) {
	authM := middleware.Auth()
	activityM := middleware.UpdateLastActivity()
	downloadGateM := middleware.DownloadAuthGate(h.SettingsSvc)

	// =====================
	// PUBLIC ROUTES
	// =====================

	// Articles
	// Important: Static/specific routes must come before dynamic /:id routes
	public.Get("/articles", h.Articles.List)
	public.Get("/articles/download", h.Articles.DownloadFileSigned)
	public.Get("/articles/by-class/:grade_level", h.Articles.ByClass)
	public.Get("/articles/by-keyword/:keyword", h.Articles.ByKeyword)
	public.Get("/articles/file/:id/download", downloadGateM, activityM, h.Articles.DownloadFile)
	public.Get("/articles/file/:id/download-url", downloadGateM, activityM, h.Articles.GetDownloadToken)
	public.Get("/articles/:id", h.Articles.Show)
	public.Get("/articles/:id/ad-status", h.ContentAudit.PublicArticleQualityStatus)

	// Posts
	public.Get("/posts", h.Posts.List)
	public.Get("/posts/download", h.Posts.DownloadFileSigned)
	public.Get("/posts/file/:id/download-url", downloadGateM, activityM, h.Posts.GetDownloadToken)
	public.Get("/posts/:id/ad-status", h.ContentAudit.PublicPostQualityStatus)
	public.Get("/posts/:id", h.Posts.Show)
	public.Post("/posts/:id/increment-view", h.Posts.IncrementView)

	// Categories
	public.Get("/categories", h.Categories.List)
	public.Get("/categories/:id", h.Categories.Show)

	// Comments
	public.Get("/comments/:database", h.Comments.List)

	// Files
	public.Get("/files/:id/info", h.Files.Info)
	public.Post("/files/:id/increment-view", h.Files.IncrementView)

	// Keywords
	public.Get("/keywords", h.Keywords.Index)
	public.Get("/keywords/:keyword", h.Keywords.Show)

	// =====================
	// USER AUTHENTICATED ROUTES
	// =====================
	// NOTE: Do NOT use api.Group("", Auth, ...) — in Fiber v2, an empty-prefix Group
	// adds its middleware as global USE middleware for ALL /api/* routes, breaking public endpoints.
	// Use per-route inline middleware instead.
	// Reactions (Comments)
	api.Post("/comments/:database", authM, activityM, h.Comments.Create)
	api.Post("/reactions", authM, activityM, h.Comments.CreateReaction)
	api.Delete("/reactions/:comment_id", authM, activityM, h.Comments.DeleteReaction)
	api.Get("/reactions/:comment_id", authM, activityM, h.Comments.GetReactions)

	// File upload
	api.Post("/upload/image", authM, activityM, h.Files.UploadImage)
	api.Post("/upload/file", authM, activityM, h.Files.UploadDocument)

	// Secure file view
	api.Get("/secure/view", authM, activityM, h.Files.SecureView)

	// AI generation is an editorial capability. The production frontend uses
	// /dashboard/ai/*; keep the legacy authenticated endpoints permission-gated
	// so a normal account cannot consume AI generation resources.
	api.Post("/ai/generate", authM, activityM, middleware.Can("manage articles"), h.AI.Generate)
	api.Get("/ai/status/:id", authM, activityM, middleware.Can("manage articles"), h.AI.Status)

	// =====================
	// ADMIN DASHBOARD ROUTES
	// =====================

	// Articles management
	dashArticles := dash.Group("/articles", middleware.Can("manage articles"))
	dashArticles.Get("/stats", h.Articles.DashboardStats)
	dashArticles.Get("/create", h.Articles.DashboardCreateData)
	dashArticles.Get("", h.Articles.DashboardList)
	dashArticles.Post("", h.Articles.DashboardCreate)
	dashArticles.Get("/:id/edit", h.Articles.DashboardEditData)
	dashArticles.Post("/:id/publish", h.Articles.DashboardPublish)
	dashArticles.Post("/:id/unpublish", h.Articles.DashboardUnpublish)
	dashArticles.Get("/:id", h.Articles.Show)
	dashArticles.Put("/:id", h.Articles.DashboardUpdate)
	dashArticles.Delete("/:id", h.Articles.DashboardDelete)

	// Posts management
	dashPosts := dash.Group("/posts", middleware.Can("manage posts"))
	dashPosts.Get("", h.Posts.DashboardList)
	dashPosts.Get("/:id", h.Posts.DashboardShow)
	dashPosts.Post("", h.Posts.DashboardCreate)
	dashPosts.Post("/:id/toggle-status", h.Posts.DashboardToggleStatus)
	dashPosts.Put("/:id", h.Posts.DashboardUpdate)
	// Multipart form uploads (new image / attachments) are parsed reliably on
	// POST but not on PUT (fasthttp), so file-bearing edits use POST while a
	// plain metadata edit keeps the JSON PUT above.
	dashPosts.Post("/:id", h.Posts.DashboardUpdate)
	dashPosts.Delete("/:id", h.Posts.DashboardDelete)

	// Categories management
	dashCategories := dash.Group("/categories", middleware.Can("manage categories"))
	dashCategories.Get("", h.Categories.DashboardList)
	dashCategories.Post("", h.Categories.DashboardCreate)
	dashCategories.Get("/:id", h.Categories.DashboardShow)
	dashCategories.Put("/:id", h.Categories.DashboardUpdate)
	dashCategories.Post("/:id/images", h.Categories.DashboardUploadImages)
	dashCategories.Post("/:id/toggle", h.Categories.DashboardToggleStatus)
	dashCategories.Delete("/:id", h.Categories.DashboardDelete)

	// Comments management
	dashComments := dash.Group("/comments", middleware.Can("manage comments"))
	dashComments.Get("/:database", h.Comments.DashboardList)
	dashComments.Post("/:database", h.Comments.DashboardCreate)
	dashComments.Post("/:database/bulk-delete", h.Comments.DashboardBulkDelete)
	dashComments.Post("/:database/:id/approve", h.Comments.DashboardApprove)
	dashComments.Post("/:database/:id/reject", h.Comments.DashboardReject)
	dashComments.Delete("/:database/:id", h.Comments.DashboardDelete)

	// Files management
	dashFiles := dash.Group("/files", middleware.Can("manage files"))
	dashFiles.Get("", h.Files.DashboardList)
	dashFiles.Post("", h.Files.DashboardUpload)
	dashFiles.Get("/:id/info", h.Files.Info)
	dashFiles.Get("/:id/download", h.Files.DashboardDownload)
	dashFiles.Get("/:id", h.Files.DashboardShow)
	dashFiles.Put("/:id", h.Files.DashboardUpdate)
	dashFiles.Delete("/:id", h.Files.DashboardDelete)

	// Secure file upload (dashboard)
	dash.Post("/secure/upload-image", middleware.Can("upload files"), h.Files.SecureUploadImage)
	dash.Post("/secure/upload-document", middleware.Can("upload files"), h.Files.SecureUploadDocument)

	// AI (dashboard)
	dash.Post("/ai/generate", middleware.Can("manage articles"), h.AI.Generate)
	dash.Get("/ai/status/:id", middleware.Can("manage articles"), h.AI.Status)
	// Draft-time content assist: works on unsaved editor text, no article/post ID
	// required — surfaces SEO suggestions while writing instead of only after
	// publish (see CONTENT_QUALITY_GOVERNANCE_CENTER_PLAN.md §0.3).
	dash.Post("/ai/suggest-meta-description", middleware.Can("manage articles"), h.AI.SuggestMetaDescription)
	dash.Post("/ai/suggest-keywords", middleware.Can("manage articles"), h.AI.SuggestKeywords)
}
