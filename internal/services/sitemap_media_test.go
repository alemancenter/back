package services

import (
	"encoding/xml"
	"strings"
	"testing"
)

func TestMediaSitemapXMLNamespaces(t *testing.T) {
	imageXML, err := xml.Marshal(imageURLSet{
		Xmlns: "http://www.sitemaps.org/schemas/sitemap/0.9", XmlnsImage: "http://www.google.com/schemas/sitemap-image/1.1",
		URLs: []imageSitemapEntry{{Loc: "https://imanjo.com/jo/posts/1", Images: []imageSitemapItem{{Loc: "https://imanjo.com/storage/a.jpg", Title: "صورة"}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	value := string(imageXML)
	for _, expected := range []string{`xmlns:image="http://www.google.com/schemas/sitemap-image/1.1"`, "<image:image>", "<image:loc>"} {
		if !strings.Contains(value, expected) {
			t.Fatalf("expected %q in %s", expected, value)
		}
	}

	newsXML, err := xml.Marshal(newsURLSet{
		Xmlns: "http://www.sitemaps.org/schemas/sitemap/0.9", XmlnsNews: "http://www.google.com/schemas/sitemap-news/0.9",
		URLs: []newsSitemapEntry{{Loc: "https://imanjo.com/jo/posts/1", News: newsSitemapItem{Publication: newsPublication{Name: "موقع الإيمان", Language: "ar"}, PublicationDate: "2026-08-30T00:00:00Z", Title: "خبر"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if value := string(newsXML); !strings.Contains(value, "<news:publication>") || !strings.Contains(value, `xmlns:news="http://www.google.com/schemas/sitemap-news/0.9"`) {
		t.Fatalf("invalid news sitemap: %s", value)
	}

	videoXML, err := xml.Marshal(videoURLSet{
		Xmlns: "http://www.sitemaps.org/schemas/sitemap/0.9", XmlnsVideo: "http://www.google.com/schemas/sitemap-video/1.1",
		URLs: []videoSitemapEntry{{Loc: "https://imanjo.com/jo/posts/1", Videos: []videoSitemapItem{{ThumbnailLoc: "https://imanjo.com/storage/thumb.jpg", ContentLoc: "https://imanjo.com/storage/video.mp4", Title: "فيديو", Description: "وصف"}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	videoValue := string(videoXML)
	for _, expected := range []string{`xmlns:video="http://www.google.com/schemas/sitemap-video/1.1"`, "<video:thumbnail_loc>", "<video:content_loc>"} {
		if !strings.Contains(videoValue, expected) {
			t.Fatalf("expected %q in %s", expected, videoValue)
		}
	}
}

func TestSitemapMediaURLs(t *testing.T) {
	image := sitemapImageURL("https://imanjo.com", "storage/posts/صورة.webp")
	if !strings.Contains(image, "/api/img?") || !strings.Contains(image, "w=1600") || strings.Contains(image, "صورة") {
		t.Fatalf("unexpected proxied image URL: %s", image)
	}
	video := sitemapFileURL("https://imanjo.com", "files/video.mp4")
	if video != "https://imanjo.com/storage/files/video.mp4" {
		t.Fatalf("unexpected video URL: %s", video)
	}
}
