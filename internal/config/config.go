package config

import (
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/spf13/viper"
)

var (
	cfg    *Config
	once   sync.Once
	cfgMu  sync.RWMutex
)

// UpdateMailConfig replaces the in-memory mail configuration at runtime.
func UpdateMailConfig(mailCfg MailConfig) {
	cfgMu.Lock()
	defer cfgMu.Unlock()
	if cfg != nil {
		cfg.Mail = mailCfg
	}
}

// UpdateBounceConfig replaces the in-memory bounce mailbox configuration at runtime.
func UpdateBounceConfig(bounceCfg BounceMboxConfig) {
	cfgMu.Lock()
	defer cfgMu.Unlock()
	if cfg != nil {
		cfg.Mail.Bounce = bounceCfg
	}
}

// UpdateBounceAddress replaces only the bounce address inside MailConfig at runtime.
func UpdateBounceAddress(addr string) {
	cfgMu.Lock()
	defer cfgMu.Unlock()
	if cfg != nil {
		cfg.Mail.BounceAddress = addr
	}
}

// UpdateGoogleConfig replaces the in-memory Google OAuth configuration at runtime.
func UpdateGoogleConfig(googleCfg GoogleConfig) {
	cfgMu.Lock()
	defer cfgMu.Unlock()
	if cfg != nil {
		cfg.Google = googleCfg
	}
}

type Config struct {
	App       AppConfig
	JWT       JWTConfig
	Frontend  FrontendConfig
	Database  DatabaseConfig
	Redis     RedisConfig
	Mail      MailConfig
	Google    GoogleConfig
	FCM       FCMConfig
	Storage   StorageConfig
	Security  SecurityConfig
	Log       LogConfig
	GeoIP     GeoIPConfig
	AI        AIConfig
	OneSignal OneSignalConfig
}

type AppConfig struct {
	Name           string
	Env            string
	Debug          bool
	Host           string
	Port           int
	URL            string
	Timezone       string
	Locale         string
	TrustedProxies []string
}

type JWTConfig struct {
	Secret       string
	ExpireHours  int
	RefreshHours int
}

type FrontendConfig struct {
	APIKey          string
	URL             string
	CORSOrigins     []string
	RateLimit       bool
	RateLimitMax    int
	RateLimitWindow int
	LoginRateLimit  int
	APIRateLimit    int
	SSRRateLimitMax int
	SSRTrustedIPs   []string
}

type DBConnection struct {
	Host     string
	Port     int
	Name     string
	User     string
	Password string
	Charset  string
	MaxIdle  int
	MaxOpen  int
	MaxLife  int
}

type DatabaseConfig struct {
	Jordan    DBConnection
	Saudi     DBConnection
	Egypt     DBConnection
	Palestine DBConnection
}

type RedisConfig struct {
	Host     string
	Port     int
	Password string
	DB       int
	CacheDB  int
	QueueDB  int
	Prefix   string
}

type MailConfig struct {
	Host          string
	Port          int
	Username      string
	Password      string
	Encryption    string
	FromAddress   string
	FromName      string
	BounceAddress string
	Bounce        BounceMboxConfig
}

// BounceMboxConfig holds IMAP credentials for the bounce mailbox (e.g. bounce@example.com).
type BounceMboxConfig struct {
	Enabled  bool
	Host     string
	Port     int
	Username string
	Password string
	TLS      bool
}

type GoogleConfig struct {
	ClientID     string
	ClientSecret string
	RedirectURI  string
}

type FCMConfig struct {
	Enabled            bool
	ProjectID          string
	ServiceAccountFile string
}

type OneSignalConfig struct {
	AppID  string
	APIKey string
}

type StorageConfig struct {
	Driver string
	Path   string
	URL    string
}

type SecurityConfig struct {
	AddHSTS           bool
	SessionLifetime   int
	VisitorActiveMins int
	VisitorPruneMins  int
}

type LogConfig struct {
	Level      string
	Path       string
	MaxSize    int
	MaxBackups int
	MaxAge     int
}

type GeoIPConfig struct {
	DBPath string
}

type AIConfig struct {
	TogetherAPIKey string
}

