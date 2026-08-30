package services

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/imanjo/fiber-api/internal/database"
	"github.com/imanjo/fiber-api/internal/models"
	"github.com/imanjo/fiber-api/internal/repositories"
	"gorm.io/gorm"
)

var (
	ErrSEOInvalidContentType = errors.New("invalid SEO content type")
	ErrSEOInvalidURL         = errors.New("invalid URL")
	ErrSEORedirectLoop       = errors.New("redirect loop")
	ErrSEOInvalidSchema      = errors.New("invalid schema JSON")
	ErrSEOInvalidInput       = errors.New("invalid SEO input")
	ErrSEOIntegration        = errors.New("SEO integration failed")
	ErrSEOAuditRunning       = errors.New("SEO audit already running")
	ErrSEOConflict           = errors.New("SEO record conflict")
	seoSensitiveQueryKey     = regexp.MustCompile(`(?i)(token|password|passwd|secret|email|session|auth|key|code)`)
	seoContentLinkPath       = regexp.MustCompile(`^/(jo|sa|eg|ps)/(lesson/articles|posts)/([0-9]+)/?$`)
)

type SEOMetadataInput struct {
	SEOTitle           string `json:"seo_title"`
	MetaDescription    string `json:"meta_description"`
	FocusKeyword       string `json:"focus_keyword"`
	AdditionalKeywords string `json:"additional_keywords"`
	CanonicalURL       string `json:"canonical_url"`
	RobotsIndex        bool   `json:"robots_index"`
	RobotsFollow       bool   `json:"robots_follow"`
	RobotsNoArchive    bool   `json:"robots_noarchive"`
	RobotsNoSnippet    bool   `json:"robots_nosnippet"`
	MaxSnippet         int    `json:"max_snippet"`
	MaxImagePreview    string `json:"max_image_preview"`
	MaxVideoPreview    int    `json:"max_video_preview"`
	OGTitle            string `json:"og_title"`
	OGDescription      string `json:"og_description"`
	OGImage            string `json:"og_image"`
	TwitterTitle       string `json:"twitter_title"`
	TwitterDescription string `json:"twitter_description"`
	TwitterImage       string `json:"twitter_image"`
	SchemaType         string `json:"schema_type"`
	SchemaJSON         string `json:"schema_json"`
	Cornerstone        bool   `json:"cornerstone"`
	ChangeNote         string `json:"change_note"`
}

type EffectiveSEO struct {
	ContentType        string            `json:"content_type"`
	ContentID          uint              `json:"content_id"`
	Title              string            `json:"title"`
	Description        string            `json:"description"`
	Keywords           string            `json:"keywords"`
	CanonicalURL       string            `json:"canonical_url"`
	Robots             string            `json:"robots"`
	RobotsIndex        bool              `json:"robots_index"`
	RobotsFollow       bool              `json:"robots_follow"`
	OGTitle            string            `json:"og_title"`
	OGDescription      string            `json:"og_description"`
	OGImage            string            `json:"og_image"`
	TwitterTitle       string            `json:"twitter_title"`
	TwitterDescription string            `json:"twitter_description"`
	TwitterImage       string            `json:"twitter_image"`
	SchemaType         string            `json:"schema_type"`
	SchemaJSON         json.RawMessage   `json:"schema_json,omitempty"`
	Cornerstone        bool              `json:"cornerstone"`
	Score              int               `json:"score"`
	Analysis           SEOAnalysisResult `json:"analysis"`
	Customized         bool              `json:"customized"`
}

type SEORedirectInput struct {
	SourcePath    string `json:"source_path"`
	TargetURL     string `json:"target_url"`
	StatusCode    int    `json:"status_code"`
	MatchType     string `json:"match_type"`
	PreserveQuery bool   `json:"preserve_query"`
	Active        bool   `json:"active"`
}

type SEORedirectMatch struct {
	ID            uint   `json:"id"`
	TargetURL     string `json:"target_url"`
	StatusCode    int    `json:"status_code"`
	PreserveQuery bool   `json:"preserve_query"`
}

type SEOLinkSuggestion struct {
	ContentType string  `json:"content_type"`
	ContentID   uint    `json:"content_id"`
	Title       string  `json:"title"`
	URL         string  `json:"url"`
	Score       float64 `json:"score"`
	Reason      string  `json:"reason"`
}

type SEOOverview struct {
	TotalContent  int64               `json:"total_content"`
	Configured    int64               `json:"configured"`
	Scores        map[string]int64    `json:"scores"`
	Redirects     int64               `json:"redirects"`
	Unresolved404 int64               `json:"unresolved_404"`
	LatestAudit   *models.SEOAuditRun `json:"latest_audit,omitempty"`
}

type SEOService interface {
	Analyze(SEOAnalysisInput) SEOAnalysisResult
	ContentPreview(context.Context, database.CountryID, string, uint) (string, string, error)
	GetEffective(context.Context, database.CountryID, string, uint) (*EffectiveSEO, error)
	GetMetadata(context.Context, database.CountryID, string, uint) (*models.SEOMetadata, error)
	SaveMetadata(context.Context, database.CountryID, string, uint, SEOMetadataInput, uint) (*models.SEOMetadata, error)
	ListContent(context.Context, database.CountryID, string, string, int, int) ([]repositories.SEOContent, int64, error)
	Overview(context.Context, database.CountryID) (*SEOOverview, error)
	ListRevisions(context.Context, database.CountryID, string, uint, int) ([]models.SEORevision, error)
	RestoreRevision(context.Context, database.CountryID, string, uint, uint, uint) (*models.SEOMetadata, error)
	CreateRedirect(context.Context, database.CountryID, SEORedirectInput, uint) (*models.SEORedirect, error)
	UpdateRedirect(context.Context, database.CountryID, uint, SEORedirectInput, uint) (*models.SEORedirect, error)
	ListRedirects(context.Context, database.CountryID, string, int, int) ([]models.SEORedirect, int64, error)
	DeleteRedirect(context.Context, database.CountryID, uint) error
	ResolveRedirect(context.Context, database.CountryID, string, string) (*SEORedirectMatch, error)
	Record404(context.Context, database.CountryID, string, string, string, string) error
	List404(context.Context, database.CountryID, bool, string, int, int) ([]models.SEO404Log, int64, error)
	Resolve404(context.Context, database.CountryID, uint, *uint) error
	Clear404(context.Context, database.CountryID, bool) error
	LinkSuggestions(context.Context, database.CountryID, string, uint, int) ([]SEOLinkSuggestion, error)
	StartAudit(context.Context, database.CountryID, uint) (*models.SEOAuditRun, error)
	GetAudit(context.Context, database.CountryID, uint) (*models.SEOAuditRun, error)
	ListAudits(context.Context, database.CountryID, int) ([]models.SEOAuditRun, error)
	ListAuditIssues(context.Context, database.CountryID, uint, string, int, int) ([]models.SEOIssue, int64, error)
	UpsertAuthor(context.Context, models.SEOAuthorProfile) (*models.SEOAuthorProfile, error)
	GetAuthor(context.Context, uint) (*models.SEOAuthorProfile, error)
	ListAuthors(context.Context, bool) ([]models.SEOAuthorProfile, error)
	SubmitIndexNow(context.Context, database.CountryID, []string) ([]models.SEOIndexNowSubmission, error)
}

type seoService struct {
	repo       repositories.SEORepository
	settings   SettingService
	sitemap    SitemapService
	http       *http.Client
	auditMu    sync.Mutex
	redirectRE sync.Map // regex source string -> *regexp.Regexp, hot-path 404 resolver cache
}

