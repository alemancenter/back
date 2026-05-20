package repositories

import (
	"context"
	"time"

	"github.com/alemancenter/fiber-api/internal/database"
	"github.com/alemancenter/fiber-api/internal/models"
)

type AnalyticsRepository interface {
	GetVisitorStats(dbCode database.CountryID, activeWindow, todayStart, yesterdayStart time.Time) (currentActive, currentMembers, currentGuests, totalToday, totalYesterday int64)
	GetActiveVisitors(dbCode database.CountryID, activeWindow time.Time) ([]ActiveRow, error)
	GetUserStats(todayStart, activeWindow time.Time) (totalUsers, activeUsers, newToday int64)
	GetCountryStats(dbCode database.CountryID, since time.Time) ([]CountryRow, error)
	GetDailyChartData(dbCode database.CountryID, since time.Time) ([]DailyRow, error)
	GetDeviceStats(dbCode database.CountryID, since time.Time) ([]DeviceRow, error)
	GetTrafficSources(dbCode database.CountryID, since time.Time) ([]RefRow, error)
	GetTrafficSourcesPrev(dbCode database.CountryID, prevSince, since time.Time) ([]RefRow, error)
	GetPrevTotalVisits(dbCode database.CountryID, prevSince, since time.Time) int64
	GetTotalVisitsSince(dbCode database.CountryID, since time.Time) int64
	PruneVisitorTracking(dbCode database.CountryID, cutoff time.Time) int64

	GetTotals(dbCode database.CountryID, fiveMinAgo time.Time) (articleCount, newsCount, userCount, onlineCount int64)
	GetTrends(dbCode database.CountryID, thisMonthStart, lastMonthStart time.Time) (artTrend, newsTrend, userTrend TrendRow)
	GetRecentActivities() ([]ActivityRow, error)

	GetTopArticles(dbCode database.CountryID) ([]ArticleView, error)
	GetTopPosts(dbCode database.CountryID) ([]PostView, error)
	GetArticleCountsByStatus(dbCode database.CountryID) (published, draft int64)

	GetAnalyticsData(dbCode database.CountryID, days int) (dates []string, articles, news, comments, views, authors []int)
	GetOnlineUsers(fiveMinAgo time.Time) ([]models.User, error)

	GetRedisInfo() (string, error)
	PingRedis() error
}

type analyticsRepository struct{}

func NewAnalyticsRepository() AnalyticsRepository {
	return &analyticsRepository{}
}

// â”€â”€â”€ Structs for raw scans â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€

type ActiveRow struct {
	IPAddress string  `gorm:"column:ip_address"`
	Country   *string `gorm:"column:country"`
	City      *string `gorm:"column:city"`
	Browser   *string `gorm:"column:browser"`
	OS        *string `gorm:"column:os"`
	UserAgent string  `gorm:"column:user_agent"`
	URL       *string `gorm:"column:url"`
	UserID    *uint   `gorm:"column:user_id"`
	UserName  *string `gorm:"column:user_name"`
	UserEmail *string `gorm:"column:user_email"`
	LastAct   string  `gorm:"column:last_activity"`
	CreatedAt string  `gorm:"column:created_at"`
}

type CountryRow struct {
	Country string `gorm:"column:country" json:"country"`
	Count   int64  `gorm:"column:count"   json:"count"`
}

type DailyRow struct {
	Date      string `gorm:"column:date"`
	Visitors  int64  `gorm:"column:visitors"`
	PageViews int64  `gorm:"column:page_views"`
}

type DeviceRow struct {
	OS    *string `gorm:"column:os"`
	Count int64   `gorm:"column:count"`
}

type RefRow struct {
	Referer *string `gorm:"column:referer"`
	Count   int64   `gorm:"column:count"`
}

type TrendRow struct {
	ThisMonth int64 `gorm:"column:this_month"`
	LastMonth int64 `gorm:"column:last_month"`
}

type ActivityRow struct {
	ID          uint      `gorm:"column:id"`
	Description string    `gorm:"column:description"`
	SubjectType *string   `gorm:"column:subject_type"`
	CauserName  string    `gorm:"column:causer_name"`
	CreatedAt   time.Time `gorm:"column:created_at"`
}

type ArticleView struct {
	ID         uint   `json:"id"`
	Title      string `json:"title"`
	VisitCount int    `json:"visit_count"`
}

type PostView struct {
	ID    uint   `json:"id"`
	Title string `json:"title"`
	Views int    `json:"views"`
}

// â”€â”€â”€ Implementations â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€

