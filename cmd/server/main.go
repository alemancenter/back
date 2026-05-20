package main

import (
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/alemancenter/fiber-api/internal/config"
	"github.com/alemancenter/fiber-api/internal/database"
	"github.com/alemancenter/fiber-api/internal/middleware"
	"github.com/alemancenter/fiber-api/internal/models"
	"github.com/alemancenter/fiber-api/internal/repositories"
	"github.com/alemancenter/fiber-api/internal/routes"
	"github.com/alemancenter/fiber-api/internal/services"
	contentauditService "github.com/alemancenter/fiber-api/internal/services/contentaudit"
	"github.com/alemancenter/fiber-api/internal/utils"
	"github.com/alemancenter/fiber-api/pkg/logger"

	_ "github.com/alemancenter/fiber-api/docs" // Swagger docs

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/gofiber/swagger"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// @title Alemancenter API
// @version 2.0.0
// @description Backend API for Alemancenter Educational Platform.
// @termsOfService http://swagger.io/terms/
// @security FrontendKeyAuth

// @contact.name API Support
// @contact.email support@alemedu.com

// @license.name MIT
// @license.url https://opensource.org/licenses/MIT

// @host api.alemedu.com
// @BasePath /api
// @schemes https http

// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization
// @description Type "Bearer" followed by a space and JWT token.

// @securityDefinitions.apikey FrontendKeyAuth
// @in header
// @name X-Frontend-Key
// @description Frontend identifier key for public endpoints.

func main() {
	// Load configuration
	cfg := config.Load()

	// Initialize logger
	log := logger.Init(logger.Config{
		Level:      cfg.Log.Level,
		FilePath:   cfg.Log.Path,
		MaxSize:    cfg.Log.MaxSize,
		MaxBackups: cfg.Log.MaxBackups,
		MaxAge:     cfg.Log.MaxAge,
		Debug:      cfg.App.Debug || cfg.App.IsDevelopment(),
	})
	defer log.Sync()

	logger.Info("Starting Alemancenter Fiber API",
		zap.String("app", cfg.App.Name),
		zap.String("env", cfg.App.Env),
		zap.String("version", "2.0.0"),
	)

	// Initialize databases
	logger.Info("Connecting to databases...")
	dbManager := database.GetManager()
	healthResults := dbManager.HealthCheck()
	for country, ok := range healthResults {
		if !ok {
			logger.Error("Database connection failed", zap.String("country", country))
		}
	}

	// Auto-migrate: add any missing columns (safe — never drops existing data)
	migrateTargets := []interface{}{
		&models.Article{},
		&models.BlockedIP{},
		&models.TrustedIP{},
		&models.SecurityLog{},
		&models.VisitorTracking{},
		&models.VisitorSession{},
		&models.Comment{},
		&models.Permission{},
		&models.PolicyAuditRun{},
		&models.PolicyAuditFinding{},
		&models.ContentAIDecision{},
		&models.ContentAIIssue{},
		&models.ContentAISuggestion{},
		&models.ContentAIFixPreview{},
		&models.ContentAIApprovalLog{},
	}
	seen := make(map[*gorm.DB]bool)
	for _, id := range []database.CountryID{database.CountryJordan, database.CountrySaudi, database.CountryEgypt, database.CountryPalestine} {
		db := dbManager.Get(id)
		if seen[db] {
			continue
		}
		seen[db] = true
		// Drop legacy incompatible FK constraints left by Laravel before migrating
		if db.Migrator().HasConstraint(&models.Article{}, "articles_grade_level_foreign") {
			db.Migrator().DropConstraint(&models.Article{}, "articles_grade_level_foreign")
		}
		if err := db.AutoMigrate(migrateTargets...); err != nil {
			logger.Warn("auto-migrate failed", zap.String("country", database.CountryCode(id)), zap.Error(err))
		}
		ensureContentAISchema(db, database.CountryCode(id))
	}
	ensurePermission("manage content audit")

	// Initialize Redis
	logger.Info("Connecting to Redis...")
	database.GetRedis()

	// Start background workers
	services.StartViewSyncWorker(1 * time.Minute)
	services.StartVisitorWorker(5 * time.Second)
	startContentAuditScheduler(cfg)

	// Periodically prune expired AI generation jobs from the in-memory store.
	go func() {
		ticker := time.NewTicker(30 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			services.GetAIJobStore().Prune()
		}
	}()

	// Create Fiber app
	app := fiber.New(fiber.Config{
		AppName:                 cfg.App.Name + " v2.0.0",
		ServerHeader:            "", // Don't expose server info
		StrictRouting:           false,
		CaseSensitive:           false,
		Immutable:               false,
		UnescapePath:            true,
		BodyLimit:               100 * 1024 * 1024, // 100MB
		ReadTimeout:             30 * time.Second,
		WriteTimeout:            120 * time.Second, // AI generation can take up to 90s
		IdleTimeout:             120 * time.Second,
		ReadBufferSize:          8192,
		WriteBufferSize:         8192,
		CompressedFileSuffix:    ".fiber.gz",
		ProxyHeader:             "X-Forwarded-For",
		EnableTrustedProxyCheck: true,
		TrustedProxies:          cfg.App.TrustedProxies,
		EnableIPValidation:      true,
		ErrorHandler: func(c *fiber.Ctx, err error) error {
			code := fiber.StatusInternalServerError
			message := "حدث خطأ داخلي في الخادم"

			if e, ok := err.(*fiber.Error); ok {
				code = e.Code
				switch code {
				case fiber.StatusNotFound:
					message = "المسار المطلوب غير موجود"
				case fiber.StatusMethodNotAllowed:
					message = "طريقة الطلب غير مدعومة"
				case fiber.StatusTooManyRequests:
					message = "تم تجاوز الحد المسموح للطلبات"
				}
			}

			fields := []zap.Field{
				zap.Int("status", code),
				zap.String("method", c.Method()),
				zap.String("path", c.Path()),
				zap.String("ip", c.IP()),
				zap.String("error", err.Error()),
			}

			// 408 is usually produced by fasthttp before routing when a client,
			// proxy, browser preconnect, or scanner opens a socket but does not
			// complete the request within ReadTimeout. It is not an application
			// failure, so avoid noisy ERROR stacktraces.
			switch {
			case code == fiber.StatusRequestTimeout:
				logger.Debug("client request timed out", fields...)
			case code >= fiber.StatusBadRequest && code < fiber.StatusInternalServerError:
				logger.Warn("client request error", fields...)
			default:
				logger.Error("unhandled server error", append(fields, zap.Error(err))...)
			}

			return c.Status(code).JSON(utils.APIResponse{
				Success: false,
				Message: message,
			})
		},
	})

	// Recover from panics
	app.Use(recover.New(recover.Config{
		EnableStackTrace: cfg.App.Debug,
		StackTraceHandler: func(c *fiber.Ctx, e interface{}) {
			logger.Error("panic recovered",
				zap.String("path", c.Path()),
				zap.Any("error", e),
			)
		},
	}))

	// Method Override (Laravel style _method for PUT/PATCH/DELETE in POST forms)
	app.Use(middleware.MethodOverride())

	// Serve static storage files (uploads, settings images, etc.)
	// Next.js rewrites /storage/:path* → backend /storage/:path*
	// STORAGE_PATH must match Laravel's storage/app/public layout so that
	// a file stored at {STORAGE_PATH}/files/foo.doc is served at /storage/files/foo.doc
	storageRoot := cfg.Storage.Path
	if storageRoot == "" {
		storageRoot = "./storage/app/public"
	}
	app.Static("/storage", storageRoot, fiber.Static{
		Compress:  true,
		ByteRange: true,
		Browse:    false,
		MaxAge:    31536000,
	})

	// Register all routes
	routes.Setup(app)

	// Setup Swagger UI route
	app.Get("/swagger/*", swagger.HandlerDefault)

	// 404 handler
	app.Use(func(c *fiber.Ctx) error {
		return c.Status(fiber.StatusNotFound).JSON(utils.APIResponse{
			Success: false,
			Message: "المسار المطلوب غير موجود",
		})
	})

	// Start server
	addr := fmt.Sprintf("%s:%d", cfg.App.Host, cfg.App.Port)
	logger.Info("Server starting", zap.String("addr", addr))

	// Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		if err := app.Listen(addr); err != nil {
			logger.Fatal("failed to start server", zap.Error(err))
		}
	}()

	logger.Info("Server is running",
		zap.String("addr", addr),
		zap.String("url", cfg.App.URL),
	)

	// Wait for shutdown signal
	<-quit
	logger.Info("Shutting down server...")

	if err := app.ShutdownWithTimeout(10 * time.Second); err != nil {
		logger.Error("error during shutdown", zap.Error(err))
	}

	logger.Info("Server stopped gracefully")
}