func NewSEOService(repo repositories.SEORepository, settings SettingService, sitemap SitemapService) SEOService {
	return &seoService{repo: repo, settings: settings, sitemap: sitemap, http: &http.Client{Timeout: 12 * time.Second}}
}

func (s *seoService) Analyze(input SEOAnalysisInput) SEOAnalysisResult { return AnalyzeSEO(input) }

// ContentPreview returns the title and raw body of an article/post — the input
// the AI SEO optimizer needs when it is triggered from a list (the editor form
// already has both in the DOM, but a drawer does not).
func (s *seoService) ContentPreview(ctx context.Context, countryID database.CountryID, contentType string, contentID uint) (string, string, error) {
	if !validSEOContentType(contentType) || contentID == 0 {
		return "", "", ErrSEOInvalidContentType
	}
	content, err := s.repo.GetContent(ctx, countryID, contentType, contentID)
	if err != nil {
		return "", "", MapError(err)
	}
	return content.Title, content.Content, nil
}

func validSEOContentType(value string) bool {
	return value == models.SEOContentTypeArticle || value == models.SEOContentTypePost
}

func (s *seoService) GetMetadata(ctx context.Context, countryID database.CountryID, contentType string, contentID uint) (*models.SEOMetadata, error) {
	if !validSEOContentType(contentType) || contentID == 0 {
		return nil, ErrSEOInvalidContentType
	}
	item, err := s.repo.FindMetadata(ctx, countryID, contentType, contentID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return defaultSEOMetadata(database.CountryCode(countryID), contentType, contentID), nil
	}
	return item, MapError(err)
}

func defaultSEOMetadata(countryCode, contentType string, contentID uint) *models.SEOMetadata {
	schemaType := "Article"
	if contentType == models.SEOContentTypePost {
		schemaType = "BlogPosting"
	}
	return &models.SEOMetadata{ContentType: contentType, ContentID: contentID, CountryCode: countryCode, RobotsIndex: true, RobotsFollow: true, MaxSnippet: -1, MaxImagePreview: "large", MaxVideoPreview: -1, SchemaType: schemaType}
}

func (s *seoService) GetEffective(ctx context.Context, countryID database.CountryID, contentType string, contentID uint) (*EffectiveSEO, error) {
	return s.getEffective(ctx, countryID, contentType, contentID, true)
}

func (s *seoService) getEffective(ctx context.Context, countryID database.CountryID, contentType string, contentID uint, requirePublished bool) (*EffectiveSEO, error) {
	if !validSEOContentType(contentType) {
		return nil, ErrSEOInvalidContentType
	}
	content, err := s.repo.GetContent(ctx, countryID, contentType, contentID)
	if err != nil {
		return nil, MapError(err)
	}
	// The public metadata endpoint must not become a side channel for draft
	// titles or descriptions. Internal audits call this helper with false so
	// editors can still improve unpublished work before launch.
	if requirePublished && !content.Published {
		return nil, ErrNotFound
	}
	metadata, err := s.repo.FindMetadata(ctx, countryID, contentType, contentID)
	customized := err == nil
	if errors.Is(err, gorm.ErrRecordNotFound) {
		metadata = defaultSEOMetadata(database.CountryCode(countryID), contentType, contentID)
	} else if err != nil {
		return nil, MapError(err)
	}
	settings, _ := s.settings.GetPublic(ctx, countryID)
	fields := resolveSEOFields(countryID, contentType, contentID, content, metadata, settings)
	analysis := s.effectiveAnalysis(content, metadata, fields, customized)
	robots := buildSEORobots(metadata)
	var schema json.RawMessage
	if strings.TrimSpace(metadata.SchemaJSON) != "" && json.Valid([]byte(metadata.SchemaJSON)) {
		schema = json.RawMessage(metadata.SchemaJSON)
	}
	return &EffectiveSEO{ContentType: contentType, ContentID: contentID, Title: fields.title, Description: fields.description, Keywords: firstSEOValue(metadata.AdditionalKeywords, content.Keywords), CanonicalURL: fields.canonical, Robots: robots, RobotsIndex: metadata.RobotsIndex, RobotsFollow: metadata.RobotsFollow, OGTitle: firstSEOValue(metadata.OGTitle, fields.title), OGDescription: firstSEOValue(metadata.OGDescription, fields.description), OGImage: fields.image, TwitterTitle: firstSEOValue(metadata.TwitterTitle, metadata.OGTitle, fields.title), TwitterDescription: firstSEOValue(metadata.TwitterDescription, metadata.OGDescription, fields.description), TwitterImage: firstSEOValue(metadata.TwitterImage, fields.image), SchemaType: firstSEOValue(metadata.SchemaType, "Article"), SchemaJSON: schema, Cornerstone: metadata.Cornerstone, Score: analysis.Score, Analysis: analysis, Customized: customized}, nil
}

// resolvedSEOFields holds the merged presentation values shared by the public
// EffectiveSEO payload, the on-page analyzer input, and the stored analysis
// written on save. Keeping one resolver keeps all three consistent.
type resolvedSEOFields struct {
	title       string
	description string
	canonical   string
	image       string
}

func resolveSEOFields(countryID database.CountryID, contentType string, contentID uint, content *repositories.SEOContent, metadata *models.SEOMetadata, settings map[string]string) resolvedSEOFields {
	title := firstSEOValue(metadata.SEOTitle, content.Title)
	if template := strings.TrimSpace(settings["seo_title_template"]); metadata.SEOTitle == "" && template != "" {
		siteName := firstSEOValue(settings["site_name"], "موقع الإيمان")
		title = strings.NewReplacer("%title%", content.Title, "%site_name%", siteName, "%country%", database.CountryCode(countryID)).Replace(template)
	}
	description := firstSEOValue(metadata.MetaDescription, content.Description, seoExcerpt(content.Content, 160))
	canonical := strings.TrimSpace(metadata.CanonicalURL)
	if canonical == "" {
		if base := strings.TrimRight(firstSEOValue(settings["canonical_url"], settings["site_url"]), "/"); base != "" {
			canonical = base + seoContentPath(database.CountryCode(countryID), contentType, contentID)
		}
	}
	// site_logo is deliberately not a fallback: it is a brand mark, not a per-page
	// share image. Leaving this empty lets the frontend fall back to the first real
	// in-content image, which articles need because they carry no image column.
	image := firstSEOValue(metadata.OGImage, content.ImageURL)
	return resolvedSEOFields{title: title, description: description, canonical: canonical, image: image}
}

func seoAnalysisInput(content *repositories.SEOContent, metadata *models.SEOMetadata, fields resolvedSEOFields) SEOAnalysisInput {
	return SEOAnalysisInput{Title: fields.title, Content: content.Content, MetaDescription: fields.description, FocusKeyword: metadata.FocusKeyword, CanonicalURL: fields.canonical, ImageURL: fields.image, SchemaType: metadata.SchemaType, SchemaJSON: metadata.SchemaJSON}
}

// effectiveAnalysis serves the stored analysis for content that already has a
// saved SEO row (SaveMetadata / RestoreRevision recompute and persist it with the
// same resolver), and only runs a live analysis for content that was never
// configured. This keeps the public /api/seo/:type/:id endpoint — hit on every
// article/post render — off the regex pipeline on the hot path.
func (s *seoService) effectiveAnalysis(content *repositories.SEOContent, metadata *models.SEOMetadata, fields resolvedSEOFields, customized bool) SEOAnalysisResult {
	if customized && strings.TrimSpace(metadata.AnalysisJSON) != "" {
		var stored SEOAnalysisResult
		if json.Unmarshal([]byte(metadata.AnalysisJSON), &stored) == nil && len(stored.Checks) > 0 {
			return stored
		}
	}
	return AnalyzeSEO(seoAnalysisInput(content, metadata, fields))
}

