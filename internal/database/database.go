package database

import (
	"fmt"
	"sync"
	"time"

	"github.com/alemancenter/fiber-api/internal/config"
	"github.com/alemancenter/fiber-api/pkg/logger"
	"go.uber.org/zap"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// CountryID represents a supported country database
type CountryID int

const (
	CountryJordan    CountryID = 1
	CountrySaudi     CountryID = 2
	CountryEgypt     CountryID = 3
	CountryPalestine CountryID = 4
)

var countryNames = map[CountryID]string{
	CountryJordan:    "jo",
	CountrySaudi:     "sa",
	CountryEgypt:     "eg",
	CountryPalestine: "ps",
}

// Manager handles multiple database connections
type Manager struct {
	connections map[CountryID]*gorm.DB
	mu          sync.RWMutex
	cfg         *config.Config
}

var (
	manager     *Manager
	managerOnce sync.Once
)

// GetManager returns the singleton database manager
func GetManager() *Manager {
	managerOnce.Do(func() {
		manager = &Manager{
			connections: make(map[CountryID]*gorm.DB),
			cfg:         config.Get(),
		}
		if err := manager.initAll(); err != nil {
			logger.Fatal("failed to initialize databases", zap.Error(err))
		}
	})
	return manager
}

// initAll initializes all database connections.
// Jordan (primary) is required — its failure is fatal.
// Secondary databases (SA/EG/PS) log a warning and fall back to Jordan on error.
func (m *Manager) initAll() error {
	type entry struct {
		id  CountryID
		cfg config.DBConnection
	}

	// Jordan must succeed
	jordanDB, err := m.connect(m.cfg.Database.Jordan)
	if err != nil {
		return fmt.Errorf("failed to connect to primary (Jordan) database: %w", err)
	}
	m.connections[CountryJordan] = jordanDB
	logger.Info("database connected", zap.String("country", "jo"))

	// Secondary databases — warn but fall back to Jordan on error
	secondaries := []entry{
		{CountrySaudi, m.cfg.Database.Saudi},
		{CountryEgypt, m.cfg.Database.Egypt},
		{CountryPalestine, m.cfg.Database.Palestine},
	}
	for _, s := range secondaries {
		db, err := m.connect(s.cfg)
		if err != nil {
			logger.Error("secondary database unavailable — falling back to Jordan",
				zap.String("country", countryNames[s.id]),
				zap.Error(err),
			)
			m.connections[s.id] = jordanDB // safe fallback
			continue
		}
		m.connections[s.id] = db
		logger.Info("database connected", zap.String("country", countryNames[s.id]))
	}
	return nil
}

// connect establishes a single database connection
func (m *Manager) connect(dbCfg config.DBConnection) (*gorm.DB, error) {
	logLevel := gormlogger.Silent
	if m.cfg.App.Debug {
		logLevel = gormlogger.Info
	}

	db, err := gorm.Open(mysql.Open(dbCfg.DSN()), &gorm.Config{
		Logger: gormlogger.Default.LogMode(logLevel),
		NowFunc: func() time.Time {
			return time.Now().UTC()
		},
		PrepareStmt:                              true,
		SkipDefaultTransaction:                   false,
		DisableForeignKeyConstraintWhenMigrating: true,
	})
	if err != nil {
		return nil, err
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}

	sqlDB.SetMaxIdleConns(dbCfg.MaxIdle)
	sqlDB.SetMaxOpenConns(dbCfg.MaxOpen)
	sqlDB.SetConnMaxLifetime(time.Duration(dbCfg.MaxLife) * time.Second)
	sqlDB.SetConnMaxIdleTime(10 * time.Minute)

	return db, nil
}

// Get returns the database connection for a given country
func (m *Manager) Get(countryID CountryID) *gorm.DB {
	m.mu.RLock()
	defer m.mu.RUnlock()

	db, ok := m.connections[countryID]
	if !ok {
		logger.Error("unknown country database", zap.Int("country_id", int(countryID)))
		return m.connections[CountryJordan]
	}
	return db
}

// GetByCode returns DB by country code string (jo, sa, eg, ps)
func (m *Manager) GetByCode(code string) *gorm.DB {
	for id, name := range countryNames {
		if name == code {
			return m.Get(id)
		}
	}
	return m.Get(CountryJordan)
}

// Jordan returns Jordan's database connection (default)
func (m *Manager) Jordan() *gorm.DB { return m.Get(CountryJordan) }

// Saudi returns Saudi Arabia's database connection
func (m *Manager) Saudi() *gorm.DB { return m.Get(CountrySaudi) }

// Egypt returns Egypt's database connection
func (m *Manager) Egypt() *gorm.DB { return m.Get(CountryEgypt) }

// Palestine returns Palestine's database connection
func (m *Manager) Palestine() *gorm.DB { return m.Get(CountryPalestine) }

// HealthCheck pings all databases
func (m *Manager) HealthCheck() map[string]bool {
	results := make(map[string]bool)
	for id, name := range countryNames {
		db := m.Get(id)
		sqlDB, err := db.DB()
		if err != nil {
			results[name] = false
			continue
		}
		results[name] = sqlDB.Ping() == nil
	}
	return results
}

// CountryIDFromHeader converts X-Country-Id header value to CountryID
func CountryIDFromHeader(value string) CountryID {
	switch value {
	case "1", "jo":
		return CountryJordan
	case "2", "sa":
		return CountrySaudi
	case "3", "eg":
		return CountryEgypt
	case "4", "ps":
		return CountryPalestine
	default:
		return CountryJordan
	}
}

// CountryCode returns the string code for a given CountryID
func CountryCode(id CountryID) string {
	if name, ok := countryNames[id]; ok {
		return name
	}
	return "jo"
}

// DB is a convenience function to get Jordan DB (default)
func DB() *gorm.DB {
	return GetManager().Jordan()
}

// DBForCountry returns DB for a specific country
func DBForCountry(id CountryID) *gorm.DB {
	return GetManager().Get(id)
}
