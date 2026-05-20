package contentaudit

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/alemancenter/fiber-api/internal/config"
	"github.com/alemancenter/fiber-api/internal/database"
	"github.com/alemancenter/fiber-api/internal/models"
	"gorm.io/gorm"
)

const (
	DefaultArticleMinWords  = 300
	DefaultPostMinWords     = 300
	DefaultCommentMinWords  = 3
	defaultBatchSize        = 250
	defaultCommentBatchSize = 500
)

var (
	htmlTagPattern       = regexp.MustCompile(`(?is)<[^>]+>`)
	scriptStylePattern   = regexp.MustCompile(`(?is)<script\b[^>]*>.*?</script>|<style\b[^>]*>.*?</style>`)
	unsafeMarkupPattern  = regexp.MustCompile(`(?is)<\s*/?\s*(script|iframe)\b|on[a-z]+\s*=|javascript\s*:`)
	urlPattern           = regexp.MustCompile(`(?i)\b(?:https?://|www\.)[^\s<>"']+`)
	dangerousURIPattern  = regexp.MustCompile(`(?i)\b(?:javascript|data):`)
	nonWordTrimPattern   = regexp.MustCompile(`^[^\p{L}\p{N}]+|[^\p{L}\p{N}]+$`)
	englishWordTokenizer = regexp.MustCompile(`[a-z0-9]+`)
)

// Finding is one policy audit finding. CSV export keeps these names as columns.
type Finding struct {
	Type              string `json:"type"`
	ID                string `json:"id"`
	Title             string `json:"title"`
	Risk              string `json:"risk"`
	Reason            string `json:"reason"`
	URL               string `json:"url"`
	RecommendedAction string `json:"recommended_action"`
}

type Options struct {
	Config          *config.Config
	ArticleMinWords int
	PostMinWords    int
	CommentMinWords int
}

type ruleSet struct {
	risk   string
	action string
	terms  []string
}

type scanner struct {
	cfg              *config.Config
	findings         []Finding
	allowedLinkHosts map[string]struct{}
	articleMinWords  int
	postMinWords     int
	commentMinWords  int
}

var contentRules = []ruleSet{
	{
		risk:   "sexual_content",
		action: "Review and remove or rewrite adult/sexual content before AdSense review.",
		terms: []string{
			"porn", "porno", "xxx", "adult", "sex", "sexual", "nude", "naked", "escort", "prostitute",
			"جنس", "جنسي", "إباحي", "اباحي", "عاري", "عارية", "دعارة", "إغراء", "اغراء",
		},
	},
	{
		risk:   "violence",
		action: "Review for graphic violence; remove, rewrite, or restrict ads on the page.",
		terms: []string{
			"murder", "kill", "killing", "beheading", "bloodbath", "massacre", "weapon", "weapons", "bomb", "terrorist",
			"قتل", "يقتل", "مقتل", "ذبح", "دموي", "مجزرة", "سلاح", "أسلحة", "اسلحة", "قنبلة", "إرهاب", "ارهاب",
		},
	},
	{
		risk:   "hate",
		action: "Review for hateful or discriminatory content; remove or edit before publishing.",
		terms: []string{
			"hate speech", "racist", "nazi", "supremacist", "terror group",
			"خطاب كراهية", "عنصري", "عنصرية", "نازي", "تكفيري", "طائفي", "ازدراء",
		},
	},
	{
		risk:   "gambling",
		action: "Remove gambling promotion or keep ads disabled on this page.",
		terms: []string{
			"casino", "betting", "gambling", "poker", "roulette", "sportsbook", "lottery", "jackpot",
			"كازينو", "مراهنات", "رهان", "قمار", "بوكر", "يانصيب",
		},
	},
	{
		risk:   "drugs_or_medicine",
		action: "Review medical/drug claims and remove controlled-substance promotion.",
		terms: []string{
			"cocaine", "heroin", "meth", "marijuana", "cannabis", "weed", "xanax", "tramadol", "opioid", "viagra", "cialis",
			"كوكايين", "هيروين", "حشيش", "قنب", "ماريجوانا", "ترامادول", "مخدر", "مخدرات", "فياغرا", "دواء بدون وصفة",
		},
	},
}

var suspiciousHostTerms = []string{
	"porn", "xxx", "sex", "casino", "bet", "gambling", "poker", "viagra", "cialis", "xanax",
	"tramadol", "pharma", "weed", "cannabis", "loan", "payday", "crack", "warez", "nulled",
}

