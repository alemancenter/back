package services

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/imanjo/fiber-api/internal/config"
	"github.com/imanjo/fiber-api/internal/contentquality"
	"github.com/imanjo/fiber-api/internal/models"
	"github.com/imanjo/fiber-api/internal/repositories"
	"github.com/imanjo/fiber-api/pkg/logger"
	"go.uber.org/zap"
)

type SitemapService interface {
	GetStatus(dbCode string) map[string]SitemapInfo
	GenerateAll(dbCode string) []error
	ScheduleGenerate(dbCode string)
	Delete(sitemapType, dbCode string) error
}

type SitemapStatusResponse struct {
	Database string                 `json:"database"`
	Sitemaps map[string]SitemapInfo `json:"sitemaps"`
}

type sitemapService struct {
	repo          repositories.SitemapRepository
	generationMu  sync.Mutex
	refreshMu     sync.Mutex
	refreshTimers map[string]*time.Timer
}

func NewSitemapService(repo repositories.SitemapRepository) SitemapService {
	return &sitemapService{repo: repo, refreshTimers: make(map[string]*time.Timer)}
}

type SitemapInfo struct {
	Exists       bool    `json:"exists"`
	LastModified *string `json:"last_modified"`
	URL          *string `json:"url"`
	SizeBytes    int64   `json:"size_bytes"`
	Entries      int     `json:"entries"`
}

// --- XML types ---
type urlEntry struct {
	Loc        string `xml:"loc"`
	LastMod    string `xml:"lastmod,omitempty"`
	ChangeFreq string `xml:"changefreq,omitempty"`
	Priority   string `xml:"priority,omitempty"`
}

type urlSet struct {
	XMLName xml.Name   `xml:"urlset"`
	Xmlns   string     `xml:"xmlns,attr"`
	URLs    []urlEntry `xml:"url"`
}

type sitemapIndexEntry struct {
	Loc     string `xml:"loc"`
	LastMod string `xml:"lastmod,omitempty"`
}

type sitemapIndex struct {
	XMLName  xml.Name            `xml:"sitemapindex"`
	Xmlns    string              `xml:"xmlns,attr"`
	Sitemaps []sitemapIndexEntry `xml:"sitemap"`
}

type imageSitemapItem struct {
	Loc   string `xml:"image:loc"`
	Title string `xml:"image:title,omitempty"`
}
type imageSitemapEntry struct {
	Loc    string             `xml:"loc"`
	Images []imageSitemapItem `xml:"image:image"`
}
type imageURLSet struct {
	XMLName    xml.Name            `xml:"urlset"`
	Xmlns      string              `xml:"xmlns,attr"`
	XmlnsImage string              `xml:"xmlns:image,attr"`
	URLs       []imageSitemapEntry `xml:"url"`
}

type videoSitemapItem struct {
	ThumbnailLoc string `xml:"video:thumbnail_loc"`
	ContentLoc   string `xml:"video:content_loc"`
	Title        string `xml:"video:title"`
	Description  string `xml:"video:description"`
}
type videoSitemapEntry struct {
	Loc    string             `xml:"loc"`
	Videos []videoSitemapItem `xml:"video:video"`
}
type videoURLSet struct {
	XMLName    xml.Name            `xml:"urlset"`
	Xmlns      string              `xml:"xmlns,attr"`
	XmlnsVideo string              `xml:"xmlns:video,attr"`
	URLs       []videoSitemapEntry `xml:"url"`
}

type newsPublication struct {
	Name     string `xml:"news:name"`
	Language string `xml:"news:language"`
}
type newsSitemapItem struct {
	Publication     newsPublication `xml:"news:publication"`
	PublicationDate string          `xml:"news:publication_date"`
	Title           string          `xml:"news:title"`
}
type newsSitemapEntry struct {
	Loc  string          `xml:"loc"`
	News newsSitemapItem `xml:"news:news"`
}
type newsURLSet struct {
	XMLName   xml.Name           `xml:"urlset"`
	Xmlns     string             `xml:"xmlns,attr"`
	XmlnsNews string             `xml:"xmlns:news,attr"`
	URLs      []newsSitemapEntry `xml:"url"`
}

// --- helpers ---
func (s *sitemapService) sitemapDir() string {
	return filepath.Join(config.Get().Storage.Path, "sitemaps")
}

