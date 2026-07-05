package repositories

import (
	"strings"
	"time"

	"github.com/alemancenter/fiber-api/internal/database"
	"github.com/alemancenter/fiber-api/internal/models"
	"gorm.io/gorm"
)

type TeacherSubscriptionRepository interface {
	DB() *gorm.DB
	FirstActivePlan() (*models.SubscriptionPlan, error)
	UpsertDefaultPlan() (*models.SubscriptionPlan, error)
	GetCurrentSubscription(userID uint) (*models.TeacherSubscription, error)
	CreateOrUpdateProfile(profile *models.TeacherProfile) error
	GetProfile(userID uint) (*models.TeacherProfile, error)
	CreateOrder(order *models.SubscriptionOrder) error
	GetOrder(id uint) (*models.SubscriptionOrder, error)
	ListOrders(status string, limit, offset int) ([]models.SubscriptionOrder, int64, error)
	AdminStats() (map[string]int64, error)
	AdminListSubscriptions(status, search string, limit, offset int) ([]models.TeacherSubscription, int64, error)
	AdminListTeachers(search string, limit, offset int) ([]models.TeacherProfile, int64, error)
	AdminListDevices(userID uint, active string, limit, offset int) ([]models.TeacherDevice, int64, error)
	AdminListPremiumDownloads(userID uint, limit, offset int) ([]models.TeacherPremiumDownload, int64, error)
	AdminListAIGenerations(userID uint, limit, offset int) ([]models.TeacherAIGeneration, int64, error)

	AdminListFilesForPremium(countryID database.CountryID, search, premium, category, subject string, limit, offset int) ([]models.File, int64, error)
	AdminUpdateFilePremium(countryID database.CountryID, fileID uint, values map[string]interface{}) (*models.File, error)

	// ListTeacherPremiumFiles filters by ANY of the given subjects (teacher may
	// now subscribe to up to 3). An empty slice means "no subject filter".
	ListTeacherPremiumFiles(countryID database.CountryID, subjects []string, category, query string, limit, offset int) ([]models.TeacherPremiumFile, int64, error)
	GetTeacherPremiumFile(countryID database.CountryID, fileID uint) (*models.TeacherPremiumFile, error)
	GetTeacherPremiumFileAdmin(countryID database.CountryID, fileID uint) (*models.TeacherPremiumFile, error)
	AdminListTeacherPremiumFiles(countryID database.CountryID, search, active, category, subject string, limit, offset int) ([]models.TeacherPremiumFile, int64, error)
	AdminCreateTeacherPremiumFile(countryID database.CountryID, file *models.TeacherPremiumFile) error
	AdminUpdateTeacherPremiumFile(countryID database.CountryID, fileID uint, values map[string]interface{}) (*models.TeacherPremiumFile, error)
	ArchiveTeacherPremiumFile(countryID database.CountryID, fileID uint, reason string) (*models.TeacherPremiumFile, error)
	IncrementTeacherPremiumFileDownload(countryID database.CountryID, fileID uint) error

	CreateTeacherPremiumDownload(download *models.TeacherPremiumDownload) error
	ListPremiumDownloads(userID uint, limit, offset int) ([]models.TeacherPremiumDownload, int64, error)

	GetSubscriptionByID(id uint) (*models.TeacherSubscription, error)
	UpdateSubscription(subscription *models.TeacherSubscription) error
	CreateSubscription(subscription *models.TeacherSubscription) error
	CancelActiveSubscriptionsForUser(userID uint, adminNote string) error
	RenewSubscription(subscriptionID uint, newEndsAt time.Time, adminID uint, note string) (*models.TeacherSubscription, error)
	ExpireOverdueSubscriptions(now time.Time) (int64, error)

	DeleteTeacherProfile(userID uint) error
	DeactivateAllDevices(userID uint) error
	ListUserOrders(userID uint, limit int) ([]models.SubscriptionOrder, error)
	UpdateOrder(order *models.SubscriptionOrder) error
	CountActiveDevices(userID uint) (int64, error)
	UpsertDevice(device *models.TeacherDevice) error
	ListDevices(userID uint) ([]models.TeacherDevice, error)
	DeactivateDevice(userID, deviceID uint) error
	CountDownloads(subscriptionID uint) (int64, error)
	CountAIGenerations(subscriptionID uint) (int64, error)

	ListPremiumFiles(countryID database.CountryID, subject, category, query string, limit, offset int) ([]models.File, int64, error)
	GetPremiumFile(countryID database.CountryID, fileID uint) (*models.File, error)
	CreateLibraryItem(item *models.TeacherLibraryItem) error
	ListLibraryItems(userID uint, limit, offset int) ([]models.TeacherLibraryItem, int64, error)
	FindLibraryItem(userID uint, itemType string, itemID *uint) (*models.TeacherLibraryItem, error)
	ListAIGenerations(userID uint, limit, offset int) ([]models.TeacherAIGeneration, int64, error)

	CreateAuditLog(log *models.TeacherAuditLog) error
	ListAuditLogs(entityType string, entityID uint, limit, offset int) ([]models.TeacherAuditLog, int64, error)
	CreateExpiryNotificationIfMissing(item *models.TeacherExpiryNotification) error
	CreateTeacherAIGeneration(item *models.TeacherAIGeneration) error
	GetTeacherAIGeneration(userID uint, id uint) (*models.TeacherAIGeneration, error)
	IncrementAIGenerationExport(id uint) error
	CreateTeacherNotification(item *models.TeacherNotification) error
	ListTeacherNotifications(userID uint, limit, offset int) ([]models.TeacherNotification, int64, error)
	ListPaymentSettings() ([]models.TeacherPaymentSetting, error)
	UpsertPaymentSetting(item *models.TeacherPaymentSetting) error
	UpdateTeacherPremiumDownloadWatermark(downloadID uint, applied bool, text string, path string) error
}
type teacherSubscriptionRepository struct{}

