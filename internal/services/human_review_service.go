// Human review is the admin-facing approval step for AI policy fixes: instead of the AI fix
// (services.ContentDraftService.FixPolicyContent) handing a draft straight to the normal
// article/post edit page for the admin to open and save, it's queued here as a pending
// HumanReviewItem — carrying both the original and the AI-fixed title/content/meta
// description side by side — until the admin explicitly approves (applies it through the
// same ArticleService/PostService update path a manual save uses) or rejects (discards it)
// from the /dashboard/human-review page. Pending items are stored in Redis per country, the
// same best-effort cache pattern internal/handlers/adsensepolicy's scan result already uses,
// not a database table — this is a transient review queue, not permanent record-keeping.
package services

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/imanjo/fiber-api/internal/database"
)

// HumanReviewItem is one AI policy fix awaiting a human decision. Original* fields are
// captured at the moment the fix ran (not re-read live) so the comparison the admin sees
// never shifts out from under them between queueing and review.
type HumanReviewItem struct {
	ID          string `json:"id"`
	ContentType string `json:"content_type"` // "article" | "post"
	ContentID   uint64 `json:"content_id"`
	Title       string `json:"title"`
	PublicURL   string `json:"public_url"`
	EditURL     string `json:"edit_url"`

	OriginalTitle           string `json:"original_title"`
	OriginalContentHTML     string `json:"original_content_html"`
	OriginalMetaDescription string `json:"original_meta_description"`

	NewTitle           string `json:"new_title"`
	NewContentHTML     string `json:"new_content_html"`
	NewMetaDescription string `json:"new_meta_description"`

	WordCount       int      `json:"word_count"`
	IssuesAddressed []string `json:"issues_addressed"`
	SEOScore        int      `json:"seo_score,omitempty"`
	Warning         string   `json:"warning,omitempty"`

	CreatedAt time.Time `json:"created_at"`
}

// ErrHumanReviewNotFound is returned by Approve/Reject when the review id no longer exists in
// the pending list — most likely already decided (approved or rejected) from another tab or
// by another admin.
var ErrHumanReviewNotFound = errors.New("لم يتم العثور على عنصر المراجعة، قد يكون تمت مراجعته بالفعل")

type HumanReviewService interface {
	// QueueFix runs the AI policy fix for one article/post and stores the result as a pending
	// review item. The caller loops this once per selected item for a "batch" fix rather than
	// this service running a batch itself — each fix is an independent, potentially slow
	// (~30-90s) external AI call, and looping client-side keeps any one request's timeout
	// bounded and lets the admin see per-item progress instead of one all-or-nothing call.
	QueueFix(ctx context.Context, countryID database.CountryID, contentType string, id uint64) (*HumanReviewItem, error)
	List(ctx context.Context, countryID database.CountryID) ([]HumanReviewItem, error)
	Approve(ctx context.Context, countryID database.CountryID, reviewID string, authorID uint) (*HumanReviewItem, error)
	Reject(ctx context.Context, countryID database.CountryID, reviewID string) (*HumanReviewItem, error)
}

type humanReviewService struct {
	draftSvc   ContentDraftService
	articleSvc ArticleService
	postSvc    PostService
}

func NewHumanReviewService(draftSvc ContentDraftService, articleSvc ArticleService, postSvc PostService) HumanReviewService {
	return &humanReviewService{draftSvc: draftSvc, articleSvc: articleSvc, postSvc: postSvc}
}

// humanReviewCacheTTL is generous (this is a review queue an admin is expected to act on, not
// a cache that needs freshness) so a pending item surviving a long weekend isn't silently
// dropped — it only exists so an item nobody ever reviews doesn't linger in Redis forever.
const humanReviewCacheTTL = 90 * 24 * time.Hour

func humanReviewCacheKey(countryID database.CountryID) string {
	return database.Redis().Key("human_review", "pending", string(database.CountryCode(countryID)))
}

func (s *humanReviewService) loadPending(ctx context.Context, countryID database.CountryID) []HumanReviewItem {
	var items []HumanReviewItem
	database.Redis().GetJSON(ctx, humanReviewCacheKey(countryID), &items)
	return items
}

func (s *humanReviewService) savePending(ctx context.Context, countryID database.CountryID, items []HumanReviewItem) error {
	return database.Redis().SetJSON(ctx, humanReviewCacheKey(countryID), items, humanReviewCacheTTL)
}

func (s *humanReviewService) findPending(ctx context.Context, countryID database.CountryID, reviewID string) ([]HumanReviewItem, int) {
	pending := s.loadPending(ctx, countryID)
	for i := range pending {
		if pending[i].ID == reviewID {
			return pending, i
		}
	}
	return pending, -1
}

type originalContent struct {
	Title           string
	ContentHTML     string
	MetaDescription string
	PublicURL       string
	EditURL         string
}

