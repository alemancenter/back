package contentaudit

import (
	"context"
	"strings"
	"testing"

	coreai "github.com/imanjo/fiber-api/internal/services"
)

type stubSEOAI struct {
	resp *coreai.ContentIntelligenceResponse
	err  error
}

func (s stubSEOAI) GenerateSEOArticle(string, string) (*coreai.SEOArticle, error) { return nil, nil }
func (s stubSEOAI) GenerateSEOArticleWithContext(string, string, coreai.SEOGenerationContext) (*coreai.SEOArticle, error) {
	return nil, nil
}
func (s stubSEOAI) GenerateArticleContent(string) (string, error) { return "", nil }
func (s stubSEOAI) RunContentIntelligence(context.Context, coreai.ContentIntelligenceRequest) (*coreai.ContentIntelligenceResponse, error) {
	return s.resp, s.err
}

const seoBundleContent = "<h2>التعلم النشط في الصف</h2><p>" +
	"يقوم التعلم النشط على إشراك الطالب في بناء المعرفة عبر أسئلة وأنشطة صفية واضحة تساعد على الفهم والتطبيق. " +
	"يوضح هذا الشرح خطوات التخطيط للدرس وتنفيذ الأنشطة وتقويم أداء الطلاب في بيئة تعليمية آمنة ومنظمة. " +
	"</p>"

func TestGenerateDraftSEOBundleWhitelistsSchemaAndGroundsKeyword(t *testing.T) {
	ai := stubSEOAI{resp: &coreai.ContentIntelligenceResponse{
		SEOTitle:              "التعلم النشط: دليل عملي للمعلم والطالب في الصف",
		SEOFocusKeyword:       "عبارة غير موجودة إطلاقا",
		FixedMetaDescription:  "يوضح هذا الشرح خطوات التخطيط للدرس وتنفيذ الأنشطة وتقويم أداء الطلاب في بيئة تعليمية آمنة ومنظمة مناسبة للطالب.",
		SEOAdditionalKeywords: "التعلم التفاعلي, الأنشطة الصفية, مشاركة الطلاب",
		SEOSchemaType:         "FAQPage", // not in the analyzer-safe subset
	}}
	bundle, provider, _, err := GenerateDraftSEOBundle(context.Background(), ai, SEOOptimizeInput{
		Title:        "التعلم النشط",
		ContentHTML:  seoBundleContent,
		FocusKeyword: "التعلم النشط",
		ContentType:  "article",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if provider == "" {
		t.Fatal("expected a provider label")
	}
	if !seoOptimizeSchemaTypes[bundle.SchemaType] {
		t.Fatalf("schema type not whitelisted: %q", bundle.SchemaType)
	}
	if strings.Contains(bundle.FocusKeyword, "غير موجودة") {
		t.Fatalf("ungrounded focus keyword was accepted: %q", bundle.FocusKeyword)
	}
	if !seoPhraseInText(bundle.FocusKeyword, normalizePlainText(seoBundleContent), "التعلم النشط") {
		t.Fatalf("final focus keyword %q is not present in the source", bundle.FocusKeyword)
	}
	if bundle.MetaDescription == "" || bundle.SEOTitle == "" {
		t.Fatalf("bundle missing core fields: %#v", bundle)
	}
	if bundle.OGTitle == "" || bundle.TwitterDescription == "" {
		t.Fatalf("social fields not backfilled: %#v", bundle)
	}
}

func TestGenerateDraftSEOBundleDegradesWithoutAI(t *testing.T) {
	bundle, provider, _, err := GenerateDraftSEOBundle(context.Background(), nil, SEOOptimizeInput{
		Title:       "توزيع علامات مبحث الرياضيات",
		ContentHTML: strings.Repeat("<p>يشرح هذا الدرس توزيع العلامات على المهارات والأسئلة التعليمية وفق محتوى الصفحة.</p>", 6),
		ContentType: "post",
	})
	if err != nil {
		t.Fatalf("nil AI must degrade gracefully, got: %v", err)
	}
	if provider != "extractive_fallback" {
		t.Fatalf("expected extractive_fallback provider, got %q", provider)
	}
	if bundle.MetaDescription == "" || bundle.FocusKeyword == "" || bundle.SEOTitle == "" {
		t.Fatalf("deterministic bundle missing fields: %#v", bundle)
	}
	if bundle.SchemaType != "BlogPosting" {
		t.Fatalf("post default schema should be BlogPosting, got %q", bundle.SchemaType)
	}
}

func TestGenerateDraftSEOBundleRejectsEmptySource(t *testing.T) {
	if _, _, _, err := GenerateDraftSEOBundle(context.Background(), nil, SEOOptimizeInput{}); err != ErrDraftAssistEmptySource {
		t.Fatalf("expected ErrDraftAssistEmptySource, got %v", err)
	}
}