func (r *analyticsRepository) GetVisitorStats(dbCode database.CountryID, activeWindow, todayStart, yesterdayStart time.Time) (currentActive, currentMembers, currentGuests, totalToday, totalYesterday int64) {
	db := database.DBForCountry(dbCode)
	// Count unique visitors (not raw page-view rows) for the "active now" badge.
	db.Raw(`SELECT COUNT(DISTINCT COALESCE(CAST(user_id AS CHAR), ip_address)) FROM visitors_tracking WHERE last_activity >= ?`, activeWindow).Scan(&currentActive)
	db.Raw(`SELECT COUNT(DISTINCT user_id) FROM visitors_tracking WHERE last_activity >= ? AND user_id IS NOT NULL`, activeWindow).Scan(&currentMembers)
	db.Raw(`SELECT COUNT(DISTINCT ip_address) FROM visitors_tracking WHERE last_activity >= ? AND user_id IS NULL`, activeWindow).Scan(&currentGuests)
	db.Model(&models.VisitorTracking{}).Where("created_at >= ?", todayStart).Count(&totalToday)
	db.Model(&models.VisitorTracking{}).Where("created_at >= ? AND created_at < ?", yesterdayStart, todayStart).Count(&totalYesterday)
	return
}

func (r *analyticsRepository) GetActiveVisitors(dbCode database.CountryID, activeWindow time.Time) ([]ActiveRow, error) {
	db := database.DBForCountry(dbCode)
	var activeRows []ActiveRow
	// ROW_NUMBER deduplicates: one row per unique visitor (by user_id for members,
	// ip_address for guests), keeping their most-recently-visited page.
	err := db.Raw(`
		SELECT ip_address, country, city, browser, os, user_agent, url, user_id,
		       user_name, user_email, last_activity, created_at
		FROM (
		  SELECT vt.ip_address, vt.country, vt.city, vt.browser, vt.os, vt.user_agent,
		         vt.url, vt.user_id, u.name AS user_name, u.email AS user_email,
		         vt.last_activity, vt.created_at,
		         ROW_NUMBER() OVER (
		           PARTITION BY COALESCE(CAST(vt.user_id AS CHAR), vt.ip_address)
		           ORDER BY vt.last_activity DESC
		         ) AS rn
		  FROM visitors_tracking vt
		  LEFT JOIN users u ON u.id = vt.user_id
		  WHERE vt.last_activity >= ?
		) ranked
		WHERE rn = 1
		ORDER BY last_activity DESC
		LIMIT 100`, activeWindow).Scan(&activeRows).Error
	return activeRows, err
}

func (r *analyticsRepository) GetUserStats(todayStart, activeWindow time.Time) (totalUsers, activeUsers, newToday int64) {
	mainDB := database.DB()
	mainDB.Model(&models.User{}).Count(&totalUsers)
	mainDB.Model(&models.User{}).Where("last_activity >= ?", activeWindow).Count(&activeUsers)
	mainDB.Model(&models.User{}).Where("created_at >= ?", todayStart).Count(&newToday)
	return
}

func (r *analyticsRepository) GetCountryStats(dbCode database.CountryID, since time.Time) ([]CountryRow, error) {
	db := database.DBForCountry(dbCode)
	var countryStats []CountryRow
	err := db.Raw(`SELECT COALESCE(country, 'Unknown') AS country, COUNT(*) AS count
		        FROM visitors_tracking
		        WHERE created_at >= ? AND country IS NOT NULL
		        GROUP BY country ORDER BY count DESC LIMIT 20`, since).Scan(&countryStats).Error
	return countryStats, err
}

func (r *analyticsRepository) GetDailyChartData(dbCode database.CountryID, since time.Time) ([]DailyRow, error) {
	db := database.DBForCountry(dbCode)
	var dailyRows []DailyRow
	err := db.Raw(`SELECT DATE_FORMAT(created_at, '%Y-%m-%d') AS date,
		               COUNT(DISTINCT ip_address) AS visitors,
		               COUNT(*) AS page_views
		        FROM visitors_tracking
		        WHERE created_at >= ?
		        GROUP BY DATE_FORMAT(created_at, '%Y-%m-%d') ORDER BY date ASC`, since).Scan(&dailyRows).Error
	return dailyRows, err
}

func (r *analyticsRepository) GetDeviceStats(dbCode database.CountryID, since time.Time) ([]DeviceRow, error) {
	db := database.DBForCountry(dbCode)
	var deviceRows []DeviceRow
	err := db.Raw(`SELECT os, COUNT(*) AS count FROM visitors_tracking
		        WHERE created_at >= ?
		        GROUP BY os`, since).Scan(&deviceRows).Error
	return deviceRows, err
}

