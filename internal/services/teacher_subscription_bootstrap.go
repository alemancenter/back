package services

import (
	"time"

	"github.com/alemancenter/fiber-api/internal/models"
	"gorm.io/gorm"
)

// EnsureTeacherSubscriptionDatabase finalizes the database layer for the teacher
// semester subscription module. It is safe and idempotent; repeated calls update
// the default plan, role, permissions, and backfill missing Teacher Pro roles.
func EnsureTeacherSubscriptionDatabase(db *gorm.DB) error {
	if db == nil {
		return nil
	}

	if err := db.AutoMigrate(
		&models.SubscriptionPlan{},
		&models.TeacherProfile{},
		&models.TeacherPremiumFile{},
		&models.TeacherSubscription{},
		&models.SubscriptionOrder{},
		&models.TeacherDevice{},
		&models.TeacherLibraryItem{},
		&models.TeacherPremiumDownload{},
		&models.TeacherExpiryNotification{},
		&models.TeacherPaymentSetting{},
		&models.TeacherNotification{},
		&models.TeacherAuditLog{},
		&models.TeacherAIGeneration{},
	); err != nil {
		return err
	}

	if err := ensureTeacherPremiumFileColumns(db); err != nil {
		return err
	}
	if err := ensureTeacherPremiumDownloadWatermarkColumns(db); err != nil {
		return err
	}

	if err := ensureTeacherSubscriptionPlan(db); err != nil {
		return err
	}

	// Some country databases may not contain the authorization tables.
	// Teacher subscription tables should still bootstrap safely in those databases.
	if !hasTeacherAuthTables(db) {
		return nil
	}

	role, err := ensureTeacherProRoleOnDB(db)
	if err != nil {
		return err
	}
	if err := backfillTeacherProRoleForActiveSubscriptions(db, role.ID); err != nil {
		return err
	}
	return cleanupTeacherProRoleWithoutActiveSubscription(db, role.ID)
}

func ensureTeacherPremiumDownloadWatermarkColumns(db *gorm.DB) error {
	if db == nil || !db.Migrator().HasTable("teacher_premium_downloads") {
		return nil
	}

	columns := []struct {
		name string
		sql  string
	}{
		{"watermark_applied", "ALTER TABLE teacher_premium_downloads ADD COLUMN watermark_applied BOOLEAN NOT NULL DEFAULT FALSE"},
		{"watermark_text", "ALTER TABLE teacher_premium_downloads ADD COLUMN watermark_text TEXT"},
		{"watermarked_path", "ALTER TABLE teacher_premium_downloads ADD COLUMN watermarked_path VARCHAR(1000) NOT NULL DEFAULT ''"},
	}

	for _, column := range columns {
		if !db.Migrator().HasColumn("teacher_premium_downloads", column.name) {
			if err := db.Exec(column.sql).Error; err != nil {
				return err
			}
		}
	}

	return nil
}

func hasTeacherAuthTables(db *gorm.DB) bool {
	if db == nil {
		return false
	}
	required := []string{
		"roles",
		"permissions",
		"model_has_roles",
		"role_has_permissions",
	}
	for _, table := range required {
		if !db.Migrator().HasTable(table) {
			return false
		}
	}
	return true
}

func ensureTeacherPremiumFileColumns(db *gorm.DB) error {
	if db == nil || !db.Migrator().HasTable("files") {
		return nil
	}

	columns := []struct {
		name string
		sql  string
	}{
		{"is_premium", "ALTER TABLE files ADD COLUMN is_premium BOOLEAN NOT NULL DEFAULT FALSE"},
		{"premium_audience", "ALTER TABLE files ADD COLUMN premium_audience VARCHAR(40) NOT NULL DEFAULT ''"},
		{"premium_category", "ALTER TABLE files ADD COLUMN premium_category VARCHAR(80) NOT NULL DEFAULT ''"},
		{"premium_requires_subscription", "ALTER TABLE files ADD COLUMN premium_requires_subscription BOOLEAN NOT NULL DEFAULT FALSE"},
		{"premium_subject", "ALTER TABLE files ADD COLUMN premium_subject VARCHAR(255) NOT NULL DEFAULT ''"},
		{"premium_download_count", "ALTER TABLE files ADD COLUMN premium_download_count BIGINT NOT NULL DEFAULT 0"},
	}

	for _, column := range columns {
		if !db.Migrator().HasColumn("files", column.name) {
			if err := db.Exec(column.sql).Error; err != nil {
				return err
			}
		}
	}

	return nil
}

