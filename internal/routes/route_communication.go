package routes

import (
	"github.com/alemancenter/fiber-api/internal/middleware"
	"github.com/gofiber/fiber/v2"
)

// registerCommunicationRoutes handles user and admin communication modules:
// Calendar, Events, Messages, and Notifications.
func registerCommunicationRoutes(public, dash fiber.Router, h *Handlers) {
	// =====================
	// PUBLIC ROUTES
	// =====================

	// Calendar & Events
	public.Get("/home/calendar", h.Calendar.PublicEvents)
	public.Get("/home/event/:id", h.Calendar.PublicEventDetail)

	// Public support chatbot. OptionalAuth is already applied on the public router.
	public.Get("/chatbot/suggestions", h.Chatbot.Suggestions)
	public.Post("/chatbot/message", h.Chatbot.Message)
	public.Post("/chatbot/feedback", h.Chatbot.Feedback)

	// =====================
	// ADMIN DASHBOARD ROUTES
	// =====================

	// Calendar management
	dashCalendar := dash.Group("/calendar")
	dashCalendar.Get("/databases", h.Calendar.Databases)
	dashCalendar.Get("/events", h.Calendar.GetEvents)
	dashCalendar.Post("/events", h.Calendar.CreateEvent)
	dashCalendar.Put("/events/:id", h.Calendar.UpdateEvent)
	dashCalendar.Delete("/events/:id", h.Calendar.DeleteEvent)

	// Messages
	dashMessages := dash.Group("/messages")
	dashMessages.Get("/inbox", h.Messages.Inbox)
	dashMessages.Get("/sent", h.Messages.Sent)
	dashMessages.Get("/drafts", h.Messages.Drafts)
	dashMessages.Post("/send", h.Messages.Send)
	dashMessages.Post("/draft", h.Messages.Draft)
	dashMessages.Post("/save-draft", h.Messages.Draft)
	dashMessages.Get("/:id", h.Messages.Get)
	dashMessages.Post("/:id/read", h.Messages.MarkAsRead)
	dashMessages.Post("/:id/important", h.Messages.ToggleImportant)
	dashMessages.Delete("/:id", h.Messages.Delete)

	// Contact form messages (submitted via the public contact form)
	dashContactMessages := dash.Group("/contact-messages", middleware.Can("manage settings"))
	dashContactMessages.Get("", h.ContactMessages.List)
	dashContactMessages.Get("/:id", h.ContactMessages.Get)
	dashContactMessages.Post("/:id/read", h.ContactMessages.MarkAsRead)
	dashContactMessages.Delete("/:id", h.ContactMessages.Delete)

	// Chatbot management
	dashChatbot := dash.Group("/chatbot", middleware.Can("manage settings"))
	dashChatbot.Get("/sessions", h.Chatbot.DashboardSessions)
	dashChatbot.Get("/knowledge", h.Chatbot.DashboardKnowledge)
	dashChatbot.Post("/knowledge", h.Chatbot.StoreKnowledge)
	dashChatbot.Put("/knowledge/:id", h.Chatbot.UpdateKnowledge)
	dashChatbot.Delete("/knowledge/:id", h.Chatbot.DeleteKnowledge)

	// Notifications
	dashNotifications := dash.Group("/notifications")
	dashNotifications.Get("/latest", h.Notifications.Latest)
	dashNotifications.Post("/read-all", h.Notifications.MarkAllRead)
	dashNotifications.Post("/bulk", h.Notifications.BulkAction)
	dashNotifications.Post("/prune", h.Notifications.Prune)
	dashNotifications.Post("/broadcast", h.Notifications.Broadcast)
	dashNotifications.Get("", h.Notifications.List)
	dashNotifications.Post("", h.Notifications.Create)
	dashNotifications.Post("/:id/read", h.Notifications.MarkAsRead)
	dashNotifications.Delete("/:id", h.Notifications.Delete)
}
