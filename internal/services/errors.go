package services

import (
	"errors"
	"fmt"

	"github.com/imanjo/fiber-api/internal/contentquality"
	"gorm.io/gorm"
)

// ErrNotFound is returned when a requested record is not found in the database.
var ErrNotFound = errors.New("record not found")

// ErrForbidden is returned when the caller lacks ownership of the target resource.
var ErrForbidden = errors.New("forbidden")

// ErrProtectedRole prevents destructive changes to the platform's root access roles.
var ErrProtectedRole = errors.New("protected role")

// DuplicateContentError blocks saving an article/post whose content is an exact- or
// near-duplicate of another existing article/post (see
// contentquality.DetectDuplicateAgainstCorpus). This is the content-uniqueness gate: reused
// explanations across grades/subjects (only the title/attachment changed) were a confirmed
// AdSense "low-value content" rejection cause for this site, so duplication is refused at
// save time instead of only being flagged later in the admin similarity report.
type DuplicateContentError struct {
	// Kind is contentquality.SimilarityKindExact or SimilarityKindNear.
	Kind string
	// MatchKey identifies the existing content, e.g. "article:2380" or "post:41".
	MatchKey   string
	MatchTitle string
	Similarity float64
}

func (e *DuplicateContentError) Error() string {
	return fmt.Sprintf("duplicate content (%s, %.0f%% similar to %s)", e.Kind, e.Similarity*100, e.MatchKey)
}

// UserMessage is the Arabic message shown to the editor in the dashboard, explaining exactly
// which existing item the new content duplicates so they can rewrite it instead of guessing.
func (e *DuplicateContentError) UserMessage() string {
	verb := "يتطابق تمامًا مع"
	if e.Kind == contentquality.SimilarityKindNear {
		verb = fmt.Sprintf("يتشابه بنسبة %.0f%% تقريبًا مع", e.Similarity*100)
	}
	title := e.MatchTitle
	if title == "" {
		title = e.MatchKey
	}
	return fmt.Sprintf(
		"تعذّر الحفظ: محتوى هذه الصفحة %s «%s». يجب أن يكون الشرح مخصصًا وفريدًا لهذا الصف/المادة، وليس نسخًا من محتوى آخر — Google يرفض المواقع بسبب المحتوى المكرر.",
		verb, title,
	)
}

// ThinContentError blocks *publishing* (never drafting) an article/post whose body falls
// below the site's own established "thin content" bar (contentquality.DiagnosticReviewMinWords
// — the same 120-word cutoff already used for the readiness dashboard's "thin_content" problem
// and the informational ContentQualitySignal). A two-sentence AI-generated draft that only
// restates the title (confirmed in production, e.g. article #2380) must never reach a public,
// crawlable, "منشور" URL — that is exactly the "low-value content" pattern Google's AdSense
// review rejected this site for. Saving as a draft (status=0 / is_active=false) is never
// affected: editors can still save work-in-progress and keep iterating.
type ThinContentError struct {
	WordCount int
	MinWords  int
}

func (e *ThinContentError) Error() string {
	return fmt.Sprintf("thin content (%d words, minimum %d to publish)", e.WordCount, e.MinWords)
}

// UserMessage is the Arabic message shown to the editor in the dashboard.
func (e *ThinContentError) UserMessage() string {
	return fmt.Sprintf(
		"تعذّر النشر: عدد كلمات المحتوى الحالي (%d كلمة) أقل من الحد الأدنى المطلوب للنشر (%d كلمة). "+
			"هذا بالضبط نوع المحتوى الذي ترفضه Google كـ«محتوى منخفض القيمة». احفظ كمسودة وأكمل الشرح، أو استخدم «توليد بالذكاء الاصطناعي» ثم راجعه وأثرِه قبل النشر.",
		e.WordCount, e.MinWords,
	)
}

// enforceMinimumDepthForPublish is the shared gate ArticleService and PostService call before
// persisting a Create/Update whose final state is published (published=true — status=1 for
// articles, is_active=true for posts). It never affects saving as a draft. See ThinContentError.
func enforceMinimumDepthForPublish(published bool, content string) error {
	if !published {
		return nil
	}
	words := countWords(stripHTML(content))
	if words >= contentquality.DiagnosticReviewMinWords {
		return nil
	}
	return &ThinContentError{WordCount: words, MinWords: contentquality.DiagnosticReviewMinWords}
}

// MapError translates data layer errors to service layer errors.
func MapError(err error) error {
	if err == gorm.ErrRecordNotFound {
		return ErrNotFound
	}
	return err
}

func MapErr0(err error) error {
	return MapError(err)
}

func MapErr1[T any](v T, err error) (T, error) {
	return v, MapError(err)
}

func MapErr2[T1 any, T2 any](v1 T1, v2 T2, err error) (T1, T2, error) {
	return v1, v2, MapError(err)
}

func MapErr3[T1 any, T2 any, T3 any](v1 T1, v2 T2, v3 T3, err error) (T1, T2, T3, error) {
	return v1, v2, v3, MapError(err)
}
