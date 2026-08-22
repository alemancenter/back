package contentaudit

import (
	"testing"
	"time"
)

func TestCorruptionItemsFromCandidates(t *testing.T) {
	now := time.Now().UTC()
	rows := []corruptionCandidate{
		{ID: 7, Title: "عنوان مصاب", Content: "النص قبل $1 وبعده", MetaDescription: "", Published: true, UpdatedAt: now},
		{ID: 8, Title: "عنوان سليم", Content: "السعر $10 فقط", Published: false, UpdatedAt: now},
	}
	items := corruptionItemsFromCandidates("jo", "article", rows, "")
	if len(items) != 1 {
		t.Fatalf("expected 1 corrupted item, got %d", len(items))
	}
	item := items[0]
	if item.ID != 7 || item.Type != "article" || item.MatchCount != 1 {
		t.Fatalf("unexpected item: %#v", item)
	}
	if item.PublicURL != "/jo/lesson/articles/7" {
		t.Fatalf("unexpected public url %q", item.PublicURL)
	}
	if item.EditURL != "/dashboard/articles/7/edit" {
		t.Fatalf("unexpected edit url %q", item.EditURL)
	}
	if item.Matches[0].Field != "content" || item.Matches[0].Token != "$1" {
		t.Fatalf("unexpected match: %#v", item.Matches[0])
	}
}

func TestCorruptionItemsSearchAndPostURLs(t *testing.T) {
	rows := []corruptionCandidate{
		{ID: 21, Title: "منشور الكيمياء", Content: "ت$2جربة", Published: true},
		{ID: 22, Title: "منشور آخر", Content: "نص $3 مصاب", Published: true},
	}
	items := corruptionItemsFromCandidates("jo", "post", rows, "كيمياء")
	if len(items) != 1 || items[0].ID != 21 {
		t.Fatalf("unexpected filtered items: %#v", items)
	}
	if items[0].PublicURL != "/jo/posts/21" || items[0].EditURL != "/dashboard/posts/21/edit" {
		t.Fatalf("unexpected post urls: %#v", items[0])
	}
}

func TestSummarizeCorruption(t *testing.T) {
	items := []corruptionItem{
		{Type: "article", Published: true, MatchCount: 3},
		{Type: "article", Published: false, MatchCount: 1},
		{Type: "post", Published: true, MatchCount: 2},
	}
	summary := summarizeCorruption(items)
	if summary.Total != 3 || summary.Articles != 2 || summary.Posts != 1 || summary.Published != 2 || summary.Drafts != 1 || summary.Matches != 6 {
		t.Fatalf("unexpected summary: %#v", summary)
	}
}

func TestCorruptionCountryIsStrict(t *testing.T) {
	for _, country := range []string{"jo", "sa", "eg", "ps"} {
		got, ok := corruptionCountry(country)
		if !ok || got != country {
			t.Fatalf("expected %q to be accepted, got %q %v", country, got, ok)
		}
	}
	if _, ok := corruptionCountry("xx"); ok {
		t.Fatal("unexpected acceptance of unsupported country")
	}
}
