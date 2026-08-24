package ai

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"strings"
	"time"

	"github.com/imanjo/fiber-api/internal/services"
	"github.com/imanjo/fiber-api/internal/services/contentaudit"
	"github.com/imanjo/fiber-api/internal/utils"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

// groundedGenerationTimeout bounds the whole grounded-from-file pipeline (fact extraction +
// up to 3 writer/validator rounds — several sequential AI calls, unlike the single-call
// fallback generator's AIOverallTimeout). Keep in sync with the frontend poll windows.
const groundedGenerationTimeout = 5 * time.Minute

// Handler contains AI route handlers.
type Handler struct {
	svc         services.AIService
	groundedSvc *contentaudit.Service
}

// New creates a new AI Handler. groundedSvc powers the file-aware generation path; svc remains
// the legacy no-file generator. The file-aware path may deliberately use general knowledge
// when a scanned/image-only PDF has no extractable text, but now discloses that provenance.
func New(svc services.AIService, groundedSvc *contentaudit.Service) *Handler {
	return &Handler{svc: svc, groundedSvc: groundedSvc}
}

type GenerateRequest struct {
	Title             string `json:"title"`
	ContentType       string `json:"content_type"` // "article" (default) or "post"
	CountryCode       string `json:"country_code,omitempty"`
	Country           string `json:"country,omitempty"`
	FileIDs           []uint `json:"file_ids,omitempty"`
	GradeLevel        string `json:"grade_level,omitempty"`
	GradeName         string `json:"grade_name,omitempty"`
	SubjectID         string `json:"subject_id,omitempty"`
	SubjectName       string `json:"subject_name,omitempty"`
	SemesterID        string `json:"semester_id,omitempty"`
	SemesterName      string `json:"semester_name,omitempty"`
	CategoryID        string `json:"category_id,omitempty"`
	CategoryName      string `json:"category_name,omitempty"`
	CurriculumContext string `json:"curriculum_context,omitempty"`
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

	// grade_level in the dashboard payload is the internal school_classes ID/order
	// (e.g. 12 can mean "الصف الأول الثانوي" because kindergarten occupies ID 1).
	// Resolve it to the human grade name before any AI pipeline sees the context.
	// Numeric internal IDs are never forwarded as semantic grade numbers.
	generationContext := buildGenerationContext(req, lookupSchoolClassGradeName)

	jobID := uuid.New().String()
	store := services.GetAIJobStore()
	store.Create(jobID)
	fileIDs := req.FileIDs

	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("AI generation panic | job=%s | title=%q | panic=%v", jobID, title, r)
				store.Fail(jobID, "تعذر توليد المحتوى بسبب خطأ داخلي مؤقت. يرجى المحاولة مرة أخرى.")
			}
		}()

		var article interface{}
		var err error
		if len(fileIDs) > 0 && h.groundedSvc != nil {
			// The file-aware pipeline makes several sequential AI calls. Readable files go
			// through fact extraction + independent claim validation; unreadable/image-only
			// files retain the intentional general-knowledge fallback with explicit provenance.
			ctx, cancel := context.WithTimeout(context.Background(), groundedGenerationTimeout)
			defer cancel()
			article, err = h.groundedSvc.GenerateGroundedDraftWithProvenance(ctx, contentaudit.GroundedGenerateRequest{
				Title:             title,
				ContentType:       contentType,
				CountryCode:       generationContext.CountryCode,
				FileIDs:           fileIDs,
				GradeName:         generationContext.GradeName,
				SubjectName:       generationContext.SubjectName,
				SemesterName:      generationContext.SemesterName,
				CategoryName:      generationContext.CategoryName,
				CurriculumContext: generationContext.CurriculumContext,
			})
			if err != nil {
				log.Printf("grounded AI generation failed | job=%s | title=%q | error=%v", jobID, title, err)
				store.Fail(jobID, err.Error())
				return
			}
		} else {
			article, err = h.svc.GenerateSEOArticleWithContext(title, contentType, generationContext)
			if err != nil {
				log.Printf("AI generation failed | job=%s | title=%q | error=%v", jobID, title, err)
				store.Fail(jobID, clientAIErrorMessage(err))
				return
			}
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

// Status returns the current state of an async AI content generation job.
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
		// Preserve the complete generated JSON instead of decoding into SEOArticle and
		// accidentally discarding newer provenance fields such as source_mode/grounding_score.
		var article map[string]interface{}
		if err := json.Unmarshal([]byte(job.Content), &article); err == nil {
			resp["article"] = article
			if html, _ := article["content_html"].(string); html != "" {
				resp["content"] = html
				resp["content_html"] = html
			} else if content, _ := article["content"].(string); content != "" {
				resp["content"] = content
			}
			if sourceMode, _ := article["source_mode"].(string); sourceMode != "" {
				resp["source_mode"] = sourceMode
			}
			if warning, _ := article["source_warning"].(string); warning != "" {
				resp["source_warning"] = warning
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

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
