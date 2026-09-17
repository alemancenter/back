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

func TestDetectDuplicateAgainstCorpusFindsExact(t *testing.T) {
	body := strings.Join(series("كلمة", 60), " ")
	candidate := SimilarityDocument{Key: "article:0", Title: "تحضير الصف العاشر", Content: `<p>` + body + `</p>`}
	corpus := []SimilarityDocument{
		{Key: "article:2380", Title: "تحضير الصف السابع", Content: body},
		{Key: "article:99", Title: "غير ذي صلة", Content: strings.Join(series("مختلف", 70), " ")},
	}
	matches := DetectDuplicateAgainstCorpus(candidate, corpus, DefaultSimilarityOptions())
	if len(matches) != 1 {
		t.Fatalf("expected exactly one match, got %+v", matches)
	}
	if matches[0].Kind != SimilarityKindExact || matches[0].Key != "article:2380" {
		t.Fatalf("expected exact match against article:2380, got %+v", matches[0])
	}
}

func TestDetectDuplicateAgainstCorpusFindsNear(t *testing.T) {
	base := series("مشترك", 100)
	variant := append([]string(nil), base...)
	for i := 42; i < 49; i++ {
		variant[i] = fmt.Sprintf("مختلف%d", i)
	}
	candidate := SimilarityDocument{Key: "article:0", Title: "درس العلوم للثامن", Content: strings.Join(variant, " ")}
	corpus := []SimilarityDocument{
		{Key: "article:10", Title: "درس العلوم للعاشر", Content: strings.Join(base, " ")},
	}
	matches := DetectDuplicateAgainstCorpus(candidate, corpus, DefaultSimilarityOptions())
	if len(matches) != 1 || matches[0].Kind != SimilarityKindNear {
		t.Fatalf("expected one near match, got %+v", matches)
	}
}

func TestDetectDuplicateAgainstCorpusIgnoresSelfAndShort(t *testing.T) {
	body := strings.Join(series("كلمة", 60), " ")
	candidate := SimilarityDocument{Key: "article:5", Title: "عنوان", Content: body}
	corpus := []SimilarityDocument{
		{Key: "article:5", Title: "عنوان", Content: body}, // same key as candidate: must be skipped (self on edit)
		{Key: "article:6", Title: "قصير", Content: "نص قصير جدا"},
	}
	matches := DetectDuplicateAgainstCorpus(candidate, corpus, DefaultSimilarityOptions())
	if len(matches) != 0 {
		t.Fatalf("expected no matches (self excluded, other too short), got %+v", matches)
	}
}

func TestDetectDuplicateAgainstCorpusNoFalsePositive(t *testing.T) {
	candidate := SimilarityDocument{Key: "article:0", Title: "أ", Content: strings.Join(series("الف", 70), " ")}
	corpus := []SimilarityDocument{
		{Key: "article:1", Title: "ب", Content: strings.Join(series("باء", 70), " ")},
	}
	matches := DetectDuplicateAgainstCorpus(candidate, corpus, DefaultSimilarityOptions())
	if len(matches) != 0 {
		t.Fatalf("expected no matches for unrelated content, got %+v", matches)
	}
}

// Two identical short pages (well below MinWords) must still be flagged as an exact content
// match — a word-for-word duplicate is exact regardless of length, and short+duplicated is
// exactly the thin/templated-content pattern this scan exists to catch.
func TestDetectSimilarityExactMatchIgnoresMinWords(t *testing.T) {
	report := DetectSimilarity([]SimilarityDocument{
		{Key: "article:1", Title: "عنوان أول", Content: "نص قصير جدا للاختبار"},
		{Key: "post:2", Title: "عنوان مختلف", Content: "نص قصير جدا للاختبار"},
	}, DefaultSimilarityOptions())
	if len(report.Pairs) != 1 {
		t.Fatalf("expected one exact pair for identical short content, got %+v", report.Pairs)
	}
	pair := report.Pairs[0]
	if pair.Kind != SimilarityKindExact || pair.Similarity != 1 {
		t.Fatalf("unexpected pair: %+v", pair)
	}
	if len(pair.MatchedOn) != 1 || pair.MatchedOn[0] != "content" {
		t.Fatalf("expected MatchedOn=[content], got %+v", pair.MatchedOn)
	}
}