func (s *seoService) SaveMetadata(ctx context.Context, countryID database.CountryID, contentType string, contentID uint, input SEOMetadataInput, userID uint) (*models.SEOMetadata, error) {
	if !validSEOContentType(contentType) || contentID == 0 {
		return nil, ErrSEOInvalidContentType
	}
	content, err := s.repo.GetContent(ctx, countryID, contentType, contentID)
	if err != nil {
		return nil, MapError(err)
	}
	if strings.TrimSpace(input.SchemaType) == "" {
		input.SchemaType = "Article"
		if contentType == models.SEOContentTypePost {
			input.SchemaType = "BlogPosting"
		}
	}
	if err := validateSEOMetadataInput(&input); err != nil {
		return nil, err
	}
	item, err := s.repo.FindMetadata(ctx, countryID, contentType, contentID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		item = defaultSEOMetadata(database.CountryCode(countryID), contentType, contentID)
		item.CreatedBy = optionalSEOUser(userID)
	} else if err != nil {
		return nil, MapError(err)
	}
	applySEOMetadataInput(item, input)
	item.UpdatedBy = optionalSEOUser(userID)
	settings, _ := s.settings.GetPublic(ctx, countryID)
	fields := resolveSEOFields(countryID, contentType, contentID, content, item, settings)
	analysis := AnalyzeSEO(seoAnalysisInput(content, item, fields))
	encoded, _ := json.Marshal(analysis)
	item.Score, item.AnalysisJSON = analysis.Score, string(encoded)
	if err := s.repo.SaveMetadata(ctx, countryID, item); err != nil {
		return nil, MapError(err)
	}
	if err := s.createRevision(ctx, countryID, item, input.ChangeNote, userID); err != nil {
		return nil, MapError(err)
	}
	if s.sitemap != nil {
		s.sitemap.ScheduleGenerate(database.CountryCode(countryID))
	}
	InvalidateContentHealthCache(countryID)
	if content.Published {
		go s.submitIndexNowBackground(countryID, contentType, contentID)
	}
	return item, nil
}

func validateSEOMetadataInput(input *SEOMetadataInput) error {
	trim := func(value *string, max int) error {
		*value = strings.TrimSpace(*value)
		if utf8.RuneCountInString(*value) > max {
			return fmt.Errorf("%w: value exceeds %d characters", ErrSEOInvalidInput, max)
		}
		return nil
	}
	for value, max := range map[*string]int{&input.SEOTitle: 500, &input.MetaDescription: 500, &input.FocusKeyword: 255, &input.AdditionalKeywords: 4000, &input.CanonicalURL: 1000, &input.OGTitle: 500, &input.OGDescription: 500, &input.OGImage: 1000, &input.TwitterTitle: 500, &input.TwitterDescription: 500, &input.TwitterImage: 1000, &input.SchemaType: 80, &input.ChangeNote: 500} {
		if err := trim(value, max); err != nil {
			return err
		}
	}
	for _, value := range []string{input.CanonicalURL, input.OGImage, input.TwitterImage} {
		if value != "" && !validSEOAbsoluteURL(value) {
			return ErrSEOInvalidURL
		}
	}
	if input.MaxSnippet < -1 || input.MaxVideoPreview < -1 {
		return fmt.Errorf("%w: invalid robots preview value", ErrSEOInvalidInput)
	}
	if input.MaxImagePreview == "" {
		input.MaxImagePreview = "large"
	}
	if input.MaxImagePreview != "none" && input.MaxImagePreview != "standard" && input.MaxImagePreview != "large" {
		return fmt.Errorf("%w: invalid max image preview", ErrSEOInvalidInput)
	}
	if input.SchemaType == "" {
		input.SchemaType = "Article"
	}
	allowedSchemas := map[string]bool{"Article": true, "NewsArticle": true, "BlogPosting": true, "HowTo": true, "FAQPage": true, "WebPage": true, "Course": true, "LearningResource": true, "VideoObject": true}
	if !allowedSchemas[input.SchemaType] {
		return ErrSEOInvalidSchema
	}
	if input.SchemaJSON != "" {
		if len(input.SchemaJSON) > 65536 || !json.Valid([]byte(input.SchemaJSON)) {
			return ErrSEOInvalidSchema
		}
		var object map[string]interface{}
		if json.Unmarshal([]byte(input.SchemaJSON), &object) != nil {
			return ErrSEOInvalidSchema
		}
		typeValue, hasType := object["@type"].(string)
		graphValue, hasGraph := object["@graph"].([]interface{})
		hasType = hasType && strings.TrimSpace(typeValue) != ""
		hasGraph = hasGraph && len(graphValue) > 0
		if !hasType && !hasGraph {
			return ErrSEOInvalidSchema
		}
	}
	return nil
}

func applySEOMetadataInput(item *models.SEOMetadata, input SEOMetadataInput) {
	item.SEOTitle, item.MetaDescription, item.FocusKeyword, item.AdditionalKeywords = input.SEOTitle, input.MetaDescription, input.FocusKeyword, input.AdditionalKeywords
	item.CanonicalURL, item.RobotsIndex, item.RobotsFollow = input.CanonicalURL, input.RobotsIndex, input.RobotsFollow
	item.RobotsNoArchive, item.RobotsNoSnippet = input.RobotsNoArchive, input.RobotsNoSnippet
	item.MaxSnippet, item.MaxImagePreview, item.MaxVideoPreview = input.MaxSnippet, input.MaxImagePreview, input.MaxVideoPreview
	item.OGTitle, item.OGDescription, item.OGImage = input.OGTitle, input.OGDescription, input.OGImage
	item.TwitterTitle, item.TwitterDescription, item.TwitterImage = input.TwitterTitle, input.TwitterDescription, input.TwitterImage
	item.SchemaType, item.SchemaJSON, item.Cornerstone = input.SchemaType, input.SchemaJSON, input.Cornerstone
}

func (s *seoService) createRevision(ctx context.Context, countryID database.CountryID, item *models.SEOMetadata, note string, userID uint) error {
	snapshot, err := json.Marshal(item)
	if err != nil {
		return err
	}
	for attempt := 0; attempt < 3; attempt++ {
		version, versionErr := s.repo.NextRevisionVersion(ctx, countryID, item.ID)
		if versionErr != nil {
			return versionErr
		}
		createErr := s.repo.CreateRevision(ctx, countryID, &models.SEORevision{MetadataID: item.ID, Version: version, Snapshot: string(snapshot), ChangeNote: strings.TrimSpace(note), ChangedBy: optionalSEOUser(userID), CreatedAt: time.Now().UTC()})
		if createErr == nil {
			return nil
		}
		if !strings.Contains(strings.ToLower(createErr.Error()), "duplicate") {
			return createErr
		}
	}
	return errors.New("could not allocate SEO revision version")
}

func (s *seoService) ListRevisions(ctx context.Context, countryID database.CountryID, contentType string, contentID uint, limit int) ([]models.SEORevision, error) {
	item, err := s.repo.FindMetadata(ctx, countryID, contentType, contentID)
	if err != nil {
		return nil, MapError(err)
	}
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	return s.repo.ListRevisions(ctx, countryID, item.ID, limit)
}