func (s *sitemapService) sitemapFilename(sitemapType, dbCode string) string {
	return filepath.Join(s.sitemapDir(), fmt.Sprintf("sitemap_%s_%s.xml", sitemapType, dbCode))
}

func (s *sitemapService) writeXML(path string, payload any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return MapError(err)
	}
	f, err := os.Create(path)
	if err != nil {
		return MapError(err)
	}
	defer f.Close()
	f.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	enc := xml.NewEncoder(f)
	enc.Indent("", "  ")
	return enc.Encode(payload)
}

func (s *sitemapService) siteURL() string {
	if url, err := s.repo.GetSiteURL(); err == nil {
		if normalized := strings.TrimRight(strings.TrimSpace(url), "/"); normalized != "" {
			return normalized
		}
	}

	cfg := config.Get()
	if normalized := strings.TrimRight(strings.TrimSpace(cfg.Frontend.URL), "/"); normalized != "" {
		return normalized
	}
	return strings.TrimRight(strings.TrimSpace(cfg.App.URL), "/")
}

func (s *sitemapService) fileInfo(path string) (exists bool, lastMod string, sizeBytes int64, entries int) {
	info, err := os.Stat(path)
	if err != nil {
		return false, "", 0, 0
	}
	content, _ := os.ReadFile(path)
	entries = bytes.Count(content, []byte("<url>")) + bytes.Count(content, []byte("<sitemap>"))
	return true, info.ModTime().UTC().Format(time.RFC3339), info.Size(), entries
}

func sitemapQualityIndexable(decision *models.ContentAIDecision) bool {
	return contentquality.Evaluate(decision).Indexable
}

func qualityDecisionForSitemap(decisions map[uint]models.ContentAIDecision, id uint) *models.ContentAIDecision {
	decision, ok := decisions[id]
	if !ok {
		return nil
	}
	return &decision
}

func (s *sitemapService) GetStatus(dbCode string) map[string]SitemapInfo {
	types := []string{"articles", "post", "static", "images", "videos", "news", "index"}
	baseURL := s.siteURL()
	result := make(map[string]SitemapInfo, len(types))

	for _, t := range types {
		path := s.sitemapFilename(t, dbCode)
		exists, mod, sizeBytes, entries := s.fileInfo(path)
		info := SitemapInfo{Exists: exists, SizeBytes: sizeBytes, Entries: entries}
		if exists {
			info.LastModified = &mod
			u := baseURL + "/storage/sitemaps/" + fmt.Sprintf("sitemap_%s_%s.xml", t, dbCode)
			info.URL = &u
		}
		result[t] = info
	}

	return result
}

