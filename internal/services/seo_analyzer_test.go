package services

import (
	"strings"
	"testing"
)

func TestAnalyzeSEOArabicContent(t *testing.T) {
	paragraph := "التعلم النشط أسلوب تعليمي يجعل الطالب مشاركًا في الدرس ويعتمد على أسئلة واضحة وأنشطة مناسبة تساعد على الفهم والتطبيق. "
	content := `<h2>خطوات التعلم النشط</h2><p>` + strings.Repeat(paragraph, 45) + `</p><a href="/jo/posts/2">دليل مرتبط</a><a href="https://example.edu/source">مصدر</a><img src="lesson.jpg" alt="طلاب يطبقون التعلم النشط">`
	result := AnalyzeSEO(SEOAnalysisInput{
		Title:           "التعلم النشط: دليل عملي للمعلم والطالب",
		Content:         content,
		MetaDescription: "دليل تطبيقي شامل حول التعلم النشط، يوضح خطوات التخطيط والتنفيذ والتقويم مع أنشطة صفية مفيدة للمعلم والطالب في البيئة التعليمية.",
		FocusKeyword:    "التعلّم النشط",
	})
	if result.Score < 75 {
		t.Fatalf("expected a strong score, got %d: %#v", result.Score, result.Checks)
	}
	if result.InternalLinkCount != 1 || result.ExternalLinkCount != 1 {
		t.Fatalf("unexpected link counts: internal=%d external=%d", result.InternalLinkCount, result.ExternalLinkCount)
	}
	if result.MissingAltCount != 0 {
		t.Fatalf("expected image alt to pass, got %d", result.MissingAltCount)
	}
	if result.KeywordCount == 0 {
		t.Fatal("Arabic diacritics should not prevent keyword matching")
	}
}

func TestAnalyzeSEODetectsThinStuffedContent(t *testing.T) {
	result := AnalyzeSEO(SEOAnalysisInput{Title: "قصير", Content: "<p>رياضيات رياضيات رياضيات رياضيات</p><img src='x.jpg'>", FocusKeyword: "رياضيات"})
	if result.Score >= 50 {
		t.Fatalf("expected poor score, got %d", result.Score)
	}
	if result.MissingAltCount != 1 {
		t.Fatalf("expected one missing alt, got %d", result.MissingAltCount)
	}
	var stuffing, description bool
	for _, check := range result.Checks {
		if check.Code == "keyword_density" && check.Status == "error" {
			stuffing = true
		}
		if check.Code == "description_presence" && check.Status == "error" {
			description = true
		}
	}
	if !stuffing || !description {
		t.Fatalf("expected stuffing and missing-description checks: %#v", result.Checks)
	}
}

func TestNormalizeSEOPathAndTargets(t *testing.T) {
	if got := normalizeSEOPath("https://imanjo.com/jo/old/?x=1"); got != "/jo/old" {
		t.Fatalf("unexpected normalized path %q", got)
	}
	if !validSEORedirectTarget("/jo/new") || !validSEORedirectTarget("https://example.com/new") {
		t.Fatal("valid redirect targets rejected")
	}
	if validSEORedirectTarget("javascript:alert(1)") || validSEORedirectTarget("//evil.example") {
		t.Fatal("unsafe redirect target accepted")
	}
	if !seoPathHasPrefix("/old/lesson/1", "/old") || seoPathHasPrefix("/older/lesson/1", "/old") {
		t.Fatal("prefix redirects must match a complete path segment")
	}
	if !seoRedirectRuleMatches("/old", "exact", "/old") || seoRedirectRuleMatches("/old", "exact", "/old/child") {
		t.Fatal("exact redirect matching is incorrect")
	}
	if !seoRedirectRuleMatches("/old", "prefix", "/old/child") || seoRedirectRuleMatches("/old", "prefix", "/older") {
		t.Fatal("prefix redirect matching is incorrect")
	}
	if !seoRedirectRuleMatches(`^/archive/[0-9]+$`, "regex", "/archive/42") {
		t.Fatal("regex redirect matching is incorrect")
	}
	if target, local := seoLocalRedirectURL("https://imanjo.com/jo/new?ref=1", "imanjo.com"); !local || target.Path != "/jo/new" {
		t.Fatal("same-host absolute redirect should participate in loop validation")
	}
	if _, local := seoLocalRedirectURL("https://example.com/jo/new", "imanjo.com"); local {
		t.Fatal("external redirect must not be followed as a local chain")
	}
}

func TestSEO404PrivacySanitizers(t *testing.T) {
	query := sanitizeSEOQuery("page=2&token=secret&email=user%40example.com")
	if strings.Contains(query, "secret") || strings.Contains(query, "user%40example.com") || !strings.Contains(query, "%5Bredacted%5D") {
		t.Fatalf("sensitive query values were not redacted: %q", query)
	}
	referrer := sanitizeSEOReferrer("https://example.com/path?token=secret#section")
	if referrer != "https://example.com/path" {
		t.Fatalf("unexpected sanitized referrer: %q", referrer)
	}
}

func TestParseSEOContentLink(t *testing.T) {
	target, ok := parseSEOContentLink("https://imanjo.com/sa/lesson/articles/42?ref=nav", "imanjo.com")
	if !ok || target.CountryCode != "sa" || target.ContentType != "article" || target.ContentID != 42 {
		t.Fatalf("unexpected content link target: %#v, ok=%v", target, ok)
	}
	if _, ok := parseSEOContentLink("https://example.com/sa/lesson/articles/42", "imanjo.com"); ok {
		t.Fatal("external links must not be treated as internal content targets")
	}
	if _, ok := parseSEOContentLink("/jo/posts/not-a-number", "imanjo.com"); ok {
		t.Fatal("malformed content IDs must be ignored")
	}
}
