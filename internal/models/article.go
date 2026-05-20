package models

import (
	"time"
)

// Article represents an educational content article
type Article struct {
	ID              uint       `gorm:"primaryKey" json:"id"`
	Title           string     `gorm:"type:varchar(500);not null" json:"title"`
	Content         string     `gorm:"type:longtext" json:"content"`
	GradeLevel      *string    `gorm:"type:varchar(50)" json:"grade_level,omitempty"`
	SubjectID       *uint      `gorm:"index" json:"subject_id,omitempty"`
	SemesterID      *uint      `gorm:"index" json:"semester_id,omitempty"`
	AuthorID        *uint      `gorm:"index" json:"author_id,omitempty"`
	MetaDescription *string    `gorm:"type:varchar(500)" json:"meta_description,omitempty"`
	Status          int8       `gorm:"default:0" json:"status"` // 0=draft, 1=published
	VisitCount      int        `gorm:"default:0" json:"visit_count"`
	PublishedAt     *time.Time `json:"published_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`

	// Relationships
	Subject     *Subject     `gorm:"foreignKey:SubjectID" json:"subject,omitempty"`
	Semester    *Semester    `gorm:"foreignKey:SemesterID" json:"semester,omitempty"`
	SchoolClass *SchoolClass `gorm:"-" json:"school_class,omitempty"`
	Files       []File       `gorm:"foreignKey:ArticleID" json:"files,omitempty"`
	KeywordsRel []Keyword    `gorm:"many2many:article_keyword" json:"keywords_rel,omitempty"`
	Comments    []Comment    `gorm:"polymorphic:Commentable" json:"comments,omitempty"`
}

func (Article) TableName() string { return "articles" }

// IsPublished returns true if the article is published
func (a *Article) IsPublished() bool { return a.Status == 1 }

// ArticleKeyword is the pivot table
type ArticleKeyword struct {
	ArticleID uint `gorm:"primaryKey"`
	KeywordID uint `gorm:"primaryKey"`
}

func (ArticleKeyword) TableName() string { return "article_keyword" }

type ArticleFilter struct {
	Status       *int
	GradeLevel   string
	SubjectID    *uint
	SemesterID   *uint
	FileCategory string
	Query        string
	Order        string
}
