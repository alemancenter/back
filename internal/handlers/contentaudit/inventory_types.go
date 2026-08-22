package contentaudit

import (
	"time"

	"github.com/imanjo/fiber-api/internal/models"
)

type inventorySimilaritySignal struct {
	Kind       string  `json:"kind,omitempty"`
	Similarity float64 `json:"similarity,omitempty"`
	ClusterID  string  `json:"cluster_id,omitempty"`
}

type inventoryItem struct {
	ID                uint                             `json:"id"`
	Type              string                           `json:"type"`
	Title             string                           `json:"title"`
	Published         bool                             `json:"published"`
	Visits            int                              `json:"visits"`
	WordCount         int                              `json:"word_count"`
	FilesCount        int                              `json:"files_count"`
	ShouldIndex       bool                             `json:"should_index"`
	AdsEligible       bool                             `json:"ads_eligible"`
	Audited           bool                             `json:"audited"`
	AuditDecision     string                           `json:"audit_decision"`
	Risk              string                           `json:"risk"`
	Corrupted         bool                             `json:"corrupted"`
	Similarity        inventorySimilaritySignal        `json:"similarity"`
	PriorityScore     int                              `json:"priority_score"`
	PriorityReasons   []string                         `json:"priority_reasons"`
	SuggestedDecision string                           `json:"suggested_decision"`
	Editorial         *models.ContentEditorialDecision `json:"editorial,omitempty"`
	PublicURL         string                           `json:"public_url"`
	EditURL           string                           `json:"edit_url"`
	CreatedAt         time.Time                        `json:"created_at"`
	UpdatedAt         time.Time                        `json:"updated_at"`
}

type inventorySummary struct {
	TotalContent     int `json:"total_content"`
	PriorityQueue    int `json:"priority_queue"`
	Classified       int `json:"classified"`
	Unclassified     int `json:"unclassified"`
	Keep             int `json:"keep"`
	Improve          int `json:"improve"`
	Noindex          int `json:"noindex"`
	Merge301         int `json:"merge_301"`
	Corrupted        int `json:"corrupted"`
	ExactDuplicate   int `json:"exact_duplicate"`
	NearDuplicate    int `json:"near_duplicate"`
	TemplateSimilar  int `json:"template_similar"`
	NonIndexable     int `json:"non_indexable"`
	AdsEligible      int `json:"ads_eligible"`
}

type inventorySource struct {
	ID              uint
	Type            string
	Title           string
	Content         string
	MetaDescription string
	Keywords        string
	Published       bool
	Visits          int
	CreatedAt       time.Time
	UpdatedAt       time.Time
	FilesCount      int
}
