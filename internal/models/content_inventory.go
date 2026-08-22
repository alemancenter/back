package models

import "time"

const (
	EditorialDecisionUnclassified = "unclassified"
	EditorialDecisionKeep         = "keep"
	EditorialDecisionImprove      = "improve"
	EditorialDecisionNoindex      = "noindex"
	EditorialDecisionMerge301     = "merge_301"
)

// ContentEditorialDecision is an append-only human review decision for one
// article or post. Keeping history makes every classification auditable and
// lets an Admin/Super Admin supersede a previous choice without deleting who
// made the earlier decision or when it was made.
type ContentEditorialDecision struct {
	ID                uint      `gorm:"primaryKey" json:"id"`
	CountryCode       string    `gorm:"type:varchar(10);not null;index:idx_editorial_content,priority:1;index" json:"country_code"`
	ContentType       string    `gorm:"type:varchar(30);not null;index:idx_editorial_content,priority:2;index" json:"content_type"`
	ContentID         uint      `gorm:"not null;index:idx_editorial_content,priority:3;index" json:"content_id"`
	Decision          string    `gorm:"type:varchar(30);not null;index" json:"decision"`
	TargetContentType *string   `gorm:"type:varchar(30)" json:"target_content_type,omitempty"`
	TargetContentID   *uint     `gorm:"index" json:"target_content_id,omitempty"`
	Notes             string    `gorm:"type:text" json:"notes,omitempty"`
	CreatedByUserID   *uint     `gorm:"index" json:"created_by_user_id,omitempty"`
	CreatedAt         time.Time `gorm:"index:idx_editorial_content,priority:4" json:"created_at"`
}

func (ContentEditorialDecision) TableName() string { return "content_editorial_decisions" }
