package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sync"

	"github.com/alemancenter/fiber-api/internal/repositories"
	"golang.org/x/oauth2/google"
)

type PushService interface {
	SendToUsers(userIDs []uint, title, body, actionURL string)
}

type pushService struct {
	userRepo       repositories.UserRepository
	fcmEnabled     bool
	fcmProjectID   string
	fcmSAFile      string
	oneSignalAppID string
	oneSignalKey   string
	httpClient     *http.Client
}

func NewPushService(
	userRepo repositories.UserRepository,
	fcmEnabled bool,
	fcmProjectID, fcmSAFile,
	oneSignalAppID, oneSignalKey string,
) PushService {
	return &pushService{
		userRepo:       userRepo,
		fcmEnabled:     fcmEnabled,
		fcmProjectID:   fcmProjectID,
		fcmSAFile:      fcmSAFile,
		oneSignalAppID: oneSignalAppID,
		oneSignalKey:   oneSignalKey,
		httpClient:     &http.Client{},
	}
}

func (s *pushService) SendToUsers(userIDs []uint, title, body, actionURL string) {
	if len(userIDs) == 0 {
		return
	}
	tokens, err := s.userRepo.GetPushTokensByUserIDs(userIDs)
	if err != nil || len(tokens) == 0 {
		return
	}

	var fcmTokens, onesignalTokens []string
	for _, t := range tokens {
		switch t.Platform {
		case "fcm", "web":
			fcmTokens = append(fcmTokens, t.Token)
		case "onesignal":
			onesignalTokens = append(onesignalTokens, t.Token)
		}
	}

	var wg sync.WaitGroup
	if s.fcmEnabled && s.fcmProjectID != "" && s.fcmSAFile != "" && len(fcmTokens) > 0 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.sendFCM(fcmTokens, title, body, actionURL)
		}()
	}
	if s.oneSignalAppID != "" && s.oneSignalKey != "" && len(onesignalTokens) > 0 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.sendOneSignal(onesignalTokens, title, body, actionURL)
		}()
	}
	wg.Wait()
}

func (s *pushService) sendFCM(tokens []string, title, body, actionURL string) {
	saData, err := os.ReadFile(s.fcmSAFile)
	if err != nil {
		return
	}
	conf, err := google.JWTConfigFromJSON(saData, "https://www.googleapis.com/auth/firebase.messaging")
	if err != nil {
		return
	}
	client := conf.Client(context.Background())
	url := fmt.Sprintf("https://fcm.googleapis.com/v1/projects/%s/messages:send", s.fcmProjectID)

	for _, token := range tokens {
		payload := map[string]interface{}{
			"message": map[string]interface{}{
				"token": token,
				"notification": map[string]string{
					"title": title,
					"body":  body,
				},
				"data": map[string]string{
					"action_url": actionURL,
				},
			},
		}
		b, _ := json.Marshal(payload)
		resp, err := client.Post(url, "application/json", bytes.NewBuffer(b))
		if err != nil {
			continue
		}
		resp.Body.Close()
	}
}

func (s *pushService) sendOneSignal(playerIDs []string, title, body, actionURL string) {
	payload := map[string]interface{}{
		"app_id":             s.oneSignalAppID,
		"include_player_ids": playerIDs,
		"headings":           map[string]string{"en": title},
		"contents":           map[string]string{"en": body},
		"data":               map[string]string{"action_url": actionURL},
	}
	b, _ := json.Marshal(payload)
	req, err := http.NewRequest("POST", "https://onesignal.com/api/v1/notifications", bytes.NewBuffer(b))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Basic "+s.oneSignalKey)
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return
	}
	resp.Body.Close()
}
