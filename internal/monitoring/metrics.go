package monitoring

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// maxErrorSamples is how many recent error samples we keep per route so the
// dashboard can show what actually failed (not just a count).
const maxErrorSamples = 8

// ErrorSample captures one failed request for diagnostics.
type ErrorSample struct {
	Status    int    `json:"status"`
	Message   string `json:"message"`
	Path      string `json:"path"`
	Timestamp string `json:"timestamp"`
}

type routeMetric struct {
	Count        uint64
	Errors       uint64
	TotalMillis  float64
	MaxMillis    float64
	RecentErrors []ErrorSample
}

type Snapshot struct {
	UptimeSeconds int64                    `json:"uptime_seconds"`
	RequestsTotal uint64                   `json:"requests_total"`
	ErrorsTotal   uint64                   `json:"errors_total"`
	AvgLatencyMS  float64                  `json:"avg_latency_ms"`
	Routes        map[string]RouteSnapshot `json:"routes"`
}

type RouteSnapshot struct {
	Count        uint64        `json:"count"`
	Errors       uint64        `json:"errors"`
	AvgLatencyMS float64       `json:"avg_latency_ms"`
	MaxLatencyMS float64       `json:"max_latency_ms"`
	RecentErrors []ErrorSample `json:"recent_errors,omitempty"`
}

type collector struct {
	mu            sync.RWMutex
	startedAt     time.Time
	requestsTotal uint64
	errorsTotal   uint64
	routes        map[string]*routeMetric
}

var defaultCollector = &collector{startedAt: time.Now(), routes: make(map[string]*routeMetric)}

func routeKey(method, path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		path = "/"
	}
	return method + " " + path
}

func RecordRequest(method, path string, status int, latency time.Duration) {
	RecordRequestWithError(method, path, status, latency, "", "")
}

// RecordRequestWithError records a request and, on a 5xx, keeps a diagnostic
// sample (status, error message, actual request path). actualPath is the
// concrete URL (with real ids) while path is the templated route key.
func RecordRequestWithError(method, path string, status int, latency time.Duration, errMsg, actualPath string) {
	key := routeKey(method, path)
	ms := float64(latency.Microseconds()) / 1000.0
	defaultCollector.mu.Lock()
	defer defaultCollector.mu.Unlock()
	defaultCollector.requestsTotal++
	m := defaultCollector.routes[key]
	if m == nil {
		m = &routeMetric{}
		defaultCollector.routes[key] = m
	}
	m.Count++
	m.TotalMillis += ms
	if ms > m.MaxMillis {
		m.MaxMillis = ms
	}
	if status >= 500 {
		defaultCollector.errorsTotal++
		m.Errors++
		if errMsg == "" {
			errMsg = fmt.Sprintf("HTTP %d", status)
		}
		sample := ErrorSample{Status: status, Message: errMsg, Path: actualPath, Timestamp: time.Now().UTC().Format(time.RFC3339)}
		m.RecentErrors = append(m.RecentErrors, sample)
		if len(m.RecentErrors) > maxErrorSamples {
			m.RecentErrors = m.RecentErrors[len(m.RecentErrors)-maxErrorSamples:]
		}
	}
}

func SnapshotData() Snapshot {
	defaultCollector.mu.RLock()
	defer defaultCollector.mu.RUnlock()
	out := Snapshot{UptimeSeconds: int64(time.Since(defaultCollector.startedAt).Seconds()), RequestsTotal: defaultCollector.requestsTotal, ErrorsTotal: defaultCollector.errorsTotal, Routes: make(map[string]RouteSnapshot, len(defaultCollector.routes))}
	var totalMillis float64
	for key, m := range defaultCollector.routes {
		avg := 0.0
		if m.Count > 0 {
			avg = m.TotalMillis / float64(m.Count)
		}
		totalMillis += m.TotalMillis
		var recent []ErrorSample
		if len(m.RecentErrors) > 0 {
			recent = make([]ErrorSample, len(m.RecentErrors))
			copy(recent, m.RecentErrors)
		}
		out.Routes[key] = RouteSnapshot{Count: m.Count, Errors: m.Errors, AvgLatencyMS: avg, MaxLatencyMS: m.MaxMillis, RecentErrors: recent}
	}
	if out.RequestsTotal > 0 {
		out.AvgLatencyMS = totalMillis / float64(out.RequestsTotal)
	}
	return out
}

func PrometheusText() string {
	s := SnapshotData()
	var b strings.Builder
	b.WriteString("# HELP alemancenter_uptime_seconds Application uptime in seconds\n")
	b.WriteString("# TYPE alemancenter_uptime_seconds gauge\n")
	b.WriteString(fmt.Sprintf("alemancenter_uptime_seconds %d\n", s.UptimeSeconds))
	b.WriteString("# HELP alemancenter_http_requests_total Total HTTP requests\n")
	b.WriteString("# TYPE alemancenter_http_requests_total counter\n")
	b.WriteString(fmt.Sprintf("alemancenter_http_requests_total %d\n", s.RequestsTotal))
	b.WriteString("# HELP alemancenter_http_errors_total Total HTTP 5xx requests\n")
	b.WriteString("# TYPE alemancenter_http_errors_total counter\n")
	b.WriteString(fmt.Sprintf("alemancenter_http_errors_total %d\n", s.ErrorsTotal))
	b.WriteString("# HELP alemancenter_http_latency_average_ms Average HTTP latency in milliseconds\n")
	b.WriteString("# TYPE alemancenter_http_latency_average_ms gauge\n")
	b.WriteString(fmt.Sprintf("alemancenter_http_latency_average_ms %.3f\n", s.AvgLatencyMS))

	keys := make([]string, 0, len(s.Routes))
	for key := range s.Routes {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		parts := strings.SplitN(key, " ", 2)
		method, path := parts[0], ""
		if len(parts) == 2 {
			path = parts[1]
		}
		r := s.Routes[key]
		b.WriteString(fmt.Sprintf("alemancenter_http_route_requests_total{method=%q,path=%q} %d\n", method, path, r.Count))
		b.WriteString(fmt.Sprintf("alemancenter_http_route_errors_total{method=%q,path=%q} %d\n", method, path, r.Errors))
		b.WriteString(fmt.Sprintf("alemancenter_http_route_latency_average_ms{method=%q,path=%q} %.3f\n", method, path, r.AvgLatencyMS))
		b.WriteString(fmt.Sprintf("alemancenter_http_route_latency_max_ms{method=%q,path=%q} %.3f\n", method, path, r.MaxLatencyMS))
	}
	return b.String()
}