func (s *seoService) RestoreRevision(ctx context.Context, countryID database.CountryID, contentType string, contentID, revisionID, userID uint) (*models.SEOMetadata, error) {
	item, err := s.repo.FindMetadata(ctx, countryID, contentType, contentID)
	if err != nil {
		return nil, MapError(err)
	}
	revision, err := s.repo.GetRevision(ctx, countryID, item.ID, revisionID)
	if err != nil {
		return nil, MapError(err)
	}
	var snapshot models.SEOMetadata
	if err := json.Unmarshal([]byte(revision.Snapshot), &snapshot); err != nil {
		return nil, err
	}
	content, err := s.repo.GetContent(ctx, countryID, contentType, contentID)
	if err != nil {
		return nil, MapError(err)
	}
	snapshot.ID, snapshot.ContentType, snapshot.ContentID, snapshot.CountryCode = item.ID, item.ContentType, item.ContentID, item.CountryCode
	snapshot.CreatedAt, snapshot.CreatedBy, snapshot.UpdatedBy = item.CreatedAt, item.CreatedBy, optionalSEOUser(userID)
	settings, _ := s.settings.GetPublic(ctx, countryID)
	fields := resolveSEOFields(countryID, contentType, contentID, content, &snapshot, settings)
	analysis := AnalyzeSEO(seoAnalysisInput(content, &snapshot, fields))
	encoded, _ := json.Marshal(analysis)
	snapshot.Score, snapshot.AnalysisJSON = analysis.Score, string(encoded)
	if err := s.repo.SaveMetadata(ctx, countryID, &snapshot); err != nil {
		return nil, MapError(err)
	}
	if err := s.createRevision(ctx, countryID, &snapshot, "استعادة النسخة #"+strconv.Itoa(revision.Version), userID); err != nil {
		return nil, err
	}
	if s.sitemap != nil {
		s.sitemap.ScheduleGenerate(database.CountryCode(countryID))
	}
	InvalidateContentHealthCache(countryID)
	if content.Published {
		go s.submitIndexNowBackground(countryID, contentType, contentID)
	}
	return &snapshot, nil
}

