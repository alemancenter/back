package ai

import (
	"encoding/json"
	"errors"
	"log"
	"strings"

	"github.com/alemancenter/fiber-api/internal/services"
	"github.com/alemancenter/fiber-api/internal/utils"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

// Handler contains AI route handlers.
type Handler struct {
	svc services.AIService
}

// New creates a new AI Handler.
func New(svc services.AIService) *Handler {
	return &Handler{svc: svc}
}

type GenerateRequest struct {
	Title       string `json:"title"`
	ContentType string `json:"content_type"` // "article" (default) or "post"
}

// Generate starts an async AI content generation job and returns a job ID immediately.
func (h *Handler) Generate(c *fiber.Ctx) error {
	var req GenerateRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.BadRequest(c, "بيانات غير صالحة")
	}

	title := strings.TrimSpace(req.Title)
	if len([]rune(title)) < 5 || len([]rune(title)) > 200 {
		return utils.BadRequest(c, "عنوان المقال يجب أن يكون بين 5 و 200 حرف")
	}

	contentType := strings.TrimSpace(req.ContentType)
	if contentType != "post" {
		contentType = "article"
	}

	jobID := uuid.New().String()
	store := services.GetAIJobStore()
	store.Create(jobID)

	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("AI generation panic | job=%s | title=%q | panic=%v", jobID, title, r)
				store.Fail(jobID, "تعذر توليد المحتوى بسبب خطأ داخلي مؤقت. يرجى المحاولة مرة أخرى.")
			}
		}()

		article, err := h.svc.GenerateSEOArticle(title, contentType)
		if err != nil {
			log.Printf("AI generation failed | job=%s | title=%q | error=%v", jobID, title, err)
			store.Fail(jobID, clientAIErrorMessage(err))
			return
		}
		articleJSON, err := json.Marshal(article)
		if err != nil {
			store.Fail(jobID, "failed to serialize article")
			return
		}
		store.Complete(jobID, string(articleJSON))
	}()

	return c.Status(fiber.StatusAccepted).JSON(fiber.Map{
		"success": true,
		"job_id":  jobID,
		"status":  services.JobPending,
	})
}

// Status returns the current state of an async AI generation job.
func (h *Handler) Status(c *fiber.Ctx) error {
	jobID := c.Params("id")
	if jobID == "" {
		return utils.BadRequest(c, "معرف المهمة مطلوب")
	}

	if _, err := uuid.Parse(jobID); err != nil {
		return utils.BadRequest(c, "معرف المهمة غير صالح")
	}

	job, ok := services.GetAIJobStore().Get(jobID)
	if !ok {
		return utils.NotFound(c, "المهمة غير موجودة أو انتهت صلاحيتها")
	}

	resp := fiber.Map{
		"success": true,
		"job_id":  job.ID,
		"status":  job.Status,
	}

	if job.Status == services.JobDone {
		var article services.SEOArticle
		if err := json.Unmarshal([]byte(job.Content), &article); err == nil {
			resp["article"] = article
			if article.ContentHTML != "" {
				resp["content"] = article.ContentHTML
				resp["content_html"] = article.ContentHTML
			} else {
				resp["content"] = article.Content
			}
		} else {
			resp["content"] = job.Content
		}
	}

	if job.Status == services.JobFailed {
		resp["error"] = job.Error
	}

	return c.JSON(resp)
}

func clientAIErrorMessage(err error) string {
	if err == nil {
		return "تعذر توليد المحتوى. يرجى المحاولة مرة أخرى."
	}

	msg := strings.ToLower(err.Error())
	switch {
	case errors.Is(err, services.ErrAIGenerationTimeout) || strings.Contains(msg, "timeout") || strings.Contains(msg, "deadline"):
		return "استغرق توليد المقال وقتا أطول من المتوقع. يرجى المحاولة مرة أخرى بعد قليل."
	case strings.Contains(msg, "api key"):
		return "خدمة الذكاء الاصطناعي غير مهيأة. يرجى التحقق من مفتاح الخدمة."
	case errors.Is(err, services.ErrAIProviderFailed), strings.Contains(msg, "provider"), strings.Contains(msg, "api error"):
		return "تعذر الاتصال بخدمة الذكاء الاصطناعي حاليا. يرجى المحاولة لاحقا."
	default:
		return "تعذر توليد محتوى صالح لهذا العنوان. يرجى تعديل العنوان والمحاولة مرة أخرى."
	}
}
