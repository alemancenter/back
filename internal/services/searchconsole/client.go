// Package searchconsole talks to Google's Search Console REST APIs (URL
// Inspection, Search Analytics, Sitemaps) using a service-account credential —
// see back/docs/reports/CONTENT_QUALITY_GOVERNANCE_CENTER_PLAN.md §4.1. No
// per-user OAuth flow, and deliberately no google.golang.org/api client
// dependency: golang.org/x/oauth2/google (already part of this project's
// existing golang.org/x/oauth2 requirement) mints the bearer token, and plain
// net/http calls the documented REST endpoints directly.
package searchconsole

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// Scopes covers URL Inspection, Search Analytics, and Sitemaps — one service
// account credential, one scope, all three read/write operations this package
// needs.
var scopes = []string{"https://www.googleapis.com/auth/webmasters"}

// ErrNotConfigured means no (or an empty) service account credential was given.
var ErrNotConfigured = fmt.Errorf("google search console credentials are not configured")

type Client struct {
	httpClient *http.Client
}

// NewClient builds a client from a service-account credential. The value may be:
//   - the raw JSON key content (starts with "{")
//   - an absolute path to a JSON key file (avoids all env-file escaping issues
//     with the "\n"s inside private_key)
//   - base64-encoded JSON (also env-file safe)
func NewClient(ctx context.Context, serviceAccountJSON string) (*Client, error) {
	serviceAccountJSON = strings.TrimSpace(serviceAccountJSON)
	if serviceAccountJSON == "" {
		return nil, ErrNotConfigured
	}
	if !strings.HasPrefix(serviceAccountJSON, "{") {
		if data, readErr := os.ReadFile(serviceAccountJSON); readErr == nil && bytes.HasPrefix(bytes.TrimSpace(data), []byte("{")) {
			serviceAccountJSON = string(data)
		} else if decoded, decErr := base64.StdEncoding.DecodeString(serviceAccountJSON); decErr == nil && bytes.HasPrefix(bytes.TrimSpace(decoded), []byte("{")) {
			serviceAccountJSON = string(decoded)
		}
	}
	creds, err := google.CredentialsFromJSON(ctx, []byte(serviceAccountJSON), scopes...)
	if err != nil {
		return nil, fmt.Errorf("invalid GSC service account credentials: %w", err)
	}
	httpClient := oauth2.NewClient(ctx, creds.TokenSource)
	httpClient.Timeout = 30 * time.Second
	return &Client{httpClient: httpClient}, nil
}

// doWithBackoff retries once on a 429/quota response after a short pause —
// the URL Inspection API's per-minute quota is tight enough (≈600/min) that a
// sync loop calling it in a row can trip it even while staying under the daily
// cap. Callers are expected to also throttle between calls; see service.go.
func (c *Client) doWithBackoff(req *http.Request, bodyBytes []byte) (*http.Response, []byte, error) {
	for attempt := 0; attempt < 2; attempt++ {
		if bodyBytes != nil {
			req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		}
		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, nil, err
		}
		raw, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			return resp, nil, readErr
		}
		if resp.StatusCode == http.StatusTooManyRequests && attempt == 0 {
			time.Sleep(2 * time.Second)
			continue
		}
		return resp, raw, nil
	}
	return nil, nil, fmt.Errorf("exhausted retries")
}

// URLInspectionResult is the subset of the URL Inspection API response this
// system persists — see models.GSCURLStatus, which stores the mapped
// IndexStatus alongside the raw response for debugging.
type URLInspectionResult struct {
	IndexStatus     string // one of models.GSCIndexStatus*
	CoverageState   string // Google's raw human-readable state, e.g. "Submitted and indexed"
	Verdict         string // PASS / FAIL / NEUTRAL / PARTIAL / VERDICT_UNSPECIFIED
	RobotsTxtState  string
	GoogleCanonical string
	UserCanonical   string
	LastCrawlTime   *time.Time
	Raw             string
}

type urlInspectionRequest struct {
	InspectionURL string `json:"inspectionUrl"`
	SiteURL       string `json:"siteUrl"`
}

type urlInspectionResponse struct {
	InspectionResult struct {
		IndexStatusResult struct {
			Verdict         string `json:"verdict"`
			CoverageState   string `json:"coverageState"`
			RobotsTxtState  string `json:"robotsTxtState"`
			LastCrawlTime   string `json:"lastCrawlTime"`
			GoogleCanonical string `json:"googleCanonical"`
			UserCanonical   string `json:"userCanonical"`
		} `json:"indexStatusResult"`
	} `json:"inspectionResult"`
}

// InspectURL calls the URL Inspection API (index:inspect) for one URL under
// siteURL. siteURL must match a property this service account has access to —
// see plan §4.1's one-time manual "add user" step.
func (c *Client) InspectURL(ctx context.Context, siteURL, inspectionURL string) (*URLInspectionResult, error) {
	payload, err := json.Marshal(urlInspectionRequest{InspectionURL: inspectionURL, SiteURL: siteURL})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://searchconsole.googleapis.com/v1/urlInspection/index:inspect", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, raw, err := c.doWithBackoff(req, payload)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("url inspection failed: status %d: %s", resp.StatusCode, truncateForError(raw))
	}

	var parsed urlInspectionResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("url inspection: decode response: %w", err)
	}

	result := &URLInspectionResult{
		CoverageState:   parsed.InspectionResult.IndexStatusResult.CoverageState,
		Verdict:         parsed.InspectionResult.IndexStatusResult.Verdict,
		RobotsTxtState:  parsed.InspectionResult.IndexStatusResult.RobotsTxtState,
		GoogleCanonical: parsed.InspectionResult.IndexStatusResult.GoogleCanonical,
		UserCanonical:   parsed.InspectionResult.IndexStatusResult.UserCanonical,
		Raw:             string(raw),
	}
	result.IndexStatus = mapCoverageStateToIndexStatus(result.CoverageState, result.Verdict)
	if t, err := time.Parse(time.RFC3339, parsed.InspectionResult.IndexStatusResult.LastCrawlTime); err == nil {
		result.LastCrawlTime = &t
	}
	return result, nil
}