var shortenerHosts = map[string]struct{}{
	"bit.ly":      {},
	"cutt.ly":     {},
	"goo.gl":      {},
	"is.gd":       {},
	"rebrand.ly":  {},
	"shorturl.at": {},
	"t.co":        {},
	"tiny.cc":     {},
	"tinyurl.com": {},
	"ow.ly":       {},
}

var trustedExternalHosts = []string{
	"google.com",
	"googleapis.com",
	"gstatic.com",
	"youtube.com",
	"youtu.be",
	"youtube-nocookie.com",
	"vimeo.com",
	"dailymotion.com",
	"wikipedia.org",
	"archive.org",
	"un.org",
	"who.int",
}

var macroExtensions = map[string]struct{}{
	".docm": {},
	".dotm": {},
	".xlsm": {},
	".xltm": {},
	".xlam": {},
	".pptm": {},
	".potm": {},
	".ppam": {},
	".ppsm": {},
	".sldm": {},
}

func (o Options) withDefaults() Options {
	if o.Config == nil {
		o.Config = config.Get()
	}
	if o.ArticleMinWords <= 0 {
		o.ArticleMinWords = DefaultArticleMinWords
	}
	if o.PostMinWords <= 0 {
		o.PostMinWords = DefaultPostMinWords
	}
	if o.CommentMinWords <= 0 {
		o.CommentMinWords = DefaultCommentMinWords
	}
	return o
}

// Scan runs the full content policy audit across all configured country databases.
func Scan(ctx context.Context, opts Options) ([]Finding, error) {
	opts = opts.withDefaults()
	s := &scanner{
		cfg:              opts.Config,
		allowedLinkHosts: buildAllowedHosts(opts.Config),
		articleMinWords:  opts.ArticleMinWords,
		postMinWords:     opts.PostMinWords,
		commentMinWords:  opts.CommentMinWords,
	}
	if err := s.run(ctx); err != nil {
		return nil, err
	}
	return s.findings, nil
}

func (s *scanner) run(ctx context.Context) error {
	manager := database.GetManager()
	seen := map[*gorm.DB]bool{}

	for _, countryID := range []database.CountryID{
		database.CountryJordan,
		database.CountrySaudi,
		database.CountryEgypt,
		database.CountryPalestine,
	} {
		if err := ctx.Err(); err != nil {
			return err
		}

		db := manager.Get(countryID)
		if seen[db] {
			continue
		}
		seen[db] = true

		countryCode := database.CountryCode(countryID)
		if err := s.scanDatabase(ctx, db.WithContext(ctx), countryCode); err != nil {
			return fmt.Errorf("%s: %w", countryCode, err)
		}
	}

	return nil
}

func (s *scanner) scanDatabase(ctx context.Context, db *gorm.DB, countryCode string) error {
	scanners := []struct {
		table string
		fn    func(context.Context, *gorm.DB, string) error
	}{
		{"articles", s.scanArticles},
		{"posts", s.scanPosts},
		{"comments", s.scanComments},
		{"files", s.scanFiles},
		{"categories", s.scanCategories},
	}

	for _, scanner := range scanners {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !db.Migrator().HasTable(scanner.table) {
			continue
		}
		if err := scanner.fn(ctx, db, countryCode); err != nil {
			return fmt.Errorf("scan %s: %w", scanner.table, err)
		}
	}

	return nil
}

func (s *scanner) scanArticles(ctx context.Context, db *gorm.DB, countryCode string) error {
	var articles []models.Article
	return db.WithContext(ctx).FindInBatches(&articles, defaultBatchSize, func(tx *gorm.DB, batch int) error {
		for _, article := range articles {
			fields := []string{article.Title, article.Content, derefString(article.MetaDescription)}
			itemURL := joinURL(s.cfg.Frontend.URL, countryCode, "lesson", "articles", fmt.Sprint(article.ID))
			s.auditText("article", countryCode, article.ID, article.Title, fields, itemURL)
			s.auditShortContent("article", countryCode, article.ID, article.Title, itemURL, article.Content, s.articleMinWords)
		}
		return ctx.Err()
	}).Error
}