func (r *analyticsRepository) GetTrafficSources(dbCode database.CountryID, since time.Time) ([]RefRow, error) {
	db := database.DBForCountry(dbCode)
	var refRows []RefRow
	err := db.Raw(`SELECT referer, COUNT(*) AS count FROM visitors_tracking
		        WHERE created_at >= ?
		          AND (referer IS NULL OR referer LIKE 'http://%' OR referer LIKE 'https://%')
		        GROUP BY referer ORDER BY count DESC LIMIT 100`, since).Scan(&refRows).Error
	return refRows, err
}

func (r *analyticsRepository) GetTrafficSourcesPrev(dbCode database.CountryID, prevSince, since time.Time) ([]RefRow, error) {
	db := database.DBForCountry(dbCode)
	var refRows []RefRow
	err := db.Raw(`SELECT referer, COUNT(*) AS count FROM visitors_tracking
		        WHERE created_at >= ? AND created_at < ?
		          AND (referer IS NULL OR referer LIKE 'http://%' OR referer LIKE 'https://%')
		        GROUP BY referer ORDER BY count DESC LIMIT 100`, prevSince, since).Scan(&refRows).Error
	return refRows, err
}

func (r *analyticsRepository) GetPrevTotalVisits(dbCode database.CountryID, prevSince, since time.Time) int64 {
	db := database.DBForCountry(dbCode)
	var prevTotalVisits int64
	db.Model(&models.VisitorTracking{}).Where("created_at >= ? AND created_at < ?", prevSince, since).Count(&prevTotalVisits)
	return prevTotalVisits
}

func (r *analyticsRepository) GetTotalVisitsSince(dbCode database.CountryID, since time.Time) int64 {
	db := database.DBForCountry(dbCode)
	var totalCurrent int64
	db.Model(&models.VisitorTracking{}).Where("created_at >= ?", since).Count(&totalCurrent)
	return totalCurrent
}

func (r *analyticsRepository) PruneVisitorTracking(dbCode database.CountryID, cutoff time.Time) int64 {
	db := database.DBForCountry(dbCode)
	result := db.Where("created_at < ?", cutoff).Delete(&models.VisitorTracking{})
	return result.RowsAffected
}

func (r *analyticsRepository) GetTotals(dbCode database.CountryID, fiveMinAgo time.Time) (articleCount, newsCount, userCount, onlineCount int64) {
	db := database.DBForCountry(dbCode)
	mainDB := database.DB()
	db.Model(&models.Article{}).Where("status = ?", 1).Count(&articleCount)
	db.Model(&models.Post{}).Where("is_active = ?", true).Count(&newsCount)
	mainDB.Model(&models.User{}).Count(&userCount)
	mainDB.Model(&models.User{}).Where("last_activity >= ? OR last_seen >= ?", fiveMinAgo, fiveMinAgo).Count(&onlineCount)
	return
}

func (r *analyticsRepository) GetTrends(dbCode database.CountryID, thisMonthStart, lastMonthStart time.Time) (artTrend, newsTrend, userTrend TrendRow) {
	db := database.DBForCountry(dbCode)
	mainDB := database.DB()

	db.Raw(`
			SELECT
			  SUM(CASE WHEN created_at >= ? THEN 1 ELSE 0 END) AS this_month,
			  SUM(CASE WHEN created_at >= ? AND created_at < ? THEN 1 ELSE 0 END) AS last_month
			FROM articles
			WHERE status = 1`,
		thisMonthStart, lastMonthStart, thisMonthStart,
	).Scan(&artTrend)

	db.Raw(`
			SELECT
			  SUM(CASE WHEN created_at >= ? THEN 1 ELSE 0 END) AS this_month,
			  SUM(CASE WHEN created_at >= ? AND created_at < ? THEN 1 ELSE 0 END) AS last_month
			FROM posts
			WHERE is_active = 1`,
		thisMonthStart, lastMonthStart, thisMonthStart,
	).Scan(&newsTrend)

	mainDB.Raw(`
			SELECT
			  SUM(CASE WHEN created_at >= ? THEN 1 ELSE 0 END) AS this_month,
			  SUM(CASE WHEN created_at >= ? AND created_at < ? THEN 1 ELSE 0 END) AS last_month
			FROM users`,
		thisMonthStart, lastMonthStart, thisMonthStart,
	).Scan(&userTrend)

	return
}