func NewTeacherSubscriptionRepository() TeacherSubscriptionRepository {
	return &teacherSubscriptionRepository{}
}

func (r *teacherSubscriptionRepository) DB() *gorm.DB {
	return database.DB()
}

func (r *teacherSubscriptionRepository) FirstActivePlan() (*models.SubscriptionPlan, error) {
	var plan models.SubscriptionPlan
	err := r.DB().Where("code = ? AND is_active = ?", "teacher_semester", true).First(&plan).Error
	return &plan, err
}

func (r *teacherSubscriptionRepository) UpsertDefaultPlan() (*models.SubscriptionPlan, error) {
	plan := models.SubscriptionPlan{
		Code:              "teacher_semester",
		Name:              "اشتراك المعلم للفصل الدراسي",
		Description:       "اشتراك مخصص لمعلمي الأردن يتضمن نماذج امتحانات، خطط، تحليل محتوى، ملفات قابلة للتعديل، وأدوات تعليمية للمعلم طوال الفصل الدراسي.",
		TargetAudience:    "teacher",
		PriceJOD:          25,
		Currency:          "JOD",
		DurationDays:      150,
		DeviceLimit:       2,
		DownloadLimit:     300,
		AIGenerationLimit: 100,
		ExportLimit:       100,
		FeaturesJSON:      `["نماذج امتحانات حديثة ومتنوعة","خطط فصلية وتحليل محتوى","أوراق عمل وخطط علاجية","ملفات Word/PDF قابلة للطباعة","أدوات AI للمعلم","مكتبة وسجل استخدام للمعلم","جهازان موثقان","Watermark لحماية الملفات المدفوعة"]`,
		PermissionsJSON:   `["teacher.subscription.access","teacher.files.premium.download","teacher.files.word_pdf.export","teacher.ai.exam.generate","teacher.ai.answer_key.generate","teacher.ai.worksheet.generate","teacher.ai.remedial_plan.generate","teacher.library.access","teacher.devices.manage","teacher.usage.view"]`,
		LimitsJSON:        `{"devices":2,"premium_downloads":300,"ai_generations":100,"exports":100,"duration_days":150}`,
		SortOrder:         10,
		IsActive:          true,
	}

	var existing models.SubscriptionPlan
	err := r.DB().Where("code = ?", plan.Code).First(&existing).Error
	if err == nil {
		existing.Name = plan.Name
		existing.Description = plan.Description
		existing.TargetAudience = plan.TargetAudience
		existing.PriceJOD = plan.PriceJOD
		existing.Currency = plan.Currency
		existing.DurationDays = plan.DurationDays
		existing.DeviceLimit = plan.DeviceLimit
		existing.DownloadLimit = plan.DownloadLimit
		existing.AIGenerationLimit = plan.AIGenerationLimit
		existing.ExportLimit = plan.ExportLimit
		existing.FeaturesJSON = plan.FeaturesJSON
		existing.PermissionsJSON = plan.PermissionsJSON
		existing.LimitsJSON = plan.LimitsJSON
		existing.SortOrder = plan.SortOrder
		existing.IsActive = true
		if err := r.DB().Save(&existing).Error; err != nil {
			return nil, err
		}
		return &existing, nil
	}
	if err != nil && err != gorm.ErrRecordNotFound {
		return nil, err
	}
	if err := r.DB().Create(&plan).Error; err != nil {
		return nil, err
	}
	return &plan, nil
}

func (r *teacherSubscriptionRepository) GetCurrentSubscription(userID uint) (*models.TeacherSubscription, error) {
	var sub models.TeacherSubscription
	now := time.Now()
	err := r.DB().Preload("Plan").
		Where("user_id = ? AND status = ? AND starts_at <= ? AND ends_at >= ?", userID, "active", now, now).
		Order("ends_at DESC").First(&sub).Error
	return &sub, err
}

func (r *teacherSubscriptionRepository) CreateOrUpdateProfile(profile *models.TeacherProfile) error {
	var existing models.TeacherProfile
	err := r.DB().Where("user_id = ?", profile.UserID).First(&existing).Error
	if err == nil {
		existing.Subject = profile.Subject
		existing.Subjects = profile.Subjects
		existing.School = profile.School
		existing.Phone = profile.Phone
		existing.City = profile.City
		return r.DB().Save(&existing).Error
	}
	if err != nil && err != gorm.ErrRecordNotFound {
		return err
	}
	return r.DB().Create(profile).Error
}

