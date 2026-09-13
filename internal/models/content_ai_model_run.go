package models

import "time"

// ContentAIModelRun records cost/usage/reliability for every LLM call made through
// services.AIService, regardless of caller. Split out of the removed content-audit
// subsystem because it is also written by the teacher-subscription AI generation
// feature (services/ai_service.go's recordContentAIModelRun), which is unrelated to and
// unaffected by that removal.
type ContentAIModelRun struct {
	ID               uint      `gorm:"primaryKey" json:"id"`
	JobID            string    `gorm:"type:varchar(80);index" json:"job_id,omitempty"`
	JobItemID        *uint     `gorm:"index" json:"job_item_id,omitempty"`
	ContentType      string    `gorm:"type:varchar(30);index" json:"content_type,omitempty"`
	ContentID        string    `gorm:"type:varchar(80);index" json:"content_id,omitempty"`
	CountryCode      string    `gorm:"type:varchar(10);index" json:"country_code,omitempty"`
	TaskType         string    `gorm:"type:varchar(60);not null;index" json:"task_type"`
	ModelStrategy    string    `gorm:"type:varchar(40);index" json:"model_strategy,omitempty"`
	Provider         string    `gorm:"type:varchar(60);index" json:"provider,omitempty"`
	Model            string    `gorm:"type:varchar(180);not null;index" json:"model"`
	Role             string    `gorm:"type:varchar(40);index" json:"role,omitempty"`
	InputTokens      int       `gorm:"not null;default:0" json:"input_tokens"`
	OutputTokens     int       `gorm:"not null;default:0" json:"output_tokens"`
	EstimatedCostUSD float64   `gorm:"type:decimal(12,6);not null;default:0" json:"estimated_cost_usd"`
	DurationMS       int64     `gorm:"not null;default:0" json:"duration_ms"`
	Status           string    `gorm:"type:varchar(30);not null;index" json:"status"`
	Error            string    `gorm:"type:text" json:"error,omitempty"`
	CreatedAt        time.Time `gorm:"index" json:"created_at"`
}

func (ContentAIModelRun) TableName() string { return "content_ai_model_runs" }
