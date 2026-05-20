package services

import (
	"strings"
	"time"

	"github.com/alemancenter/fiber-api/internal/database"
	"github.com/alemancenter/fiber-api/internal/models"
	"github.com/alemancenter/fiber-api/pkg/logger"
	"go.uber.org/zap"
)

const (
	visitorChanBuffer = 10_000
	visitorBatchSize  = 200
)

// VisitorEvent is the payload sent from TrackVisitor middleware via EnqueueVisitor.
type VisitorEvent struct {
	IPAddress    string
	UserAgent    string
	URL          string
	Referer      string
	UserID       *uint
	CountryCode  string
	StatusCode   int
	ResponseTime float64 // milliseconds
	Timestamp    time.Time
}

// visitorCh is the in-process, non-blocking queue between middleware and worker.
var visitorCh = make(chan VisitorEvent, visitorChanBuffer)

// EnqueueVisitor pushes an event into the in-process channel.
// Non-blocking: silently drops when the buffer is full rather than stalling the request.
func EnqueueVisitor(ev VisitorEvent) {
	select {
	case visitorCh <- ev:
	default:
	}
}

// StartVisitorWorker starts the background batch-insert loop.
// maxWait is the longest time before a partial batch is flushed.
func StartVisitorWorker(maxWait time.Duration) {
	go runVisitorWorker(maxWait)
}

func runVisitorWorker(maxWait time.Duration) {
	ticker := time.NewTicker(maxWait)
	defer ticker.Stop()
	batch := make([]VisitorEvent, 0, visitorBatchSize)

	for {
		select {
		case ev := <-visitorCh:
			batch = append(batch, ev)
			if len(batch) >= visitorBatchSize {
				flushVisitorBatch(batch)
				batch = batch[:0]
				ticker.Reset(maxWait)
			}
		case <-ticker.C:
			if len(batch) > 0 {
				flushVisitorBatch(batch)
				batch = batch[:0]
			}
		}
	}
}

func flushVisitorBatch(events []VisitorEvent) {
	// Group events by country so each shard gets one batch INSERT.
	type shard struct {
		countryID database.CountryID
		events    []VisitorEvent
	}
	shards := make(map[string]*shard, 4)

	for i := range events {
		cc := events[i].CountryCode
		if cc == "" {
			cc = "jo"
		}
		s, ok := shards[cc]
		if !ok {
			id := database.CountryIDFromHeader(cc)
			if id == 0 {
				continue
			}
			s = &shard{countryID: id}
			shards[cc] = s
		}
		s.events = append(s.events, events[i])
	}

	now := time.Now()

	for cc, s := range shards {
		records := make([]models.VisitorTracking, 0, len(s.events))
		for _, ev := range s.events {
			browser, os := parseUserAgent(ev.UserAgent)
			records = append(records, models.VisitorTracking{
				IPAddress:    ev.IPAddress,
				UserAgent:    ev.UserAgent,
				Browser:      visStrPtr(browser),
				OS:           visStrPtr(os),
				URL:          visStrPtr(ev.URL),
				Referer:      visStrPtr(ev.Referer),
				UserID:       ev.UserID,
				StatusCode:   visIntPtr(ev.StatusCode),
				ResponseTime: visF64Ptr(ev.ResponseTime),
				LastActivity: ev.Timestamp,
				CreatedAt:    ev.Timestamp,
				UpdatedAt:    now,
			})
		}

		db := database.DBForCountry(s.countryID)
		if err := db.CreateInBatches(&records, 100).Error; err != nil {
			logger.Error("visitor batch insert failed",
				zap.String("country", cc),
				zap.Int("count", len(records)),
				zap.Error(err),
			)
		}
	}
}

// parseUserAgent extracts browser and OS via fast substring matching.
// No allocations beyond the returned strings; no external dependencies.
func parseUserAgent(ua string) (browser, os string) {
	switch {
	case strings.Contains(ua, "Windows NT"):
		os = "Windows"
	case strings.Contains(ua, "Android"):
		os = "Android"
	case strings.Contains(ua, "iPhone"), strings.Contains(ua, "iPad"):
		os = "iOS"
	case strings.Contains(ua, "Mac OS X"):
		os = "macOS"
	case strings.Contains(ua, "Linux"):
		os = "Linux"
	}

	switch {
	case strings.Contains(ua, "Edg/"):
		browser = "Edge"
	case strings.Contains(ua, "OPR/"), strings.Contains(ua, "Opera"):
		browser = "Opera"
	case strings.Contains(ua, "Firefox/"):
		browser = "Firefox"
	case strings.Contains(ua, "Chrome/"):
		browser = "Chrome"
	case strings.Contains(ua, "Safari/") && strings.Contains(ua, "Version/"):
		browser = "Safari"
	case strings.Contains(ua, "MSIE"), strings.Contains(ua, "Trident/"):
		browser = "IE"
	}

	return
}

func visStrPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func visIntPtr(i int) *int {
	if i == 0 {
		return nil
	}
	return &i
}

func visF64Ptr(f float64) *float64 {
	if f == 0 {
		return nil
	}
	return &f
}
