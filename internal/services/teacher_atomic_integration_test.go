package services

import (
	"fmt"
	"github.com/imanjo/fiber-api/internal/models"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func atomicTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("IMANJO_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("set IMANJO_TEST_MYSQL_DSN for isolated MySQL integration tests")
	}
	// DSN must end with /?parseTime=true: tests create/drop their own random database.
	if !strings.HasSuffix(dsn, "/?parseTime=true") {
		t.Fatal("test DSN must have no database and end in /?parseTime=true")
	}
	root, err := gorm.Open(mysql.Open(dsn), &gorm.Config{DisableForeignKeyConstraintWhenMigrating: true})
	if err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("imanjo_test_%d", time.Now().UnixNano())
	if err = root.Exec("CREATE DATABASE " + name).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { root.Exec("DROP DATABASE " + name); sqlDB, _ := root.DB(); sqlDB.Close() })
	db, err := gorm.Open(mysql.Open(strings.Replace(dsn, "/?parseTime=true", "/"+name+"?parseTime=true", 1)), &gorm.Config{DisableForeignKeyConstraintWhenMigrating: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sqlDB, _ := db.DB(); sqlDB.Close() })
	if err := db.AutoMigrate(&models.SubscriptionPlan{}, &models.SubscriptionOrder{}, &models.TeacherSubscription{}, &models.TeacherNotification{}, &models.TeacherPremiumDownload{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("CREATE TABLE IF NOT EXISTS model_has_roles (role_id BIGINT UNSIGNED, model_type VARCHAR(255), model_id BIGINT UNSIGNED, UNIQUE KEY assignment(role_id,model_type,model_id))").Error; err != nil {
		t.Fatal(err)
	}
	return db
}
func TestAtomicOrderApproval(t *testing.T) {
	db := atomicTestDB(t)
	plan := models.SubscriptionPlan{Code: "test", Name: "Test", DurationDays: 30}
	if err := db.Create(&plan).Error; err != nil {
		t.Fatal(err)
	}
	order := models.SubscriptionOrder{UserID: 1, PlanID: plan.ID, Status: "pending", PaymentMethod: "manual"}
	if err := db.Create(&order).Error; err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, err := approveTeacherOrder(db, order.ID, 1, 1, ""); errs <- err }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var count int64
	db.Model(&models.TeacherSubscription{}).Where("source_order_id = ?", order.ID).Count(&count)
	if count != 1 {
		t.Fatal("duplicate approval", count)
	}
	// Force a later transaction failure: the order must remain pending and no subscription persists.
	failed := models.SubscriptionOrder{UserID: 2, PlanID: plan.ID, Status: "pending", PaymentMethod: "manual"}
	db.Create(&failed)
	db.Migrator().DropTable(&models.TeacherNotification{})
	if _, err := approveTeacherOrder(db, failed.ID, 1, 1, ""); err == nil {
		t.Fatal("expected failure")
	}
	db.First(&failed, failed.ID)
	if failed.Status != "pending" {
		t.Fatal("partial approval persisted")
	}
	db.Model(&models.TeacherSubscription{}).Where("source_order_id = ?", failed.ID).Count(&count)
	if count != 0 {
		t.Fatal("partial subscription persisted")
	}
}
func TestAtomicDownloadQuota(t *testing.T) {
	db := atomicTestDB(t)
	sub := models.TeacherSubscription{UserID: 1, PlanID: 1, Status: "active", StartsAt: time.Now().Add(-time.Hour), EndsAt: time.Now().Add(time.Hour), DownloadLimit: 1}
	if err := db.Create(&sub).Error; err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- reserveTeacherDownload(db, &models.TeacherPremiumDownload{UserID: 1, SubscriptionID: sub.ID, Status: "reserved"})
		}()
	}
	wg.Wait()
	close(results)
	allowed := 0
	for err := range results {
		if err == nil {
			allowed++
		} else if err != ErrTeacherDeviceLimit {
			t.Fatal(err)
		}
	}
	if allowed != 1 {
		t.Fatal("quota overspent", allowed)
	}
	db.Model(&models.TeacherPremiumDownload{}).Where("subscription_id = ?", sub.ID).Update("status", "failed")
	if err := reserveTeacherDownload(db, &models.TeacherPremiumDownload{UserID: 1, SubscriptionID: sub.ID, Status: "reserved"}); err != nil {
		t.Fatal("failed preparation did not refund quota", err)
	}
}