func (s *sitemapService) GenerateAll(dbCode string) []error {
	// Only one generation may write sitemap files at a time. This also protects
	// manual generation from overlapping an automatic content-triggered refresh.
	s.generationMu.Lock()
	defer s.generationMu.Unlock()

	base := s.siteURL()
	cc := dbCode // country code used in frontend URL segments
	features, featureErr := s.repo.GetSitemapFeatures(dbCode)
	if featureErr != nil {
		return []error{featureErr}
	}

	var wg sync.WaitGroup
	errs := make([]error, 7)

	// Articles
	wg.Add(1)
	go func() {
		defer wg.Done()
		rows, err := s.repo.GetActiveArticles(dbCode)
		if err != nil {
			errs[0] = err
			return
		}
		decisions, err := s.repo.GetLatestQualityDecisions(dbCode, "article")
		if err != nil {
			errs[0] = err
			return
		}
		corrupted, err := s.repo.GetCorruptedContentIDs(dbCode, "article")
		if err != nil {
			errs[0] = err
			return
		}
		set := urlSet{Xmlns: "http://www.sitemaps.org/schemas/sitemap/0.9"}
		for _, r := range rows {
			if !sitemapQualityIndexable(qualityDecisionForSitemap(decisions, r.ID)) {
				continue
			}
			if _, blocked := corrupted[r.ID]; blocked {
				continue
			}
			set.URLs = append(set.URLs, urlEntry{
				Loc:        fmt.Sprintf("%s/%s/lesson/articles/%d", base, cc, r.ID),
				LastMod:    r.UpdatedAt.UTC().Format(time.RFC3339),
				ChangeFreq: "monthly",
				Priority:   "0.8",
			})
		}
		errs[0] = s.writeXML(s.sitemapFilename("articles", dbCode), set)
	}()

	// Posts
	wg.Add(1)
	go func() {
		defer wg.Done()
		rows, err := s.repo.GetActivePosts(dbCode)
		if err != nil {
			errs[1] = err
			return
		}
		decisions, err := s.repo.GetLatestQualityDecisions(dbCode, "post")
		if err != nil {
			errs[1] = err
			return
		}
		corrupted, err := s.repo.GetCorruptedContentIDs(dbCode, "post")
		if err != nil {
			errs[1] = err
			return
		}
		set := urlSet{Xmlns: "http://www.sitemaps.org/schemas/sitemap/0.9"}
		for _, r := range rows {
			if !sitemapQualityIndexable(qualityDecisionForSitemap(decisions, r.ID)) {
				continue
			}
			if _, blocked := corrupted[r.ID]; blocked {
				continue
			}
			set.URLs = append(set.URLs, urlEntry{
				Loc:        fmt.Sprintf("%s/%s/posts/%d", base, cc, r.ID),
				LastMod:    r.UpdatedAt.UTC().Format(time.RFC3339),
				ChangeFreq: "weekly",
				Priority:   "0.7",
			})
		}
		errs[1] = s.writeXML(s.sitemapFilename("post", dbCode), set)
	}()

	// Static pages (categories + school classes)
	wg.Add(1)
	go func() {
		defer wg.Done()
		set := urlSet{Xmlns: "http://www.sitemaps.org/schemas/sitemap/0.9"}

		// Home
		set.URLs = append(set.URLs, urlEntry{
			Loc:        fmt.Sprintf("%s/%s", base, cc),
			ChangeFreq: "daily",
			Priority:   "1.0",
		})

		// Categories
		cats, err := s.repo.GetActiveCategories(dbCode)
		if err == nil {
			for _, cat := range cats {
				set.URLs = append(set.URLs, urlEntry{
					Loc:        fmt.Sprintf("%s/%s/posts/category/%s", base, cc, cat.Slug),
					LastMod:    cat.UpdatedAt.UTC().Format(time.RFC3339),
					ChangeFreq: "weekly",
					Priority:   "0.6",
				})
			}
		}

		// School classes
		classes, err := s.repo.GetActiveSchoolClasses(dbCode)
		if err == nil {
			for _, cl := range classes {
				set.URLs = append(set.URLs, urlEntry{
					Loc:        fmt.Sprintf("%s/%s/lesson/%d", base, cc, cl.GradeLevel),
					LastMod:    cl.UpdatedAt.UTC().Format(time.RFC3339),
					ChangeFreq: "weekly",
					Priority:   "0.7",
				})
			}
		}

		errs[2] = s.writeXML(s.sitemapFilename("static", dbCode), set)
	}()

	// Images linked to indexable articles and posts.
	wg.Add(1)
	go func() {
		defer wg.Done()
		if !features["images"] {
			err := os.Remove(s.sitemapFilename("images", dbCode))
			if err != nil && !os.IsNotExist(err) {
				errs[3] = MapError(err)
			}
			return
		}
		rows, err := s.repo.GetSitemapImages(dbCode)
		if err != nil {
			errs[3] = err
			return
		}
		allowed, err := s.indexableContentIDs(dbCode)
		if err != nil {
			errs[3] = err
			return
		}
		grouped := make(map[string]*imageSitemapEntry)
		order := make([]string, 0)
		for _, row := range rows {
			key := fmt.Sprintf("%s:%d", row.ContentType, row.ContentID)
			if !allowed[key] {
				continue
			}
			entry := grouped[key]
			if entry == nil {
				entry = &imageSitemapEntry{Loc: base + seoSitemapContentPath(cc, row.ContentType, row.ContentID)}
				grouped[key] = entry
				order = append(order, key)
			}
			entry.Images = append(entry.Images, imageSitemapItem{Loc: sitemapImageURL(base, row.Path), Title: row.Title})
		}
		set := imageURLSet{Xmlns: "http://www.sitemaps.org/schemas/sitemap/0.9", XmlnsImage: "http://www.google.com/schemas/sitemap-image/1.1"}
		for _, key := range order {
			set.URLs = append(set.URLs, *grouped[key])
		}
		errs[3] = s.writeXML(s.sitemapFilename("images", dbCode), set)
	}()

	// Uploaded video attachments. An empty map is still valid and makes feature
	// availability explicit without inventing video metadata.
	wg.Add(1)
	go func() {
		defer wg.Done()
		if !features["videos"] {
			err := os.Remove(s.sitemapFilename("videos", dbCode))
			if err != nil && !os.IsNotExist(err) {
				errs[4] = MapError(err)
			}
			return
		}
		rows, err := s.repo.GetSitemapVideos(dbCode)
		if err != nil {
			errs[4] = err
			return
		}
		allowed, err := s.indexableContentIDs(dbCode)
		if err != nil {
			errs[4] = err
			return
		}
		grouped := make(map[string]*videoSitemapEntry)
		order := make([]string, 0)
		for _, row := range rows {
			if strings.TrimSpace(row.Thumbnail) == "" {
				// thumbnail_loc is mandatory in Google's video sitemap format;
				// omit videos that do not have a real thumbnail instead of
				// publishing structurally invalid or fabricated metadata.
				continue
			}
			key := fmt.Sprintf("%s:%d", row.ContentType, row.ContentID)
			if !allowed[key] {
				continue
			}
			entry := grouped[key]
			if entry == nil {
				entry = &videoSitemapEntry{Loc: base + seoSitemapContentPath(cc, row.ContentType, row.ContentID)}
				grouped[key] = entry
				order = append(order, key)
			}
			description := strings.TrimSpace(row.Description)
			if description == "" {
				description = row.Title
			}
			entry.Videos = append(entry.Videos, videoSitemapItem{ThumbnailLoc: sitemapImageURL(base, row.Thumbnail), ContentLoc: sitemapFileURL(base, row.Path), Title: row.Title, Description: description})
		}
		set := videoURLSet{Xmlns: "http://www.sitemaps.org/schemas/sitemap/0.9", XmlnsVideo: "http://www.google.com/schemas/sitemap-video/1.1"}
		for _, key := range order {
			set.URLs = append(set.URLs, *grouped[key])
		}
		errs[4] = s.writeXML(s.sitemapFilename("videos", dbCode), set)
	}()

	// Google News accepts URLs published in the trailing 48 hours.
	wg.Add(1)
	go func() {
		defer wg.Done()
		if !features["news"] {
			err := os.Remove(s.sitemapFilename("news", dbCode))
			if err != nil && !os.IsNotExist(err) {
				errs[5] = MapError(err)
			}
			return
		}
		rows, err := s.repo.GetRecentNews(dbCode, time.Now().Add(-48*time.Hour))
		if err != nil {
			errs[5] = err
			return
		}
		allowed, err := s.indexableContentIDs(dbCode)
		if err != nil {
			errs[5] = err
			return
		}
		set := newsURLSet{Xmlns: "http://www.sitemaps.org/schemas/sitemap/0.9", XmlnsNews: "http://www.google.com/schemas/sitemap-news/0.9"}
		for _, row := range rows {
			if !allowed[fmt.Sprintf("post:%d", row.ID)] {
				continue
			}
			set.URLs = append(set.URLs, newsSitemapEntry{
				Loc:  fmt.Sprintf("%s/%s/posts/%d", base, cc, row.ID),
				News: newsSitemapItem{Publication: newsPublication{Name: "موقع الإيمان", Language: "ar"}, PublicationDate: row.PublishedAt.UTC().Format(time.RFC3339), Title: row.Title},
			})
		}
		errs[5] = s.writeXML(s.sitemapFilename("news", dbCode), set)
	}()

	wg.Wait()

	// Download landing pages are utility pages and are intentionally noindex.
	// Remove any sitemap generated by older releases so it cannot remain discoverable.
	if err := os.Remove(s.sitemapFilename("download", dbCode)); err != nil && !os.IsNotExist(err) {
		errs[6] = MapError(err)
	}

	if errs[0] == nil && errs[1] == nil && errs[2] == nil && errs[3] == nil && errs[4] == nil && errs[5] == nil && errs[6] == nil {
		sitemapTypes := []string{"articles", "post", "static"}
		for _, sitemapType := range []string{"images", "videos", "news"} {
			if features[sitemapType] {
				sitemapTypes = append(sitemapTypes, sitemapType)
			}
		}
		errs[6] = s.writeSitemapIndex(
			dbCode,
			base,
			sitemapTypes,
		)
	}

	var actualErrors []error
	for _, e := range errs {
		if e != nil {
			actualErrors = append(actualErrors, e)
		}
	}

	return actualErrors
}