func ensureTeacherSubscriptionPlan(db *gorm.DB) error {
	plan := models.SubscriptionPlan{
		Code:              TeacherSemesterPlanCode,
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
	err := db.Where("code = ?", TeacherSemesterPlanCode).First(&existing).Error
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
		return db.Save(&existing).Error
	}
	if err != nil && err != gorm.ErrRecordNotFound {
		return err
	}
	return db.Create(&plan).Error
}

func ensureTeacherProRoleOnDB(db *gorm.DB) (*models.Role, error) {
	permissionNames := append([]string{}, TeacherSemesterPermissions...)
	permissionNames = append(permissionNames, TeacherAdminPermissions...)

	for _, name := range permissionNames {
		permission := models.Permission{Name: name, GuardName: "api"}
		if err := db.Where("name = ?", name).FirstOrCreate(&permission, models.Permission{Name: name, GuardName: "api"}).Error; err != nil {
			return nil, err
		}
	}

	role := models.Role{Name: TeacherProRoleName, GuardName: "api"}
	if err := db.Where("name = ?", TeacherProRoleName).FirstOrCreate(&role, models.Role{Name: TeacherProRoleName, GuardName: "api"}).Error; err != nil {
		return nil, err
	}

	for _, name := range TeacherSemesterPermissions {
		var permission models.Permission
		if err := db.Where("name = ?", name).First(&permission).Error; err != nil {
			return nil, err
		}
		if err := db.Exec(
			"INSERT IGNORE INTO role_has_permissions (permission_id, role_id) VALUES (?, ?)",
			permission.ID, role.ID,
		).Error; err != nil {
			return nil, err
		}
	}

	return &role, nil
}

func backfillTeacherProRoleForActiveSubscriptions(db *gorm.DB, roleID uint) error {
	type row struct {
		UserID uint
	}
	var rows []row
	now := time.Now()
	if err := db.Model(&models.TeacherSubscription{}).
		Select("DISTINCT user_id").
		Where("status = ? AND starts_at <= ? AND ends_at >= ?", "active", now, now).
		Scan(&rows).Error; err != nil {
		return err
	}

	for _, item := range rows {
		if item.UserID == 0 {
			continue
		}
		if err := db.Exec(
			"INSERT IGNORE INTO model_has_roles (role_id, model_type, model_id) VALUES (?, ?, ?)",
			roleID, modelTypeUser, item.UserID,
		).Error; err != nil {
			return err
		}
		InvalidateUserCache(item.UserID)
	}

	return nil
}

func cleanupTeacherProRoleWithoutActiveSubscription(db *gorm.DB, roleID uint) error {
	now := time.Now()

	type row struct {
		UserID uint
	}
	var rows []row

	if err := db.Raw(`
		SELECT DISTINCT mhr.model_id AS user_id
		FROM model_has_roles mhr
		WHERE mhr.role_id = ?
		  AND mhr.model_id NOT IN (
		    SELECT user_id
		    FROM teacher_subscriptions
		    WHERE status = 'active'
		      AND starts_at <= ?
		      AND ends_at >= ?
		  )
	`, roleID, now, now).Scan(&rows).Error; err != nil {
		return err
	}

	for _, item := range rows {
		if item.UserID == 0 {
			continue
		}

		if err := db.Exec("DELETE FROM model_has_roles WHERE role_id = ? AND model_id = ?", roleID, item.UserID).Error; err != nil {
			return err
		}

		for _, permissionName := range TeacherSemesterPermissions {
			if err := db.Exec(`
				DELETE mhp FROM model_has_permissions mhp
				INNER JOIN permissions p ON p.id = mhp.permission_id
				WHERE mhp.model_id = ? AND p.name = ?
			`, item.UserID, permissionName).Error; err != nil {
				return err
			}
		}

		_ = db.Model(&models.TeacherDevice{}).
			Where("user_id = ?", item.UserID).
			Updates(map[string]interface{}{"is_active": false, "updated_at": time.Now()}).Error

		InvalidateUserCache(item.UserID)
	}

	return nil
}
