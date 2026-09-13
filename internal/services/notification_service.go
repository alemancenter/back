package services

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/imanjo/fiber-api/internal/database"
	"github.com/imanjo/fiber-api/internal/models"
	"github.com/imanjo/fiber-api/internal/repositories"
	"github.com/google/uuid"
)

type NotificationService interface {
	List(userID uint, search, status string, offset, limit int) ([]models.Notification, int64, error)
	Stats(userID uint) (map[string]int64, error)
	GetLatest(userID uint, limit int) ([]models.Notification, int64, error)
	MarkAsRead(id string, userID uint) error
	MarkAllRead(userID uint) error
	Create(reqType string, notifiableID uint, data string) (*models.Notification, error)
	CreateBulk(reqType string, userIDs []uint, data string) error
	Broadcast(reqType, title, message, actionURL, role string) error
	NotifyUsersWithPermissions(reqType, title, message, actionURL string, permissions []string, includeUserIDs ...uint) error
	Delete(id string, userID uint) error
	Prune(daysOld int) (int64, error)
	BulkAction(action string, ids []string, userID uint) error
}

// ErrNotificationsDisabled is returned by Create/CreateBulk (and therefore Broadcast/
// NotifyUsersWithPermissions, which both funnel through CreateBulk) while the public
// "enable_notifications" setting is off. Previously this toggle had no enforcement anywhere —
// every one of these call sites still created and pushed notifications regardless of its value.
var ErrNotificationsDisabled = errors.New("الإشعارات موقوفة حاليًا")

type PruneNotificationsResponse struct {
	Deleted int64 `json:"deleted"`
}

type LatestNotificationsResponse struct {
	Notifications []models.Notification `json:"notifications"`
	UnreadCount   int64                 `json:"unread_count"`
}

type notificationService struct {
	repo        repositories.NotificationRepository
	userRepo    repositories.UserRepository
	push        PushService
	settingsSvc SettingService
}

func NewNotificationService(
	repo repositories.NotificationRepository,
	userRepo repositories.UserRepository,
	push PushService,
	settingsSvc SettingService,
) NotificationService {
	return &notificationService{repo: repo, userRepo: userRepo, push: push, settingsSvc: settingsSvc}
}

// notificationsEnabled reads the public "enable_notifications" toggle. There's no per-country
// context at any of this service's call sites (Create/CreateBulk/Broadcast/
// NotifyUsersWithPermissions all take plain user IDs, not a country), so — like
// maintenance_mode and enable_registration elsewhere in this codebase — this setting is treated
// as a single site-wide switch, read from the primary (Jordan) database. GetPublic is
// Redis-cached (settingsCacheTTL, 2h) so this adds no real per-call cost.
func (s *notificationService) notificationsEnabled() bool {
	settings, err := s.settingsSvc.GetPublic(context.Background(), database.CountryJordan)
	if err != nil || settings == nil {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(settings["enable_notifications"])) {
	case "false", "0", "no", "off":
		return false
	default:
		return true
	}
}

func (s *notificationService) List(userID uint, search, status string, offset, limit int) ([]models.Notification, int64, error) {
	return s.repo.List(userID, search, status, offset, limit)
}

func (s *notificationService) Stats(userID uint) (map[string]int64, error) {
	return s.repo.Stats(userID)
}

func (s *notificationService) GetLatest(userID uint, limit int) ([]models.Notification, int64, error) {
	var (
		wg            sync.WaitGroup
		notifications []models.Notification
		unreadCount   int64
		err1, err2    error
	)
	wg.Add(2)
	go func() {
		defer wg.Done()
		notifications, err1 = s.repo.GetLatest(userID, limit)
	}()
	go func() {
		defer wg.Done()
		unreadCount, err2 = s.repo.GetUnreadCount(userID)
	}()
	wg.Wait()

	if err1 != nil {
		return nil, 0, err1
	}
	if err2 != nil {
		return nil, 0, err2
	}

	return notifications, unreadCount, nil
}