func (s *sitemapService) indexableContentIDs(dbCode string) (map[string]bool, error) {
	allowed := make(map[string]bool)
	for _, contentType := range []string{"article", "post"} {
		decisions, err := s.repo.GetLatestQualityDecisions(dbCode, contentType)
		if err != nil {
			return nil, err
		}
		corrupted, err := s.repo.GetCorruptedContentIDs(dbCode, contentType)
		if err != nil {
			return nil, err
		}
		if contentType == "article" {
			rows, err := s.repo.GetActiveArticles(dbCode)
			if err != nil {
				return nil, err
			}
			for _, row := range rows {
				if sitemapQualityIndexable(qualityDecisionForSitemap(decisions, row.ID)) {
					if _, blocked := corrupted[row.ID]; !blocked {
						allowed[fmt.Sprintf("article:%d", row.ID)] = true
					}
				}
			}
		} else {
			rows, err := s.repo.GetActivePosts(dbCode)
			if err != nil {
				return nil, err
			}
			for _, row := range rows {
				if sitemapQualityIndexable(qualityDecisionForSitemap(decisions, row.ID)) {
					if _, blocked := corrupted[row.ID]; !blocked {
						allowed[fmt.Sprintf("post:%d", row.ID)] = true
					}
				}
			}
		}
	}
	return allowed, nil
}

