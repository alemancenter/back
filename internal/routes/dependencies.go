package routes

import (
	"context"

	"github.com/imanjo/fiber-api/internal/config"
	"github.com/imanjo/fiber-api/internal/database"
	"github.com/imanjo/fiber-api/internal/handlers/ai"
	"github.com/imanjo/fiber-api/internal/handlers/analytics"
	"github.com/imanjo/fiber-api/internal/handlers/articles"
	"github.com/imanjo/fiber-api/internal/handlers/auth"
	"github.com/imanjo/fiber-api/internal/handlers/calendar"
	"github.com/imanjo/fiber-api/internal/handlers/categories"
	chatbotHandler "github.com/imanjo/fiber-api/internal/handlers/chatbot"
	"github.com/imanjo/fiber-api/internal/handlers/comments"
	contactmsgHandler "github.com/imanjo/fiber-api/internal/handlers/contact_messages"
	contentauditHandler "github.com/imanjo/fiber-api/internal/handlers/contentaudit"
	"github.com/imanjo/fiber-api/internal/handlers/dashboard"
	"github.com/imanjo/fiber-api/internal/handlers/emailbounce"
	"github.com/imanjo/fiber-api/internal/handlers/emailverification"
	"github.com/imanjo/fiber-api/internal/handlers/files"
	"github.com/imanjo/fiber-api/internal/handlers/grades"
	"github.com/imanjo/fiber-api/internal/handlers/health"
	"github.com/imanjo/fiber-api/internal/handlers/home"
	"github.com/imanjo/fiber-api/internal/handlers/keywords"
	"github.com/imanjo/fiber-api/internal/handlers/messages"
	"github.com/imanjo/fiber-api/internal/handlers/notifications"
	"github.com/imanjo/fiber-api/internal/handlers/posts"
	redisHandler "github.com/imanjo/fiber-api/internal/handlers/redis"
	"github.com/imanjo/fiber-api/internal/handlers/roles"
	searchconsoleHandler "github.com/imanjo/fiber-api/internal/handlers/searchconsole"
	"github.com/imanjo/fiber-api/internal/handlers/security"
	seoHandler "github.com/imanjo/fiber-api/internal/handlers/seo"
	"github.com/imanjo/fiber-api/internal/handlers/settings"
	"github.com/imanjo/fiber-api/internal/handlers/sitemap"
	"github.com/imanjo/fiber-api/internal/handlers/teacher_subscription"
	"github.com/imanjo/fiber-api/internal/handlers/users"
	"github.com/imanjo/fiber-api/internal/repositories"
	chatbotRepo "github.com/imanjo/fiber-api/internal/repositories/chatbot"
	"github.com/imanjo/fiber-api/internal/services"
	chatbotSvc "github.com/imanjo/fiber-api/internal/services/chatbot"
	contentauditService "github.com/imanjo/fiber-api/internal/services/contentaudit"
	searchconsoleService "github.com/imanjo/fiber-api/internal/services/searchconsole"
	"github.com/imanjo/fiber-api/pkg/logger"
	"go.uber.org/zap"
)

type Handlers struct {
	Dashboard           *dashboard.Handler
	Auth                *auth.Handler
	Articles            *articles.Handler
	Posts               *posts.Handler
	Users               *users.Handler
	Files               *files.Handler
	Comments            *comments.Handler
	Categories          *categories.Handler
	Grades              *grades.Handler
	Calendar            *calendar.Handler
	Notifications       *notifications.Handler
	Messages            *messages.Handler
	ContactMessages     *contactmsgHandler.Handler
	Chatbot             *chatbotHandler.Handler
	Security            *security.Handler
	Settings            *settings.Handler
	Sitemap             *sitemap.Handler
	Analytics           *analytics.Handler
	Roles               *roles.Handler
	Redis               *redisHandler.Handler
	Health              *health.Handler
	Home                *home.Handler
	Keywords            *keywords.Handler
	AI                  *ai.Handler
	ContentAudit        *contentauditHandler.Handler
	SearchConsole       *searchconsoleHandler.Handler
	SEO                 *seoHandler.Handler
	EmailVerify         *emailverification.Handler
	EmailBounce         *emailbounce.Handler
	TeacherSubscription *teacher_subscription.Handler
	BounceReader        *services.BounceIMAPReader

	// SettingsSvc is exposed so middlewares (e.g. download auth gate) can read
	// settings at request time without re-instantiating the service.
	SettingsSvc services.SettingService
}