func (s *seoService) ListContent(ctx context.Context, countryID database.CountryID, contentType, search string, limit, offset int) ([]repositories.SEOContent, int64, error) {
	if contentType != "" && contentType != "all" && !validSEOContentType(contentType) {
		return nil, 0, ErrSEOInvalidContentType
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	return s.repo.ListContent(ctx, countryID, contentType, search, limit, offset)
}

func (s *seoService) Overview(ctx context.Context, countryID database.CountryID) (*SEOOverview, error) {
	_, total, err := s.repo.ListContent(ctx, countryID, "", "", 1, 0)
	if err != nil {
		return nil, err
	}
	scores, err := s.repo.MetadataStats(ctx, countryID)
	if err != nil {
		return nil, err
	}
	configured := scores["good"] + scores["needs_work"] + scores["poor"] + scores["noindex"]
	_, redirects, err := s.repo.ListRedirects(ctx, countryID, "", 1, 0)
	if err != nil {
		return nil, err
	}
	_, unresolved, err := s.repo.List404(ctx, countryID, false, "", 1, 0)
	if err != nil {
		return nil, err
	}
	runs, err := s.repo.ListAuditRuns(ctx, countryID, 1)
	if err != nil {
		return nil, err
	}
	var latest *models.SEOAuditRun
	if len(runs) > 0 {
		latest = &runs[0]
	}
	return &SEOOverview{TotalContent: total, Configured: configured, Scores: scores, Redirects: redirects, Unresolved404: unresolved, LatestAudit: latest}, nil
}

func (s *seoService) CreateRedirect(ctx context.Context, countryID database.CountryID, input SEORedirectInput, userID uint) (*models.SEORedirect, error) {
	item := &models.SEORedirect{CountryCode: database.CountryCode(countryID), CreatedBy: optionalSEOUser(userID)}
	if err := s.applyRedirectInput(ctx, countryID, item, input, userID); err != nil {
		return nil, err
	}
	if err := s.repo.CreateRedirect(ctx, countryID, item); err != nil {
		return nil, mapSEOConflict(err)
	}
	return item, nil
}

func (s *seoService) UpdateRedirect(ctx context.Context, countryID database.CountryID, id uint, input SEORedirectInput, userID uint) (*models.SEORedirect, error) {
	item, err := s.repo.GetRedirect(ctx, countryID, id)
	if err != nil {
		return nil, MapError(err)
	}
	if err := s.applyRedirectInput(ctx, countryID, item, input, userID); err != nil {
		return nil, err
	}
	if err := s.repo.UpdateRedirect(ctx, countryID, item); err != nil {
		return nil, mapSEOConflict(err)
	}
	return item, nil
}

func (s *seoService) applyRedirectInput(ctx context.Context, countryID database.CountryID, item *models.SEORedirect, input SEORedirectInput, userID uint) error {
	input.MatchType = strings.TrimSpace(input.MatchType)
	if input.MatchType == "" {
		input.MatchType = models.SEORedirectMatchExact
	}
	if input.MatchType != models.SEORedirectMatchExact && input.MatchType != models.SEORedirectMatchPrefix && input.MatchType != models.SEORedirectMatchRegex {
		return fmt.Errorf("%w: invalid redirect match type", ErrSEOInvalidInput)
	}
	if input.MatchType == models.SEORedirectMatchRegex {
		input.SourcePath = strings.TrimSpace(input.SourcePath)
	} else {
		input.SourcePath = normalizeSEOPath(input.SourcePath)
	}
	input.TargetURL = strings.TrimSpace(input.TargetURL)
	if input.SourcePath == "" || utf8.RuneCountInString(input.SourcePath) > 1500 || utf8.RuneCountInString(input.TargetURL) > 1500 {
		return ErrSEOInvalidURL
	}
	if input.MatchType == models.SEORedirectMatchRegex {
		if _, err := regexp.Compile(input.SourcePath); err != nil {
			return fmt.Errorf("%w: invalid redirect regular expression", ErrSEOInvalidInput)
		}
	}
	if input.StatusCode == 0 {
		input.StatusCode = 301
	}
	if input.StatusCode != 301 && input.StatusCode != 302 && input.StatusCode != 307 && input.StatusCode != 308 && input.StatusCode != 410 {
		return fmt.Errorf("%w: invalid redirect status", ErrSEOInvalidInput)
	}
	if input.StatusCode != 410 && !validSEORedirectTarget(input.TargetURL) {
		return ErrSEOInvalidURL
	}
	if input.StatusCode == 410 {
		input.TargetURL = ""
	}
	localHost := ""
	if settings, settingsErr := s.settings.GetPublic(ctx, countryID); settingsErr == nil {
		if site, parseErr := url.Parse(firstSEOValue(settings["canonical_url"], settings["site_url"])); parseErr == nil {
			localHost = site.Host
		}
	}
	if target, local := seoLocalRedirectURL(input.TargetURL, localHost); local && seoRedirectRuleMatches(input.SourcePath, input.MatchType, target.Path) {
		return ErrSEORedirectLoop
	}
	// Walk the existing relative chain one hop at a time and overlay the new
	// rule in memory. Calling the fully-collapsing resolver here used to miss a
	// cycle when A was edited to point at B while B already pointed at A: the
	// resolver followed A's *old* target and hid the intermediate hop.
	visited := make(map[string]bool)
	nextTarget := input.TargetURL
	for depth := 0; ; depth++ {
		parsedTarget, local := seoLocalRedirectURL(nextTarget, localHost)
		if !local {
			break
		}
		if depth >= 12 {
			return ErrSEORedirectLoop
		}
		nextPath := normalizeSEOPath(parsedTarget.Path)
		if nextPath == "" || visited[nextPath] {
			return ErrSEORedirectLoop
		}
		visited[nextPath] = true
		if input.Active && seoRedirectRuleMatches(input.SourcePath, input.MatchType, nextPath) {
			return ErrSEORedirectLoop
		}
		match, resolveErr := s.resolveRedirectOnce(ctx, countryID, nextPath, parsedTarget.RawQuery, false)
		if errors.Is(resolveErr, ErrNotFound) {
			break
		}
		if resolveErr != nil {
			return resolveErr
		}
		// If the stored rule being edited matched through its old source, it
		// disappears once this update is saved. The new rule was already tested
		// above, so there is no further existing hop to follow here.
		if match.ID == item.ID || match.StatusCode == 410 {
			break
		}
		nextTarget = match.TargetURL
	}
	hash := sha256.Sum256([]byte(input.SourcePath))
	item.SourcePath, item.SourceHash, item.TargetURL = input.SourcePath, hex.EncodeToString(hash[:]), input.TargetURL
	item.StatusCode, item.MatchType, item.PreserveQuery, item.Active = input.StatusCode, input.MatchType, input.PreserveQuery, input.Active
	item.UpdatedBy = optionalSEOUser(userID)
	return nil
}

func (s *seoService) ResolveRedirect(ctx context.Context, countryID database.CountryID, rawPath, rawQuery string) (*SEORedirectMatch, error) {
	return s.resolveRedirect(ctx, countryID, rawPath, rawQuery, true)
}

func (s *seoService) resolveRedirect(ctx context.Context, countryID database.CountryID, rawPath, rawQuery string, recordHit bool) (*SEORedirectMatch, error) {
	originalPath := normalizeSEOPath(rawPath)
	if originalPath == "" {
		return nil, ErrSEOInvalidURL
	}
	first, err := s.resolveRedirectOnce(ctx, countryID, originalPath, rawQuery, recordHit)
	if err != nil {
		return nil, err
	}
	result := *first
	visited := map[string]bool{originalPath: true}
	for hop := 0; hop < 12; hop++ {
		if result.StatusCode == 410 || result.TargetURL == "" || !strings.HasPrefix(result.TargetURL, "/") || strings.HasPrefix(result.TargetURL, "//") {
			return &result, nil
		}
		parsedTarget, parseErr := url.Parse(result.TargetURL)
		if parseErr != nil {
			return nil, ErrSEOInvalidURL
		}
		nextPath := normalizeSEOPath(parsedTarget.Path)
		if visited[nextPath] {
			return nil, ErrSEORedirectLoop
		}
		visited[nextPath] = true
		next, resolveErr := s.resolveRedirectOnce(ctx, countryID, nextPath, parsedTarget.RawQuery, recordHit)
		if errors.Is(resolveErr, ErrNotFound) {
			return &result, nil
		}
		if resolveErr != nil {
			return nil, resolveErr
		}
		result.TargetURL = next.TargetURL
		if next.StatusCode == 410 {
			result.StatusCode = 410
			result.TargetURL = ""
			return &result, nil
		}
	}
	return nil, ErrSEORedirectLoop
}

func (s *seoService) resolveRedirectOnce(ctx context.Context, countryID database.CountryID, rawPath, rawQuery string, recordHit bool) (*SEORedirectMatch, error) {
	path := normalizeSEOPath(rawPath)
	if path == "" {
		return nil, ErrSEOInvalidURL
	}
	hash := sha256.Sum256([]byte(path))
	item, err := s.repo.FindExactRedirect(ctx, countryID, hex.EncodeToString(hash[:]))
	if errors.Is(err, gorm.ErrRecordNotFound) {
		candidates, listErr := s.repo.ListCandidateRedirects(ctx, countryID)
		if listErr != nil {
			return nil, listErr
		}
		for i := range candidates {
			candidate := &candidates[i]
			switch candidate.MatchType {
			case models.SEORedirectMatchPrefix:
				if seoPathHasPrefix(path, candidate.SourcePath) {
					item = candidate
					err = nil
					candidate.TargetURL = strings.Replace(candidate.TargetURL, "{path}", strings.TrimPrefix(path, candidate.SourcePath), 1)
				}
			case models.SEORedirectMatchRegex:
				re, compileErr := s.cachedRedirectRegex(candidate.SourcePath)
				if compileErr == nil && re.MatchString(path) {
					candidate.TargetURL = re.ReplaceAllString(path, candidate.TargetURL)
					item = candidate
					err = nil
				}
			}
			if err == nil {
				break
			}
		}
	}
	if err != nil {
		return nil, MapError(err)
	}
	target := item.TargetURL
	if item.PreserveQuery && rawQuery != "" && target != "" {
		separator := "?"
		if strings.Contains(target, "?") {
			separator = "&"
		}
		target += separator + strings.TrimPrefix(rawQuery, "?")
	}
	if recordHit {
		_ = s.repo.RecordRedirectHit(ctx, countryID, item.ID)
	}
	return &SEORedirectMatch{ID: item.ID, TargetURL: target, StatusCode: item.StatusCode, PreserveQuery: item.PreserveQuery}, nil
}

// cachedRedirectRegex compiles a regex-redirect source once and reuses it. The
// 404 resolver runs on every site miss and previously recompiled every regex
// candidate (up to 500) on each request. Sources are validated at write time, so
// a compile failure here means corrupt data — it is simply skipped, not cached.
func (s *seoService) cachedRedirectRegex(pattern string) (*regexp.Regexp, error) {
	if cached, ok := s.redirectRE.Load(pattern); ok {
		return cached.(*regexp.Regexp), nil
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}
	s.redirectRE.Store(pattern, re)
	return re, nil
}

func (s *seoService) ListRedirects(ctx context.Context, countryID database.CountryID, search string, limit, offset int) ([]models.SEORedirect, int64, error) {
	return s.repo.ListRedirects(ctx, countryID, search, limit, offset)
}
func (s *seoService) DeleteRedirect(ctx context.Context, countryID database.CountryID, id uint) error {
	return MapError(s.repo.DeleteRedirect(ctx, countryID, id))
}

func (s *seoService) Record404(ctx context.Context, countryID database.CountryID, path, query, referrer, userAgent string) error {
	path = normalizeSEOPath(path)
	if path == "" {
		return ErrSEOInvalidURL
	}
	hash := sha256.Sum256([]byte(path))
	now := time.Now().UTC()
	item := &models.SEO404Log{CountryCode: database.CountryCode(countryID), Path: truncateSEORunes(path, 1500), PathHash: hex.EncodeToString(hash[:]), LastQuery: sanitizeSEOQuery(query), LastReferrer: sanitizeSEOReferrer(referrer), LastUserAgent: truncateSEORunes(userAgent, 500), HitCount: 1, FirstSeenAt: now, LastSeenAt: now}
	return s.repo.Record404(ctx, countryID, item)
}
func (s *seoService) List404(ctx context.Context, countryID database.CountryID, resolved bool, search string, limit, offset int) ([]models.SEO404Log, int64, error) {
	return s.repo.List404(ctx, countryID, resolved, search, limit, offset)
}
func (s *seoService) Resolve404(ctx context.Context, countryID database.CountryID, id uint, redirectID *uint) error {
	if redirectID != nil {
		if _, err := s.repo.GetRedirect(ctx, countryID, *redirectID); err != nil {
			return MapError(err)
		}
	}
	return MapError(s.repo.Resolve404(ctx, countryID, id, redirectID))
}
func (s *seoService) Clear404(ctx context.Context, countryID database.CountryID, resolved bool) error {
	return s.repo.Clear404(ctx, countryID, resolved)
}

func (s *seoService) LinkSuggestions(ctx context.Context, countryID database.CountryID, contentType string, contentID uint, limit int) ([]SEOLinkSuggestion, error) {
	source, err := s.repo.GetContent(ctx, countryID, contentType, contentID)
	if err != nil {
		return nil, MapError(err)
	}
	candidates, _, err := s.repo.ListContent(ctx, countryID, "", "", 600, 0)
	if err != nil {
		return nil, err
	}
	sourceTokens := seoTokenSet(source.Title + " " + seoPlainText(source.Content))
	existing := make(map[string]bool)
	for _, match := range seoLinkPattern.FindAllStringSubmatch(source.Content, -1) {
		if len(match) > 1 {
			existing[match[1]] = true
		}
	}
	result := make([]SEOLinkSuggestion, 0)
	for _, candidate := range candidates {
		if candidate.ContentType == contentType && candidate.ContentID == contentID || !candidate.Published {
			continue
		}
		path := seoContentPath(database.CountryCode(countryID), candidate.ContentType, candidate.ContentID)
		if existing[path] {
			continue
		}
		score := seoJaccard(sourceTokens, seoTokenSet(candidate.Title+" "+candidate.Keywords))
		if score < 0.05 {
			continue
		}
		result = append(result, SEOLinkSuggestion{ContentType: candidate.ContentType, ContentID: candidate.ContentID, Title: candidate.Title, URL: path, Score: mathRound(score*100, 1), Reason: "تشابه موضوعي بين كلمات العنوان والمحتوى"})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Score > result[j].Score })
	if limit <= 0 || limit > 30 {
		limit = 10
	}
	if len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}

