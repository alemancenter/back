package contentquality

import (
	"fmt"
	"strings"
	"testing"
)

func TestNormalizeForSimilarityArabicAndHTML(t *testing.T) {
	left := `<p>إِنَّ <strong>الإيمان</strong> أساسُ التعلُّم.</p>`
	right := `ان الايمان اساس التعلم`
	if got, want := NormalizeForSimilarity(left), NormalizeForSimilarity(right); got != want {
		t.Fatalf("normalized mismatch: got %q want %q", got, want)
	}
}

func TestDetectSimilarityExactDuplicate(t *testing.T) {
	body := strings.Join(series("كلمة", 60), " ")
	report := DetectSimilarity([]SimilarityDocument{
		{Key: "article:1", Title: "العنوان الأول", Content: `<p>` + body + `</p>`},
		{Key: "post:2", Title: "عنوان مختلف", Content: body},
	}, DefaultSimilarityOptions())
	if len(report.Pairs) != 1 {
		t.Fatalf("expected one exact pair, got %d", len(report.Pairs))
	}
	if report.Pairs[0].Kind != SimilarityKindExact || report.Pairs[0].Similarity != 1 {
		t.Fatalf("unexpected exact pair: %+v", report.Pairs[0])
	}
	if len(report.Clusters) != 1 || len(report.Clusters[0].Members) != 2 {
		t.Fatalf("expected one two-member cluster: %+v", report.Clusters)
	}
}

func TestDetectSimilarityNearDuplicate(t *testing.T) {
	base := series("مشترك", 100)
	variant := append([]string(nil), base...)
	for i := 42; i < 49; i++ {
		variant[i] = fmt.Sprintf("مختلف%d", i)
	}
	report := DetectSimilarity([]SimilarityDocument{
		{Key: "article:10", Title: "درس العلوم", Content: strings.Join(base, " ")},
		{Key: "article:11", Title: "درس العلوم المعدل", Content: strings.Join(variant, " ")},
	}, DefaultSimilarityOptions())
	if len(report.Pairs) != 1 {
		t.Fatalf("expected one near pair, got %+v", report.Pairs)
	}
	if report.Pairs[0].Kind != SimilarityKindNear {
		t.Fatalf("expected near duplicate, got %+v", report.Pairs[0])
	}
	if report.Pairs[0].Similarity < 0.78 {
		t.Fatalf("near similarity below threshold: %+v", report.Pairs[0])
	}
}

func TestDetectSimilarityTemplateClone(t *testing.T) {
	shared := series("مشترك", 75)
	left := append(append([]string(nil), shared...), series("عربي", 20)...)
	right := append(append([]string(nil), shared...), series("تاريخ", 20)...)
	report := DetectSimilarity([]SimilarityDocument{
		{Key: "post:237", Title: "رتب معلمي اللغة العربية", Content: strings.Join(left, " ")},
		{Key: "post:240", Title: "رتب معلمي التاريخ", Content: strings.Join(right, " ")},
	}, DefaultSimilarityOptions())
	if len(report.Pairs) != 1 {
		t.Fatalf("expected one template pair, got %+v", report.Pairs)
	}
	if report.Pairs[0].Kind != SimilarityKindTemplate {
		t.Fatalf("expected template clone, got %+v", report.Pairs[0])
	}
}

func TestDetectSimilarityIgnoresUnrelatedAndShortDocuments(t *testing.T) {
	report := DetectSimilarity([]SimilarityDocument{
		{Key: "article:1", Title: "أ", Content: strings.Join(series("الف", 70), " ")},
		{Key: "article:2", Title: "ب", Content: strings.Join(series("باء", 70), " ")},
		{Key: "article:3", Title: "قصير", Content: "نص قصير جدا"},
	}, DefaultSimilarityOptions())
	if len(report.Pairs) != 0 || len(report.Clusters) != 0 {
		t.Fatalf("expected no similarities, got %+v", report)
	}
	if report.IgnoredDocuments != 1 {
		t.Fatalf("expected one ignored short document, got %d", report.IgnoredDocuments)
	}
}

func TestDetectSimilarityClustersTransitivePairs(t *testing.T) {
	base := series("قاعدة", 100)
	v1 := append([]string(nil), base...)
	v2 := append([]string(nil), base...)
	for i := 20; i < 27; i++ {
		v1[i] = fmt.Sprintf("تعديلأ%d", i)
	}
	for i := 70; i < 77; i++ {
		v2[i] = fmt.Sprintf("تعديلب%d", i)
	}
	report := DetectSimilarity([]SimilarityDocument{
		{Key: "article:1", Title: "واحد", Content: strings.Join(base, " ")},
		{Key: "article:2", Title: "اثنان", Content: strings.Join(v1, " ")},
		{Key: "article:3", Title: "ثلاثة", Content: strings.Join(v2, " ")},
	}, DefaultSimilarityOptions())
	if len(report.Clusters) != 1 {
		t.Fatalf("expected one cluster, got %+v", report.Clusters)
	}
	if len(report.Clusters[0].Members) != 3 {
		t.Fatalf("expected three members, got %+v", report.Clusters[0])
	}
}

func series(prefix string, count int) []string {
	items := make([]string, count)
	for i := range items {
		items[i] = fmt.Sprintf("%s%d", prefix, i)
	}
	return items
}
