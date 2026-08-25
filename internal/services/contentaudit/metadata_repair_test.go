package contentaudit

import (
	"strings"
	"testing"
)

func TestDeriveSafeMetaDescriptionIsBoundedAndGrounded(t *testing.T) {
	title := "دليل توزيع علامات مبحث اللغة الإنجليزية للعام الدراسي الجديد"
	plain := strings.Repeat("يوضح الدليل توزيع العلامات على المهارات والأسئلة التعليمية وفق محتوى الصفحة. ", 8)
	candidate := deriveSafeMetaDescription(title, plain)
	validated, err := validateSafeMetaDescription(candidate, title, plain)
	if err != nil {
		t.Fatalf("expected extractive metadata to validate: %v; candidate=%q", err, candidate)
	}
	if validated != candidate {
		t.Fatalf("validation changed candidate: %q != %q", validated, candidate)
	}
}

func TestValidateSafeMetaDescriptionRejectsUnsupportedURLAndNumber(t *testing.T) {
	title := "دليل تعليمي واضح لتوزيع العلامات"
	plain := strings.Repeat("يشرح المحتوى توزيع العلامات وأساليب الاستفادة من الدليل التعليمي للطلاب والمعلمين. ", 5)
	withURL := "يشرح هذا الوصف توزيع العلامات وأساليب الاستفادة من الدليل التعليمي للطلاب والمعلمين ويمكن متابعة التفاصيل كاملة عبر https://example.com الآن"
	if _, err := validateSafeMetaDescription(withURL, title, plain); err == nil {
		t.Fatal("expected URL to be rejected")
	}
	withNumber := "يشرح المحتوى توزيع العلامات وأساليب الاستفادة من الدليل التعليمي للطلاب والمعلمين مع نتيجة مضمونة بنسبة 99 بالمئة لجميع الطلاب"
	if _, err := validateSafeMetaDescription(withNumber, title, plain); err == nil {
		t.Fatal("expected unsupported number to be rejected")
	}
}