func (s *seoService) StartAudit(ctx context.Context, countryID database.CountryID, userID uint) (*models.SEOAuditRun, error) {
	s.auditMu.Lock()
	defer s.auditMu.Unlock()
	if running, err := s.repo.FindRunningAudit(ctx, countryID); err == nil {
		if time.Since(running.StartedAt) < 30*time.Minute {
			return running, ErrSEOAuditRunning
		}
		finished := time.Now().UTC()
		running.Status, running.ErrorMessage, running.FinishedAt = models.SEOAuditStatusFailed, "انتهت مهلة التدقيق السابق", &finished
		_ = s.repo.UpdateAuditRun(ctx, countryID, running)
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, MapError(err)
	}
	run := &models.SEOAuditRun{CountryCode: database.CountryCode(countryID), Status: models.SEOAuditStatusRunning, TriggeredBy: optionalSEOUser(userID), StartedAt: time.Now().UTC()}
	if err := s.repo.CreateAuditRun(ctx, countryID, run); err != nil {
		return nil, err
	}
	go s.runAudit(database.CountryID(countryID), run.ID)
	return run, nil
}

func (s *seoService) runAudit(countryID database.CountryID, runID uint) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	run, err := s.repo.GetAuditRun(ctx, countryID, runID)
	if err != nil {
		return
	}
	finish := func(status, message string) {
		now := time.Now().UTC()
		run.Status, run.ErrorMessage, run.FinishedAt = status, message, &now
		finishCtx, finishCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer finishCancel()
		_ = s.repo.UpdateAuditRun(finishCtx, countryID, run)
	}
	contents := make([]repositories.SEOContent, 0)
	for _, contentType := range []string{models.SEOContentTypeArticle, models.SEOContentTypePost} {
		for offset := 0; ; {
			batch, _, listErr := s.repo.ListContent(ctx, countryID, contentType, "", 500, offset)
			if listErr != nil {
				finish(models.SEOAuditStatusFailed, listErr.Error())
				return
			}
			contents = append(contents, batch...)
			if len(batch) == 0 || len(batch) < 500 {
				break
			}
			offset += len(batch)
		}
	}
	published := contents[:0]
	for _, content := range contents {
		if content.Published {
			published = append(published, content)
		}
	}
	contents = published
	run.TotalURLs = len(contents)
	_ = s.repo.UpdateAuditRun(ctx, countryID, run)
	issues := make([]models.SEOIssue, 0)
	// Fetch settings once for the whole run instead of once per audited URL
	// (getEffective used to re-fetch them on every iteration).
	settings, _ := s.settings.GetPublic(ctx, countryID)
	localHost := ""
	if site, parseErr := url.Parse(firstSEOValue(settings["canonical_url"], settings["site_url"])); parseErr == nil {
		localHost = site.Hostname()
	}
	linkAvailability := make(map[string]*bool)
	for _, summary := range contents {
		content, getErr := s.repo.GetContent(ctx, countryID, summary.ContentType, summary.ContentID)
		if getErr != nil {
			continue
		}
		metadata, metaErr := s.repo.FindMetadata(ctx, countryID, summary.ContentType, summary.ContentID)
		if errors.Is(metaErr, gorm.ErrRecordNotFound) {
			metadata = defaultSEOMetadata(database.CountryCode(countryID), summary.ContentType, summary.ContentID)
		} else if metaErr != nil {
			continue
		}
		// The audit intentionally re-analyzes against current content rather than
		// trusting the stored analysis, so a content edit that lowered quality
		// without a SEO re-save still surfaces here.
		fields := resolveSEOFields(countryID, summary.ContentType, summary.ContentID, content, metadata, settings)
		analysis := AnalyzeSEO(seoAnalysisInput(content, metadata, fields))
		urlPath := seoContentPath(database.CountryCode(countryID), summary.ContentType, summary.ContentID)
		for _, check := range analysis.Checks {
			if check.Status == "good" {
				continue
			}
			severity := models.SEOIssueSeverityNotice
			if check.Status == "warning" {
				severity = models.SEOIssueSeverityWarning
			}
			if check.Status == "error" {
				severity = models.SEOIssueSeverityError
			}
			issues = append(issues, models.SEOIssue{RunID: runID, CountryCode: database.CountryCode(countryID), ContentType: content.ContentType, ContentID: content.ContentID, URL: urlPath, Code: check.Code, Severity: severity, Message: check.Message, CreatedAt: time.Now().UTC()})
		}
		seenTargets := make(map[string]bool)
		for _, match := range seoLinkPattern.FindAllStringSubmatch(content.Content, -1) {
			if len(match) < 2 {
				continue
			}
			target, ok := parseSEOContentLink(match[1], localHost)
			if !ok || seenTargets[target.Key()] {
				continue
			}
			seenTargets[target.Key()] = true
			available := linkAvailability[target.Key()]
			if available == nil {
				linked, linkedErr := s.repo.GetContent(ctx, database.CountryIDFromHeader(target.CountryCode), target.ContentType, target.ContentID)
				if linkedErr != nil && !errors.Is(linkedErr, gorm.ErrRecordNotFound) {
					// Infrastructure failures are not broken links. Leave the
					// result unknown so a later audit can retry it.
					continue
				}
				value := linkedErr == nil && linked != nil && linked.Published
				available = &value
				linkAvailability[target.Key()] = available
			}
			if !*available {
				issues = append(issues, models.SEOIssue{RunID: runID, CountryCode: database.CountryCode(countryID), ContentType: content.ContentType, ContentID: content.ContentID, URL: urlPath, Code: "broken_internal_link", Severity: models.SEOIssueSeverityError, Message: "رابط داخلي يشير إلى محتوى مفقود أو غير منشور: " + target.Path, CreatedAt: time.Now().UTC()})
			}
		}
		run.CheckedURLs++
		if run.CheckedURLs%50 == 0 {
			_ = s.repo.UpdateAuditRun(ctx, countryID, run)
		}
	}
	if err := s.repo.ReplaceAuditIssues(ctx, countryID, runID, issues); err != nil {
		finish(models.SEOAuditStatusFailed, err.Error())
		return
	}
	for _, issue := range issues {
		if issue.Severity == models.SEOIssueSeverityError {
			run.Errors++
		} else if issue.Severity == models.SEOIssueSeverityWarning {
			run.Warnings++
		} else {
			run.Notices++
		}
	}
	finish(models.SEOAuditStatusCompleted, "")
}