func (s *scanner) scanPosts(ctx context.Context, db *gorm.DB, fallbackCountryCode string) error {
	var posts []models.Post
	return db.WithContext(ctx).FindInBatches(&posts, defaultBatchSize, func(tx *gorm.DB, batch int) error {
		for _, post := range posts {
			countryCode := firstNonEmpty(post.Country, fallbackCountryCode)
			fields := []string{
				post.Title,
				post.Slug,
				post.Content,
				derefString(post.Alt),
				derefString(post.Keywords),
				derefString(post.MetaDescription),
				derefString(post.Image),
			}
			itemURL := joinURL(s.cfg.Frontend.URL, countryCode, "posts", fmt.Sprint(post.ID))
			s.auditText("post", countryCode, post.ID, post.Title, fields, itemURL)
			s.auditShortContent("post", countryCode, post.ID, post.Title, itemURL, post.Content, s.postMinWords)
		}
		return ctx.Err()
	}).Error
}

func (s *scanner) scanComments(ctx context.Context, db *gorm.DB, fallbackCountryCode string) error {
	var comments []models.Comment
	return db.WithContext(ctx).FindInBatches(&comments, defaultCommentBatchSize, func(tx *gorm.DB, batch int) error {
		for _, comment := range comments {
			countryCode := firstNonEmpty(comment.Database, fallbackCountryCode)
			title := fmt.Sprintf("comment on %s #%d", comment.CommentableType, comment.CommentableID)
			itemURL := s.commentURL(countryCode, comment)
			s.auditText("comment", countryCode, comment.ID, title, []string{comment.Body, comment.CommentableType}, itemURL)
			if strings.EqualFold(comment.Status, models.CommentStatusApproved) {
				s.auditShortContent("comment", countryCode, comment.ID, title, itemURL, comment.Body, s.commentMinWords)
			}
		}
		return ctx.Err()
	}).Error
}

func (s *scanner) scanFiles(ctx context.Context, db *gorm.DB, countryCode string) error {
	var files []models.File
	return db.WithContext(ctx).FindInBatches(&files, defaultCommentBatchSize, func(tx *gorm.DB, batch int) error {
		for _, file := range files {
			title := firstNonEmpty(file.FileName, path.Base(normalizePath(file.FilePath)))
			itemURL := s.fileURL(file)
			fields := []string{
				file.FileName,
				file.FilePath,
				file.FileType,
				file.MimeType,
				derefString(file.FileCategory),
			}
			s.auditText("file", countryCode, file.ID, title, fields, itemURL)
			s.auditMacroFile(countryCode, file, itemURL)
			if file.FileSize <= 0 {
				s.addFinding("file", countryCode, file.ID, title, "empty_file", "File size is zero or missing.", itemURL, "Verify the upload and remove broken files before AdSense review.")
			}
		}
		return ctx.Err()
	}).Error
}

func (s *scanner) scanCategories(ctx context.Context, db *gorm.DB, fallbackCountryCode string) error {
	var categories []models.Category
	return db.WithContext(ctx).FindInBatches(&categories, defaultCommentBatchSize, func(tx *gorm.DB, batch int) error {
		for _, category := range categories {
			countryCode := firstNonEmpty(category.Country, fallbackCountryCode)
			fields := []string{
				category.Name,
				category.Slug,
				derefString(category.Icon),
				derefString(category.Image),
				derefString(category.IconImage),
			}
			itemURL := joinURL(s.cfg.Frontend.URL, countryCode, "posts", "category", fmt.Sprint(category.ID))
			s.auditText("category", countryCode, category.ID, category.Name, fields, itemURL)
			if wordCount(category.Name) == 0 {
				s.addFinding("category", countryCode, category.ID, category.Name, "empty_category", "Category name is empty.", itemURL, "Rename or remove the empty category.")
			}
		}
		return ctx.Err()
	}).Error
}

func (s *scanner) auditText(contentType, countryCode string, id uint, title string, fields []string, itemURL string) {
	combined := strings.Join(fields, "\n")
	if strings.TrimSpace(combined) == "" {
		return
	}

	normalized := normalizeText(combined)
	for _, rule := range contentRules {
		matches := matchingTerms(normalized, rule.terms)
		if len(matches) > 0 {
			sort.Strings(matches)
			s.addFinding(
				contentType,
				countryCode,
				id,
				title,
				rule.risk,
				"Matched policy terms: "+strings.Join(matches, ", "),
				itemURL,
				rule.action,
			)
		}
	}

	if unsafeMarkupPattern.MatchString(combined) {
		s.addFinding(
			contentType,
			countryCode,
			id,
			title,
			"unsafe_markup",
			"Content contains script, iframe, inline handler, or javascript URI.",
			itemURL,
			"Remove unsafe markup and sanitize the stored content.",
		)
	}

	for _, reason := range dangerousLinkReasons(combined, s.allowedLinkHosts) {
		s.addFinding(
			contentType,
			countryCode,
			id,
			title,
			"dangerous_external_link",
			reason,
			itemURL,
			"Remove the link or manually verify the target before AdSense review.",
		)
	}
}

