package services

import (
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/alemancenter/fiber-api/internal/config"
	"github.com/alemancenter/fiber-api/internal/database"
	"github.com/alemancenter/fiber-api/internal/models"
	"github.com/alemancenter/fiber-api/internal/repositories"
)

type AnalyticsService interface {
	GetVisitorAnalytics(dbCode database.CountryID, days int) *VisitorAnalyticsResponse
	PruneAnalytics(dbCode database.CountryID, days int) int64
	GetDashboardSummary(dbCode database.CountryID) *DashboardSummaryResponse
	GetContentAnalytics(dbCode database.CountryID) *ContentAnalyticsResponse
	GetPerformanceSummary() *PerformanceSummaryResponse
	GetPerformanceLive() map[string]interface{}
	GetPerformanceResponseTime() map[string]interface{}
	GetPerformanceCache() map[string]interface{}
	GetPerformanceRaw() map[string]interface{}
}

type PruneAnalyticsResponse struct {
	Deleted int64 `json:"deleted"`
}

type PerformanceSummaryResponse struct {
	RedisInfo string    `json:"redis_info"`
	Timestamp time.Time `json:"timestamp"`
}

type VisitorAnalyticsResponse struct {
	VisitorStats   VisitorStatsData          `json:"visitor_stats"`
	UserStats      UserStatsData             `json:"user_stats"`
	CountryStats   []repositories.CountryRow `json:"country_stats"`
	ChartData      []ChartDataRow            `json:"chart_data"`
	DeviceStats    []DeviceStatRow           `json:"device_stats"`
	TrafficSources []TrafficSourceRow        `json:"traffic_sources"`
}

type VisitorStatsData struct {
	Current            int64              `json:"current"`
	CurrentMembers     int64              `json:"current_members"`
	CurrentGuests      int64              `json:"current_guests"`
	TotalToday         int64              `json:"total_today"`
	TotalCombinedToday int64              `json:"total_combined_today"`
	Change             float64            `json:"change"`
	History            []ChartDataRow     `json:"history"`
	ActiveVisitors     []ActiveVisitorRow `json:"active_visitors"`
}

type ActiveVisitorRow struct {
	IP              string `json:"ip"`
	Country         string `json:"country"`
	City            string `json:"city"`
	Browser         string `json:"browser"`
	OS              string `json:"os"`
	UserAgent       string `json:"user_agent"`
	CurrentPage     string `json:"current_page"`
	CurrentPageFull string `json:"current_page_full"`
	IsMember        bool   `json:"is_member"`
	LastActive      string `json:"last_active"`
	SessionStart    string `json:"session_start"`
	UserID          *uint  `json:"user_id,omitempty"`
	UserName        string `json:"user_name,omitempty"`
	UserEmail       string `json:"user_email,omitempty"`
}

type UserStatsData struct {
	Total    int64 `json:"total"`
	Active   int64 `json:"active"`
	NewToday int64 `json:"new_today"`
}

type ChartDataRow struct {
	Name      string `json:"name"`
	FullDate  string `json:"full_date"`
	Visitors  int64  `json:"visitors"`
	PageViews int64  `json:"pageViews"`
}

type DeviceStatRow struct {
	Name  string  `json:"name"`
	Value float64 `json:"value"`
	Count int64   `json:"count"`
	Color string  `json:"color"`
}

type TrafficSourceRow struct {
	Source string  `json:"source"`
	Visits int64   `json:"visits"`
	Change float64 `json:"change"`
}

type DashboardSummaryResponse struct {
	Totals           DashboardTotals    `json:"totals"`
	Trends           DashboardTrends    `json:"trends"`
	Analytics        DashboardAnalytics `json:"analytics"`
	OnlineUsers      []interface{}      `json:"onlineUsers"`
	RecentActivities []ActivityOut      `json:"recentActivities"`
}

type DashboardTotals struct {
	Articles    int64 `json:"articles"`
	News        int64 `json:"news"`
	Users       int64 `json:"users"`
	OnlineUsers int64 `json:"online_users"`
}

type DashboardTrends struct {
	Articles TrendData `json:"articles"`
	News     TrendData `json:"news"`
	Users    TrendData `json:"users"`
}