// SearchAnalyticsRow is one (query, page, date) row from Search Analytics.
type SearchAnalyticsRow struct {
	Query       string
	URL         string
	Date        time.Time
	Clicks      int
	Impressions int
	CTR         float64
	Position    float64
}

type searchAnalyticsRequest struct {
	StartDate  string   `json:"startDate"`
	EndDate    string   `json:"endDate"`
	Dimensions []string `json:"dimensions"`
	RowLimit   int      `json:"rowLimit"`
	StartRow   int      `json:"startRow,omitempty"`
}

type searchAnalyticsResponse struct {
	Rows []struct {
		Keys        []string `json:"keys"`
		Clicks      float64  `json:"clicks"`
		Impressions float64  `json:"impressions"`
		CTR         float64  `json:"ctr"`
		Position    float64  `json:"position"`
	} `json:"rows"`
}

// QuerySearchAnalytics fetches per-query, per-page, per-day metrics
// for [startDate, endDate]. Data typically lags 2-3 days behind real time and
// covers at most the trailing 16 months — callers should not query further
// back than that.
func (c *Client) QuerySearchAnalytics(ctx context.Context, siteURL string, startDate, endDate time.Time) ([]SearchAnalyticsRow, error) {
	endpoint := fmt.Sprintf("https://www.googleapis.com/webmasters/v3/sites/%s/searchAnalytics/query", url.PathEscape(siteURL))
	const pageSize = 25000
	const maxPages = 20 // bounded safety cap: at most 500k stored rows per sync
	rows := make([]SearchAnalyticsRow, 0, pageSize)
	for page := 0; page < maxPages; page++ {
		payload, err := json.Marshal(searchAnalyticsRequest{
			StartDate:  startDate.Format("2006-01-02"),
			EndDate:    endDate.Format("2006-01-02"),
			Dimensions: []string{"query", "page", "date"},
			RowLimit:   pageSize,
			StartRow:   page * pageSize,
		})
		if err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		resp, raw, err := c.doWithBackoff(req, payload)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return nil, fmt.Errorf("search analytics query failed: status %d: %s", resp.StatusCode, truncateForError(raw))
		}
		var parsed searchAnalyticsResponse
		if err := json.Unmarshal(raw, &parsed); err != nil {
			return nil, fmt.Errorf("search analytics: decode response: %w", err)
		}
		for _, row := range parsed.Rows {
			if len(row.Keys) < 3 {
				continue
			}
			date, parseErr := time.Parse("2006-01-02", row.Keys[2])
			if parseErr != nil {
				continue
			}
			rows = append(rows, SearchAnalyticsRow{
				Query: row.Keys[0], URL: row.Keys[1], Date: date,
				Clicks: int(row.Clicks), Impressions: int(row.Impressions), CTR: row.CTR, Position: row.Position,
			})
		}
		if len(parsed.Rows) < pageSize {
			break
		}
	}
	return rows, nil
}

// SubmitSitemap pings the Sitemaps API after a real publish event — the one
// place a direct Google-facing notification on publish is correct (unlike the
// Indexing API, which this project deliberately never calls for ordinary
// content — see plan §8).
func (c *Client) SubmitSitemap(ctx context.Context, siteURL, sitemapURL string) error {
	endpoint := fmt.Sprintf("https://www.googleapis.com/webmasters/v3/sites/%s/sitemaps/%s",
		url.PathEscape(siteURL), url.QueryEscape(sitemapURL))
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, endpoint, nil)
	if err != nil {
		return err
	}
	resp, raw, err := c.doWithBackoff(req, nil)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("sitemap submission failed: status %d: %s", resp.StatusCode, truncateForError(raw))
	}
	return nil
}

// mapCoverageStateToIndexStatus maps Google's free-text coverageState (there is
// no stable enum in the API response) onto the 4+1 badge states this system
// uses everywhere else — see models.GSCIndexStatus*. Falls back to "unknown"
// rather than guessing when Google returns a state string not seen before;
// the raw response is always kept on the row for a human to inspect.
func mapCoverageStateToIndexStatus(coverageState, verdict string) string {
	lower := strings.ToLower(coverageState)
	switch {
	case strings.Contains(lower, "submitted and indexed"),
		strings.Contains(lower, "indexed, though"),
		verdict == "PASS":
		return "indexed"
	case strings.Contains(lower, "crawled - currently not indexed"),
		strings.Contains(lower, "discovered - currently not indexed"):
		return "crawled_not_indexed"
	case strings.Contains(lower, "discovered - currently not crawled"):
		return "discovered_not_crawled"
	case lower != "" && strings.Contains(lower, "not indexed"):
		return "not_indexed"
	default:
		return "unknown_not_synced"
	}
}

func truncateForError(raw []byte) string {
	const max = 500
	if len(raw) <= max {
		return string(raw)
	}
	return string(raw[:max]) + "..."
}
