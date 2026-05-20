package contentaudit

import (
	"context"
	"errors"
	"io"
	"sync"
	"time"

	"github.com/alemancenter/fiber-api/internal/models"
	"github.com/alemancenter/fiber-api/internal/repositories"
	coreai "github.com/alemancenter/fiber-api/internal/services"
	"github.com/alemancenter/fiber-api/pkg/logger"
	"go.uber.org/zap"
)

var ErrAlreadyRunning = errors.New("content audit is already running")

var processLock = struct {
	sync.Mutex
	running bool
}{}

type Service struct {
	repo         repositories.ContentAuditRepository
	opts         Options
	ai           coreai.AIService
	notification coreai.NotificationService
}

func NewService(repo repositories.ContentAuditRepository, opts Options) *Service {
	return &Service{repo: repo, opts: opts.withDefaults()}
}

func NewServiceWithAI(repo repositories.ContentAuditRepository, opts Options, ai coreai.AIService) *Service {
	return &Service{repo: repo, opts: opts.withDefaults(), ai: ai}
}

func NewServiceWithAIAndNotifications(repo repositories.ContentAuditRepository, opts Options, ai coreai.AIService, notification coreai.NotificationService) *Service {
	return &Service{repo: repo, opts: opts.withDefaults(), ai: ai, notification: notification}
}

func (s *Service) SetAI(ai coreai.AIService) {
	s.ai = ai
}

func (s *Service) Start(ctx context.Context, triggeredBy string, userID *uint) (*models.PolicyAuditRun, error) {
	if triggeredBy == "" {
		triggeredBy = models.PolicyAuditTriggerManual
	}
	if triggeredBy != models.PolicyAuditTriggerManual && triggeredBy != models.PolicyAuditTriggerScheduled {
		triggeredBy = models.PolicyAuditTriggerManual
	}

	if !acquireProcessLock() {
		return nil, ErrAlreadyRunning
	}

	now := time.Now()
	run := &models.PolicyAuditRun{
		Status:            models.PolicyAuditStatusRunning,
		TriggeredBy:       triggeredBy,
		TriggeredByUserID: userID,
		StartedAt:         now,
	}
	if err := s.repo.CreateRun(ctx, run); err != nil {
		releaseProcessLock()
		return nil, err
	}

	go s.execute(run.ID)
	return run, nil
}

func (s *Service) execute(runID uint) {
	defer releaseProcessLock()

	ctx := context.Background()
	findings, scanErr := Scan(ctx, s.opts)

	run, err := s.repo.GetRun(ctx, uint64(runID))
	if err != nil {
		logger.Error("content audit run disappeared", zap.Uint("run_id", runID), zap.Error(err))
		return
	}

	now := time.Now()
	run.FinishedAt = &now

	if scanErr != nil {
		message := scanErr.Error()
		run.Status = models.PolicyAuditStatusFailed
		run.ErrorMessage = &message
		if err := s.repo.UpdateRun(ctx, run); err != nil {
			logger.Error("failed to mark content audit failed", zap.Uint("run_id", runID), zap.Error(err))
		}
		return
	}

	dbFindings := make([]models.PolicyAuditFinding, 0, len(findings))
	for _, finding := range findings {
		dbFindings = append(dbFindings, models.PolicyAuditFinding{
			RunID:             run.ID,
			ContentType:       finding.Type,
			ContentID:         finding.ID,
			Title:             finding.Title,
			Risk:              finding.Risk,
			Reason:            finding.Reason,
			URL:               finding.URL,
			RecommendedAction: finding.RecommendedAction,
		})
	}

	if err := s.repo.ReplaceFindings(ctx, run.ID, dbFindings); err != nil {
		message := err.Error()
		run.Status = models.PolicyAuditStatusFailed
		run.ErrorMessage = &message
		if updateErr := s.repo.UpdateRun(ctx, run); updateErr != nil {
			logger.Error("failed to mark content audit storage failure", zap.Uint("run_id", runID), zap.Error(updateErr))
		}
		return
	}

	run.Status = models.PolicyAuditStatusCompleted
	run.FindingsCount = len(dbFindings)
	run.ErrorMessage = nil
	if err := s.repo.UpdateRun(ctx, run); err != nil {
		logger.Error("failed to mark content audit completed", zap.Uint("run_id", runID), zap.Error(err))
	}
}

func (s *Service) ListRuns(ctx context.Context, limit, offset int) ([]models.PolicyAuditRun, int64, error) {
	return s.repo.ListRuns(ctx, normalizeLimit(limit), normalizeOffset(offset))
}

func (s *Service) GetRun(ctx context.Context, id uint64) (*models.PolicyAuditRun, error) {
	return s.repo.GetRun(ctx, id)
}

func (s *Service) ListFindings(ctx context.Context, runID uint64, risk, contentType, search string, limit, offset int) ([]models.PolicyAuditFinding, int64, error) {
	return s.repo.ListFindings(ctx, runID, risk, contentType, search, normalizeLimit(limit), normalizeOffset(offset))
}

func (s *Service) ExportCSV(ctx context.Context, runID uint64, w io.Writer) error {
	if _, err := s.repo.GetRun(ctx, runID); err != nil {
		return err
	}
	dbFindings, err := s.repo.AllFindings(ctx, runID)
	if err != nil {
		return err
	}

	findings := make([]Finding, 0, len(dbFindings))
	for _, finding := range dbFindings {
		findings = append(findings, Finding{
			Type:              finding.ContentType,
			ID:                finding.ContentID,
			Title:             finding.Title,
			Risk:              finding.Risk,
			Reason:            finding.Reason,
			URL:               finding.URL,
			RecommendedAction: finding.RecommendedAction,
		})
	}
	return WriteCSV(w, findings)
}

func (s *Service) StartScheduler(interval, initialDelay time.Duration) {
	if interval <= 0 {
		return
	}

	go func() {
		if initialDelay > 0 {
			timer := time.NewTimer(initialDelay)
			<-timer.C
		}

		for {
			if _, err := s.Start(context.Background(), models.PolicyAuditTriggerScheduled, nil); err != nil && !errors.Is(err, ErrAlreadyRunning) {
				logger.Warn("scheduled content audit failed to start", zap.Error(err))
			}
			timer := time.NewTimer(interval)
			<-timer.C
		}
	}()
}

func acquireProcessLock() bool {
	processLock.Lock()
	defer processLock.Unlock()
	if processLock.running {
		return false
	}
	processLock.running = true
	return true
}

func releaseProcessLock() {
	processLock.Lock()
	processLock.running = false
	processLock.Unlock()
}

func normalizeLimit(limit int) int {
	if limit <= 0 {
		return 20
	}
	if limit > 200 {
		return 200
	}
	return limit
}

func normalizeOffset(offset int) int {
	if offset < 0 {
		return 0
	}
	return offset
}