type DashboardAnalytics struct {
	Dates    []string `json:"dates"`
	Articles []int    `json:"articles"`
	News     []int    `json:"news"`
	Comments []int    `json:"comments"`
	Views    []int    `json:"views"`
	Authors  []int    `json:"authors"`
}

type TrendData struct {
	Percentage float64 `json:"percentage"`
	Trend      string  `json:"trend"`
}

type ActivityUser struct {
	Name string `json:"name"`
}

type ActivityOut struct {
	ID        uint         `json:"id"`
	Type      string       `json:"type"`
	Title     string       `json:"title"`
	User      ActivityUser `json:"user"`
	CreatedAt time.Time    `json:"created_at"`
}

type ContentAnalyticsResponse struct {
	TopArticles       []repositories.ArticleView `json:"top_articles"`
	TopPosts          []repositories.PostView    `json:"top_posts"`
	PublishedArticles int64                      `json:"published_articles"`
	DraftArticles     int64                      `json:"draft_articles"`
}

type analyticsService struct {
	repo repositories.AnalyticsRepository
}

func NewAnalyticsService(repo repositories.AnalyticsRepository) AnalyticsService {
	return &analyticsService{repo: repo}
}

func (s *analyticsService) GetVisitorAnalytics(dbCode database.CountryID, days int) *VisitorAnalyticsResponse {
	now := time.Now()
	since := now.AddDate(0, 0, -days)
	prevSince := now.AddDate(0, 0, -days*2)
	activeWindow := now.Add(-15 * time.Minute)
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	yesterdayStart := todayStart.AddDate(0, 0, -1)

	var wg sync.WaitGroup

	var currentActive, currentMembers, currentGuests, totalToday, totalYesterday int64
	var activeRows []repositories.ActiveRow

	var totalUsers, activeUsers, newToday int64
	var countryStats []repositories.CountryRow
	var dailyRows []repositories.DailyRow
	var deviceRows []repositories.DeviceRow
	var refRows []repositories.RefRow
	var prevRefRows []repositories.RefRow

	wg.Add(6)

	// visitor_stats
	go func() {
		defer wg.Done()
		currentActive, currentMembers, currentGuests, totalToday, totalYesterday = s.repo.GetVisitorStats(dbCode, activeWindow, todayStart, yesterdayStart)
		activeRows, _ = s.repo.GetActiveVisitors(dbCode, activeWindow)
	}()

	// user_stats
	go func() {
		defer wg.Done()
		totalUsers, activeUsers, newToday = s.repo.GetUserStats(todayStart, now.AddDate(0, 0, -30))
	}()

	// country_stats
	go func() {
		defer wg.Done()
		countryStats, _ = s.repo.GetCountryStats(dbCode, since)
	}()

	// chart_data
	go func() {
		defer wg.Done()
		dailyRows, _ = s.repo.GetDailyChartData(dbCode, since)
	}()

	// device_stats
	go func() {
		defer wg.Done()
		deviceRows, _ = s.repo.GetDeviceStats(dbCode, since)
	}()

	// traffic_sources
	go func() {
		defer wg.Done()
		refRows, _ = s.repo.GetTrafficSources(dbCode, since)
		prevRefRows, _ = s.repo.GetTrafficSourcesPrev(dbCode, prevSince, since)
	}()

	wg.Wait()

	// ---- assemble visitor_stats ----
	changeVal := 0.0
	if totalYesterday > 0 {
		changeVal = float64(totalToday-totalYesterday) / float64(totalYesterday) * 100
	}

	activeVisitors := make([]ActiveVisitorRow, 0, len(activeRows))
	for _, r := range activeRows {
		av := ActiveVisitorRow{
			IP:              r.IPAddress,
			Country:         strVal(r.Country),
			City:            strVal(r.City),
			Browser:         strVal(r.Browser),
			OS:              strVal(r.OS),
			UserAgent:       r.UserAgent,
			CurrentPage:     strVal(r.URL),
			CurrentPageFull: strVal(r.URL),
			IsMember:        r.UserID != nil,
			LastActive:      r.LastAct,
			SessionStart:    r.CreatedAt,
		}
		if r.UserID != nil {
			av.UserID = r.UserID
			userName := strVal(r.UserName)
			av.UserName = userName
			userEmail := strVal(r.UserEmail)
			av.UserEmail = userEmail
		}
		activeVisitors = append(activeVisitors, av)
	}

	// ---- chart_data ----
	chartData := make([]ChartDataRow, 0, len(dailyRows))
	for _, r := range dailyRows {
		dateStr := r.Date
		if len(dateStr) > 10 {
			dateStr = dateStr[:10]
		}
		t, _ := time.Parse("2006-01-02", dateStr)
		chartData = append(chartData, ChartDataRow{
			Name:      t.Format("02 Jan"),
			FullDate:  r.Date,
			Visitors:  r.Visitors,
			PageViews: r.PageViews,
		})
	}

	// ---- device_stats ----
	var mobile, tablet, desktop int64
	for _, r := range deviceRows {
		os := ""
		if r.OS != nil {
			os = *r.OS
		}
		switch {
		case containsAny(os, "Android", "iPhone", "iOS"):
			mobile += r.Count
		case containsAny(os, "iPad", "Tablet"):
			tablet += r.Count
		default:
			desktop += r.Count
		}
	}
	totalDevices := mobile + tablet + desktop
	deviceStats := []DeviceStatRow{
		{Name: "Desktop", Value: pct(desktop, totalDevices), Count: desktop, Color: "#63E6E2"},
		{Name: "Mobile", Value: pct(mobile, totalDevices), Count: mobile, Color: "#0EA5E9"},
		{Name: "Tablet", Value: pct(tablet, totalDevices), Count: tablet, Color: "#6366F1"},
	}

	// ---- traffic_sources ----
	// Build own-domain set from CORS origins so self-referrals count as Direct.
	ownDomains := map[string]bool{}
	for _, origin := range config.Get().Frontend.CORSOrigins {
		if d := extractDomain(strings.TrimSpace(origin)); d != "" {
			ownDomains[d] = true
			ownDomains["www."+d] = true
			ownDomains["api."+d] = true
		}
	}

	refDomain := func(ref *string) string {
		if ref == nil || *ref == "" {
			return "Direct"
		}
		d := extractDomain(*ref)
		if d == "" || ownDomains[d] {
			return "Direct"
		}
		return d
	}

	srcMap := map[string]int64{}
	for _, r := range refRows {
		srcMap[refDomain(r.Referer)] += r.Count
	}
	prevSrcMap := map[string]int64{}
	for _, r := range prevRefRows {
		prevSrcMap[refDomain(r.Referer)] += r.Count
	}

	trafficSources := make([]TrafficSourceRow, 0, len(srcMap))
	for src, visits := range srcMap {
		prev := prevSrcMap[src]
		change := 0.0
		if prev > 0 {
			change = float64(visits-prev) / float64(prev) * 100
		}
		trafficSources = append(trafficSources, TrafficSourceRow{
			Source: src,
			Visits: visits,
			Change: change,
		})
	}

	if countryStats == nil {
		countryStats = []repositories.CountryRow{}
	}

	return &VisitorAnalyticsResponse{
		VisitorStats: VisitorStatsData{
			Current:            currentActive,
			CurrentMembers:     currentMembers,
			CurrentGuests:      currentGuests,
			TotalToday:         totalToday,
			TotalCombinedToday: totalToday,
			Change:             changeVal,
			History:            chartData,
			ActiveVisitors:     activeVisitors,
		},
		UserStats: UserStatsData{
			Total:    totalUsers,
			Active:   activeUsers,
			NewToday: newToday,
		},
		CountryStats:   countryStats,
		ChartData:      chartData,
		DeviceStats:    deviceStats,
		TrafficSources: trafficSources,
	}
}

