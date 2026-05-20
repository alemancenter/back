package redis

import (
	"context"
	"strings"
	"time"

	_ "github.com/alemancenter/fiber-api/internal/models"
	"github.com/alemancenter/fiber-api/internal/services"
	"github.com/alemancenter/fiber-api/internal/utils"
	"github.com/gofiber/fiber/v2"
)

// Handler contains Redis management route handlers
type Handler struct {
	svc services.RedisService
}

// New creates a new Redis Handler
func New(svc services.RedisService) *Handler {
	return &Handler{svc: svc}
}

// ListKeys lists Redis keys matching a pattern with real TTL/type/memory metadata.
func (h *Handler) ListKeys(c *fiber.Ctx) error {
	pattern := strings.TrimSpace(c.Query("pattern", "*"))
	if pattern == "" {
		pattern = "*"
	}
	ttlFilter := services.NormalizeRedisTTLFilter(c.Query("ttl_filter", "all"))
	page, perPage, offset := redisKeysPagination(c)

	keys, hasMore, err := h.svc.ListKeys(context.Background(), pattern, perPage, offset, ttlFilter)
	if err != nil {
		return utils.InternalError(c, "فشل الحصول على مفاتيح Redis")
	}

	total := offset + len(keys)
	if hasMore {
		total++
	}
	lastPage := total / perPage
	if total%perPage != 0 {
		lastPage++
	}
	if lastPage == 0 {
		lastPage = 1
	}
	from, to := 0, 0
	if len(keys) > 0 {
		from = offset + 1
		to = offset + len(keys)
	}

	plainKeys := make([]string, 0, len(keys))
	for _, item := range keys {
		plainKeys = append(plainKeys, item.Key)
	}

	return utils.Success(c, "success", services.RedisKeysResponse{
		Data:        keys,
		Keys:        plainKeys,
		Count:       len(keys),
		CurrentPage: page,
		PerPage:     perPage,
		Total:       total,
		LastPage:    lastPage,
		From:        from,
		To:          to,
		HasMore:     hasMore,
	})
}

func redisKeysPagination(c *fiber.Ctx) (page, perPage, offset int) {
	page = c.QueryInt("page", 1)
	perPage = c.QueryInt("per_page", 25)
	if c.Query("per_page") == "" && c.Query("limit") != "" {
		perPage = c.QueryInt("limit", 25)
	}

	if page < 1 {
		page = 1
	}
	if perPage < 1 {
		perPage = 25
	}
	if perPage > 100 {
		perPage = 100
	}

	return page, perPage, (page - 1) * perPage
}

type SetRedisKeyRequest struct {
	Key     string `json:"key" validate:"required"`
	Value   string `json:"value" validate:"required"`
	TTL     int    `json:"ttl"` // seconds
	Persist bool   `json:"persist"`
}

// SetKey sets a Redis key. TTL defaults to 1 hour to avoid accidental persistent cache keys.
func (h *Handler) SetKey(c *fiber.Ctx) error {
	var req SetRedisKeyRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.BadRequest(c, "بيانات غير صحيحة")
	}

	if errs := utils.Validate(req); errs != nil {
		return utils.ValidationError(c, errs)
	}

	ttl := time.Duration(req.TTL) * time.Second
	if req.TTL <= 0 && !req.Persist {
		ttl = time.Hour
	}

	if err := h.svc.SetKey(context.Background(), req.Key, req.Value, ttl, req.Persist); err != nil {
		return utils.InternalError(c, "فشل تعيين المفتاح")
	}

	return utils.Success(c, "تم تعيين المفتاح بنجاح", fiber.Map{
		"ttl_seconds": int(ttl.Seconds()),
		"persist":     req.Persist,
	})
}

// DeleteKey deletes a Redis key
func (h *Handler) DeleteKey(c *fiber.Ctx) error {
	key := c.Params("key")
	if err := h.svc.DeleteKey(context.Background(), key); err != nil {
		return utils.InternalError(c, "فشل حذف المفتاح")
	}
	return utils.Success(c, "تم حذف المفتاح بنجاح", nil)
}

type ExpireRedisKeyRequest struct {
	TTL int `json:"ttl"`
}

