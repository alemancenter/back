package chatbot

import (
	"strings"
	"testing"
)

func TestDownloadProblemDoesNotRunContentSearch(t *testing.T) {
	message := "المشكلة بحيث لا أستطيع تحميل الملفات"
	intent, _ := detectIntent(message)
	entities := extractSearchEntities(message)

	if intent != "download_problem" {
		t.Fatalf("intent = %q, want download_problem", intent)
	}
	if shouldRunContentSearch(intent, message, entities) {
		t.Fatal("download support question should not trigger content search results")
	}
}

func TestExplicitContentSearchStillRuns(t *testing.T) {
	message := "أريد امتحان اللغة العربية الصف الأول الفصل الثاني"
	intent, _ := detectIntent(message)
	entities := extractSearchEntities(message)

	if !shouldRunContentSearch(intent, message, entities) {
		t.Fatal("explicit educational content request should trigger content search")
	}
}

func TestChatbotIntentCoverageForRealUserPhrases(t *testing.T) {
	tests := []struct {
		name    string
		message string
		want    string
	}{
		{name: "facebook in-app download", message: "فاتح الموقع داخل فيسبوك وما بقدر احمل الملف", want: "download_problem"},
		{name: "download button stuck", message: "زر التحميل لا يستجيب والعداد واقف", want: "download_problem"},
		{name: "wrong email typo", message: "كتبت الايميل غلط وما وصلت رسالة التفعيل", want: "email_verification_problem"},
		{name: "lost email access", message: "لا أستطيع الوصول إلى بريدي القديم", want: "email_verification_problem"},
		{name: "bad search results", message: "نتائج البحث غير مناسبة وما لقيت الملف", want: "search_content"},
		{name: "country mismatch", message: "الملفات لا تطابق منهجي والدولة غلط", want: "country_or_curriculum"},
		{name: "mobile button problem", message: "زر في الموقع لا يعمل على الهاتف", want: "site_error"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _ := detectIntent(tt.message)
			if got != tt.want {
				t.Fatalf("intent = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNamedLookupFallbackTriggersForBareTitles(t *testing.T) {
	// Titles/names WITHOUT the usual search keywords (نموذج/امتحان/صف/مادة) must
	// still trigger the fallback content search — this is the reported gap.
	lookups := []string{
		"دوسية المرجع الشامل",
		"unit 3 grammar workbook",
		"أطلس المصور الحديث",
		"مذكرة الأوائل النهائية",
	}
	for _, msg := range lookups {
		intent, _ := detectIntent(msg)
		if !allowNamedLookupFallback(intent, msg) {
			t.Fatalf("bare title %q (intent %q) should allow the named-lookup fallback", msg, intent)
		}
	}
}

func TestNamedLookupFallbackSkipsSupportAndGreetings(t *testing.T) {
	// The fallback must never hijack clear support intents or greetings.
	cases := []string{
		"المشكلة بحيث لا أستطيع تحميل الملفات", // download_problem
		"مرحبا",                                 // greeting
		"شكرا",                                  // greeting/thanks
		"ملف",                                   // single short fragment
	}
	for _, msg := range cases {
		intent, _ := detectIntent(msg)
		if allowNamedLookupFallback(intent, msg) {
			t.Fatalf("message %q (intent %q) should NOT trigger the named-lookup fallback", msg, intent)
		}
	}
}

func TestMergeResetsStaleEntitiesOnNewTopic(t *testing.T) {
	// Switching subject/grade must not inherit the previous topic's content type
	// or semester, which caused stale, irrelevant search results.
	prev := searchEntities{Subject: "اللغة العربية", Grade: "الصف التاسع", ContentType: "امتحانات", Semester: "الفصل الأول"}
	got := mergeSearchEntities(prev, extractSearchEntities("تربية اسلامية الصف الاول"))
	if got.Subject != "التربية الإسلامية" {
		t.Fatalf("Subject = %q, want التربية الإسلامية", got.Subject)
	}
	if got.ContentType != "" || got.Semester != "" {
		t.Fatalf("stale entities leaked: contentType=%q semester=%q", got.ContentType, got.Semester)
	}

	// A pure refinement of the SAME topic keeps the earlier subject/grade.
	got2 := mergeSearchEntities(searchEntities{Subject: "اللغة العربية", Grade: "الصف التاسع"}, extractSearchEntities("الفصل الاول"))
	if got2.Subject != "اللغة العربية" || got2.Grade != "الصف التاسع" {
		t.Fatalf("refinement lost context: %+v", got2)
	}
}

func TestContextualSpecializedAnswers(t *testing.T) {
	tests := []struct {
		name   string
		intent string
		step   string
		needle string
	}{
		{name: "wrong email", intent: "email_verification_problem", step: "wrong_email", needle: "البريد الخاطئ"},
		{name: "in app browser", intent: "download_problem", step: "in_app_browser_download", needle: "Facebook أو Instagram"},
		{name: "download button", intent: "download_problem", step: "download_button_issue", needle: "زر التحميل لا يستجيب"},
		{name: "search refine", intent: "search_content", step: "search_refine", needle: "نوع الملف"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			answer := contextualAnswer(tt.intent, tt.step, "", "")
			if !strings.Contains(answer, tt.needle) {
				t.Fatalf("answer %q does not contain %q", answer, tt.needle)
			}
		})
	}
}