func seoSitemapContentPath(country, contentType string, id uint) string {
	if contentType == "article" {
		return fmt.Sprintf("/%s/lesson/articles/%d", country, id)
	}
	return fmt.Sprintf("/%s/posts/%d", country, id)
}

func sitemapImageURL(base, path string) string {
	path = strings.TrimSpace(strings.ReplaceAll(path, "\\", "/"))
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return path
	}
	relative := strings.TrimPrefix(strings.TrimPrefix(path, "/"), "storage/")
	if relative == "" {
		return ""
	}
	return base + "/api/img?src=" + url.QueryEscape(relative) + "&w=1600"
}

func sitemapFileURL(base, path string) string {
	path = strings.TrimSpace(strings.ReplaceAll(path, "\\", "/"))
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return path
	}
	return base + "/storage/" + strings.TrimPrefix(strings.TrimPrefix(path, "/"), "storage/")
}

// ScheduleGenerate coalesces rapid content changes into one background refresh.
// Content persistence is never rolled back if sitemap I/O fails; failures are
// logged so the dashboard can still be used to retry generation manually.
func (s *sitemapService) ScheduleGenerate(dbCode string) {
	if dbCode == "" {
		dbCode = "jo"
	}

	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()
	if timer, exists := s.refreshTimers[dbCode]; exists {
		timer.Reset(750 * time.Millisecond)
		return
	}

	var timer *time.Timer
	timer = time.AfterFunc(750*time.Millisecond, func() {
		s.refreshMu.Lock()
		if s.refreshTimers[dbCode] != timer {
			s.refreshMu.Unlock()
			return
		}
		delete(s.refreshTimers, dbCode)
		s.refreshMu.Unlock()

		errs := s.GenerateAll(dbCode)
		for _, err := range errs {
			logger.Error("automatic sitemap refresh failed", zap.String("database", dbCode), zap.Error(err))
		}
	})
	s.refreshTimers[dbCode] = timer
}

func (s *sitemapService) writeSitemapIndex(dbCode, baseURL string, sitemapTypes []string) error {
	index := sitemapIndex{Xmlns: "http://www.sitemaps.org/schemas/sitemap/0.9"}

	for _, sitemapType := range sitemapTypes {
		path := s.sitemapFilename(sitemapType, dbCode)
		exists, lastMod, _, _ := s.fileInfo(path)
		if !exists {
			continue
		}

		index.Sitemaps = append(index.Sitemaps, sitemapIndexEntry{
			Loc:     fmt.Sprintf("%s/storage/sitemaps/sitemap_%s_%s.xml", baseURL, sitemapType, dbCode),
			LastMod: lastMod,
		})
	}

	return s.writeXML(s.sitemapFilename("index", dbCode), index)
}

func (s *sitemapService) Delete(sitemapType, dbCode string) error {
	path := s.sitemapFilename(sitemapType, dbCode)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return MapError(err)
	}
	return nil
}
