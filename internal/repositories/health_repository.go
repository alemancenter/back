package repositories

import "github.com/alemancenter/fiber-api/internal/database"

type HealthRepository interface {
	CheckDatabases() map[string]bool
	CheckRedis() map[string]bool
}

type healthRepository struct{}

func NewHealthRepository() HealthRepository { return &healthRepository{} }

func (r *healthRepository) CheckDatabases() map[string]bool {
	return database.GetManager().HealthCheck()
}

func (r *healthRepository) CheckRedis() map[string]bool {
	return database.GetRedis().HealthCheck()
}
