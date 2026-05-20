package services

import (
	"fmt"
	"strings"
	"testing"
)

func TestNewAIServiceUsesEnvFallbacksAndOverrides(t *testing.T) {
	t.Setenv("TOGETHER_AI_KEY", "legacy-key")
	t.Setenv("TOGETHER_AI_BASE_URL", "https://example.test/v1/")
	t.Setenv("TOGETHER_AI_MODEL", "primary-model")
	t.Setenv("TOGETHER_AI_FALLBACK_MODELS", "primary-model, fallback-a, fallback-a, fallback-b")

	svc := NewAIService("").(*aiService)

	if svc.apiKey != "legacy-key" {
		t.Fatalf("expected legacy env key, got %q", svc.apiKey)
	}
	if svc.baseURL != "https://example.test/v1" {
		t.Fatalf("expected trimmed base URL, got %q", svc.baseURL)
	}
	if svc.model != "primary-model" {
		t.Fatalf("expected env model override, got %q", svc.model)
	}

	wantFallbacks := []string{"fallback-a", "fallback-b"}
	if strings.Join(svc.fallbackModels, ",") != strings.Join(wantFallbacks, ",") {
		t.Fatalf("expected fallbacks %v, got %v", wantFallbacks, svc.fallbackModels)
	}
}

func TestNewAIServiceUsesOfficialDefaults(t *testing.T) {
	t.Setenv("TOGETHER_AI_API_KEY", "api-key")
	t.Setenv("TOGETHER_AI_KEY", "")
	t.Setenv("TOGETHER_API_KEY", "")
	t.Setenv("TOGETHER_AI_BASE_URL", "")
	t.Setenv("TOGETHER_AI_MODEL", "")
	t.Setenv("TOGETHER_AI_FALLBACK_MODELS", "")

	svc := NewAIService("").(*aiService)

	if svc.baseURL != defaultAIBaseURL {
		t.Fatalf("expected default base URL %q, got %q", defaultAIBaseURL, svc.baseURL)
	}
	if svc.model != defaultAIModel {
		t.Fatalf("expected default model %q, got %q", defaultAIModel, svc.model)
	}
	if len(svc.fallbackModels) == 0 {
		t.Fatal("expected at least one default fallback model")
	}
}

func TestParseSEOArticleRepairsRawNewlines(t *testing.T) {
	raw := "noise\n```json\n{\n" +
		"  \"meta_description\": \"وصف تعليمي طويل بما يكفي لشرح قيمة المقال بطريقة واضحة ومباشرة للقارئ المستهدف.\",\n" +
		"  \"keywords\": [\"التعلم\", \"الطالب\", \"المعلم\", \"الدراسة\", \"المهارات\"],\n" +
		"  \"faq\": [{\"question\": \"ما فائدة المقال؟\", \"answer\": \"يساعد القارئ على فهم الفكرة وتطبيقها بخطوات واضحة ومباشرة.\"}],\n" +
		"  \"content\": \"السطر الأول\nالسطر الثاني\"\n" +
		"}\n```\ntail"

	article, err := parseSEOArticle(raw)
	if err != nil {
		t.Fatalf("parseSEOArticle returned error: %v", err)
	}
	if !strings.Contains(article.Content, "\n") {
		t.Fatalf("expected repaired content to preserve newline, got %q", article.Content)
	}
}

func TestCleanAndValidateSEOArticle(t *testing.T) {
	article := cleanSEOArticle(&SEOArticle{
		MetaDescription: testMetaDescription(),
		Keywords:        []string{"التعلم", "تنظيم الوقت", "مهارات الدراسة", "الطالب", "المعلم"},
		FAQ: []FAQItem{
			{
				Question: "كيف يستفيد الطالب من هذه المهارات؟",
				Answer:   "يستفيد الطالب عندما يحول المهارة إلى عادة يومية واضحة يمكن قياسها ومراجعتها مع الوقت.",
			},
			{
				Question: "ما دور المعلم في تحسين التعلم؟",
				Answer:   "يساعد المعلم عبر تنظيم الخطوات وتقديم أمثلة قريبة من مستوى الطالب واحتياجاته.",
			},
		},
		Content: longArabicContent(24),
	}, "مهارات التعلم الفعال للطلاب", true, "article")

	if err := validateSEOArticle(article); err != nil {
		t.Fatalf("expected article to validate, got %v", err)
	}
	if article.WordCount < minSEOArticleWords {
		t.Fatalf("expected word count >= %d, got %d", minSEOArticleWords, article.WordCount)
	}
	if !strings.Contains(article.ContentHTML, "<h2>") {
		t.Fatalf("expected formatted HTML to include headings, got %q", article.ContentHTML[:min(80, len(article.ContentHTML))])
	}
	if !strings.Contains(article.SchemaHTML, "application/ld+json") {
		t.Fatal("expected schema HTML to include JSON-LD")
	}
	if !strings.Contains(article.SchemaHTML, "abstract") {
		t.Fatal("expected schema HTML to include featured snippet abstract")
	}
	if article.Slug == "" || article.Slug == "article" {
		t.Fatalf("expected Arabic slug, got %q", article.Slug)
	}
	if article.FeaturedSnippet == "" {
		t.Fatal("expected featured snippet to be generated")
	}
	if len(article.TitleVariants) == 0 {
		t.Fatal("expected title variants to be generated")
	}
	if len(article.InternalLinks) == 0 {
		t.Fatal("expected internal links to be generated")
	}
	if !strings.Contains(article.ContentHTML, `class="related-links"`) {
		t.Fatal("expected related links to be injected into content HTML")
	}
	if article.SEOScore < 75 {
		t.Fatalf("expected publishable SEO score, got %d issues=%v", article.SEOScore, article.SEOIssues)
	}
}

