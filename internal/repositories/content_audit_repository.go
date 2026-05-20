package repositories

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/alemancenter/fiber-api/internal/database"
	"github.com/alemancenter/fiber-api/internal/models"
	"gorm.io/gorm"
)

type ContentAuditRepository interface {
	CreateRun(ctx context.Context, run *models.PolicyAuditRun) error
	UpdateRun(ctx context.Context, run *models.PolicyAuditRun) error
	GetRun(ctx context.Context, id uint64) (*models.PolicyAuditRun, error)
	ListRuns(ctx context.Context, limit, offset int) ([]models.PolicyAuditRun, int64, error)
	ReplaceFindings(ctx context.Context, runID uint, findings []models.PolicyAuditFinding) error
	ListFindings(ctx context.Context, runID uint64, risk, contentType, search string, limit, offset int) ([]models.PolicyAuditFinding, int64, error)
	AllFindings(ctx context.Context, runID uint64) ([]models.PolicyAuditFinding, error)
	SaveAIDecision(ctx context.Context, decision *models.ContentAIDecision) error
	GetAIDecision(ctx context.Context, id uint64) (*models.ContentAIDecision, error)
	LatestAIDecision(ctx context.Context, contentType, contentID, countryCode string) (*models.ContentAIDecision, error)
	SaveFixPreview(ctx context.Context, preview *models.ContentAIFixPreview) error
	GetFixPreview(ctx context.Context, id uint64) (*models.ContentAIFixPreview, error)
	LatestFixPreviewByDecision(ctx context.Context, decisionID uint64) (*models.ContentAIFixPreview, error)
	UpdateFixPreview(ctx context.Context, preview *models.ContentAIFixPreview) error
	CreateApprovalLog(ctx context.Context, log *models.ContentAIApprovalLog) error
}

type contentAuditRepository struct{}

func NewContentAuditRepository() ContentAuditRepository {
	return &contentAuditRepository{}
}

func (r *contentAuditRepository) CreateRun(ctx context.Context, run *models.PolicyAuditRun) error {
	return database.DB().WithContext(ctx).Create(run).Error
}

func (r *contentAuditRepository) UpdateRun(ctx context.Context, run *models.PolicyAuditRun) error {
	return database.DB().WithContext(ctx).Save(run).Error
}

func (r *contentAuditRepository) GetRun(ctx context.Context, id uint64) (*models.PolicyAuditRun, error) {
	var run models.PolicyAuditRun
	if err := database.DB().WithContext(ctx).First(&run, id).Error; err != nil {
		return nil, err
	}
	return &run, nil
}

func (r *contentAuditRepository) ListRuns(ctx context.Context, limit, offset int) ([]models.PolicyAuditRun, int64, error) {
	var runs []models.PolicyAuditRun
	var total int64
	query := database.DB().WithContext(ctx).Model(&models.PolicyAuditRun{})
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if err := query.Order("started_at DESC").Limit(limit).Offset(offset).Find(&runs).Error; err != nil {
		return nil, 0, err
	}
	return runs, total, nil
}

func (r *contentAuditRepository) ReplaceFindings(ctx context.Context, runID uint, findings []models.PolicyAuditFinding) error {
	return database.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("run_id = ?", runID).Delete(&models.PolicyAuditFinding{}).Error; err != nil {
			return err
		}
		if len(findings) == 0 {
			return nil
		}
		return tx.CreateInBatches(findings, 500).Error
	})
}

func (r *contentAuditRepository) ListFindings(ctx context.Context, runID uint64, risk, contentType, search string, limit, offset int) ([]models.PolicyAuditFinding, int64, error) {
	var findings []models.PolicyAuditFinding
	var total int64
	query := database.DB().WithContext(ctx).Model(&models.PolicyAuditFinding{}).Where("run_id = ?", runID)

	if risk != "" {
		query = query.Where("risk = ?", risk)
	}
	if contentType != "" {
		query = query.Where("content_type = ?", contentType)
	}
	if search != "" {
		like := "%" + search + "%"
		query = query.Where("(content_id LIKE ? OR title LIKE ? OR reason LIKE ? OR url LIKE ?)", like, like, like, like)
	}

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if err := query.Order("risk ASC, content_type ASC, content_id ASC").Limit(limit).Offset(offset).Find(&findings).Error; err != nil {
		return nil, 0, err
	}
	return findings, total, nil
}

func (r *contentAuditRepository) AllFindings(ctx context.Context, runID uint64) ([]models.PolicyAuditFinding, error) {
	var findings []models.PolicyAuditFinding
	err := database.DB().WithContext(ctx).
		Where("run_id = ?", runID).
		Order("risk ASC, content_type ASC, content_id ASC").
		Find(&findings).Error
	return findings, err
}