func Load() *Config {
	once.Do(func() {
		v := viper.New()
		v.SetConfigFile(".env")
		v.SetConfigType("env")
		v.AutomaticEnv()
		v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

		// Set defaults
		v.SetDefault("APP_NAME", "Alemancenter API")
		v.SetDefault("APP_ENV", "production")
		v.SetDefault("APP_DEBUG", false)
		v.SetDefault("APP_HOST", "0.0.0.0")
		v.SetDefault("APP_PORT", 8080)
		v.SetDefault("APP_TIMEZONE", "UTC")
		v.SetDefault("APP_LOCALE", "ar")
		v.SetDefault("JWT_EXPIRE_HOURS", 24)
		v.SetDefault("JWT_REFRESH_HOURS", 168)
		v.SetDefault("DB_PORT_JO", 3306)
		v.SetDefault("DB_PORT_SA", 3306)
		v.SetDefault("DB_PORT_EG", 3306)
		v.SetDefault("DB_PORT_PS", 3306)
		v.SetDefault("DB_CHARSET", "utf8mb4")
		v.SetDefault("DB_MAX_IDLE_CONNS", 10)
		v.SetDefault("DB_MAX_OPEN_CONNS", 100)
		v.SetDefault("DB_CONN_MAX_LIFETIME", 3600)
		v.SetDefault("REDIS_HOST", "127.0.0.1")
		v.SetDefault("REDIS_PORT", 6379)
		v.SetDefault("REDIS_DB", 0)
		v.SetDefault("REDIS_CACHE_DB", 1)
		v.SetDefault("REDIS_QUEUE_DB", 2)
		v.SetDefault("REDIS_PREFIX", "alemancenter")
		v.SetDefault("TRUSTED_PROXIES", "127.0.0.1")
		v.SetDefault("FRONTEND_RATE_LIMIT", true)
		v.SetDefault("FRONTEND_RATE_LIMIT_MAX", 100)
		v.SetDefault("FRONTEND_RATE_LIMIT_WINDOW", 60)
		v.SetDefault("LOGIN_RATE_LIMIT", 5)
		v.SetDefault("API_RATE_LIMIT", 60)
		v.SetDefault("SSR_RATE_LIMIT_MAX", 2000)
		v.SetDefault("MAIL_PORT", 587)
		v.SetDefault("MAIL_ENCRYPTION", "tls")
		v.SetDefault("BOUNCE_IMAP_PORT", 993)
		v.SetDefault("BOUNCE_IMAP_TLS", true)
		v.SetDefault("BOUNCE_PROCESSOR_ENABLED", false)
		v.SetDefault("STORAGE_DRIVER", "local")
		v.SetDefault("STORAGE_PATH", "./storage/uploads")
		v.SetDefault("APP_ADD_HSTS", true)
		v.SetDefault("SESSION_LIFETIME", 30)
		v.SetDefault("VISITOR_ACTIVE_MINUTES", 5)
		v.SetDefault("VISITOR_PRUNE_MINUTES", 30)
		v.SetDefault("LOG_LEVEL", "error")
		v.SetDefault("LOG_PATH", "./storage/logs/app.log")
		v.SetDefault("LOG_MAX_SIZE", 100)
		v.SetDefault("LOG_MAX_BACKUPS", 5)
		v.SetDefault("LOG_MAX_AGE", 30)

		_ = v.ReadInConfig()

		jwtSecret := v.GetString("JWT_SECRET")
		if len(jwtSecret) < 32 {
			fmt.Fprintf(os.Stderr, "FATAL: JWT_SECRET must be at least 32 characters (got %d). Set a strong secret in .env\n", len(jwtSecret))
			os.Exit(1)
		}
		if jwtSecretLowEntropy(jwtSecret) {
			fmt.Fprintf(os.Stderr, "FATAL: JWT_SECRET has insufficient entropy. Use a cryptographically random value (e.g. openssl rand -hex 32).\n")
			os.Exit(1)
		}

		// Validate required environment variables that have no safe default
		required := []string{
			"JWT_SECRET",
			"DB_HOST_JO", "DB_NAME_JO", "DB_USER_JO",
			"APP_URL", "FRONTEND_URL",
		}
		var missing []string
		for _, key := range required {
			if v.GetString(key) == "" {
				missing = append(missing, key)
			}
		}
		if len(missing) > 0 {
			fmt.Fprintf(os.Stderr, "FATAL: missing required environment variables: %v\n", missing)
			os.Exit(1)
		}

		cfg = &Config{
			App: AppConfig{
				Name:           v.GetString("APP_NAME"),
				Env:            v.GetString("APP_ENV"),
				Debug:          v.GetBool("APP_DEBUG"),
				Host:           v.GetString("APP_HOST"),
				Port:           v.GetInt("APP_PORT"),
				URL:            v.GetString("APP_URL"),
				Timezone:       v.GetString("APP_TIMEZONE"),
				Locale:         v.GetString("APP_LOCALE"),
				TrustedProxies: strings.Split(v.GetString("TRUSTED_PROXIES"), ","),
			},
			JWT: JWTConfig{
				Secret:       v.GetString("JWT_SECRET"),
				ExpireHours:  v.GetInt("JWT_EXPIRE_HOURS"),
				RefreshHours: v.GetInt("JWT_REFRESH_HOURS"),
			},
			Frontend: FrontendConfig{
				APIKey:          v.GetString("FRONTEND_API_KEY"),
				URL:             v.GetString("FRONTEND_URL"),
				CORSOrigins:     strings.Split(v.GetString("CORS_ALLOWED_ORIGINS"), ","),
				RateLimit:       v.GetBool("FRONTEND_RATE_LIMIT"),
				RateLimitMax:    v.GetInt("FRONTEND_RATE_LIMIT_MAX"),
				RateLimitWindow: v.GetInt("FRONTEND_RATE_LIMIT_WINDOW"),
				LoginRateLimit:  v.GetInt("LOGIN_RATE_LIMIT"),
				APIRateLimit:    v.GetInt("API_RATE_LIMIT"),
				SSRRateLimitMax: v.GetInt("SSR_RATE_LIMIT_MAX"),
				SSRTrustedIPs:   strings.Split(v.GetString("SSR_TRUSTED_IPS"), ","),
			},
			Database: DatabaseConfig{
				Jordan: DBConnection{
					Host:     v.GetString("DB_HOST_JO"),
					Port:     v.GetInt("DB_PORT_JO"),
					Name:     v.GetString("DB_NAME_JO"),
					User:     v.GetString("DB_USER_JO"),
					Password: v.GetString("DB_PASS_JO"),
					Charset:  v.GetString("DB_CHARSET"),
					MaxIdle:  v.GetInt("DB_MAX_IDLE_CONNS"),
					MaxOpen:  v.GetInt("DB_MAX_OPEN_CONNS"),
					MaxLife:  v.GetInt("DB_CONN_MAX_LIFETIME"),
				},
				Saudi: DBConnection{
					Host:     v.GetString("DB_HOST_SA"),
					Port:     v.GetInt("DB_PORT_SA"),
					Name:     v.GetString("DB_NAME_SA"),
					User:     v.GetString("DB_USER_SA"),
					Password: v.GetString("DB_PASS_SA"),
					Charset:  v.GetString("DB_CHARSET"),
					MaxIdle:  v.GetInt("DB_MAX_IDLE_CONNS"),
					MaxOpen:  v.GetInt("DB_MAX_OPEN_CONNS"),
					MaxLife:  v.GetInt("DB_CONN_MAX_LIFETIME"),
				},
				Egypt: DBConnection{
					Host:     v.GetString("DB_HOST_EG"),
					Port:     v.GetInt("DB_PORT_EG"),
					Name:     v.GetString("DB_NAME_EG"),
					User:     v.GetString("DB_USER_EG"),
					Password: v.GetString("DB_PASS_EG"),
					Charset:  v.GetString("DB_CHARSET"),
					MaxIdle:  v.GetInt("DB_MAX_IDLE_CONNS"),
					MaxOpen:  v.GetInt("DB_MAX_OPEN_CONNS"),
					MaxLife:  v.GetInt("DB_CONN_MAX_LIFETIME"),
				},
				Palestine: DBConnection{
					Host:     v.GetString("DB_HOST_PS"),
					Port:     v.GetInt("DB_PORT_PS"),
					Name:     v.GetString("DB_NAME_PS"),
					User:     v.GetString("DB_USER_PS"),
					Password: v.GetString("DB_PASS_PS"),
					Charset:  v.GetString("DB_CHARSET"),
					MaxIdle:  v.GetInt("DB_MAX_IDLE_CONNS"),
					MaxOpen:  v.GetInt("DB_MAX_OPEN_CONNS"),
					MaxLife:  v.GetInt("DB_CONN_MAX_LIFETIME"),
				},
			},
			Redis: RedisConfig{
				Host:     v.GetString("REDIS_HOST"),
				Port:     v.GetInt("REDIS_PORT"),
				Password: v.GetString("REDIS_PASSWORD"),
				DB:       v.GetInt("REDIS_DB"),
				CacheDB:  v.GetInt("REDIS_CACHE_DB"),
				QueueDB:  v.GetInt("REDIS_QUEUE_DB"),
				Prefix:   v.GetString("REDIS_PREFIX"),
			},
			Mail: MailConfig{
				Host:          v.GetString("MAIL_HOST"),
				Port:          v.GetInt("MAIL_PORT"),
				Username:      v.GetString("MAIL_USERNAME"),
				Password:      v.GetString("MAIL_PASSWORD"),
				Encryption:    v.GetString("MAIL_ENCRYPTION"),
				FromAddress:   v.GetString("MAIL_FROM_ADDRESS"),
				FromName:      v.GetString("MAIL_FROM_NAME"),
				BounceAddress: v.GetString("MAIL_BOUNCE_ADDRESS"),
				Bounce: BounceMboxConfig{
					Enabled:  v.GetBool("BOUNCE_PROCESSOR_ENABLED"),
					Host:     v.GetString("BOUNCE_IMAP_HOST"),
					Port:     v.GetInt("BOUNCE_IMAP_PORT"),
					Username: v.GetString("BOUNCE_IMAP_USERNAME"),
					Password: v.GetString("BOUNCE_IMAP_PASSWORD"),
					TLS:      v.GetBool("BOUNCE_IMAP_TLS"),
				},
			},
			Google: GoogleConfig{
				ClientID:     v.GetString("GOOGLE_CLIENT_ID"),
				ClientSecret: v.GetString("GOOGLE_CLIENT_SECRET"),
				RedirectURI:  v.GetString("GOOGLE_REDIRECT_URI"),
			},
			FCM: FCMConfig{
				Enabled:            v.GetBool("FCM_ENABLED"),
				ProjectID:          v.GetString("FCM_PROJECT_ID"),
				ServiceAccountFile: v.GetString("FCM_SERVICE_ACCOUNT_FILE"),
			},
			OneSignal: OneSignalConfig{
				AppID:  v.GetString("ONESIGNAL_APP_ID"),
				APIKey: v.GetString("ONESIGNAL_API_KEY"),
			},
			Storage: StorageConfig{
				Driver: v.GetString("STORAGE_DRIVER"),
				Path:   v.GetString("STORAGE_PATH"),
				URL:    v.GetString("STORAGE_URL"),
			},
			Security: SecurityConfig{
				AddHSTS:           v.GetBool("APP_ADD_HSTS"),
				SessionLifetime:   v.GetInt("SESSION_LIFETIME"),
				VisitorActiveMins: v.GetInt("VISITOR_ACTIVE_MINUTES"),
				VisitorPruneMins:  v.GetInt("VISITOR_PRUNE_MINUTES"),
			},
			Log: LogConfig{
				Level:      v.GetString("LOG_LEVEL"),
				Path:       v.GetString("LOG_PATH"),
				MaxSize:    v.GetInt("LOG_MAX_SIZE"),
				MaxBackups: v.GetInt("LOG_MAX_BACKUPS"),
				MaxAge:     v.GetInt("LOG_MAX_AGE"),
			},
			GeoIP: GeoIPConfig{
				DBPath: v.GetString("GEOIP_DB_PATH"),
			},
			AI: AIConfig{
				TogetherAPIKey: v.GetString("TOGETHER_AI_API_KEY"),
			},
		}
	})
	return cfg
}

func Get() *Config {
	if cfg == nil {
		return Load()
	}
	return cfg
}

func (d *DBConnection) DSN() string {
	return fmt.Sprintf(
		"%s:%s@tcp(%s:%d)/%s?charset=%s&parseTime=True&loc=Local&collation=utf8mb4_unicode_ci",
		d.User, d.Password, d.Host, d.Port, d.Name, d.Charset,
	)
}

func (r *RedisConfig) Addr() string {
	return fmt.Sprintf("%s:%d", r.Host, r.Port)
}

func (a *AppConfig) IsProduction() bool {
	return a.Env == "production"
}

func (a *AppConfig) IsDevelopment() bool {
	return a.Env == "development" || a.Env == "local"
}

// jwtSecretLowEntropy returns true when the secret has fewer than 8 unique
// characters — a strong signal it was not randomly generated (e.g. all 'a's).
func jwtSecretLowEntropy(secret string) bool {
	seen := make(map[rune]struct{}, len(secret))
	for _, c := range secret {
		seen[c] = struct{}{}
	}
	return len(seen) < 8
}