func TestValidateSEOArticleRejectsWeakContent(t *testing.T) {
	t.Run("short", func(t *testing.T) {
		article := cleanSEOArticle(&SEOArticle{
			MetaDescription: testMetaDescription(),
			Keywords:        []string{"التعلم", "الطالب", "المعلم"},
			Content:         "شرح قصير لا يكفي لبناء مقال تعليمي مفيد.",
		}, "مهارات التعلم الفعال للطلاب", true, "article")

		if err := validateSEOArticle(article); err == nil {
			t.Fatal("expected short content to fail validation")
		}
	})

	t.Run("repetitive", func(t *testing.T) {
		article := cleanSEOArticle(&SEOArticle{
			MetaDescription: testMetaDescription(),
			Keywords:        []string{"التعلم", "الطالب", "المعلم", "الدراسة", "المهارات"},
			Content:         strings.Repeat("الطالب يحتاج إلى شرح واضح ومنظم يساعده على فهم الدرس بطريقة عملية. ", 80),
		}, "مهارات التعلم الفعال للطلاب", true, "article")

		if err := validateSEOArticle(article); err == nil {
			t.Fatal("expected repetitive content to fail validation")
		}
	})
}

func TestCleanContentAppliesArabicCorrections(t *testing.T) {
	input := "عدم المفاهيم يضعف هذا الدقة ويجعل النشاط مكماًلاً بصعوبة، بينما interaction humanي مهم لل نجاح. هذا الخطة تحتاج شرحاً  ،وتنظيماً جيداً."

	got := cleanContent(input, true)

	for _, bad := range []string{"عدم المفاهيم", "هذا الدقة", "مكماًلاً", "لل نجاح", "interaction humanي", "humanي", "هذا الخطة", "  ،"} {
		if strings.Contains(got, bad) {
			t.Fatalf("expected %q to be removed from %q", bad, got)
		}
	}

	for _, want := range []string{"عدم فهم المفاهيم", "هذه الدقة", "مكملاً", "للنجاح", "التفاعل الإنساني", "هذه الخطة", "شرحاً، وتنظيماً"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %q in %q", want, got)
		}
	}
}

func TestSplitLongArabicContent(t *testing.T) {
	input := strings.TrimSpace(strings.Repeat("هذه جملة عربية طويلة تساعد الطالب على فهم المفاهيم وتنظيم التعلم وتحسين المراجعة داخل الصف والمنزل. ", 12))

	got := splitLongArabicContent(input)

	if strings.Count(got, "\n\n") < 2 {
		t.Fatalf("expected long content to be split into paragraphs, got %q", got)
	}

	for _, paragraph := range strings.Split(got, "\n\n") {
		if words := countWords(paragraph); words > 80 {
			t.Fatalf("expected paragraph to stay reasonably sized, got %d words in %q", words, paragraph)
		}
	}
}

func TestSplitLongArabicContentKeepsAlreadySplitContent(t *testing.T) {
	input := "فقرة أولى.\n\nفقرة ثانية.\n\nفقرة ثالثة.\n\nفقرة رابعة.\n\nفقرة خامسة."

	if got := splitLongArabicContent(input); got != input {
		t.Fatalf("expected already split content to remain unchanged, got %q", got)
	}
}

func TestImproveHeadings(t *testing.T) {
	input := "مقدمة قصيرة\nالهدف هو تحسين مستوى الطالب\nتفاصيل الخطة"

	got := improveHeadings(input)

	if strings.Contains(got, "الهدف هو") {
		t.Fatalf("expected weak heading to be replaced, got %q", got)
	}
	if !strings.Contains(got, "أهداف الخطة العلاجية") {
		t.Fatalf("expected improved heading in %q", got)
	}
}

func TestGenerateFeaturedSnippet(t *testing.T) {
	article := &SEOArticle{Content: "الجملة الأولى تقدم شرحا عمليا واضحا للطالب والمعلم والأسرة. الجملة الثانية تضيف خطوات تطبيقية قابلة للقياس داخل الصف. الجملة الثالثة تربط الخطة بنتائج التعلم اليومية."}

	got := generateFeaturedSnippet(article)

	if got == "" {
		t.Fatal("expected featured snippet")
	}
	if len([]rune(got)) > 320 {
		t.Fatalf("expected snippet to be truncated to 320 runes, got %d", len([]rune(got)))
	}
	if !strings.HasSuffix(got, ".") && !strings.HasSuffix(got, "؟") {
		t.Fatalf("expected snippet to end with punctuation, got %q", got)
	}
}