func (s *seoService) GetAudit(ctx context.Context, countryID database.CountryID, id uint) (*models.SEOAuditRun, error) {
	return s.repo.GetAuditRun(ctx, countryID, id)
}
func (s *seoService) ListAudits(ctx context.Context, countryID database.CountryID, limit int) ([]models.SEOAuditRun, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	return s.repo.ListAuditRuns(ctx, countryID, limit)
}
func (s *seoService) ListAuditIssues(ctx context.Context, countryID database.CountryID, runID uint, severity string, limit, offset int) ([]models.SEOIssue, int64, error) {
	return s.repo.ListAuditIssues(ctx, countryID, runID, severity, limit, offset)
}

func (s *seoService) UpsertAuthor(ctx context.Context, item models.SEOAuthorProfile) (*models.SEOAuthorProfile, error) {
	item.PublicName = strings.TrimSpace(item.PublicName)
	if item.UserID == 0 || item.PublicName == "" {
		return nil, fmt.Errorf("%w: user_id and public_name are required", ErrSEOInvalidInput)
	}
	for value, max := range map[*string]int{
		&item.PublicName: 255, &item.Headline: 500, &item.Biography: 10000,
		&item.Expertise: 10000, &item.Credentials: 10000, &item.Education: 10000,
		&item.Awards: 10000, &item.Employer: 500, &item.ProfileURL: 1000, &item.ImageURL: 1000,
	} {
		*value = strings.TrimSpace(*value)
		if utf8.RuneCountInString(*value) > max {
			return nil, fmt.Errorf("%w: author field exceeds %d characters", ErrSEOInvalidInput, max)
		}
	}
	for _, value := range []string{item.ProfileURL, item.ImageURL} {
		if value != "" && !validSEOAbsoluteURL(value) {
			return nil, ErrSEOInvalidURL
		}
	}
	if len(item.SocialLinksJSON) > 32768 || item.SocialLinksJSON != "" && !json.Valid([]byte(item.SocialLinksJSON)) {
		return nil, fmt.Errorf("%w: invalid social links JSON", ErrSEOInvalidInput)
	}
	if len(item.KnowsAboutJSON) > 32768 || item.KnowsAboutJSON != "" && !json.Valid([]byte(item.KnowsAboutJSON)) {
		return nil, fmt.Errorf("%w: invalid expertise JSON", ErrSEOInvalidInput)
	}
	if item.SocialLinksJSON != "" {
		var links map[string]string
		if json.Unmarshal([]byte(item.SocialLinksJSON), &links) != nil || len(links) > 20 {
			return nil, fmt.Errorf("%w: invalid social links JSON", ErrSEOInvalidInput)
		}
		for network, link := range links {
			if utf8.RuneCountInString(network) > 50 || !validSEOAbsoluteURL(strings.TrimSpace(link)) {
				return nil, fmt.Errorf("%w: invalid social profile URL", ErrSEOInvalidInput)
			}
		}
		normalized, _ := json.Marshal(links)
		item.SocialLinksJSON = string(normalized)
	}
	if item.KnowsAboutJSON != "" {
		var topics []string
		if json.Unmarshal([]byte(item.KnowsAboutJSON), &topics) != nil || len(topics) > 50 {
			return nil, fmt.Errorf("%w: invalid expertise JSON", ErrSEOInvalidInput)
		}
		for i := range topics {
			topics[i] = strings.TrimSpace(topics[i])
			if topics[i] == "" || utf8.RuneCountInString(topics[i]) > 150 {
				return nil, fmt.Errorf("%w: invalid expertise value", ErrSEOInvalidInput)
			}
		}
		normalized, _ := json.Marshal(topics)
		item.KnowsAboutJSON = string(normalized)
	}
	exists, err := s.repo.UserExists(ctx, item.UserID)
	if err != nil {
		return nil, MapError(err)
	}
	if !exists {
		return nil, ErrNotFound
	}
	if err := s.repo.UpsertAuthor(ctx, &item); err != nil {
		return nil, MapError(err)
	}
	return &item, nil
}
func (s *seoService) GetAuthor(ctx context.Context, userID uint) (*models.SEOAuthorProfile, error) {
	return s.repo.GetAuthor(ctx, userID)
}
func (s *seoService) ListAuthors(ctx context.Context, active bool) ([]models.SEOAuthorProfile, error) {
	return s.repo.ListAuthors(ctx, active)
}

func (s *seoService) SubmitIndexNow(ctx context.Context, countryID database.CountryID, urls []string) ([]models.SEOIndexNowSubmission, error) {
	settings, err := s.settings.GetAll(ctx, countryID)
	if err != nil {
		return nil, err
	}
	if !seoBool(settings["indexnow_enabled"]) {
		return nil, fmt.Errorf("%w: IndexNow is disabled", ErrSEOInvalidInput)
	}
	key := strings.TrimSpace(settings["indexnow_key"])
	if !regexp.MustCompile(`^[A-Za-z0-9_-]{8,128}$`).MatchString(key) {
		return nil, fmt.Errorf("%w: invalid IndexNow key", ErrSEOInvalidInput)
	}
	hostURL := strings.TrimRight(firstSEOValue(settings["canonical_url"], settings["site_url"]), "/")
	parsedHost, err := url.Parse(hostURL)
	if err != nil || parsedHost.Host == "" || (parsedHost.Scheme != "http" && parsedHost.Scheme != "https") {
		return nil, ErrSEOInvalidURL
	}
	clean := make([]string, 0, len(urls))
	seen := make(map[string]bool)
	for _, value := range urls {
		value = strings.TrimSpace(value)
		if strings.HasPrefix(value, "/") {
			value = hostURL + value
		}
		parsedURL, parseErr := url.Parse(value)
		if parseErr == nil && validSEOAbsoluteURL(value) && strings.EqualFold(parsedURL.Host, parsedHost.Host) && !seen[value] {
			seen[value] = true
			clean = append(clean, value)
		}
		if len(clean) == 10000 {
			break
		}
	}
	if len(clean) == 0 {
		return nil, ErrSEOInvalidURL
	}
	now := time.Now().UTC()
	rows := make([]models.SEOIndexNowSubmission, 0, len(clean))
	for _, value := range clean {
		row := models.SEOIndexNowSubmission{CountryCode: database.CountryCode(countryID), URL: value, Status: "pending", SubmittedAt: now}
		if err := s.repo.CreateIndexNowSubmission(ctx, countryID, &row); err == nil {
			rows = append(rows, row)
		}
	}
	payload, _ := json.Marshal(map[string]interface{}{"host": parsedHost.Host, "key": key, "keyLocation": hostURL + "/indexnow/" + database.CountryCode(countryID) + "/" + key + ".txt", "urlList": clean})
	// Keep the destination fixed: allowing a database setting to replace this
	// endpoint would turn an admin-facing SEO field into a server-side request
	// primitive. IndexNow's official endpoint accepts every supported engine.
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.indexnow.org/indexnow", bytes.NewReader(payload))
	if err != nil {
		return rows, fmt.Errorf("%w: request creation", ErrSEOIntegration)
	}
	req.Header.Set("Content-Type", "application/json")
	response, requestErr := s.http.Do(req)
	status := 0
	if response != nil {
		status = response.StatusCode
		response.Body.Close()
	}
	completed := time.Now().UTC()
	for i := range rows {
		rows[i].HTTPStatus, rows[i].CompletedAt = status, &completed
		if requestErr != nil {
			rows[i].Status, rows[i].ErrorMessage = "failed", truncateSEORunes(requestErr.Error(), 2000)
		} else if status >= 200 && status < 300 {
			rows[i].Status = "completed"
		} else {
			rows[i].Status, rows[i].ErrorMessage = "failed", "IndexNow HTTP "+strconv.Itoa(status)
		}
		_ = s.repo.UpdateIndexNowSubmission(ctx, countryID, &rows[i])
	}
	if requestErr != nil {
		return rows, fmt.Errorf("%w: %v", ErrSEOIntegration, requestErr)
	}
	if status < 200 || status >= 300 {
		return rows, fmt.Errorf("%w: IndexNow returned HTTP %d", ErrSEOIntegration, status)
	}
	return rows, nil
}

