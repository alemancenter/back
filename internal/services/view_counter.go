package services

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/alemancenter/fiber-api/internal/database"
	"github.com/alemancenter/fiber-api/internal/models"
	"github.com/alemancenter/fiber-api/pkg/logger"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

type ViewCounterService interface {
	IncrementArticleView(countryID database.CountryID, id uint64) error
	IncrementFileView(countryID database.CountryID, id uint64) error
	IncrementPostView(countryID database.CountryID, id uint64) error
}

type viewCounterService struct{}

var ViewCounter = &viewCounterService{}

func (s *viewCounterService) increment(countryID database.CountryID, entityType string, id uint64) error {
	ctx := context.Background()
	key := viewSyncKey(countryID, entityType)
	field := strconv.FormatUint(id, 10)

	_, err := database.Redis().Default().HIncrBy(ctx, key, field, 1).Result()
	if err != nil {
		logger.Error("Failed to increment view in Redis", zap.Error(err), zap.String("key", key), zap.String("field", field))
		return err
	}
	return nil
}

func (s *viewCounterService) IncrementArticleView(countryID database.CountryID, id uint64) error {
	return s.increment(countryID, "articles", id)
}

func (s *viewCounterService) IncrementFileView(countryID database.CountryID, id uint64) error {
	// File counters are needed immediately in dashboard and older installations use views_count.
	// Write directly to DB instead of relying only on the Redis sync worker.
	return incrementFileCounterColumns(countryID, id, []string{"view_count", "views_count"}, 1)
}

func IncrementFileDownload(countryID database.CountryID, id uint64) error {
	return incrementFileCounterColumns(countryID, id, []string{"download_count"}, 1)
}

func incrementFileCounterColumns(countryID database.CountryID, id uint64, columns []string, amount int64) error {
	db := database.DBForCountry(countryID)
	if db == nil {
		return nil
	}
	updated := false
	for _, col := range columns {
		if !db.Migrator().HasColumn(&models.File{}, col) {
			continue
		}
		if err := db.Exec("UPDATE files SET "+col+" = LEAST(COALESCE("+col+", 0) + ?, 2147483647) WHERE id = ?", amount, id).Error; err != nil {
			return err
		}
		updated = true
	}
	if !updated {
		logger.Warn("No file counter columns available", zap.Uint64("id", id))
	}
	return nil
}

func (s *viewCounterService) IncrementPostView(countryID database.CountryID, id uint64) error {
	return s.increment(countryID, "posts", id)
}

func viewSyncKey(countryID database.CountryID, entityType string) string {
	countryCode := database.CountryCode(countryID)
	return fmt.Sprintf("views:sync:%s:%s", countryCode, entityType)
}

func (s *viewCounterService) PendingViews(countryID database.CountryID, entityType string, ids []uint64) map[uint64]int64 {
	result := make(map[uint64]int64, len(ids))
	if len(ids) == 0 {
		return result
	}

	ctx, cancel := context.WithTimeout(context.Background(), 750*time.Millisecond)
	defer cancel()

	redisClient := database.Redis().Default()
	key := viewSyncKey(countryID, entityType)
	fields := make([]string, 0, len(ids))
	for _, id := range ids {
		fields = append(fields, strconv.FormatUint(id, 10))
	}

	values, err := redisClient.HMGet(ctx, key, fields...).Result()
	if err != nil {
		logger.Warn("Failed to read pending view counts from Redis", zap.Error(err), zap.String("key", key))
		return result
	}

	for i, raw := range values {
		if raw == nil {
			continue
		}
		count, err := strconv.ParseInt(fmt.Sprint(raw), 10, 64)
		if err != nil || count <= 0 {
			continue
		}
		result[ids[i]] = count
	}
	return result
}

func (s *viewCounterService) PendingTotalViews(countryID database.CountryID, entityType string) int64 {
	ctx, cancel := context.WithTimeout(context.Background(), 750*time.Millisecond)
	defer cancel()

	redisClient := database.Redis().Default()
	key := viewSyncKey(countryID, entityType)
	views, err := redisClient.HGetAll(ctx, key).Result()
	if err != nil {
		logger.Warn("Failed to read pending total view counts from Redis", zap.Error(err), zap.String("key", key))
		return 0
	}

	var total int64
	for _, countStr := range views {
		count, err := strconv.ParseInt(countStr, 10, 64)
		if err == nil && count > 0 {
			total += count
		}
	}
	return total
}

// StartViewSyncWorker starts a background worker that syncs Redis views to MySQL.
func StartViewSyncWorker(interval time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		for range ticker.C {
			syncViewsToDB()
		}
	}()
}

func syncViewsToDB() {
	ctx := context.Background()
	redisClient := database.Redis().Default()

	// Find all view sync keys
	keys, err := redisClient.Keys(ctx, "views:sync:*").Result()
	if err != nil {
		logger.Error("Failed to fetch view sync keys", zap.Error(err))
		return
	}

	for _, key := range keys {
		// key format: views:sync:{countryCode}:{entityType}
		parts := strings.Split(key, ":")
		if len(parts) != 4 {
			continue
		}
		countryCode := parts[2]
		entityType := parts[3]

		// To prevent race conditions, rename the key to a processing key
		processingKey := fmt.Sprintf("processing:%s:%s", key, uuid.New().String())
		success, err := redisClient.RenameNX(ctx, key, processingKey).Result()
		if err != nil || !success {
			// Either key didn't exist or rename failed, skip
			continue
		}

		// Fetch all fields (IDs) and values (counts)
		views, err := redisClient.HGetAll(ctx, processingKey).Result()
		if err != nil {
			logger.Error("Failed to get hash from processing key", zap.Error(err), zap.String("key", processingKey))
			// Attempt to restore the key or let it expire (we should probably just process it next time, but for now we'll just log)
			continue
		}

		// Sync to database
		countryID := database.CountryIDFromHeader(countryCode)
		if countryID == 0 {
			logger.Error("Invalid country code in view sync", zap.String("code", countryCode))
			redisClient.Del(ctx, processingKey)
			continue
		}

		db := database.DBForCountry(countryID)

		// Group updates or do them individually
		for idStr, countStr := range views {
			id, err := strconv.ParseUint(idStr, 10, 64)
			if err != nil {
				continue
			}
			count, err := strconv.ParseInt(countStr, 10, 64)
			if err != nil {
				continue
			}

			var updateErr error
			// LEAST() caps the column at INT max (2^31-1) to prevent signed integer overflow.
			switch entityType {
			case "articles":
				updateErr = db.Exec("UPDATE articles SET visit_count = LEAST(visit_count + ?, 2147483647) WHERE id = ?", count, id).Error
			case "files":
				updateErr = incrementFileCounterColumns(countryID, id, []string{"view_count", "views_count"}, count)
			case "posts":
				updateErr = db.Exec("UPDATE posts SET views = LEAST(views + ?, 2147483647) WHERE id = ?", count, id).Error
			}

			if updateErr != nil {
				logger.Error("Failed to update view count in DB", zap.Error(updateErr), zap.String("entity", entityType), zap.Uint64("id", id))
			}
		}

		// Delete processing key after successful sync
		redisClient.Del(ctx, processingKey)
	}
}