func (s *analyticsService) PruneAnalytics(dbCode database.CountryID, days int) int64 {
	cutoff := time.Now().AddDate(0, 0, -days)
	return s.repo.PruneVisitorTracking(dbCode, cutoff)
}

func (s *analyticsService) GetDashboardSummary(dbCode database.CountryID) *DashboardSummaryResponse {
	fiveMinAgo := time.Now().Add(-5 * time.Minute)

	var wg sync.WaitGroup
	var articleCount, newsCount, userCount, onlineCount int64
	var artTrend, newsTrend, userTrend repositories.TrendRow

	var dates []string
	var articlesArr, newsArr, commentsArr, viewsArr, authorsArr []int
	var onlineUsers []models.User

	wg.Add(4)
	go func() {
		defer wg.Done()
		articleCount, newsCount, userCount, onlineCount = s.repo.GetTotals(dbCode, fiveMinAgo)
	}()

	go func() {
		defer wg.Done()
		now := time.Now()
		thisMonthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
		lastMonthStart := thisMonthStart.AddDate(0, -1, 0)
		artTrend, newsTrend, userTrend = s.repo.GetTrends(dbCode, thisMonthStart, lastMonthStart)
	}()

	go func() {
		defer wg.Done()
		dates, articlesArr, newsArr, commentsArr, viewsArr, authorsArr = s.repo.GetAnalyticsData(dbCode, 7)
	}()

	go func() {
		defer wg.Done()
		onlineUsers, _ = s.repo.GetOnlineUsers(fiveMinAgo)
	}()

	wg.Wait()

	rawActivities, _ := s.repo.GetRecentActivities()

	activities := make([]ActivityOut, 0, len(rawActivities))
	for _, a := range rawActivities {
		atype := "article"
		if a.SubjectType != nil {
			switch *a.SubjectType {
			case "Post":
				atype = "news"
			case "Comment":
				atype = "comment"
			}
		}
		activities = append(activities, ActivityOut{
			ID:        a.ID,
			Type:      atype,
			Title:     a.Description,
			User:      ActivityUser{Name: a.CauserName},
			CreatedAt: a.CreatedAt,
		})
	}

	// Map online users
	var onlineUsersOut []interface{}
	for _, u := range onlineUsers {
		status := "online" // Since they were active in the last 5 mins

		// Fallback to last_seen when last_activity is nil
		var lastAct *time.Time
		if u.LastActivity != nil {
			lastAct = u.LastActivity
		} else if u.LastSeen != nil {
			lastAct = u.LastSeen
		}

		onlineUsersOut = append(onlineUsersOut, map[string]interface{}{
			"id":                 u.ID,
			"name":               u.Name,
			"profile_photo_path": u.ProfilePhotoPath,
			"last_activity":      u.LastActivity,
			"last_seen":          u.LastSeen,
			"status":             status,
			"lastAct":            lastAct,
		})
	}

	return &DashboardSummaryResponse{
		Totals: DashboardTotals{
			Articles:    articleCount,
			News:        newsCount,
			Users:       userCount,
			OnlineUsers: onlineCount,
		},
		Trends: DashboardTrends{
			Articles: trendData(artTrend.LastMonth, artTrend.ThisMonth),
			News:     trendData(newsTrend.LastMonth, newsTrend.ThisMonth),
			Users:    trendData(userTrend.LastMonth, userTrend.ThisMonth),
		},
		Analytics: DashboardAnalytics{
			Dates:    dates,
			Articles: articlesArr,
			News:     newsArr,
			Comments: commentsArr,
			Views:    viewsArr,
			Authors:  authorsArr,
		},
		OnlineUsers:      onlineUsersOut,
		RecentActivities: activities,
	}
}

