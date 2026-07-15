package chatbot

import (
	"testing"

	repo "github.com/alemancenter/fiber-api/internal/repositories/chatbot"
)

func TestTrainingPatternsIntentPriority(t *testing.T) {
	tests := []struct {
		message string
		want    string
	}{
		{"أريد تصفح الصفوف التعليمية", "open_classes"},
		{"عرض الصفوف التعليمية", "open_classes"},
		{"فتح صفحة البحث", "open_search"},
		{"لم تصلني رسالة استعادة كلمة المرور", "password_reset_problem"},
		{"عملت تحميل ولا أعرف أين أجد الملف", "download_location"},
	}

	for _, tt := range tests {
		got, _ := detectIntent(tt.message)
		if got != tt.want {
			t.Fatalf("detectIntent(%q) = %q, want %q", tt.message, got, tt.want)
		}
	}
}

func TestTrainingPatternsExtractEducationalBookQuery(t *testing.T) {
	entities := extractSearchEntities("اريد كتاب الصف الرابع الاساسي لمادة اللغة العربية الفصل الدراسي الثاني")

	if entities.ContentType != "كتب" {
		t.Fatalf("ContentType = %q, want كتب", entities.ContentType)
	}
	if entities.Grade != "الصف الرابع" {
		t.Fatalf("Grade = %q, want الصف الرابع", entities.Grade)
	}
	if entities.Subject != "اللغة العربية" {
		t.Fatalf("Subject = %q, want اللغة العربية", entities.Subject)
	}
	if entities.Semester != "الفصل الثاني" {
		t.Fatalf("Semester = %q, want الفصل الثاني", entities.Semester)
	}
}

func TestContentSearchDoesNotUseGenerativeAI(t *testing.T) {
	links := []repo.ContentResult{{Title: "كتاب اللغة العربية الصف الرابع", URL: "/jo/files/1"}}
	if shouldUseChatbotAI("اريد كتاب الصف الرابع", "search_content", "content_followup", "content_search", links, 0.9) {
		t.Fatal("content search should stay rule/database based and not use generative AI")
	}
}
