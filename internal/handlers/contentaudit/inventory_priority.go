package contentaudit

import (
	"fmt"

	"github.com/imanjo/fiber-api/internal/contentquality"
	"github.com/imanjo/fiber-api/internal/models"
)

func inventoryPriority(item inventoryItem) (int, []string, string) {
	score := 0
	reasons := make([]string, 0, 8)
	if item.Published {
		score += 20
	}
	if item.Corrupted {
		score += 100
		reasons = append(reasons, "فساد حتمي في المصدر")
	}
	switch item.Similarity.Kind {
	case contentquality.SimilarityKindExact:
		score += 90
		reasons = append(reasons, "تطابق محتوى كامل")
	case contentquality.SimilarityKindNear:
		score += 65
		reasons = append(reasons, fmt.Sprintf("شبه تكرار %.0f%%", item.Similarity.Similarity*100))
	case contentquality.SimilarityKindTemplate:
		score += 45
		reasons = append(reasons, fmt.Sprintf("تشابه قالبي %.0f%%", item.Similarity.Similarity*100))
	}
	if !item.ShouldIndex {
		score += 70
		reasons = append(reasons, "غير قابل للفهرسة حسب بوابة الجودة")
	}
	if !item.Audited {
		score += 15
		reasons = append(reasons, "غير مدقق")
	}
	switch item.AuditDecision {
	case models.AIDecisionRejected:
		score += 60
	case models.AIDecisionNeedsFix:
		score += 40
		reasons = append(reasons, "قرار التدقيق يحتاج إصلاح")
	case models.AIDecisionRestrictedAds:
		score += 25
		reasons = append(reasons, "الإعلانات مقيدة")
	}
	if item.FilesCount > 0 && item.WordCount < 180 {
		score += 25
		reasons = append(reasons, "صفحة ملف قصيرة تحتاج قيمة تعليمية إضافية")
	}
	switch {
	case item.Visits >= 10000:
		score += 30
		reasons = append(reasons, "زيارات مرتفعة جدًا")
	case item.Visits >= 1000:
		score += 20
		reasons = append(reasons, "زيارات مرتفعة")
	case item.Visits >= 100:
		score += 10
	}

	suggested := models.EditorialDecisionImprove
	switch {
	case item.Corrupted:
		suggested = models.EditorialDecisionImprove
	case item.Similarity.Kind == contentquality.SimilarityKindExact:
		suggested = models.EditorialDecisionMerge301
	case !item.ShouldIndex:
		suggested = models.EditorialDecisionNoindex
	case item.Similarity.Kind == contentquality.SimilarityKindNear || item.Similarity.Kind == contentquality.SimilarityKindTemplate:
		suggested = models.EditorialDecisionImprove
	case item.AdsEligible:
		suggested = models.EditorialDecisionKeep
	}
	return score, reasons, suggested
}