func (s *analyticsService) GetContentAnalytics(dbCode database.CountryID) *ContentAnalyticsResponse {
	var wg sync.WaitGroup
	var topArticles []repositories.ArticleView
	var topPosts []repositories.PostView
	var publishedArticles, draftArticles int64

	wg.Add(3)
	go func() {
		defer wg.Done()
		topArticles, _ = s.repo.GetTopArticles(dbCode)
	}()
	go func() {
		defer wg.Done()
		topPosts, _ = s.repo.GetTopPosts(dbCode)
	}()
	go func() {
		defer wg.Done()
		publishedArticles, draftArticles = s.repo.GetArticleCountsByStatus(dbCode)
	}()

	wg.Wait()

	if topArticles == nil {
		topArticles = []repositories.ArticleView{}
	}
	if topPosts == nil {
		topPosts = []repositories.PostView{}
	}

	return &ContentAnalyticsResponse{
		TopArticles:       topArticles,
		TopPosts:          topPosts,
		PublishedArticles: publishedArticles,
		DraftArticles:     draftArticles,
	}
}

// ---- helper funcs ----

func strVal(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func pct(part, total int64) float64 {
	if total == 0 {
		return 0
	}
	return float64(part) / float64(total) * 100
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if len(s) >= len(sub) {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
		}
	}
	return false
}