// TestDetectSimilarityTitleOnlyMatchIsNotExact locks in the fix for a real false-positive: two
// documents sharing the exact same title but with entirely unrelated content (e.g. two catalog
// worksheets that legitimately reuse the same structured title phrase for different subjects)
// must NOT be reported as SimilarityKindExact — that is the top-severity kind that tells a
// reviewer to consider merging/deleting/redirecting one of the pages, which would be actively
// wrong here since the pages are unique and not an AdSense duplicate-content risk at all. This
// scenario was previously misclassified as exact purely from the title hash matching.
func TestDetectSimilarityTitleOnlyMatchIsNotExact(t *testing.T) {
	report := DetectSimilarity([]SimilarityDocument{
		{Key: "article:1", Title: "نفس العنوان تمامًا", Content: strings.Join(series("الف", 70), " ")},
		{Key: "article:2", Title: "نفس العنوان تمامًا", Content: strings.Join(series("باء", 70), " ")},
	}, DefaultSimilarityOptions())
	var titleMatch *SimilarityPair
	for i := range report.Pairs {
		if report.Pairs[i].Kind == SimilarityKindExact {
			t.Fatalf("a title-only match must never be reported as SimilarityKindExact, got %+v", report.Pairs[i])
		}
		if report.Pairs[i].Kind == SimilarityKindTitleOnly {
			titleMatch = &report.Pairs[i]
		}
	}
	if titleMatch == nil {
		t.Fatalf("expected a title_only pair from matching titles with unrelated content, got %+v", report.Pairs)
	}
	if len(titleMatch.MatchedOn) != 1 || titleMatch.MatchedOn[0] != "title" {
		t.Fatalf("expected MatchedOn=[title], got %+v", titleMatch.MatchedOn)
	}

	clusters := clusterSimilarityPairs(report.Pairs)
	if len(clusters) != 1 || clusters[0].Kind != SimilarityKindTitleOnly {
		t.Fatalf("expected the cluster itself to also be kind=title_only, got %+v", clusters)
	}
}

// TestDetectSimilarityContentMatchStaysExactEvenWithDifferentTitle is the flip side: when the
// CONTENT itself matches exactly, that is a genuine AdSense duplicate-content risk regardless of
// title wording, so it must stay SimilarityKindExact.
func TestDetectSimilarityContentMatchStaysExactEvenWithDifferentTitle(t *testing.T) {
	sharedContent := strings.Join(series("جيم", 70), " ")
	report := DetectSimilarity([]SimilarityDocument{
		{Key: "article:1", Title: "عنوان أول مختلف تمامًا", Content: sharedContent},
		{Key: "article:2", Title: "عنوان ثانٍ مختلف كليًا", Content: sharedContent},
	}, DefaultSimilarityOptions())
	var contentMatch *SimilarityPair
	for i := range report.Pairs {
		if report.Pairs[i].Kind == SimilarityKindExact {
			contentMatch = &report.Pairs[i]
		}
	}
	if contentMatch == nil {
		t.Fatalf("expected an exact pair from matching content, got %+v", report.Pairs)
	}
	if len(contentMatch.MatchedOn) != 1 || contentMatch.MatchedOn[0] != "content" {
		t.Fatalf("expected MatchedOn=[content], got %+v", contentMatch.MatchedOn)
	}
}

func series(prefix string, count int) []string {
	items := make([]string, count)
	for i := range items {
		items[i] = fmt.Sprintf("%s%d", prefix, i)
	}
	return items
}