func (s *seoService) submitIndexNowBackground(countryID database.CountryID, contentType string, contentID uint) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_, _ = s.SubmitIndexNow(ctx, countryID, []string{seoContentPath(database.CountryCode(countryID), contentType, contentID)})
}

func buildSEORobots(item *models.SEOMetadata) string {
	values := []string{"index", "follow"}
	if !item.RobotsIndex {
		values[0] = "noindex"
	}
	if !item.RobotsFollow {
		values[1] = "nofollow"
	}
	if item.RobotsNoArchive {
		values = append(values, "noarchive")
	}
	if item.RobotsNoSnippet {
		values = append(values, "nosnippet")
	}
	values = append(values, "max-snippet:"+strconv.Itoa(item.MaxSnippet), "max-image-preview:"+firstSEOValue(item.MaxImagePreview, "large"), "max-video-preview:"+strconv.Itoa(item.MaxVideoPreview))
	return strings.Join(values, ", ")
}
func validSEOAbsoluteURL(value string) bool {
	parsed, err := url.ParseRequestURI(value)
	return err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != ""
}
func validSEORedirectTarget(value string) bool {
	return strings.HasPrefix(value, "/") && !strings.HasPrefix(value, "//") || validSEOAbsoluteURL(value)
}
func normalizeSEOPath(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if parsed, err := url.Parse(value); err == nil && parsed.Path != "" {
		value = parsed.Path
	}
	if !strings.HasPrefix(value, "/") && !strings.HasPrefix(value, "^") {
		value = "/" + value
	}
	if len(value) > 1 && strings.HasSuffix(value, "/") {
		value = strings.TrimRight(value, "/")
	}
	return value
}

func seoPathHasPrefix(path, prefix string) bool {
	if prefix == "/" {
		return strings.HasPrefix(path, "/")
	}
	prefix = strings.TrimRight(prefix, "/")
	return path == prefix || strings.HasPrefix(path, prefix+"/")
}

func seoRedirectRuleMatches(source, matchType, path string) bool {
	switch matchType {
	case models.SEORedirectMatchExact:
		return normalizeSEOPath(source) == normalizeSEOPath(path)
	case models.SEORedirectMatchPrefix:
		return seoPathHasPrefix(normalizeSEOPath(path), normalizeSEOPath(source))
	case models.SEORedirectMatchRegex:
		re, err := regexp.Compile(source)
		return err == nil && re.MatchString(normalizeSEOPath(path))
	default:
		return false
	}
}

func seoLocalRedirectURL(rawTarget, localHost string) (*url.URL, bool) {
	target, err := url.Parse(strings.TrimSpace(rawTarget))
	if err != nil || target.Path == "" || strings.HasPrefix(rawTarget, "//") {
		return nil, false
	}
	if target.IsAbs() && (localHost == "" || !strings.EqualFold(target.Host, localHost)) {
		return target, false
	}
	return target, strings.HasPrefix(target.Path, "/")
}

type seoContentLinkTarget struct {
	CountryCode string
	ContentType string
	ContentID   uint
	Path        string
}

func (target seoContentLinkTarget) Key() string {
	return target.CountryCode + ":" + target.ContentType + ":" + strconv.FormatUint(uint64(target.ContentID), 10)
}

func parseSEOContentLink(rawHref, localHost string) (seoContentLinkTarget, bool) {
	parsed, err := url.Parse(strings.TrimSpace(html.UnescapeString(rawHref)))
	if err != nil || parsed.Path == "" {
		return seoContentLinkTarget{}, false
	}
	if parsed.IsAbs() {
		if parsed.Scheme != "http" && parsed.Scheme != "https" || localHost == "" || !strings.EqualFold(parsed.Hostname(), localHost) {
			return seoContentLinkTarget{}, false
		}
	} else if parsed.Host != "" {
		return seoContentLinkTarget{}, false
	}
	parts := seoContentLinkPath.FindStringSubmatch(parsed.Path)
	if len(parts) != 4 {
		return seoContentLinkTarget{}, false
	}
	id, parseErr := strconv.ParseUint(parts[3], 10, 64)
	if parseErr != nil || id == 0 {
		return seoContentLinkTarget{}, false
	}
	contentType := models.SEOContentTypePost
	if parts[2] == "lesson/articles" {
		contentType = models.SEOContentTypeArticle
	}
	return seoContentLinkTarget{CountryCode: parts[1], ContentType: contentType, ContentID: uint(id), Path: parsed.Path}, true
}

func seoContentPath(country, contentType string, id uint) string {
	if contentType == models.SEOContentTypeArticle {
		return fmt.Sprintf("/%s/lesson/articles/%d", country, id)
	}
	return fmt.Sprintf("/%s/posts/%d", country, id)
}
func firstSEOValue(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
func seoExcerpt(value string, max int) string {
	value = seoPlainText(value)
	runes := []rune(value)
	if len(runes) <= max {
		return value
	}
	return strings.TrimSpace(string(runes[:max])) + "…"
}
func optionalSEOUser(id uint) *uint {
	if id == 0 {
		return nil
	}
	value := id
	return &value
}
func truncateSEORunes(value string, max int) string {
	runes := []rune(value)
	if len(runes) > max {
		return string(runes[:max])
	}
	return value
}

func sanitizeSEOQuery(raw string) string {
	values, err := url.ParseQuery(strings.TrimPrefix(strings.TrimSpace(raw), "?"))
	if err != nil {
		return ""
	}
	for key, items := range values {
		if seoSensitiveQueryKey.MatchString(key) {
			values[key] = []string{"[redacted]"}
			continue
		}
		for i := range items {
			items[i] = truncateSEORunes(items[i], 200)
		}
	}
	return truncateSEORunes(values.Encode(), 2000)
}

func sanitizeSEOReferrer(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" {
		return ""
	}
	parsed.RawQuery, parsed.Fragment, parsed.User = "", "", nil
	return truncateSEORunes(parsed.String(), 1500)
}

func seoBool(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

func mapSEOConflict(err error) error {
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "duplicate") {
		return ErrSEOConflict
	}
	return MapError(err)
}

func mathRound(value float64, places int) float64 {
	factor := 1.0
	for i := 0; i < places; i++ {
		factor *= 10
	}
	return float64(int(value*factor+0.5)) / factor
}

var seoStopwords = map[string]bool{"في": true, "من": true, "على": true, "الى": true, "إلى": true, "عن": true, "هذا": true, "هذه": true, "ذلك": true, "التي": true, "الذي": true, "مع": true, "او": true, "أو": true, "ثم": true, "كل": true, "ما": true, "هو": true, "هي": true, "و": true, "a": true, "the": true, "and": true, "or": true, "of": true, "to": true, "for": true, "in": true}

func seoTokenSet(value string) map[string]bool {
	result := make(map[string]bool)
	for _, token := range seoWords(normalizeSEOArabic(value)) {
		if len([]rune(token)) >= 3 && !seoStopwords[token] {
			result[token] = true
		}
	}
	return result
}
func seoJaccard(a, b map[string]bool) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	intersection := 0
	union := make(map[string]bool, len(a)+len(b))
	for key := range a {
		union[key] = true
		if b[key] {
			intersection++
		}
	}
	for key := range b {
		union[key] = true
	}
	return float64(intersection) / float64(len(union))
}

var _ SEOService = (*seoService)(nil)
