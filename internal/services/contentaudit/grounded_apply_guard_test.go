package contentaudit

import (
	"testing"

	"github.com/imanjo/fiber-api/internal/models"
)

func TestGroundedSourceMatchesCurrentPreviewSource(t *testing.T) {
	meta := "وصف تعريفي حالي"
	originalKeywords := "رياضيات، الصف السابع"
	currentKeywords := "الصف السابع, رياضيات"
	preview := &models.ContentAIFixPreview{
		OriginalTitle:           "عنوان الدرس",
		OriginalContent:         "<p>نص الدرس</p>",
		OriginalMetaDescription: &meta,
		OriginalKeywords:        &originalKeywords,
	}

	if !groundedSourceMatches(preview.OriginalTitle, preview.OriginalContent, &meta, &currentKeywords, preview) {
		t.Fatal("expected an unchanged source with reordered keywords to match")
	}
}

func TestGroundedSourceMatchesRejectsStaleFields(t *testing.T) {
	meta := "الوصف الأصلي"
	keywords := "علوم، درس"
	preview := &models.ContentAIFixPreview{
		OriginalTitle:           "العنوان الأصلي",
		OriginalContent:         "<p>المحتوى الأصلي</p>",
		OriginalMetaDescription: &meta,
		OriginalKeywords:        &keywords,
	}

	tests := []struct {
		name     string
		title    string
		content  string
		meta     string
		keywords string
	}{
		{name: "title changed", title: "عنوان محدث", content: preview.OriginalContent, meta: meta, keywords: keywords},
		{name: "content changed", title: preview.OriginalTitle, content: "<p>محتوى محدث</p>", meta: meta, keywords: keywords},
		{name: "meta changed", title: preview.OriginalTitle, content: preview.OriginalContent, meta: "وصف محدث", keywords: keywords},
		{name: "keywords changed", title: preview.OriginalTitle, content: preview.OriginalContent, meta: meta, keywords: "علوم، اختبار"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if groundedSourceMatches(test.title, test.content, &test.meta, &test.keywords, preview) {
				t.Fatal("expected a changed source to be rejected")
			}
		})
	}
}
