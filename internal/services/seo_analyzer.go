package services

import (
	"html"
	"math"
	"regexp"
	"strings"
	"unicode"
)

type SEOAnalysisInput struct {
	Title           string `json:"title"`
	Content         string `json:"content"`
	MetaDescription string `json:"meta_description"`
	FocusKeyword    string `json:"focus_keyword"`
	CanonicalURL    string `json:"canonical_url"`
	ImageURL        string `json:"image_url"`
	SchemaType      string `json:"schema_type"`
	SchemaJSON      string `json:"schema_json"`
}

type SEOAnalysisCheck struct {
	Code           string `json:"code"`
	Status         string `json:"status"`
	Points         int    `json:"points"`
	MaxPoints      int    `json:"max_points"`
	Message        string `json:"message"`
	Recommendation string `json:"recommendation,omitempty"`
}

type SEOAnalysisResult struct {
	Score             int                `json:"score"`
	Status            string             `json:"status"`
	WordCount         int                `json:"word_count"`
	KeywordCount      int                `json:"keyword_count"`
	KeywordDensity    float64            `json:"keyword_density"`
	InternalLinkCount int                `json:"internal_link_count"`
	ExternalLinkCount int                `json:"external_link_count"`
	MissingAltCount   int                `json:"missing_alt_count"`
	Checks            []SEOAnalysisCheck `json:"checks"`
}

var (
	seoTagPattern      = regexp.MustCompile(`(?is)<[^>]*>`)
	seoScriptPattern   = regexp.MustCompile(`(?is)<(?:script|style)\b[^>]*>.*?</(?:script|style)>`)
	seoHeadingPattern  = regexp.MustCompile(`(?is)<h([1-6])\b[^>]*>(.*?)</h[1-6]>`)
	seoLinkPattern     = regexp.MustCompile(`(?is)<a\b[^>]*href\s*=\s*["']([^"']+)["'][^>]*>`)
	seoImagePattern    = regexp.MustCompile(`(?is)<img\b([^>]*)>`)
	seoAltPattern      = regexp.MustCompile(`(?is)\balt\s*=\s*["']\s*[^"']+\s*["']`)
	seoSentencePattern = regexp.MustCompile(`[.!?؟؛\n]+`)
	seoWhitespace      = regexp.MustCompile(`\s+`)
)