func (r *teacherSubscriptionRepository) GetProfile(userID uint) (*models.TeacherProfile, error) {
	var profile models.TeacherProfile
	err := r.DB().Where("user_id = ?", userID).First(&profile).Error
	return &profile, err
}

func (r *teacherSubscriptionRepository) CreateOrder(order *models.SubscriptionOrder) error {
	return r.DB().Create(order).Error
}

func (r *teacherSubscriptionRepository) GetOrder(id uint) (*models.SubscriptionOrder, error) {
	var order models.SubscriptionOrder
	err := r.DB().Preload("User").Preload("Plan").First(&order, id).Error
	return &order, err
}

func (r *teacherSubscriptionRepository) ListOrders(status string, limit, offset int) ([]models.SubscriptionOrder, int64, error) {
	var orders []models.SubscriptionOrder
	var total int64
	q := r.DB().Model(&models.SubscriptionOrder{}).Preload("User").Preload("Plan")
	if status != "" {
		q = q.Where("status = ?", status)
	}
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := q.Order("created_at DESC").Limit(limit).Offset(offset).Find(&orders).Error
	return orders, total, err
}

func (r *teacherSubscriptionRepository) ListUserOrders(userID uint, limit int) ([]models.SubscriptionOrder, error) {
	var orders []models.SubscriptionOrder
	if limit <= 0 || limit > 20 {
		limit = 10
	}
	err := r.DB().Preload("Plan").Where("user_id = ?", userID).Order("created_at DESC").Limit(limit).Find(&orders).Error
	return orders, err
}

func (r *teacherSubscriptionRepository) UpdateOrder(order *models.SubscriptionOrder) error {
	return r.DB().Save(order).Error
}

func (r *teacherSubscriptionRepository) CreateSubscription(subscription *models.TeacherSubscription) error {
	return r.DB().Create(subscription).Error
}

func (r *teacherSubscriptionRepository) CountActiveDevices(userID uint) (int64, error) {
	var count int64
	err := r.DB().Model(&models.TeacherDevice{}).Where("user_id = ? AND is_active = ?", userID, true).Count(&count).Error
	return count, err
}

func (r *teacherSubscriptionRepository) UpsertDevice(device *models.TeacherDevice) error {
	var existing models.TeacherDevice
	err := r.DB().Where("user_id = ? AND device_hash = ?", device.UserID, device.DeviceHash).First(&existing).Error
	now := time.Now()
	if err == nil {
		existing.IPHash = device.IPHash
		existing.UserAgent = device.UserAgent
		existing.IsActive = true
		existing.LastSeenAt = &now
		if existing.Label == "" {
			existing.Label = device.Label
		}
		return r.DB().Save(&existing).Error
	}
	if err != nil && err != gorm.ErrRecordNotFound {
		return err
	}
	device.LastSeenAt = &now
	return r.DB().Create(device).Error
}

func (r *teacherSubscriptionRepository) ListDevices(userID uint) ([]models.TeacherDevice, error) {
	var devices []models.TeacherDevice
	err := r.DB().Where("user_id = ?", userID).Order("is_active DESC, last_seen_at DESC, created_at DESC").Find(&devices).Error
	return devices, err
}

func (r *teacherSubscriptionRepository) DeactivateDevice(userID, deviceID uint) error {
	return r.DB().Model(&models.TeacherDevice{}).Where("id = ? AND user_id = ?", deviceID, userID).Update("is_active", false).Error
}

func (r *teacherSubscriptionRepository) CountDownloads(subscriptionID uint) (int64, error) {
	var count int64
	err := r.DB().Model(&models.TeacherPremiumDownload{}).Where("subscription_id = ?", subscriptionID).Count(&count).Error
	return count, err
}

func (r *teacherSubscriptionRepository) CountAIGenerations(subscriptionID uint) (int64, error) {
	var count int64
	err := r.DB().Model(&models.TeacherAIGeneration{}).Where("subscription_id = ?", subscriptionID).Count(&count).Error
	return count, err
}