func startContentAuditScheduler(cfg *config.Config) {
	value := strings.ToLower(strings.TrimSpace(os.Getenv("CONTENT_AUDIT_SCHEDULER")))
	if value == "0" || value == "false" || value == "off" || value == "disabled" {
		logger.Info("content audit scheduler disabled")
		return
	}

	interval := 24 * time.Hour
	if raw := strings.TrimSpace(os.Getenv("CONTENT_AUDIT_INTERVAL_HOURS")); raw != "" {
		hours, err := strconv.Atoi(raw)
		if err == nil && hours > 0 {
			interval = time.Duration(hours) * time.Hour
		} else {
			logger.Warn("invalid CONTENT_AUDIT_INTERVAL_HOURS; using default", zap.String("value", raw))
		}
	}

	initialDelay := 15 * time.Minute
	if raw := strings.TrimSpace(os.Getenv("CONTENT_AUDIT_INITIAL_DELAY_MINUTES")); raw != "" {
		minutes, err := strconv.Atoi(raw)
		if err == nil && minutes >= 0 {
			initialDelay = time.Duration(minutes) * time.Minute
		} else {
			logger.Warn("invalid CONTENT_AUDIT_INITIAL_DELAY_MINUTES; using default", zap.String("value", raw))
		}
	}

	auditRepo := repositories.NewContentAuditRepository()
	auditSvc := contentauditService.NewService(auditRepo, contentauditService.Options{Config: cfg})
	auditSvc.StartScheduler(interval, initialDelay)
	logger.Info("content audit scheduler started", zap.Duration("interval", interval), zap.Duration("initial_delay", initialDelay))
}

