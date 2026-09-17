package services

import "testing"

func TestInsertSEOHeading_AddsExactlyOneHeadingBeforeSecondParagraph(t *testing.T) {
	raw := "الأولى.\n\nالثانية.\n\nالثالثة."
	got := insertSEOHeading(raw, "عنوان الاختبار")
	want := "الأولى.\n\n## شرح عنوان الاختبار\n\nالثانية.\n\nالثالثة."
	if got != want {
		t.Fatalf("insertSEOHeading mismatch:\ngot:  %q\nwant: %q", got, want)
	}
}

func TestInsertSEOHeading_NoOpOnSingleParagraph(t *testing.T) {
	raw := "فقرة واحدة فقط بلا فواصل."
	if got := insertSEOHeading(raw, "عنوان"); got != raw {
		t.Fatalf("expected single-paragraph input unchanged, got %q", got)
	}
}

func TestPlainTextToSafeHTML_RendersHeadingMarkerAsH2(t *testing.T) {
	raw := "مقدمة.\n\n## شرح موضوع القاعدة\n\nخاتمة."
	got := plainTextToSafeHTML(raw)
	if want := "<p>مقدمة.</p><h2>شرح موضوع القاعدة</h2><p>خاتمة.</p>"; got != want {
		t.Fatalf("plainTextToSafeHTML mismatch:\ngot:  %q\nwant: %q", got, want)
	}
}