func (r *teacherSubscriptionRepository) ListPremiumFiles(countryID database.CountryID, subject, category, query string, limit, offset int) ([]models.File, int64, error) {
	if limit <= 0 || limit > 100 {
		limit = 24
	}
	db := database.DBForCountry(countryID)
	if err := ensureTeacherPremiumFileColumnsForRepository(db); err != nil {
		return nil, 0, err
	}
	q := db.Model(&models.File{}).
		Preload("Article").
		Preload("Article.Subject").
		Preload("Article.Semester").
		Preload("Post").
		Where("is_premium = ? AND premium_audience = ?", true, "teacher")

	if category != "" {
		q = q.Where("premium_category = ?", category)
	}

	if subject != "" {
		like := "%" + subject + "%"
		q = q.Where("(premium_subject = '' OR premium_subject LIKE ? OR EXISTS (SELECT 1 FROM articles a LEFT JOIN subjects s ON s.id = a.subject_id WHERE a.id = files.article_id AND s.subject_name LIKE ?))", like, like)
	}

	if query != "" {
		like := "%" + query + "%"
		q = q.Where("(file_name LIKE ? OR file_category LIKE ? OR premium_category LIKE ? OR premium_subject LIKE ?)", like, like, like, like)
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var files []models.File
	err := q.Order("created_at DESC").Limit(limit).Offset(offset).Find(&files).Error
	return files, total, err
}

func (r *teacherSubscriptionRepository) GetPremiumFile(countryID database.CountryID, fileID uint) (*models.File, error) {
	db := database.DBForCountry(countryID)
	if err := ensureTeacherPremiumFileColumnsForRepository(db); err != nil {
		return nil, err
	}
	var file models.File
	err := db.Preload("Article").
		Preload("Article.Subject").
		Preload("Article.Semester").
		Preload("Post").
		Where("id = ? AND is_premium = ? AND premium_audience = ?", fileID, true, "teacher").
		First(&file).Error
	return &file, err
}

func (r *teacherSubscriptionRepository) CreateLibraryItem(item *models.TeacherLibraryItem) error {
	return r.DB().Create(item).Error
}

func (r *teacherSubscriptionRepository) FindLibraryItem(userID uint, itemType string, itemID *uint) (*models.TeacherLibraryItem, error) {
	var item models.TeacherLibraryItem
	q := r.DB().Where("user_id = ? AND item_type = ?", userID, itemType)
	if itemID == nil {
		q = q.Where("item_id IS NULL")
	} else {
		q = q.Where("item_id = ?", *itemID)
	}
	err := q.First(&item).Error
	return &item, err
}

func (r *teacherSubscriptionRepository) ListLibraryItems(userID uint, limit, offset int) ([]models.TeacherLibraryItem, int64, error) {
	if limit <= 0 || limit > 100 {
		limit = 24
	}
	var items []models.TeacherLibraryItem
	var total int64
	q := r.DB().Model(&models.TeacherLibraryItem{}).Where("user_id = ?", userID)
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := q.Order("created_at DESC, id DESC").Limit(limit).Offset(offset).Find(&items).Error
	return items, total, err
}

func (r *teacherSubscriptionRepository) ListPremiumDownloads(userID uint, limit, offset int) ([]models.TeacherPremiumDownload, int64, error) {
	if limit <= 0 || limit > 100 {
		limit = 24
	}
	var items []models.TeacherPremiumDownload
	var total int64
	q := r.DB().Model(&models.TeacherPremiumDownload{}).Preload("User").Where("user_id = ?", userID)
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := q.Order("created_at DESC, id DESC").Limit(limit).Offset(offset).Find(&items).Error
	return items, total, err
}

func (r *teacherSubscriptionRepository) ListAIGenerations(userID uint, limit, offset int) ([]models.TeacherAIGeneration, int64, error) {
	if limit <= 0 || limit > 100 {
		limit = 24
	}
	var items []models.TeacherAIGeneration
	var total int64
	q := r.DB().Model(&models.TeacherAIGeneration{}).Preload("User").Where("user_id = ?", userID)
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := q.Order("created_at DESC").Limit(limit).Offset(offset).Find(&items).Error
	return items, total, err
}

func (r *teacherSubscriptionRepository) AdminStats() (map[string]int64, error) {
	db := r.DB()
	now := time.Now()
	stats := map[string]int64{
		"orders_pending":        0,
		"orders_approved":       0,
		"orders_rejected":       0,
		"subscriptions_active":  0,
		"subscriptions_expired": 0,
		"teachers_total":        0,
		"devices_active":        0,
		"premium_downloads":     0,
		"ai_generations":        0,
	}

	type pair struct {
		Status string
		Count  int64
	}
	var orderRows []pair
	if err := db.Model(&models.SubscriptionOrder{}).Select("status, COUNT(*) as count").Group("status").Scan(&orderRows).Error; err != nil {
		return stats, err
	}
	for _, row := range orderRows {
		switch row.Status {
		case "pending":
			stats["orders_pending"] = row.Count
		case "approved":
			stats["orders_approved"] = row.Count
		case "rejected":
			stats["orders_rejected"] = row.Count
		}
	}

	var activeSubscriptions int64
	if err := db.Model(&models.TeacherSubscription{}).Where("status = ? AND starts_at <= ? AND ends_at >= ?", "active", now, now).Count(&activeSubscriptions).Error; err != nil {
		return stats, err
	}
	var expiredSubscriptions int64
	if err := db.Model(&models.TeacherSubscription{}).Where("ends_at < ? OR status <> ?", now, "active").Count(&expiredSubscriptions).Error; err != nil {
		return stats, err
	}
	var teachersTotal int64
	if err := db.Model(&models.TeacherProfile{}).Count(&teachersTotal).Error; err != nil {
		return stats, err
	}
	var activeDevices int64
	if err := db.Model(&models.TeacherDevice{}).Where("is_active = ?", true).Count(&activeDevices).Error; err != nil {
		return stats, err
	}
	var premiumDownloads int64
	if err := db.Model(&models.TeacherPremiumDownload{}).Count(&premiumDownloads).Error; err != nil {
		return stats, err
	}
	var aiGenerations int64
	if err := db.Model(&models.TeacherAIGeneration{}).Count(&aiGenerations).Error; err != nil {
		return stats, err
	}

	stats["subscriptions_active"] = activeSubscriptions
	stats["subscriptions_expired"] = expiredSubscriptions
	stats["teachers_total"] = teachersTotal
	stats["devices_active"] = activeDevices
	stats["premium_downloads"] = premiumDownloads
	stats["ai_generations"] = aiGenerations

	return stats, nil
}

func (r *teacherSubscriptionRepository) AdminListSubscriptions(status, search string, limit, offset int) ([]models.TeacherSubscription, int64, error) {
	if limit <= 0 || limit > 100 {
		limit = 24
	}
	q := r.DB().Model(&models.TeacherSubscription{}).Preload("User").Preload("Plan")
	now := time.Now()
	switch status {
	case "active":
		q = q.Where("status = ? AND starts_at <= ? AND ends_at >= ?", "active", now, now)
	case "expired":
		q = q.Where("ends_at < ? OR status <> ?", now, "active")
	case "cancelled":
		q = q.Where("status = ?", "cancelled")
	}
	if search != "" {
		like := "%" + search + "%"
		q = q.Joins("LEFT JOIN users ON users.id = teacher_subscriptions.user_id").
			Where("(users.name LIKE ? OR users.email LIKE ?)", like, like)
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var items []models.TeacherSubscription
	err := q.Order("teacher_subscriptions.created_at DESC").Limit(limit).Offset(offset).Find(&items).Error
	return items, total, err
}

func (r *teacherSubscriptionRepository) AdminListTeachers(search string, limit, offset int) ([]models.TeacherProfile, int64, error) {
	if limit <= 0 || limit > 100 {
		limit = 24
	}
	q := r.DB().Model(&models.TeacherProfile{}).Preload("User")
	if search != "" {
		like := "%" + search + "%"
		q = q.Joins("LEFT JOIN users ON users.id = teacher_profiles.user_id").
			Where("(users.name LIKE ? OR users.email LIKE ? OR teacher_profiles.subject LIKE ? OR teacher_profiles.school LIKE ? OR teacher_profiles.phone LIKE ?)", like, like, like, like, like)
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var items []models.TeacherProfile
	err := q.Order("teacher_profiles.created_at DESC").Limit(limit).Offset(offset).Find(&items).Error
	return items, total, err
}

func (r *teacherSubscriptionRepository) AdminListDevices(userID uint, active string, limit, offset int) ([]models.TeacherDevice, int64, error) {
	if limit <= 0 || limit > 100 {
		limit = 24
	}
	q := r.DB().Model(&models.TeacherDevice{}).Preload("User")
	if userID > 0 {
		q = q.Where("user_id = ?", userID)
	}
	switch active {
	case "true", "1", "active":
		q = q.Where("is_active = ?", true)
	case "false", "0", "inactive":
		q = q.Where("is_active = ?", false)
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var items []models.TeacherDevice
	err := q.Order("last_seen_at DESC, created_at DESC").Limit(limit).Offset(offset).Find(&items).Error
	return items, total, err
}

func (r *teacherSubscriptionRepository) AdminListPremiumDownloads(userID uint, limit, offset int) ([]models.TeacherPremiumDownload, int64, error) {
	if limit <= 0 || limit > 100 {
		limit = 24
	}
	q := r.DB().Model(&models.TeacherPremiumDownload{})
	if userID > 0 {
		q = q.Where("user_id = ?", userID)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var items []models.TeacherPremiumDownload
	err := q.Order("created_at DESC").Limit(limit).Offset(offset).Find(&items).Error
	return items, total, err
}

func (r *teacherSubscriptionRepository) AdminListAIGenerations(userID uint, limit, offset int) ([]models.TeacherAIGeneration, int64, error) {
	if limit <= 0 || limit > 100 {
		limit = 24
	}
	q := r.DB().Model(&models.TeacherAIGeneration{})
	if userID > 0 {
		q = q.Where("user_id = ?", userID)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var items []models.TeacherAIGeneration
	err := q.Order("created_at DESC").Limit(limit).Offset(offset).Find(&items).Error
	return items, total, err
}

func (r *teacherSubscriptionRepository) GetSubscriptionByID(id uint) (*models.TeacherSubscription, error) {
	var subscription models.TeacherSubscription
	err := r.DB().Preload("User").Preload("Plan").First(&subscription, id).Error
	return &subscription, err
}

func (r *teacherSubscriptionRepository) UpdateSubscription(subscription *models.TeacherSubscription) error {
	return r.DB().Save(subscription).Error
}

func (r *teacherSubscriptionRepository) CancelActiveSubscriptionsForUser(userID uint, adminNote string) error {
	now := time.Now()
	return r.DB().Model(&models.TeacherSubscription{}).
		Where("user_id = ? AND status = ?", userID, "active").
		Updates(map[string]interface{}{
			"status":       "cancelled",
			"cancelled_at": &now,
			"admin_note":   adminNote,
			"updated_at":   now,
		}).Error
}

func (r *teacherSubscriptionRepository) DeleteTeacherProfile(userID uint) error {
	return r.DB().Where("user_id = ?", userID).Delete(&models.TeacherProfile{}).Error
}

func (r *teacherSubscriptionRepository) DeactivateAllDevices(userID uint) error {
	return r.DB().Model(&models.TeacherDevice{}).
		Where("user_id = ?", userID).
		Updates(map[string]interface{}{"is_active": false, "updated_at": time.Now()}).Error
}

func ensureTeacherPremiumFileColumnsForRepository(db *gorm.DB) error {
	if db == nil || !db.Migrator().HasTable(&models.File{}) {
		return nil
	}

	columns := map[string]string{
		"IsPremium":                   "is_premium",
		"PremiumAudience":             "premium_audience",
		"PremiumCategory":             "premium_category",
		"PremiumRequiresSubscription": "premium_requires_subscription",
		"PremiumSubject":              "premium_subject",
		"PremiumDownloadCount":        "premium_download_count",
	}

	for fieldName, columnName := range columns {
		if !db.Migrator().HasColumn(&models.File{}, columnName) {
			if err := db.Migrator().AddColumn(&models.File{}, fieldName); err != nil {
				return err
			}
		}
	}

	return nil
}

func (r *teacherSubscriptionRepository) AdminListFilesForPremium(countryID database.CountryID, search, premium, category, subject string, limit, offset int) ([]models.File, int64, error) {
	if limit <= 0 || limit > 100 {
		limit = 24
	}

	db := database.DBForCountry(countryID)
	if err := ensureTeacherPremiumFileColumnsForRepository(db); err != nil {
		return nil, 0, err
	}

	q := db.Model(&models.File{}).
		Preload("Article").
		Preload("Article.SchoolClass").
		Preload("Article.Subject").
		Preload("Article.Semester").
		Preload("Post")

	switch premium {
	case "1", "true", "yes", "premium":
		q = q.Where("is_premium = ?", true)
	case "0", "false", "no", "free":
		q = q.Where("is_premium = ?", false)
	}

	if category != "" {
		q = q.Where("premium_category = ?", category)
	}
	if subject != "" {
		like := "%" + subject + "%"
		q = q.Where("(premium_subject LIKE ? OR EXISTS (SELECT 1 FROM articles a LEFT JOIN subjects s ON s.id = a.subject_id WHERE a.id = files.article_id AND s.subject_name LIKE ?))", like, like)
	}
	if search != "" {
		like := "%" + search + "%"
		q = q.Where("(file_name LIKE ? OR file_type LIKE ? OR mime_type LIKE ? OR file_category LIKE ? OR premium_subject LIKE ? OR premium_category LIKE ?)", like, like, like, like, like, like)
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var files []models.File
	err := q.Order("created_at DESC").Limit(limit).Offset(offset).Find(&files).Error
	return files, total, err
}

func (r *teacherSubscriptionRepository) AdminUpdateFilePremium(countryID database.CountryID, fileID uint, values map[string]interface{}) (*models.File, error) {
	db := database.DBForCountry(countryID)
	if err := ensureTeacherPremiumFileColumnsForRepository(db); err != nil {
		return nil, err
	}

	if err := db.Model(&models.File{}).Where("id = ?", fileID).Updates(values).Error; err != nil {
		return nil, err
	}

	var file models.File
	err := db.Preload("Article").
		Preload("Article.SchoolClass").
		Preload("Article.Subject").
		Preload("Article.Semester").
		Preload("Post").
		First(&file, fileID).Error
	return &file, err
}

func ensureTeacherPremiumVaultTable(db *gorm.DB) error {
	if db == nil {
		return nil
	}
	return db.AutoMigrate(&models.TeacherPremiumFile{}, &models.TeacherPremiumDownload{}, &models.TeacherLibraryItem{})
}

func (r *teacherSubscriptionRepository) ListTeacherPremiumFiles(countryID database.CountryID, subjects []string, category, query string, limit, offset int) ([]models.TeacherPremiumFile, int64, error) {
	if limit <= 0 || limit > 100 {
		limit = 24
	}

	db := database.DBForCountry(countryID)
	if err := ensureTeacherPremiumVaultTable(db); err != nil {
		return nil, 0, err
	}

	q := db.Model(&models.TeacherPremiumFile{}).Where("is_active = ? AND archived_at IS NULL", true)

	if category != "" {
		q = q.Where("category = ?", category)
	}

	// Match the file's subject against ANY of the teacher's subjects (up to 3).
	var subjectClauses []string
	var subjectArgs []interface{}
	for _, subject := range subjects {
		subject = strings.TrimSpace(subject)
		if subject == "" {
			continue
		}
		like := "%" + subject + "%"
		subjectClauses = append(subjectClauses, "(subject_name LIKE ? OR ? LIKE CONCAT('%', subject_name, '%'))")
		subjectArgs = append(subjectArgs, like, subject)
	}
	if len(subjectClauses) > 0 {
		q = q.Where(strings.Join(subjectClauses, " OR "), subjectArgs...)
	}

	if query != "" {
		like := "%" + query + "%"
		q = q.Where("(title LIKE ? OR description LIKE ? OR original_filename LIKE ? OR subject_name LIKE ? OR category LIKE ?)", like, like, like, like, like)
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var files []models.TeacherPremiumFile
	err := q.Order("created_at DESC").Limit(limit).Offset(offset).Find(&files).Error
	return files, total, err
}

func (r *teacherSubscriptionRepository) GetTeacherPremiumFile(countryID database.CountryID, fileID uint) (*models.TeacherPremiumFile, error) {
	db := database.DBForCountry(countryID)
	if err := ensureTeacherPremiumVaultTable(db); err != nil {
		return nil, err
	}

	var file models.TeacherPremiumFile
	err := db.Where("id = ? AND is_active = ?", fileID, true).First(&file).Error
	return &file, err
}

func (r *teacherSubscriptionRepository) AdminListTeacherPremiumFiles(countryID database.CountryID, search, active, category, subject string, limit, offset int) ([]models.TeacherPremiumFile, int64, error) {
	if limit <= 0 || limit > 100 {
		limit = 24
	}

	db := database.DBForCountry(countryID)
	if err := ensureTeacherPremiumVaultTable(db); err != nil {
		return nil, 0, err
	}

	q := db.Model(&models.TeacherPremiumFile{})

	switch active {
	case "1", "true", "active":
		q = q.Where("is_active = ?", true)
	case "0", "false", "inactive":
		q = q.Where("is_active = ?", false)
	}

	if category != "" {
		q = q.Where("category = ?", category)
	}

	if subject != "" {
		like := "%" + subject + "%"
		q = q.Where("(subject_name LIKE ? OR ? LIKE CONCAT('%', subject_name, '%'))", like, subject)
	}

	if search != "" {
		like := "%" + search + "%"
		q = q.Where("(title LIKE ? OR description LIKE ? OR original_filename LIKE ? OR subject_name LIKE ? OR category LIKE ?)", like, like, like, like, like)
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var files []models.TeacherPremiumFile
	err := q.Order("created_at DESC").Limit(limit).Offset(offset).Find(&files).Error
	return files, total, err
}

func (r *teacherSubscriptionRepository) AdminCreateTeacherPremiumFile(countryID database.CountryID, file *models.TeacherPremiumFile) error {
	db := database.DBForCountry(countryID)
	if err := ensureTeacherPremiumVaultTable(db); err != nil {
		return err
	}
	return db.Create(file).Error
}

func (r *teacherSubscriptionRepository) AdminUpdateTeacherPremiumFile(countryID database.CountryID, fileID uint, values map[string]interface{}) (*models.TeacherPremiumFile, error) {
	db := database.DBForCountry(countryID)
	if err := ensureTeacherPremiumVaultTable(db); err != nil {
		return nil, err
	}

	if err := db.Model(&models.TeacherPremiumFile{}).Where("id = ?", fileID).Updates(values).Error; err != nil {
		return nil, err
	}

	var file models.TeacherPremiumFile
	err := db.First(&file, fileID).Error
	return &file, err
}

func (r *teacherSubscriptionRepository) CreateTeacherPremiumDownload(download *models.TeacherPremiumDownload) error {
	return r.DB().Create(download).Error
}

func (r *teacherSubscriptionRepository) IncrementTeacherPremiumFileDownload(countryID database.CountryID, fileID uint) error {
	db := database.DBForCountry(countryID)
	return db.Model(&models.TeacherPremiumFile{}).Where("id = ?", fileID).
		UpdateColumn("download_count", gorm.Expr("download_count + ?", 1)).Error
}

func (r *teacherSubscriptionRepository) CreateAuditLog(log *models.TeacherAuditLog) error {
	return r.DB().Create(log).Error
}

func (r *teacherSubscriptionRepository) ListAuditLogs(entityType string, entityID uint, limit, offset int) ([]models.TeacherAuditLog, int64, error) {
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	q := r.DB().Model(&models.TeacherAuditLog{}).Preload("Actor").Preload("User")
	if entityType != "" {
		q = q.Where("entity_type = ?", entityType)
	}
	if entityID > 0 {
		q = q.Where("entity_id = ?", entityID)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var items []models.TeacherAuditLog
	err := q.Order("created_at DESC, id DESC").Limit(limit).Offset(offset).Find(&items).Error
	return items, total, err
}

func (r *teacherSubscriptionRepository) GetTeacherPremiumFileAdmin(countryID database.CountryID, fileID uint) (*models.TeacherPremiumFile, error) {
	db := database.DBForCountry(countryID)
	if err := ensureTeacherPremiumVaultTable(db); err != nil {
		return nil, err
	}
	var file models.TeacherPremiumFile
	err := db.First(&file, fileID).Error
	return &file, err
}

func (r *teacherSubscriptionRepository) ArchiveTeacherPremiumFile(countryID database.CountryID, fileID uint, reason string) (*models.TeacherPremiumFile, error) {
	db := database.DBForCountry(countryID)
	if err := ensureTeacherPremiumVaultTable(db); err != nil {
		return nil, err
	}
	now := time.Now()
	if err := db.Model(&models.TeacherPremiumFile{}).
		Where("id = ?", fileID).
		Updates(map[string]interface{}{
			"is_active":      false,
			"archived_at":    &now,
			"archive_reason": reason,
			"updated_at":     now,
		}).Error; err != nil {
		return nil, err
	}
	var file models.TeacherPremiumFile
	err := db.First(&file, fileID).Error
	return &file, err
}

func (r *teacherSubscriptionRepository) RenewSubscription(subscriptionID uint, newEndsAt time.Time, adminID uint, note string) (*models.TeacherSubscription, error) {
	var sub models.TeacherSubscription
	if err := r.DB().First(&sub, subscriptionID).Error; err != nil {
		return nil, err
	}
	now := time.Now()
	sub.Status = "active"
	sub.EndsAt = newEndsAt
	sub.CancelledAt = nil
	sub.AdminNote = note
	sub.ActivatedBy = &adminID
	sub.UpdatedAt = now
	if err := r.DB().Save(&sub).Error; err != nil {
		return nil, err
	}
	return &sub, nil
}

func (r *teacherSubscriptionRepository) ExpireOverdueSubscriptions(now time.Time) (int64, error) {
	tx := r.DB().Model(&models.TeacherSubscription{}).
		Where("status = ? AND ends_at < ?", "active", now).
		Updates(map[string]interface{}{"status": "expired", "updated_at": now})
	return tx.RowsAffected, tx.Error
}

func (r *teacherSubscriptionRepository) CreateExpiryNotificationIfMissing(item *models.TeacherExpiryNotification) error {
	var existing int64
	if err := r.DB().Model(&models.TeacherExpiryNotification{}).
		Where("subscription_id = ? AND notice_type = ?", item.SubscriptionID, item.NoticeType).
		Count(&existing).Error; err != nil {
		return err
	}
	if existing > 0 {
		return nil
	}
	return r.DB().Create(item).Error
}

func ensureTeacherNotificationTable(db *gorm.DB) error {
	return db.AutoMigrate(&models.TeacherNotification{})
}

func ensureTeacherPaymentSettingsTable(db *gorm.DB) error {
	return db.AutoMigrate(&models.TeacherPaymentSetting{})
}

func (r *teacherSubscriptionRepository) CreateTeacherAIGeneration(item *models.TeacherAIGeneration) error {
	return r.DB().Create(item).Error
}

func (r *teacherSubscriptionRepository) GetTeacherAIGeneration(userID uint, id uint) (*models.TeacherAIGeneration, error) {
	var item models.TeacherAIGeneration
	err := r.DB().Where("id = ? AND user_id = ?", id, userID).First(&item).Error
	return &item, err
}

func (r *teacherSubscriptionRepository) IncrementAIGenerationExport(id uint) error {
	return r.DB().Model(&models.TeacherAIGeneration{}).Where("id = ?", id).
		UpdateColumn("export_count", gorm.Expr("export_count + ?", 1)).Error
}

func (r *teacherSubscriptionRepository) CreateTeacherNotification(item *models.TeacherNotification) error {
	db := r.DB()
	if err := ensureTeacherNotificationTable(db); err != nil {
		return err
	}
	return db.Create(item).Error
}

func (r *teacherSubscriptionRepository) ListTeacherNotifications(userID uint, limit, offset int) ([]models.TeacherNotification, int64, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	db := r.DB()
	if err := ensureTeacherNotificationTable(db); err != nil {
		return nil, 0, err
	}
	q := db.Model(&models.TeacherNotification{}).Where("user_id IS NULL OR user_id = ?", userID)
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var items []models.TeacherNotification
	err := q.Order("created_at DESC, id DESC").Limit(limit).Offset(offset).Find(&items).Error
	return items, total, err
}

func (r *teacherSubscriptionRepository) ListPaymentSettings() ([]models.TeacherPaymentSetting, error) {
	db := r.DB()
	if err := ensureTeacherPaymentSettingsTable(db); err != nil {
		return nil, err
	}
	var items []models.TeacherPaymentSetting
	err := db.Where("is_active = ?", true).Order("sort_order ASC, id ASC").Find(&items).Error
	return items, err
}

func (r *teacherSubscriptionRepository) UpsertPaymentSetting(item *models.TeacherPaymentSetting) error {
	db := r.DB()
	if err := ensureTeacherPaymentSettingsTable(db); err != nil {
		return err
	}
	var existing models.TeacherPaymentSetting
	err := db.Where("provider = ?", item.Provider).First(&existing).Error
	if err == nil {
		item.ID = existing.ID
		return db.Save(item).Error
	}
	return db.Create(item).Error
}

func (r *teacherSubscriptionRepository) UpdateTeacherPremiumDownloadWatermark(downloadID uint, applied bool, text string, path string) error {
	return r.DB().Model(&models.TeacherPremiumDownload{}).
		Where("id = ?", downloadID).
		Updates(map[string]interface{}{
			"watermark_applied": applied,
			"watermark_text":    text,
			"watermarked_path":  path,
		}).Error
}