func ensureContentAISchema(db *gorm.DB, country string) {
	statements := []string{
		"ALTER TABLE content_ai_decisions MODIFY COLUMN country_code VARCHAR(20) NOT NULL DEFAULT 'jo'",
		"ALTER TABLE content_ai_fix_previews MODIFY COLUMN country_code VARCHAR(20) NOT NULL DEFAULT 'jo'",
		"UPDATE content_ai_decisions SET country_code = 'jo', content_id = CONCAT('jo:', SUBSTRING_INDEX(content_id, ':', -1)) WHERE country_code IN ('alhurani_jo', 'jordan', 'Jordan') OR country_code LIKE '%_jo'",
		"UPDATE content_ai_fix_previews SET country_code = 'jo', content_id = CONCAT('jo:', SUBSTRING_INDEX(content_id, ':', -1)) WHERE country_code IN ('alhurani_jo', 'jordan', 'Jordan') OR country_code LIKE '%_jo'",
	}
	for _, stmt := range statements {
		if err := db.Exec(stmt).Error; err != nil {
			logger.Warn("content AI schema normalization skipped", zap.String("country", country), zap.Error(err))
		}
	}
}

func ensurePermission(name string) {
	permission := models.Permission{Name: name, GuardName: "api"}
	if err := database.DB().Where("name = ?", name).FirstOrCreate(&permission).Error; err != nil {
		logger.Warn("failed to ensure permission", zap.String("permission", name), zap.Error(err))
	}
}