func AnalyzeSEO(input SEOAnalysisInput) SEOAnalysisResult {
	title := strings.TrimSpace(html.UnescapeString(input.Title))
	description := strings.TrimSpace(html.UnescapeString(input.MetaDescription))
	plain := seoPlainText(input.Content)
	words := seoWords(plain)
	keyword := normalizeSEOArabic(input.FocusKeyword)
	keywordCount := countSEOPhrase(normalizeSEOArabic(plain), keyword)
	density := 0.0
	if len(words) > 0 && keyword != "" {
		density = math.Round((float64(keywordCount)*float64(len(seoWords(keyword)))/float64(len(words))*100)*100) / 100
	}
	internalLinks, externalLinks := countSEOLinks(input.Content)
	missingAlt := countMissingImageAlts(input.Content)
	headings := seoHeadingPattern.FindAllStringSubmatch(input.Content, -1)

	checks := make([]SEOAnalysisCheck, 0, 18)
	add := func(code, status string, points, max int, message, recommendation string) {
		checks = append(checks, SEOAnalysisCheck{Code: code, Status: status, Points: points, MaxPoints: max, Message: message, Recommendation: recommendation})
	}

	switch l := len([]rune(title)); {
	case l == 0:
		add("title_presence", "error", 0, 12, "عنوان SEO غير موجود", "أضف عنوانًا واضحًا يصف الصفحة.")
	case l >= 35 && l <= 65:
		add("title_length", "good", 12, 12, "طول العنوان مناسب لنتائج البحث", "")
	default:
		add("title_length", "warning", 6, 12, "طول العنوان خارج المجال المقترح (35–65 حرفًا)", "عدّل طول العنوان مع الحفاظ على دقته.")
	}
	switch l := len([]rune(description)); {
	case l == 0:
		add("description_presence", "error", 0, 12, "الوصف التعريفي غير موجود", "اكتب وصفًا فريدًا يلخص فائدة الصفحة.")
	case l >= 110 && l <= 165:
		add("description_length", "good", 12, 12, "طول الوصف التعريفي مناسب", "")
	default:
		add("description_length", "warning", 6, 12, "طول الوصف خارج المجال المقترح (110–165 حرفًا)", "اجعل الوصف موجزًا ومقنعًا.")
	}
	if keyword == "" {
		add("focus_keyword", "warning", 0, 14, "لم تُحدّد عبارة مفتاحية رئيسية", "اختر عبارة واحدة تعبّر عن نية الباحث.")
	} else {
		if strings.Contains(normalizeSEOArabic(title), keyword) {
			add("keyword_title", "good", 6, 6, "العبارة الرئيسية موجودة في العنوان", "")
		} else {
			add("keyword_title", "warning", 0, 6, "العبارة الرئيسية غير موجودة في العنوان", "أدرجها طبيعيًا إن كان ذلك دقيقًا.")
		}
		if strings.Contains(normalizeSEOArabic(description), keyword) {
			add("keyword_description", "good", 4, 4, "العبارة الرئيسية موجودة في الوصف", "")
		} else {
			add("keyword_description", "warning", 0, 4, "العبارة الرئيسية غير موجودة في الوصف", "أدرجها مرة واحدة بصورة طبيعية.")
		}
		intro := plain
		if rs := []rune(intro); len(rs) > 350 {
			intro = string(rs[:350])
		}
		if strings.Contains(normalizeSEOArabic(intro), keyword) {
			add("keyword_intro", "good", 4, 4, "العبارة الرئيسية تظهر في المقدمة", "")
		} else {
			add("keyword_intro", "warning", 0, 4, "العبارة الرئيسية لا تظهر في المقدمة", "وضّح موضوع الصفحة مبكرًا دون حشو.")
		}
	}
	switch {
	case len(words) >= 450:
		add("content_length", "good", 12, 12, "المحتوى متعمق بما يكفي", "")
	case len(words) >= 250:
		add("content_length", "warning", 8, 12, "طول المحتوى مقبول لكنه قابل للتوسعة", "أضف معلومات مفيدة لا تكرارًا.")
	default:
		add("content_length", "error", 2, 12, "المحتوى قصير", "وسّع الشرح بما يجيب عن أسئلة القارئ.")
	}
	if len(headings) > 0 {
		add("headings", "good", 8, 8, "المحتوى منظم بعناوين فرعية", "")
	} else {
		add("headings", "warning", 0, 8, "لا توجد عناوين فرعية", "قسّم المحتوى بعناوين H2 وH3 وصفية.")
	}
	if internalLinks > 0 {
		add("internal_links", "good", 7, 7, "توجد روابط داخلية", "")
	} else {
		add("internal_links", "warning", 0, 7, "لا توجد روابط داخلية", "اربط بصفحات مرتبطة تساعد القارئ.")
	}
	if externalLinks > 0 {
		add("external_links", "good", 3, 3, "توجد إحالات خارجية", "")
	} else {
		add("external_links", "notice", 1, 3, "لا توجد إحالات خارجية", "أضف مصدرًا موثوقًا عند الحاجة.")
	}
	if missingAlt == 0 {
		add("image_alts", "good", 6, 6, "كل الصور تحمل نصًا بديلًا أو لا توجد صور", "")
	} else {
		add("image_alts", "warning", 0, 6, "توجد صور بلا نص بديل", "اكتب وصفًا موجزًا ودقيقًا لكل صورة.")
	}
	if strings.TrimSpace(input.ImageURL) != "" || len(seoImagePattern.FindAllString(input.Content, -1)) > 0 {
		add("share_image", "good", 4, 4, "توجد صورة قابلة للاستخدام في العرض والمشاركة", "")
	} else {
		add("share_image", "notice", 1, 4, "لا توجد صورة رئيسية للمشاركة", "أضف صورة Open Graph مناسبة إن كانت الصفحة تستفيد من صورة.")
	}
	canonical := strings.TrimSpace(input.CanonicalURL)
	if canonical == "" {
		add("canonical", "good", 3, 3, "سيُنشأ الرابط الأساسي تلقائيًا", "")
	} else if validSEOAbsoluteURL(canonical) {
		add("canonical", "good", 3, 3, "الرابط الأساسي Canonical صالح", "")
	} else {
		add("canonical", "error", 0, 3, "الرابط الأساسي Canonical غير صالح", "استخدم رابطًا كاملاً يبدأ بـ https://.")
	}
	schemaType := strings.TrimSpace(input.SchemaType)
	advancedSchema := map[string]bool{"HowTo": true, "FAQPage": true, "WebPage": true, "Course": true, "LearningResource": true, "VideoObject": true}
	if schemaType == "" {
		add("schema", "warning", 0, 4, "نوع Schema غير محدد", "اختر النوع الأقرب لطبيعة الصفحة.")
	} else if advancedSchema[schemaType] && strings.TrimSpace(input.SchemaJSON) == "" {
		add("schema", "warning", 2, 4, "نوع Schema المختار يحتاج حقولًا مخصصة", "أدخل JSON-LD كاملاً حتى لا تُنشر بيانات منظمة ناقصة.")
	} else {
		add("schema", "good", 4, 4, "بيانات Schema مهيأة", "")
	}
	if keyword != "" {
		switch {
		case density >= 0.4 && density <= 2.5:
			add("keyword_density", "good", 6, 6, "كثافة العبارة الرئيسية طبيعية", "")
		case density > 3.5:
			add("keyword_density", "error", 0, 6, "تكرار العبارة الرئيسية مرتفع وقد يبدو حشوًا", "خفّض التكرار واستخدم تعبيرات طبيعية.")
		default:
			add("keyword_density", "warning", 3, 6, "كثافة العبارة الرئيسية منخفضة", "استخدم العبارة حين تخدم المعنى فقط.")
		}
	}
	avgSentence := averageSEOSentenceWords(plain)
	if avgSentence > 0 && avgSentence <= 24 {
		add("readability", "good", 8, 8, "متوسط طول الجملة مناسب للقراءة", "")
	} else {
		add("readability", "warning", 4, 8, "بعض الجمل طويلة أو يصعب تقسيم النص", "قسّم الجمل الطويلة وحافظ على فقرات قصيرة.")
	}

	total, maximum := 0, 0
	for _, check := range checks {
		total += check.Points
		maximum += check.MaxPoints
	}
	score := 0
	if maximum > 0 {
		score = int(math.Round(float64(total) / float64(maximum) * 100))
	}
	status := "poor"
	if score >= 80 {
		status = "good"
	} else if score >= 50 {
		status = "needs_work"
	}
	return SEOAnalysisResult{Score: score, Status: status, WordCount: len(words), KeywordCount: keywordCount, KeywordDensity: density, InternalLinkCount: internalLinks, ExternalLinkCount: externalLinks, MissingAltCount: missingAlt, Checks: checks}
}