func (r *contentAuditRepository) SaveAIDecision(ctx context.Context, decision *models.ContentAIDecision) error {
	decision.CountryCode, decision.ContentID = normalizeAIContentRef(decision.CountryCode, decision.ContentID)
	return database.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Omit("Issues", "Suggestions").Create(decision).Error; err != nil {
			return err
		}
		for i := range decision.Issues {
			decision.Issues[i].DecisionID = decision.ID
		}
		for i := range decision.Suggestions {
			decision.Suggestions[i].DecisionID = decision.ID
		}
		if len(decision.Issues) > 0 {
			if err := tx.Create(&decision.Issues).Error; err != nil {
				return err
			}
		}
		if len(decision.Suggestions) > 0 {
			if err := tx.Create(&decision.Suggestions).Error; err != nil {
				return err
			}
		}
		return tx.Preload("Issues").Preload("Suggestions").First(decision, decision.ID).Error
	})
}

func (r *contentAuditRepository) GetAIDecision(ctx context.Context, id uint64) (*models.ContentAIDecision, error) {
	var decision models.ContentAIDecision
	if err := database.DB().WithContext(ctx).Preload("Issues").Preload("Suggestions").First(&decision, id).Error; err != nil {
		return nil, err
	}
	return &decision, nil
}

func (r *contentAuditRepository) LatestAIDecision(ctx context.Context, contentType, contentID, countryCode string) (*models.ContentAIDecision, error) {
	var decision models.ContentAIDecision
	countryCode, contentID = normalizeAIContentRef(countryCode, contentID)
	query := database.DB().WithContext(ctx).Preload("Issues").Preload("Suggestions").
		Where("content_type = ? AND content_id = ?", contentType, contentID)
	if countryCode != "" {
		query = query.Where("country_code = ?", countryCode)
	}
	if err := query.Order("created_at DESC").First(&decision).Error; err != nil {
		return nil, err
	}
	return &decision, nil
}

func (r *contentAuditRepository) SaveFixPreview(ctx context.Context, preview *models.ContentAIFixPreview) error {
	preview.CountryCode, preview.ContentID = normalizeAIContentRef(preview.CountryCode, preview.ContentID)
	return database.DB().WithContext(ctx).Create(preview).Error
}

func (r *contentAuditRepository) GetFixPreview(ctx context.Context, id uint64) (*models.ContentAIFixPreview, error) {
	var preview models.ContentAIFixPreview
	if err := database.DB().WithContext(ctx).First(&preview, id).Error; err != nil {
		return nil, err
	}
	return &preview, nil
}

func (r *contentAuditRepository) LatestFixPreviewByDecision(ctx context.Context, decisionID uint64) (*models.ContentAIFixPreview, error) {
	var preview models.ContentAIFixPreview
	if err := database.DB().WithContext(ctx).
		Where("decision_id = ? AND status = ?", decisionID, models.AIFixStatusPreviewed).
		Order("created_at DESC, id DESC").
		First(&preview).Error; err != nil {
		return nil, err
	}
	return &preview, nil
}

func (r *contentAuditRepository) UpdateFixPreview(ctx context.Context, preview *models.ContentAIFixPreview) error {
	return database.DB().WithContext(ctx).Save(preview).Error
}

func (r *contentAuditRepository) CreateApprovalLog(ctx context.Context, log *models.ContentAIApprovalLog) error {
	return database.DB().WithContext(ctx).Create(log).Error
}

func normalizeAIContentRef(countryCode, contentID string) (string, string) {
	cc := sanitizeAIShortCountry(countryCode)
	raw := strings.TrimSpace(contentID)
	var id uint64
	if strings.Contains(raw, ":") {
		parts := strings.Split(raw, ":")
		prefix := strings.Join(parts[:len(parts)-1], ":")
		if cc == "" {
			cc = sanitizeAIShortCountry(prefix)
		}
		id, _ = strconv.ParseUint(parts[len(parts)-1], 10, 64)
	} else {
		id, _ = strconv.ParseUint(raw, 10, 64)
	}
	if cc == "" {
		cc = "jo"
	}
	return cc, fmt.Sprintf("%s:%d", cc, id)
}

func sanitizeAIShortCountry(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	for _, code := range []string{"jo", "sa", "eg", "ps"} {
		if value == code || strings.HasSuffix(value, "_"+code) || strings.HasSuffix(value, "-"+code) || strings.Contains(value, "_"+code+":") {
			return code
		}
	}
	return ""
}
