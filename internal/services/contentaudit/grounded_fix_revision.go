package contentaudit

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// One validator-guided rewrite is enough to remove unsupported claims without turning
// a fix-preview request into an unbounded model loop. If the revised draft still does not
// pass the independent validator, the request remains fail-closed and the exact unsupported
// claims are surfaced to the admin for diagnosis.
const groundedFixMaxRevisionPasses = 1

func normalizeGroundedFixDraft(draft groundedDraft, fallbackTitle string) groundedDraft {
	draft.Title = strings.TrimSpace(draft.Title)
	if draft.Title == "" {
		draft.Title = strings.TrimSpace(fallbackTitle)
	}
	draft.MetaDescription = strings.TrimSpace(draft.MetaDescription)
	draft.Keywords = compactStrings(draft.Keywords)
	draft.ContentHTML = normalizeFixedHTML(draft.ContentHTML)
	return draft
}

func validateGroundedFixDraft(draft groundedDraft, originalContent string, factCount int) error {
	plain := strings.TrimSpace(normalizePlainText(draft.ContentHTML))
	if plain == "" || strings.EqualFold(plain, normalizePlainText(originalContent)) {
		return fmt.Errorf("%w: المسودة الجديدة لا تضيف قيمة موثقة كافية", ErrGroundedValidationFailed)
	}
	if !validUsedFactIndexes(draft.UsedFactIndexes, factCount) {
		return fmt.Errorf("%w: المسودة تشير إلى حقائق غير موجودة في حزمة الأدلة", ErrGroundedValidationFailed)
	}
	return nil
}

func normalizeGroundedFixValidation(validation groundedValidation) groundedValidation {
	validation.GroundingScore = clamp(validation.GroundingScore)
	validation.UnsupportedClaims = compactStrings(validation.UnsupportedClaims)
	validation.Notes = compactStrings(validation.Notes)
	return validation
}

func groundedFixValidationPasses(validation groundedValidation) bool {
	return validation.GroundingScore >= groundingMinScore && len(validation.UnsupportedClaims) == 0
}

// runGroundedFixRevision gives the writer only the already-sanitized facts, its previous
// draft, and the independent validator's feedback. It deliberately does not provide the
// unverified existing body or invite general knowledge. The rewrite must delete or rephrase
// unsupported claims and may not replace them with new claims outside facts.
func runGroundedFixRevision(ctx context.Context, facts groundedFactExtraction, previous groundedDraft, validation groundedValidation) (groundedDraft, string, error) {
	var out groundedDraft
	factsJSON, _ := json.Marshal(facts)
	previousJSON, _ := json.Marshal(previous)
	feedback := groundingRetryFeedback(validation)

	system := `أنت محرر تصحيح لمحتوى تعليمي موثّق. أعد كتابة المسودة السابقة اعتمادًا حصريًا على facts المسموح بها. احذف أو أعد صياغة كل ادعاء وصفه المدقق بأنه غير مدعوم. لا تضف أي معلومة جديدة، ولا تستخدم معرفة عامة، ولا تستنتج رقم صف أو مرحلة من معرفات داخلية. إذا كان اسم الصف مكتوبًا نصيًا في facts مثل «الصف الأول الثانوي» فاستخدمه كما هو ولا تحوله إلى رقم. حافظ على HTML نظيف وعلى الحقول title وcontent_html وmeta_description وkeywords وused_fact_indexes. أعد JSON فقط.`
	user := fmt.Sprintf(`الحقائق المسموح بها فقط:\n%s\n\nالمسودة السابقة:\n%s\n\nملاحظات مدقق التوثيق:\n%s\n\nأعد JSON بالمفاتيح فقط: title, content_html, meta_description, keywords, used_fact_indexes. يجب أن تشير used_fact_indexes إلى facts المستخدمة فعليًا. لا تعوّض حذف الادعاءات غير المدعومة بإضافة معلومات جديدة.`, string(factsJSON), string(previousJSON), feedback)

	model, err := groundedAIJSON(ctx, "grounded_writer", system, user, &out)
	return out, model, err
}

func groundedValidationFailureDetail(validation groundedValidation) string {
	validation = normalizeGroundedFixValidation(validation)
	claims := append([]string(nil), validation.UnsupportedClaims...)
	if len(claims) > 3 {
		claims = claims[:3]
	}
	for i := range claims {
		claims[i] = strings.Join(strings.Fields(claims[i]), " ")
		runes := []rune(claims[i])
		if len(runes) > 220 {
			claims[i] = string(runes[:220]) + "…"
		}
	}

	base := fmt.Sprintf("grounding_score=%d unsupported_claims=%d", validation.GroundingScore, len(validation.UnsupportedClaims))
	if len(claims) == 0 {
		return base
	}
	return base + " details=" + strings.Join(claims, " || ")
}
