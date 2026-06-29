package routes

import (
	"github.com/alemancenter/fiber-api/internal/config"
	"github.com/alemancenter/fiber-api/internal/database"
	"github.com/alemancenter/fiber-api/internal/handlers/ai"
	"github.com/alemancenter/fiber-api/internal/handlers/analytics"
	"github.com/alemancenter/fiber-api/internal/handlers/articles"
	"github.com/alemancenter/fiber-api/internal/handlers/auth"
	"github.com/alemancenter/fiber-api/internal/handlers/calendar"
	"github.com/alemancenter/fiber-api/internal/handlers/categories"
	chatbotHandler "github.com/alemancenter/fiber-api/internal/handlers/chatbot"
	"github.com/alemancenter/fiber-api/internal/handlers/comments"
	contactmsgHandler "github.com/alemancenter/fiber-api/internal/handlers/contact_messages"
	contentauditHandler "github.com/alemancenter/fiber-api/internal/handlers/contentaudit"
	"github.com/alemancenter/fiber-api/internal/handlers/dashboard"
	"github.com/alemancenter/fiber-api/internal/handlers/emailbounce"
	"github.com/alemancenter/fiber-api/internal/handlers/emailverification"
	"github.com/alemancenter/fiber-api/internal/handlers/files"
	"github.com/alemancenter/fiber-api/internal/handlers/grades"
	"github.com/alemancenter/fiber-api/internal/handlers/health"
	"github.com/alemancenter/fiber-api/internal/handlers/home"
	"github.com/alemancenter/fiber-api/internal/handlers/keywords"
	"github.com/alemancenter/fiber-api/internal/handlers/messages"
	"github.com/alemancenter/fiber-api/internal/handlers/notifications"
	"github.com/alemancenter/fiber-api/internal/handlers/posts"
	redisHandler "github.com/alemancenter/fiber-api/internal/handlers/redis"
	"github.com/alemancenter/fiber-api/internal/handlers/roles"
	"github.com/alemancenter/fiber-api/internal/handlers/security"
	"github.com/alemancenter/fiber-api/internal/handlers/settings"
	"github.com/alemancenter/fiber-api/internal/handlers/sitemap"
	"github.com/alemancenter/fiber-api/internal/handlers/teacher_subscription"
	"github.com/alemancenter/fiber-api/internal/handlers/users"
	"github.com/alemancenter/fiber-api/internal/repositories"
	chatbotRepo "github.com/alemancenter/fiber-api/internal/repositories/chatbot"
	"github.com/alemancenter/fiber-api/internal/services"
	chatbotSvc "github.com/alemancenter/fiber-api/internal/services/chatbot"
	contentauditService "github.com/alemancenter/fiber-api/internal/services/contentaudit"
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

	fileRepo := repositories.NewFileRepository()
	fileSvc := services.NewFileService(fileRepo)
	articleRepo := repositories.NewArticleRepository()
	articleSvc := services.NewArticleService(articleRepo, fileSvc, cacheSvc)

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
	postSvc := services.NewPostService(postRepo, fileSvc, cacheSvc)

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
	roleSvc := services.NewRoleService(roleRepo)

	securityRepo := repositories.NewSecurityRepository()
	securitySvc := services.NewSecurityService(securityRepo)
	userSvc = services.NewUserService(userRepo, securitySvc)

	settingRepo := repositories.NewSettingRepository()
	settingSvc := services.NewSettingService(settingRepo)

	sitemapRepo := repositories.NewSitemapRepository()
	sitemapSvc := services.NewSitemapService(sitemapRepo)

	analyticsRepo := repositories.NewAnalyticsRepository()
	analyticsSvc := services.NewAnalyticsService(analyticsRepo)

	keywordRepo := repositories.NewKeywordRepository()
	keywordSvc := services.NewKeywordService(keywordRepo)

	teacherSubRepo := repositories.NewTeacherSubscriptionRepository()
	teacherSubSvc := services.NewTeacherSubscriptionService(teacherSubRepo)
	_, _ = teacherSubSvc.EnsureDefaultPlan()

	contentAuditRepo := repositories.NewContentAuditRepository()
	aiSvc := services.NewAIService(config.Load().AI.TogetherAPIKey)
	contentAuditSvc := contentauditService.NewServiceWithAIAndNotifications(contentAuditRepo, contentauditService.Options{}, aiSvc, notificationSvc)

	homeSvc := services.NewHomeService(articleRepo, postRepo, categoryRepo, gradeRepo, cacheSvc, settingSvc)

	bounceReader := services.NewBounceIMAPReader(services.NewBounceProcessorService())

	return &Handlers{
		Dashboard:           dashboard.New(dashboardSvc),
		Auth:                auth.New(authSvc),
		Articles:            articles.New(articleSvc, notificationSvc),
		Posts:               posts.New(postSvc, notificationSvc),
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
		AI:                  ai.New(aiSvc),
		ContentAudit:        contentauditHandler.New(contentAuditSvc),
		EmailVerify:         emailverification.New(emailVerifySvc),
		EmailBounce:         emailbounce.New(bounceReader),
		TeacherSubscription: teacher_subscription.New(teacherSubSvc),
		BounceReader:        bounceReader,
		SettingsSvc:         settingSvc,
	}
}