func (s *scanner) auditShortContent(contentType, countryCode string, id uint, title string, itemURL string, content string, minWords int) {
	words := wordCount(content)
	if words >= minWords {
		return
	}

	s.addFinding(
		contentType,
		countryCode,
		id,
		title,
		"thin_content",
		fmt.Sprintf("Content has %d words; minimum audit threshold is %d.", words, minWords),
		itemURL,
		"Expand the content, merge it with a stronger page, or keep ads disabled on this URL.",
	)
}

func (s *scanner) auditMacroFile(countryCode string, file models.File, itemURL string) {
	exts := []string{
		strings.ToLower(path.Ext(normalizePath(file.FileName))),
		strings.ToLower(path.Ext(normalizePath(file.FilePath))),
	}
	for _, ext := range exts {
		if _, ok := macroExtensions[ext]; ok {
			s.addFinding(
				"file",
				countryCode,
				file.ID,
				firstNonEmpty(file.FileName, path.Base(normalizePath(file.FilePath))),
				"macro_file",
				"File extension allows Office macros: "+ext,
				itemURL,
				"Replace with a PDF or non-macro Office file before AdSense review.",
			)
			return
		}
	}

	mime := strings.ToLower(file.MimeType + " " + file.FileType)
	if strings.Contains(mime, "macroenabled") || strings.Contains(mime, "macro-enabled") {
		s.addFinding(
			"file",
			countryCode,
			file.ID,
			firstNonEmpty(file.FileName, path.Base(normalizePath(file.FilePath))),
			"macro_file",
			"File MIME/type indicates macro-enabled Office content.",
			itemURL,
			"Replace with a PDF or non-macro Office file before AdSense review.",
		)
	}
}

func (s *scanner) addFinding(contentType, countryCode string, id uint, title, risk, reason, itemURL, action string) {
	s.findings = append(s.findings, Finding{
		Type:              contentType,
		ID:                fmt.Sprintf("%s:%d", countryCode, id),
		Title:             cleanCSVValue(title),
		Risk:              risk,
		Reason:            cleanCSVValue(reason),
		URL:               itemURL,
		RecommendedAction: action,
	})
}

func (s *scanner) commentURL(countryCode string, comment models.Comment) string {
	switch {
	case strings.Contains(strings.ToLower(comment.CommentableType), "article"):
		return joinURL(s.cfg.Frontend.URL, countryCode, "lesson", "articles", fmt.Sprint(comment.CommentableID)) + fmt.Sprintf("#comment-%d", comment.ID)
	case strings.Contains(strings.ToLower(comment.CommentableType), "post"):
		return joinURL(s.cfg.Frontend.URL, countryCode, "posts", fmt.Sprint(comment.CommentableID)) + fmt.Sprintf("#comment-%d", comment.ID)
	default:
		return joinURL(s.cfg.Frontend.URL, countryCode) + fmt.Sprintf("#comment-%d", comment.ID)
	}
}

func (s *scanner) fileURL(file models.File) string {
	raw := strings.TrimSpace(file.FilePath)
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(strings.ToLower(raw), "http://") || strings.HasPrefix(strings.ToLower(raw), "https://") {
		return raw
	}
	return joinURL(s.cfg.App.URL, "storage", strings.TrimLeft(raw, "/"))
}

