package services

import (
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/alemancenter/fiber-api/internal/config"
	"github.com/alemancenter/fiber-api/internal/repositories"
)

type SitemapService interface {
	GetStatus(dbCode string) map[string]SitemapInfo
	GenerateAll(dbCode string) []error
	Delete(sitemapType, dbCode string) error
}

type SitemapStatusResponse struct {
	Database string                 `json:"database"`
	Sitemaps map[string]SitemapInfo `json:"sitemaps"`
}

type sitemapService struct {
	repo repositories.SitemapRepository
}

func NewSitemapService(repo repositories.SitemapRepository) SitemapService {
	return &sitemapService{repo: repo}
}

type SitemapInfo struct {
	Exists       bool    `json:"exists"`
	LastModified *string `json:"last_modified"`
	URL          *string `json:"url"`
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

func (s *sitemapService) fileInfo(path string) (exists bool, lastMod string) {
	info, err := os.Stat(path)
	if err != nil {
		return false, ""
	}
	return true, info.ModTime().UTC().Format(time.RFC3339)
}

func (s *sitemapService) GetStatus(dbCode string) map[string]SitemapInfo {
	types := []string{"articles", "post", "static", "index"}
	baseURL := s.siteURL()
	result := make(map[string]SitemapInfo, len(types))

	for _, t := range types {
		path := s.sitemapFilename(t, dbCode)
		exists, mod := s.fileInfo(path)
		info := SitemapInfo{Exists: exists}
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
	base := s.siteURL()
	cc := dbCode // country code used in frontend URL segments

	var wg sync.WaitGroup
	errs := make([]error, 4)

	// Articles
	wg.Add(1)
	go func() {
		defer wg.Done()
		rows, err := s.repo.GetActiveArticles(dbCode)
		if err != nil {
			errs[0] = err
			return
		}
		set := urlSet{Xmlns: "http://www.sitemaps.org/schemas/sitemap/0.9"}
		for _, r := range rows {
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
		set := urlSet{Xmlns: "http://www.sitemaps.org/schemas/sitemap/0.9"}
		for _, r := range rows {
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

	wg.Wait()

	if errs[0] == nil && errs[1] == nil && errs[2] == nil {
		errs[3] = s.writeSitemapIndex(dbCode, base, []string{"articles", "post", "static"})
	}

	var actualErrors []error
	for _, e := range errs {
		if e != nil {
			actualErrors = append(actualErrors, e)
		}
	}

	return actualErrors
}

func (s *sitemapService) writeSitemapIndex(dbCode, baseURL string, sitemapTypes []string) error {
	index := sitemapIndex{Xmlns: "http://www.sitemaps.org/schemas/sitemap/0.9"}

	for _, sitemapType := range sitemapTypes {
		path := s.sitemapFilename(sitemapType, dbCode)
		exists, lastMod := s.fileInfo(path)
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