// ExpireKey sets TTL on an existing Redis key. Defaults to 7 days.
func (h *Handler) ExpireKey(c *fiber.Ctx) error {
	key := c.Params("key")
	var req ExpireRedisKeyRequest
	_ = c.BodyParser(&req)

	ttl := time.Duration(req.TTL) * time.Second
	if req.TTL <= 0 {
		ttl = 7 * 24 * time.Hour
	}

	if err := h.svc.ExpireKey(context.Background(), key, ttl); err != nil {
		return utils.InternalError(c, "فشل تعيين مدة انتهاء للمفتاح")
	}

	return utils.Success(c, "تم تعيين مدة انتهاء للمفتاح", fiber.Map{
		"key":         key,
		"ttl_seconds": int(ttl.Seconds()),
	})
}

// CleanExpired removes expired keys
func (h *Handler) CleanExpired(c *fiber.Ctx) error {
	if err := h.svc.CleanExpired(context.Background()); err != nil {
		return utils.InternalError(c)
	}
	return utils.Success(c, "Redis يحذف المفاتيح المنتهية تلقائياً", nil)
}

// CleanLegacyIPLocation deletes old Laravel IP location cache keys matching *_cache_ip_location_*.
func (h *Handler) CleanLegacyIPLocation(c *fiber.Ctx) error {
	deleted, err := h.svc.CleanLegacyIPLocationKeys(context.Background())
	if err != nil {
		return utils.InternalError(c, "فشل تنظيف مفاتيح IP Location القديمة")
	}
	return utils.Success(c, "تم تنظيف مفاتيح IP Location القديمة", fiber.Map{"deleted": deleted})
}

// ExpireLegacyIPLocation assigns TTL to old Laravel IP location cache keys matching *_cache_ip_location_*.
func (h *Handler) ExpireLegacyIPLocation(c *fiber.Ctx) error {
	var req ExpireRedisKeyRequest
	_ = c.BodyParser(&req)
	ttl := time.Duration(req.TTL) * time.Second
	if req.TTL <= 0 {
		ttl = 7 * 24 * time.Hour
	}

	affected, err := h.svc.ExpireLegacyIPLocationKeys(context.Background(), ttl)
	if err != nil {
		return utils.InternalError(c, "فشل تعيين مدة انتهاء لمفاتيح IP Location القديمة")
	}
	return utils.Success(c, "تم تعيين مدة انتهاء لمفاتيح IP Location القديمة", fiber.Map{"affected": affected, "ttl_seconds": int(ttl.Seconds())})
}

// TestConnection tests the Redis connection
func (h *Handler) TestConnection(c *fiber.Ctx) error {
	health, allOk := h.svc.TestConnection()
	if !allOk {
		return utils.InternalError(c, "فشل الاتصال بـ Redis")
	}
	return utils.Success(c, "الاتصال بـ Redis يعمل بشكل صحيح", health)
}

// GetInfo returns Redis server information
func (h *Handler) GetInfo(c *fiber.Ctx) error {
	info, err := h.svc.GetInfo(context.Background())
	if err != nil {
		return utils.InternalError(c, "فشل الحصول على معلومات Redis")
	}
	return utils.Success(c, "success", services.RedisInfoResponse{Info: info})
}

// UpdateEnv validates Redis settings posted from the dashboard.
func (h *Handler) UpdateEnv(c *fiber.Ctx) error {
	var req map[string]string
	if err := c.BodyParser(&req); err != nil {
		return utils.BadRequest(c, "invalid redis settings")
	}

	allowed := map[string]bool{
		"REDIS_HOST":     true,
		"REDIS_PORT":     true,
		"REDIS_PASSWORD": true,
		"REDIS_DB":       true,
	}

	sanitized := make(map[string]string)
	for key, value := range req {
		if !allowed[key] {
			continue
		}
		sanitized[key] = strings.TrimSpace(value)
	}

	if len(sanitized) == 0 {
		return utils.BadRequest(c, "no valid redis settings provided")
	}

	return utils.Success(c, "redis settings accepted; restart the service to apply runtime changes", sanitized)
}