// WriteCSV writes the policy audit report using the required CSV columns.
func WriteCSV(w io.Writer, findings []Finding) error {
	if _, err := w.Write([]byte("\xEF\xBB\xBF")); err != nil {
		return err
	}

	cw := csv.NewWriter(w)
	if err := cw.Write([]string{"type", "id", "title", "risk", "reason", "url", "recommended_action"}); err != nil {
		return err
	}

	for _, finding := range findings {
		if err := cw.Write([]string{
			finding.Type,
			finding.ID,
			finding.Title,
			finding.Risk,
			finding.Reason,
			finding.URL,
			finding.RecommendedAction,
		}); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

func matchingTerms(normalized string, terms []string) []string {
	seen := map[string]struct{}{}
	englishTokens := map[string]struct{}{}
	for _, token := range englishWordTokenizer.FindAllString(normalized, -1) {
		englishTokens[token] = struct{}{}
	}

	for _, term := range terms {
		t := strings.TrimSpace(normalizeText(term))
		if t == "" {
			continue
		}
		if isASCIITerm(t) {
			if len(t) <= 4 {
				if _, ok := englishTokens[t]; ok {
					seen[term] = struct{}{}
				}
				continue
			}
			if strings.Contains(normalized, t) {
				seen[term] = struct{}{}
			}
			continue
		}
		if strings.Contains(normalized, t) {
			seen[term] = struct{}{}
		}
	}

	matches := make([]string, 0, len(seen))
	for term := range seen {
		matches = append(matches, term)
	}
	return matches
}

func dangerousLinkReasons(raw string, allowedHosts map[string]struct{}) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var reasons []string

	if dangerousURIPattern.MatchString(raw) {
		reasons = append(reasons, "Content contains javascript: or data: URI.")
	}

	seen := map[string]struct{}{}
	for _, candidate := range urlPattern.FindAllString(raw, -1) {
		candidate = strings.TrimRight(candidate, ".,;:)]}؟،")
		parsedURL, err := parseURLCandidate(candidate)
		if err != nil {
			continue
		}
		host := strings.ToLower(parsedURL.Hostname())
		if host == "" || isAllowedHost(host, allowedHosts) {
			continue
		}

		reason := ""
		if _, ok := shortenerHosts[host]; ok {
			reason = "External URL uses a link shortener: " + host
		} else if term := firstHostTerm(host, suspiciousHostTerms); term != "" {
			reason = fmt.Sprintf("External URL host looks risky (%s): %s", term, host)
		}

		if reason != "" {
			if _, ok := seen[reason]; !ok {
				reasons = append(reasons, reason)
				seen[reason] = struct{}{}
			}
		}
	}

	return reasons
}

func buildAllowedHosts(cfg *config.Config) map[string]struct{} {
	hosts := map[string]struct{}{}
	addURLHost(hosts, cfg.App.URL)
	addURLHost(hosts, cfg.Frontend.URL)
	addURLHost(hosts, cfg.Storage.URL)

	for _, host := range trustedExternalHosts {
		hosts[host] = struct{}{}
	}
	return hosts
}

func addURLHost(hosts map[string]struct{}, raw string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	parsedURL, err := url.Parse(raw)
	if err != nil {
		return
	}
	host := strings.ToLower(parsedURL.Hostname())
	if host != "" {
		hosts[host] = struct{}{}
	}
}

func isAllowedHost(host string, allowedHosts map[string]struct{}) bool {
	for allowed := range allowedHosts {
		if host == allowed || strings.HasSuffix(host, "."+allowed) {
			return true
		}
	}
	return false
}

func parseURLCandidate(candidate string) (*url.URL, error) {
	if strings.HasPrefix(strings.ToLower(candidate), "www.") {
		candidate = "https://" + candidate
	}
	parsedURL, err := url.Parse(candidate)
	if err != nil {
		return nil, err
	}
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return nil, errors.New("unsupported URL scheme")
	}
	return parsedURL, nil
}

func firstHostTerm(host string, terms []string) string {
	for _, term := range terms {
		if strings.Contains(host, term) {
			return term
		}
	}
	return ""
}

func wordCount(raw string) int {
	text := normalizeText(scriptStylePattern.ReplaceAllString(raw, " "))
	text = htmlTagPattern.ReplaceAllString(text, " ")

	count := 0
	for _, token := range strings.Fields(text) {
		token = nonWordTrimPattern.ReplaceAllString(token, "")
		if token != "" {
			count++
		}
	}
	return count
}

func normalizeText(raw string) string {
	replacer := strings.NewReplacer(
		"أ", "ا",
		"إ", "ا",
		"آ", "ا",
		"ى", "ي",
		"ة", "ه",
	)
	return strings.ToLower(replacer.Replace(raw))
}

func isASCIITerm(term string) bool {
	for _, r := range term {
		if r > 127 {
			return false
		}
	}
	return true
}

func joinURL(base string, parts ...string) string {
	base = strings.TrimSpace(base)
	if base == "" {
		base = "http://localhost:3000"
	}
	base = strings.TrimRight(base, "/")
	cleanParts := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.Trim(part, "/")
		if part != "" {
			cleanParts = append(cleanParts, part)
		}
	}
	if len(cleanParts) == 0 {
		return base
	}
	return base + "/" + strings.Join(cleanParts, "/")
}

func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func normalizePath(value string) string {
	return strings.ReplaceAll(value, "\\", "/")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func cleanCSVValue(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}