func (r *analyticsRepository) GetRecentActivities() ([]ActivityRow, error) {
	mainDB := database.DB()
	var rawActivities []ActivityRow
	err := mainDB.Raw(`
		SELECT al.id, al.description, al.subject_type, al.created_at,
		       COALESCE(u.name, 'Ù…Ø³ØªØ®Ø¯Ù…') AS causer_name
		FROM activity_log al
		LEFT JOIN users u ON u.id = al.causer_id
		ORDER BY al.created_at DESC
		LIMIT 10`,
	).Scan(&rawActivities).Error
	return rawActivities, err
}

func (r *analyticsRepository) GetTopArticles(dbCode database.CountryID) ([]ArticleView, error) {
	db := database.DBForCountry(dbCode)
	var topArticles []ArticleView
	err := db.Model(&models.Article{}).
		Select("id, title, visit_count").
		Where("status = ?", 1).
		Order("visit_count DESC").
		Limit(10).
		Scan(&topArticles).Error
	return topArticles, err
}

func (r *analyticsRepository) GetTopPosts(dbCode database.CountryID) ([]PostView, error) {
	db := database.DBForCountry(dbCode)
	var topPosts []PostView
	err := db.Model(&models.Post{}).
		Select("id, title, views").
		Where("is_active = ?", true).
		Order("views DESC").
		Limit(10).
		Scan(&topPosts).Error
	return topPosts, err
}

func (r *analyticsRepository) GetArticleCountsByStatus(dbCode database.CountryID) (published, draft int64) {
	db := database.DBForCountry(dbCode)
	db.Model(&models.Article{}).Where("status = ?", 1).Count(&published)
	db.Model(&models.Article{}).Where("status = ?", 0).Count(&draft)
	return
}

type analyticsRow struct {
	Date    string
	Count   int
	Views   int
	Authors int
}

func (r *analyticsRepository) GetAnalyticsData(dbCode database.CountryID, days int) (dates []string, articles, news, comments, views, authors []int) {
	db := database.DBForCountry(dbCode)
	mainDB := database.DB()

	now := time.Now()
	startDate := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).AddDate(0, 0, -days+1)

	var articleRows []analyticsRow
	db.Model(&models.Article{}).
		Select("DATE(created_at) as date, COUNT(*) as count, COALESCE(SUM(visit_count), 0) as views, COUNT(DISTINCT author_id) as authors").
		Where("created_at >= ?", startDate).
		Group("DATE(created_at)").
		Scan(&articleRows)

	var newsRows []analyticsRow
	db.Model(&models.Post{}).
		Select("DATE(created_at) as date, COUNT(*) as count, COALESCE(SUM(views), 0) as views, COUNT(DISTINCT author_id) as authors").
		Where("created_at >= ?", startDate).
		Group("DATE(created_at)").
		Scan(&newsRows)

	var commentRows []analyticsRow
	mainDB.Model(&models.Comment{}).
		Select("DATE(created_at) as date, COUNT(*) as count").
		Where("created_at >= ?", startDate).
		Group("DATE(created_at)").
		Scan(&commentRows)

	// Map data by date string
	artMap := make(map[string]analyticsRow)
	for _, r := range articleRows {
		artMap[r.Date[:10]] = r // take YYYY-MM-DD
	}
	newsMap := make(map[string]analyticsRow)
	for _, r := range newsRows {
		newsMap[r.Date[:10]] = r
	}
	commentMap := make(map[string]int)
	for _, r := range commentRows {
		commentMap[r.Date[:10]] = r.Count
	}

	dates = make([]string, days)
	articles = make([]int, days)
	news = make([]int, days)
	comments = make([]int, days)
	views = make([]int, days)
	authors = make([]int, days)

	for i := 0; i < days; i++ {
		d := startDate.AddDate(0, 0, i).Format("2006-01-02")
		dates[i] = d

		art := artMap[d]
		nws := newsMap[d]

		articles[i] = art.Count
		news[i] = nws.Count
		comments[i] = commentMap[d]
		views[i] = art.Views + nws.Views
		authors[i] = art.Authors + nws.Authors
	}

	return
}

func (r *analyticsRepository) GetOnlineUsers(fiveMinAgo time.Time) ([]models.User, error) {
	mainDB := database.DB()
	var users []models.User
	err := mainDB.Where("last_activity >= ? OR last_seen >= ?", fiveMinAgo, fiveMinAgo).
		Select("id, name, profile_photo_path, last_activity, last_seen").
		Limit(5).
		Find(&users).Error
	return users, err
}

func (r *analyticsRepository) GetRedisInfo() (string, error) {
	return database.Redis().GetInfo(context.Background())
}

func (r *analyticsRepository) PingRedis() error {
	return database.Redis().Default().Ping(context.Background()).Err()
}
