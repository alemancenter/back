package chatbot

import "testing"

// The reported failure: user typed "امتحان ... الفصل الدراسي الثاني" but the real
// article title uses "اختبار ... الفصل الثاني". Relevance scoring (over expanded
// terms) must rank that article far above an unrelated one.
func TestRelevanceRanksNearTitleMatchHigh(t *testing.T) {
	query := "امتحان تربية اسلامية الصف التاسع الفصل الدراسي الثاني"
	terms := expandSearchTerms(searchTerms(query))

	target := ContentResult{Title: "اختبار نهائي تربية اسلامية الصف التاسع الفصل الثاني", Type: "article", Subject: "التربية الإسلامية"}
	unrelated := ContentResult{Title: "اختبار الشهر الاول لغة عربية الصف السادس", Type: "article", Subject: "اللغة العربية"}

	ts := scoreContentResult(query, terms, target)
	us := scoreContentResult(query, terms, unrelated)
	t.Logf("target=%d unrelated=%d", ts, us)
	if ts <= us {
		t.Fatalf("target score %d should exceed unrelated %d", ts, us)
	}
	if ts < 40 {
		t.Fatalf("target score %d too low — near-title match should score high", ts)
	}
}