func extractDomain(rawURL string) string {
	var rest string
	switch {
	case strings.HasPrefix(rawURL, "https://"):
		rest = rawURL[8:]
	case strings.HasPrefix(rawURL, "http://"):
		rest = rawURL[7:]
	default:
		return "" // not an HTTP URL â€” reject garbage values
	}
	if i := strings.IndexByte(rest, '/'); i >= 0 {
		rest = rest[:i]
	}
	// Strip port
	if i := strings.IndexByte(rest, ':'); i >= 0 {
		rest = rest[:i]
	}
	// Must look like a real hostname (has a dot, no whitespace or braces)
	if !strings.Contains(rest, ".") || strings.ContainsAny(rest, " \t\n{}") {
		return ""
	}
	return rest
}

func trendData(prev, curr int64) TrendData {
	pct := 0.0
	dir := "up"
	if prev > 0 {
		pct = float64(curr-prev) / float64(prev) * 100
	}
	if pct < 0 {
		dir = "down"
	}
	return TrendData{Percentage: pct, Trend: dir}
}

// ---- Performance Logic Moved from Handler ----

func (s *analyticsService) GetPerformanceSummary() *PerformanceSummaryResponse {
	info, _ := s.repo.GetRedisInfo()

	return &PerformanceSummaryResponse{
		RedisInfo: info,
		Timestamp: time.Now(),
	}
}

func (s *analyticsService) GetPerformanceLive() map[string]interface{} {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	total := int64(mem.Sys)
	used := int64(mem.Alloc)
	free := total - used
	if free < 0 {
		free = 0
	}

	usage := 0.0
	if total > 0 {
		usage = (float64(used) / float64(total)) * 100
	}

	return map[string]interface{}{
		"cpu": map[string]interface{}{
			"usage": 0,
			"cores": runtime.NumCPU(),
			"load":  0,
		},
		"memory": map[string]interface{}{
			"total":            total,
			"free":             free,
			"used":             used,
			"usage_percentage": usage,
			"percentage":       usage,
		},
		"disk": map[string]interface{}{
			"total":            0,
			"free":             0,
			"used":             0,
			"usage_percentage": 0,
			"percentage":       0,
		},
		"timestamp": time.Now(),
	}
}

func (s *analyticsService) GetPerformanceResponseTime() map[string]interface{} {
	start := time.Now()
	_ = s.repo.PingRedis()

	return map[string]interface{}{
		"average_ms": time.Since(start).Milliseconds(),
	}
}

func (s *analyticsService) GetPerformanceCache() map[string]interface{} {
	info, _ := s.repo.GetRedisInfo()
	parsed := parseRedisInfo(info)

	hits := parseRedisInt(parsed["keyspace_hits"])
	misses := parseRedisInt(parsed["keyspace_misses"])
	total := hits + misses

	hitRatio := 0.0
	if total > 0 {
		hitRatio = (float64(hits) / float64(total)) * 100
	}

	cacheSize := parsed["used_memory_human"]
	if cacheSize == "" {
		cacheSize = "0 B"
	}

	return map[string]interface{}{
		"hit_ratio":  hitRatio,
		"cache_size": cacheSize,
	}
}

func (s *analyticsService) GetPerformanceRaw() map[string]interface{} {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	info, _ := s.repo.GetRedisInfo()

	return map[string]interface{}{
		"redis_info": parseRedisInfo(info),
		"go": map[string]interface{}{
			"goroutines": runtime.NumGoroutine(),
			"alloc":      mem.Alloc,
			"sys":        mem.Sys,
			"num_gc":     mem.NumGC,
		},
		"timestamp": time.Now(),
	}
}

// Helpers for Redis Info

func parseRedisInfo(info string) map[string]string {
	res := make(map[string]string)
	lines := strings.Split(info, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) == 2 {
			res[parts[0]] = parts[1]
		}
	}
	return res
}

func parseRedisInt(s string) int64 {
	if s == "" {
		return 0
	}
	v, _ := strconv.ParseInt(s, 10, 64)
	return v
}