func (s *notificationService) MarkAsRead(id string, userID uint) error {
	return s.repo.MarkAsRead(id, userID)
}

func (s *notificationService) MarkAllRead(userID uint) error {
	return s.repo.MarkAllRead(userID)
}

func (s *notificationService) Create(reqType string, notifiableID uint, data string) (*models.Notification, error) {
	if !s.notificationsEnabled() {
		return nil, ErrNotificationsDisabled
	}
	notification := &models.Notification{
		ID:             uuid.New().String(),
		Type:           reqType,
		NotifiableType: "App\\Models\\User",
		NotifiableID:   notifiableID,
		Data:           json.RawMessage(data),
	}

	err := s.repo.Create(notification)
	if err == nil && s.push != nil {
		var d map[string]string
		if json.Unmarshal([]byte(data), &d) == nil {
			go s.push.SendToUsers([]uint{notifiableID}, d["title"], d["message"], d["action_url"])
		}
	}
	return notification, MapError(err)
}

func (s *notificationService) CreateBulk(reqType string, userIDs []uint, data string) error {
	if len(userIDs) == 0 {
		return nil
	}
	if !s.notificationsEnabled() {
		return ErrNotificationsDisabled
	}
	notifications := make([]*models.Notification, len(userIDs))
	for i, uid := range userIDs {
		notifications[i] = &models.Notification{
			ID:             uuid.New().String(),
			Type:           reqType,
			NotifiableType: "App\\Models\\User",
			NotifiableID:   uid,
			Data:           json.RawMessage(data),
		}
	}
	if err := s.repo.CreateBulk(notifications); err != nil {
		return MapError(err)
	}
	if s.push != nil {
		var d map[string]string
		if json.Unmarshal([]byte(data), &d) == nil {
			go s.push.SendToUsers(userIDs, d["title"], d["message"], d["action_url"])
		}
	}
	return nil
}

func (s *notificationService) Broadcast(reqType, title, message, actionURL, role string) error {
	userIDs, err := s.userRepo.GetAllUserIDs(role)
	if err != nil {
		return err
	}
	data, _ := json.Marshal(map[string]string{
		"title":      title,
		"message":    message,
		"action_url": actionURL,
	})
	return s.CreateBulk(reqType, userIDs, string(data))
}

func (s *notificationService) NotifyUsersWithPermissions(reqType, title, message, actionURL string, permissions []string, includeUserIDs ...uint) error {
	userIDs, err := s.userRepo.GetUserIDsByPermissions(permissions)
	if err != nil {
		return err
	}

	seen := make(map[uint]bool, len(userIDs)+len(includeUserIDs))
	finalUserIDs := make([]uint, 0, len(userIDs)+len(includeUserIDs))
	for _, uid := range userIDs {
		if uid == 0 || seen[uid] {
			continue
		}
		seen[uid] = true
		finalUserIDs = append(finalUserIDs, uid)
	}
	for _, uid := range includeUserIDs {
		if uid == 0 || seen[uid] {
			continue
		}
		seen[uid] = true
		finalUserIDs = append(finalUserIDs, uid)
	}

	if len(finalUserIDs) == 0 {
		return nil
	}

	data, _ := json.Marshal(map[string]string{
		"title":      title,
		"message":    message,
		"action_url": actionURL,
	})
	return s.CreateBulk(reqType, finalUserIDs, string(data))
}

func (s *notificationService) Delete(id string, userID uint) error {
	return s.repo.Delete(id, userID)
}

func (s *notificationService) Prune(daysOld int) (int64, error) {
	cutoff := time.Now().AddDate(0, 0, -daysOld)
	return s.repo.Prune(cutoff)
}

func (s *notificationService) BulkAction(action string, ids []string, userID uint) error {
	switch action {
	case "read":
		return s.repo.BulkMarkAsRead(ids, userID)
	case "delete":
		return s.repo.BulkDelete(ids, userID)
	default:
		return nil
	}
}