func NewDependencies() *Handlers {
	// Initialize Dependencies

	cacheSvc := services.NewCacheService(database.Redis().Cache())

	sitemapRepo := repositories.NewSitemapRepository()
	sitemapSvc := services.NewSitemapService(sitemapRepo)

	fileRepo := repositories.NewFileRepository()
	fileSvc := services.NewFileService(fileRepo, sitemapSvc)
	articleRepo := repositories.NewArticleRepository()
	articleSvc := services.NewArticleService(articleRepo, fileSvc, cacheSvc, sitemapSvc)

	userRepo := repositories.NewUserRepository()
	jwtSvc := services.NewJWTService()
	mailSvc := services.NewMailService()
	authSvc := services.NewAuthService(userRepo, jwtSvc, mailSvc)
	emailVerifySvc := services.NewEmailVerificationReminderService(mailSvc, jwtSvc)
	var userSvc services.UserService

	categoryRepo := repositories.NewCategoryRepository()
	categorySvc := services.NewCategoryService(categoryRepo, cacheSvc)

	commentRepo := repositories.NewCommentRepository()
	commentSvc := services.NewCommentService(commentRepo)
	postRepo := repositories.NewPostRepository()
	postSvc := services.NewPostService(postRepo, fileSvc, cacheSvc, sitemapSvc)

	gradeRepo := repositories.NewGradeRepository()
	gradeSvc := services.NewGradeService(gradeRepo, cacheSvc)

	calendarRepo := repositories.NewCalendarRepository()
	calendarSvc := services.NewCalendarService(calendarRepo)

	dashboardRepo := repositories.NewDashboardRepository()
	dashboardSvc := services.NewDashboardService(dashboardRepo)

	healthRepo := repositories.NewHealthRepository()
	healthSvc := services.NewHealthService(healthRepo)

	messageRepo := repositories.NewMessageRepository()
	messageSvc := services.NewMessageService(messageRepo)

	contactMsgRepo := repositories.NewContactMessageRepository()
	contactMsgSvc := services.NewContactMessageService(contactMsgRepo)

	chatbotRepository := chatbotRepo.NewRepository()
	chatbotService := chatbotSvc.NewService(chatbotRepository)

	notificationRepo := repositories.NewNotificationRepository()
	cfg := config.Load()
	pushSvc := services.NewPushService(
		userRepo,
		cfg.FCM.Enabled,
		cfg.FCM.ProjectID,
		cfg.FCM.ServiceAccountFile,
		cfg.OneSignal.AppID,
		cfg.OneSignal.APIKey,
	)
	notificationSvc := services.NewNotificationService(notificationRepo, userRepo, pushSvc)

	redisRepo := repositories.NewRedisRepository()
	redisSvc := services.NewRedisService(redisRepo)

	roleRepo := repositories.NewRoleRepository()
	roleSvc := services.NewRoleService(roleRepo, userRepo)

	securityRepo := repositories.NewSecurityRepository()
	securitySvc := services.NewSecurityService(securityRepo)
	userSvc = services.NewUserService(userRepo, securitySvc)

	settingRepo := repositories.NewSettingRepository()
	settingSvc := services.NewSettingService(settingRepo)
	seoRepo := repositories.NewSEORepository()
	seoSvc := services.NewSEOService(seoRepo, settingSvc, sitemapSvc)

	analyticsRepo := repositories.NewAnalyticsRepository()
	analyticsSvc := services.NewAnalyticsService(analyticsRepo)

	keywordRepo := repositories.NewKeywordRepository()
	keywordSvc := services.NewKeywordService(keywordRepo)

	aiSvc := services.NewAIService(config.Load().AI.TogetherAPIKey)

	teacherSubRepo := repositories.NewTeacherSubscriptionRepository()
	teacherSubSvc := services.NewTeacherSubscriptionService(teacherSubRepo, aiSvc)
	_, _ = teacherSubSvc.EnsureDefaultPlan()

	contentAuditRepo := repositories.NewContentAuditRepository()
	contentAuditSvc := contentauditService.NewServiceWithAIAndNotifications(contentAuditRepo, contentauditService.Options{}, aiSvc, notificationSvc)

	// Google Search Console: absent-by-default. gscClient stays nil unless a
	// service account key is configured, and searchconsoleService.Service
	// tolerates a nil client (returns ErrNotConfigured on use) — see
	// CONTENT_QUALITY_GOVERNANCE_CENTER_PLAN.md §4.1. No per-request credential
	// exchange: one service account, added as a user on each property manually.
	gscRepo := repositories.NewGSCRepository()
	gscCfg := config.Load().SearchConsole
	var gscClient *searchconsoleService.Client
	if gscCfg.Enabled && gscCfg.ServiceAccountJSON != "" {
		client, err := searchconsoleService.NewClient(context.Background(), gscCfg.ServiceAccountJSON)
		if err != nil {
			logger.Warn("google search console client init failed; GSC endpoints will report not-configured", zap.Error(err))
		} else {
			gscClient = client
		}
	}
	gscSvc := searchconsoleService.NewService(gscRepo, gscClient)

	homeSvc := services.NewHomeService(articleRepo, postRepo, categoryRepo, gradeRepo, cacheSvc, settingSvc)

	bounceReader := services.NewBounceIMAPReader(services.NewBounceProcessorService())

	return &Handlers{
		Dashboard:           dashboard.New(dashboardSvc),
		Auth:                auth.New(authSvc),
		Articles:            articles.New(articleSvc, notificationSvc),
		Posts:               posts.NewWithFileService(postSvc, notificationSvc, fileSvc),
		Users:               users.New(userSvc, notificationSvc),
		Files:               files.New(fileSvc),
		Comments:            comments.New(commentSvc),
		Categories:          categories.New(categorySvc),
		Grades:              grades.New(gradeSvc, fileSvc),
		Calendar:            calendar.New(calendarSvc),
		Notifications:       notifications.New(notificationSvc),
		Messages:            messages.New(messageSvc, notificationSvc),
		ContactMessages:     contactmsgHandler.New(contactMsgSvc),
		Chatbot:             chatbotHandler.New(chatbotService),
		Security:            security.New(securitySvc),
		Settings:            settings.New(settingSvc, notificationSvc),
		Sitemap:             sitemap.New(sitemapSvc),
		Analytics:           analytics.New(analyticsSvc),
		Roles:               roles.New(roleSvc),
		Redis:               redisHandler.New(redisSvc),
		Health:              health.New(healthSvc),
		Home:                home.New(homeSvc),
		Keywords:            keywords.New(keywordSvc),
		AI:                  ai.New(aiSvc, contentAuditSvc),
		ContentAudit:        contentauditHandler.New(contentAuditSvc),
		SearchConsole:       searchconsoleHandler.New(gscRepo, gscSvc),
		SEO:                 seoHandler.New(seoSvc),
		EmailVerify:         emailverification.New(emailVerifySvc),
		EmailBounce:         emailbounce.New(bounceReader),
		TeacherSubscription: teacher_subscription.New(teacherSubSvc),
		BounceReader:        bounceReader,
		SettingsSvc:         settingSvc,
	}
}