func seoPlainText(value string) string {
	value = seoScriptPattern.ReplaceAllString(value, " ")
	value = seoTagPattern.ReplaceAllString(value, " ")
	value = html.UnescapeString(value)
	return strings.TrimSpace(seoWhitespace.ReplaceAllString(value, " "))
}

func seoWords(value string) []string {
	return strings.FieldsFunc(value, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsNumber(r) })
}

func normalizeSEOArabic(value string) string {
	value = strings.ToLower(html.UnescapeString(value))
	var out strings.Builder
	for _, r := range value {
		switch {
		case r == '\u0640', r >= '\u064b' && r <= '\u065f', r == '\u0670', r >= '\u06d6' && r <= '\u06ed':
			continue
		case strings.ContainsRune("أإآٱ", r):
			r = 'ا'
		case r == 'ى':
			r = 'ي'
		case r == 'ؤ':
			r = 'و'
		case r == 'ئ':
			r = 'ي'
		}
		if unicode.IsLetter(r) || unicode.IsNumber(r) || unicode.IsSpace(r) {
			out.WriteRune(r)
		} else {
			out.WriteRune(' ')
		}
	}
	return strings.TrimSpace(seoWhitespace.ReplaceAllString(out.String(), " "))
}

func countSEOPhrase(text, phrase string) int {
	if text == "" || phrase == "" {
		return 0
	}
	return strings.Count(" "+text+" ", " "+phrase+" ")
}

func countSEOLinks(content string) (int, int) {
	internal, external := 0, 0
	for _, match := range seoLinkPattern.FindAllStringSubmatch(content, -1) {
		href := strings.ToLower(strings.TrimSpace(match[1]))
		if href == "" || strings.HasPrefix(href, "#") || strings.HasPrefix(href, "mailto:") || strings.HasPrefix(href, "tel:") {
			continue
		}
		if strings.HasPrefix(href, "/") || strings.Contains(href, "imanjo.com") {
			internal++
		} else if strings.HasPrefix(href, "http://") || strings.HasPrefix(href, "https://") {
			external++
		}
	}
	return internal, external
}

func countMissingImageAlts(content string) int {
	missing := 0
	for _, match := range seoImagePattern.FindAllStringSubmatch(content, -1) {
		if len(match) < 2 || !seoAltPattern.MatchString(match[1]) {
			missing++
		}
	}
	return missing
}

func averageSEOSentenceWords(content string) float64 {
	parts := seoSentencePattern.Split(content, -1)
	total, sentences := 0, 0
	for _, part := range parts {
		count := len(seoWords(part))
		if count > 0 {
			total += count
			sentences++
		}
	}
	if sentences == 0 {
		return 0
	}
	return float64(total) / float64(sentences)
}