// loadOriginal reads the item's CURRENT title/content/meta description fresh from the
// database (never trusting anything the client sent) — the same defensive stance
// policy_fix_service.go's loadPolicyFixSource takes, and for the same reason: a stale request
// must never end up comparing against (or overwriting) the wrong version of the content.
func (s *humanReviewService) loadOriginal(countryID database.CountryID, contentType string, id uint64) (*originalContent, error) {
	countryCode := string(database.CountryCode(countryID))
	switch contentType {
	case "article":
		article, err := s.articleSvc.GetByID(countryID, id)
		if err != nil {
			return nil, fmt.Errorf("تعذر العثور على المقال")
		}
		meta := ""
		if article.MetaDescription != nil {
			meta = *article.MetaDescription
		}
		return &originalContent{
			Title: article.Title, ContentHTML: article.Content, MetaDescription: meta,
			PublicURL: fmt.Sprintf("/%s/lesson/articles/%d", countryCode, id),
			EditURL:   fmt.Sprintf("/dashboard/articles/%d/edit", id),
		}, nil
	case "post":
		post, err := s.postSvc.GetByID(countryID, id)
		if err != nil {
			return nil, fmt.Errorf("تعذر العثور على المنشور")
		}
		meta := ""
		if post.MetaDescription != nil {
			meta = *post.MetaDescription
		}
		return &originalContent{
			Title: post.Title, ContentHTML: post.Content, MetaDescription: meta,
			PublicURL: fmt.Sprintf("/%s/posts/%d", countryCode, id),
			EditURL:   fmt.Sprintf("/dashboard/posts/%d/edit", id),
		}, nil
	default:
		return nil, fmt.Errorf("نوع المحتوى غير صحيح")
	}
}

func (s *humanReviewService) QueueFix(ctx context.Context, countryID database.CountryID, contentType string, id uint64) (*HumanReviewItem, error) {
	original, err := s.loadOriginal(countryID, contentType, id)
	if err != nil {
		return nil, err
	}

	fix, err := s.draftSvc.FixPolicyContent(ctx, PolicyFixRequest{ContentType: contentType, ID: id, CountryID: countryID})
	if err != nil {
		return nil, err
	}

	item := HumanReviewItem{
		ID:          fmt.Sprintf("%s-%d-%d", contentType, id, time.Now().UnixNano()),
		ContentType: contentType,
		ContentID:   id,
		Title:       original.Title,
		PublicURL:   original.PublicURL,
		EditURL:     original.EditURL,

		OriginalTitle:           original.Title,
		OriginalContentHTML:     original.ContentHTML,
		OriginalMetaDescription: original.MetaDescription,

		NewTitle:           fix.Title,
		NewContentHTML:     fix.ContentHTML,
		NewMetaDescription: fix.MetaDescription,

		WordCount:       fix.WordCount,
		IssuesAddressed: fix.IssuesAddressed,
		SEOScore:        fix.SEOScore,
		Warning:         fix.Warning,
		CreatedAt:       time.Now(),
	}

	pending := s.loadPending(ctx, countryID)
	replaced := false
	for i := range pending {
		if pending[i].ContentType == contentType && pending[i].ContentID == id {
			pending[i] = item
			replaced = true
			break
		}
	}
	if !replaced {
		pending = append(pending, item)
	}
	if err := s.savePending(ctx, countryID, pending); err != nil {
		return nil, fmt.Errorf("تعذر حفظ نتيجة الإصلاح للمراجعة البشرية")
	}
	return &item, nil
}

func (s *humanReviewService) List(ctx context.Context, countryID database.CountryID) ([]HumanReviewItem, error) {
	items := s.loadPending(ctx, countryID)
	sort.SliceStable(items, func(i, j int) bool { return items[i].CreatedAt.After(items[j].CreatedAt) })
	return items, nil
}

// Approve applies a pending review's new title/content/meta description through the exact
// same ArticleService/PostService update path the normal dashboard edit-and-save form uses —
// so cache invalidation, sitemap refresh, activity logging, the duplicate-content gate, and
// the thin-content-on-publish gate all run exactly as they would for a manual save, instead of
// this taking a shortcut straight to the database.
func (s *humanReviewService) Approve(ctx context.Context, countryID database.CountryID, reviewID string, authorID uint) (*HumanReviewItem, error) {
	pending, idx := s.findPending(ctx, countryID, reviewID)
	if idx == -1 {
		return nil, ErrHumanReviewNotFound
	}
	item := pending[idx]

	switch item.ContentType {
	case "article":
		var author *uint
		if authorID > 0 {
			author = &authorID
		}
		req := &ArticleInput{Title: item.NewTitle, Content: item.NewContentHTML, MetaDescription: item.NewMetaDescription}
		if _, _, err := s.articleSvc.UpdateArticle(countryID, item.ContentID, req, author); err != nil {
			return nil, fmt.Errorf("تعذر حفظ التعديل المعتمد: %v", err)
		}
	case "post":
		req := &UpdatePostRequest{Title: item.NewTitle, Content: item.NewContentHTML, MetaDescription: item.NewMetaDescription}
		// callerIsAdmin=true: reaching this endpoint already requires the "manage articles"/
		// "manage posts" permission gate, the same trust level the rest of this AI-fix feature
		// (FixPolicyContent) already extends to any such permitted admin regardless of authorship.
		if _, _, err := s.postSvc.Update(countryID, item.ContentID, req, authorID, true); err != nil {
			return nil, fmt.Errorf("تعذر حفظ التعديل المعتمد: %v", err)
		}
	default:
		return nil, fmt.Errorf("نوع المحتوى غير صحيح")
	}

	pending = append(pending[:idx], pending[idx+1:]...)
	_ = s.savePending(ctx, countryID, pending)
	return &item, nil
}

func (s *humanReviewService) Reject(ctx context.Context, countryID database.CountryID, reviewID string) (*HumanReviewItem, error) {
	pending, idx := s.findPending(ctx, countryID, reviewID)
	if idx == -1 {
		return nil, ErrHumanReviewNotFound
	}
	item := pending[idx]
	pending = append(pending[:idx], pending[idx+1:]...)
	_ = s.savePending(ctx, countryID, pending)
	return &item, nil
}