func TestGenerateTitleVariants(t *testing.T) {
	variants := generateTitleVariants("خطة علاجية للرياضيات")

	if len(variants) < 4 {
		t.Fatalf("expected multiple title variants, got %v", variants)
	}
	for _, variant := range variants {
		if len([]rune(variant)) > 70 {
			t.Fatalf("expected variant <= 70 runes, got %q", variant)
		}
	}
}

func TestGenerateInternalLinksAndInjection(t *testing.T) {
	links := generateInternalLinks("خطة علاجية للصف الثالث", []string{"رياضيات", "تعليم"}, "article")

	if len(links) == 0 {
		t.Fatal("expected fallback internal links")
	}
	for _, link := range links {
		if !isSafeInternalURL(link.URL) {
			t.Fatalf("expected safe existing internal URL, got %+v", link)
		}
	}
	if links[0].URL == "/remedial-plans" || links[0].URL == "/exam-models" || links[0].URL == "/worksheets" {
		t.Fatalf("expected old nonexistent URLs to be removed, got %v", links)
	}

	html := injectInternalLinks("<p>محتوى تعليمي</p>", links)
	if !strings.Contains(html, `class="related-links"`) {
		t.Fatalf("expected related links box, got %q", html)
	}

	htmlAgain := injectInternalLinks(html, links)
	if strings.Count(htmlAgain, `class="related-links"`) != 1 {
		t.Fatalf("expected related links box to be injected once, got %q", htmlAgain)
	}
}

func TestInternalLinkURLSafety(t *testing.T) {
	links := uniqueInternalLinks([]InternalLink{
		{Title: "بحث", URL: "/search?q=%D8%B1%D9%8A%D8%A7%D8%B6%D9%8A%D8%A7%D8%AA"},
		{Title: "صفوف", URL: "/classes"},
		{Title: "قديم", URL: "/remedial-plans"},
		{Title: "خارجي", URL: "https://example.com"},
		{Title: "بروتوكول نسبي", URL: "//example.com"},
	})

	if len(links) != 2 {
		t.Fatalf("expected only safe known links, got %+v", links)
	}
	for _, link := range links {
		if !isSafeInternalURL(link.URL) {
			t.Fatalf("unsafe link survived: %+v", link)
		}
	}
}

func TestCalculateSEOScore(t *testing.T) {
	article := &SEOArticle{
		MetaDescription: testMetaDescription(),
		Keywords:        []string{"تعليم", "مهارة", "طالب", "معلم", "خطة"},
		FAQ:             []FAQItem{{Question: "سؤال أول؟", Answer: "إجابة واضحة ومفيدة."}, {Question: "سؤال ثان؟", Answer: "إجابة عملية قابلة للتطبيق."}},
		FeaturedSnippet: "ملخص واضح ومباشر.",
		InternalLinks:   []InternalLink{{Title: "بحث تعليمي", URL: "/search?q=%D8%AE%D8%B7%D8%A9"}},
		WordCount:       450,
	}

	score, issues := calculateSEOScore(article)
	if score != 100 || len(issues) != 0 {
		t.Fatalf("expected perfect score, got %d issues=%v", score, issues)
	}

	article.FeaturedSnippet = ""
	article.InternalLinks = nil
	score, issues = calculateSEOScore(article)
	if score >= 100 || len(issues) == 0 {
		t.Fatalf("expected lower score with issues, got %d issues=%v", score, issues)
	}
}

func TestMakeSlugFallback(t *testing.T) {
	if got := makeSlug("!!!"); got != "article" {
		t.Fatalf("expected fallback slug, got %q", got)
	}
}

func testMetaDescription() string {
	return "دليل تعليمي عملي يساعد الطالب وولي الأمر والمعلم على فهم الموضوع بخطوات واضحة وأمثلة قابلة للتطبيق داخل الصف والمنزل."
}

func longArabicContent(sentences int) string {
	words := []string{
		"تعليم", "مهارة", "طالب", "معلم", "أسرة", "درس", "فكرة", "تطبيق",
		"مراجعة", "تخطيط", "تركيز", "تقييم", "نشاط", "مثال", "حوار", "تقدم",
		"قراءة", "كتابة", "فهم", "تنظيم", "ملاحظة", "تدريب", "هدف", "نتيجة",
	}

	var b strings.Builder
	b.WriteString("أهمية الموضوع\n\n")

	for i := 0; i < sentences; i++ {
		for j := 0; j < 24; j++ {
			fmt.Fprintf(&b, "%s%d ", words[(i+j)%len(words)], i*100+j+1)
		}
		b.WriteString(". ")

		if (i+1)%6 == 0 {
			b.WriteString("\n\nخطوات تطبيقية\n\n")
		}
	}

	return strings.TrimSpace(b.String())
}
