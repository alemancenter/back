package contentaudit

import (
	"strings"
	"testing"
)

func TestGroundedFixValidationPassesRequiresThresholdAndZeroUnsupported(t *testing.T) {
	cases := []struct {
		name string
		in   groundedValidation
		want bool
	}{
		{name: "passes", in: groundedValidation{GroundingScore: 95}, want: true},
		{name: "below threshold", in: groundedValidation{GroundingScore: 89}, want: false},
		{name: "unsupported blocks high score", in: groundedValidation{GroundingScore: 100, UnsupportedClaims: []string{"ادعاء غير موثق"}}, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := groundedFixValidationPasses(normalizeGroundedFixValidation(tc.in)); got != tc.want {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}

func TestGroundedValidationFailureDetailSurfacesUnsupportedClaims(t *testing.T) {
	validation := groundedValidation{
		GroundingScore: 75,
		UnsupportedClaims: []string{
			"المستند يتبع الصف/المرحلة 12.",
			"ادعاء ثان غير مدعوم.",
			"ادعاء ثالث غير مدعوم.",
			"ادعاء رابع لا ينبغي عرضه بسبب الحد.",
		},
	}
	detail := groundedValidationFailureDetail(validation)
	if !strings.Contains(detail, "grounding_score=75") || !strings.Contains(detail, "unsupported_claims=4") {
		t.Fatalf("missing summary: %s", detail)
	}
	if !strings.Contains(detail, "الصف/المرحلة 12") || !strings.Contains(detail, "ادعاء ثالث") {
		t.Fatalf("missing unsupported claim details: %s", detail)
	}
	if strings.Contains(detail, "ادعاء رابع") {
		t.Fatalf("detail should be bounded to three claims: %s", detail)
	}
}

func TestNormalizeGroundedFixDraftUsesFallbackTitleAndCleansFields(t *testing.T) {
	draft := groundedDraft{
		Title:           "   ",
		ContentHTML:     "<p> محتوى موثق </p>",
		MetaDescription: "  وصف  ",
		Keywords:        []string{" تربية ", "تربية", " إسلامية "},
		UsedFactIndexes: []int{0},
	}
	got := normalizeGroundedFixDraft(draft, " العنوان الأصلي ")
	if got.Title != "العنوان الأصلي" {
		t.Fatalf("unexpected title: %q", got.Title)
	}
	if got.MetaDescription != "وصف" {
		t.Fatalf("unexpected meta: %q", got.MetaDescription)
	}
	if len(got.Keywords) != 2 || got.Keywords[0] != "تربية" || got.Keywords[1] != "إسلامية" {
		t.Fatalf("unexpected keywords: %#v", got.Keywords)
	}
}

func TestValidateGroundedFixDraftRejectsInvalidFactIndexes(t *testing.T) {
	draft := groundedDraft{ContentHTML: "<p>مسودة جديدة موثقة</p>", UsedFactIndexes: []int{2}}
	if err := validateGroundedFixDraft(draft, "محتوى قديم", 2); err == nil {
		t.Fatal("expected invalid fact index to be rejected")
	}
}

// Reproduces a real generated title that leaked the pipeline's own internal
// processing vocabulary into a reader-facing field: "تحضير دروس اللغة
// الإنجليزية للصف الحادي عشر (وفقًا للمرفق) - ملاحظات حول الصف الثاني عشر في
// قاعدة البيانات". The writer prompt now instructs against this explicitly,
// but normalizeGroundedFixDraft is the deterministic backstop that must catch
// it even if a model ignores the instruction.
func TestNormalizeGroundedFixDraftRejectsTitleLeakingInternalTerms(t *testing.T) {
	cases := []string{
		"تحضير دروس اللغة الإنجليزية للصف الحادي عشر (وفقًا للمرفق) - ملاحظات حول الصف الثاني عشر في قاعدة البيانات",
		"شرح الدرس - كما ورد في الملف المرفق",
		"ملخص المصدر الموثوق لهذا الموضوع",
	}
	for _, title := range cases {
		draft := groundedDraft{Title: title, ContentHTML: "<p>محتوى</p>"}
		got := normalizeGroundedFixDraft(draft, "العنوان الأصلي السليم")
		if got.Title != "العنوان الأصلي السليم" {
			t.Fatalf("title leaking internal terms should fall back to original, got: %q", got.Title)
		}
	}

	// A clean title with none of the banned terms must pass through untouched.
	draft := groundedDraft{Title: "قواعد اللغة الإنجليزية للصف الحادي عشر", ContentHTML: "<p>محتوى</p>"}
	got := normalizeGroundedFixDraft(draft, "عنوان آخر")
	if got.Title != "قواعد اللغة الإنجليزية للصف الحادي عشر" {
		t.Fatalf("clean title should not be replaced, got: %q", got.Title)
	}
}
