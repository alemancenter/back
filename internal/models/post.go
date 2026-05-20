package models

import (
	"time"
)

// Post represents a forum/news post
type Post struct {
	ID              uint      `gorm:"primaryKey" json:"id"`
	CategoryID      *uint     `gorm:"index" json:"category_id,omitempty"`
	Title           string    `gorm:"type:varchar(500);not null" json:"title"`
	Slug            string    `gorm:"type:varchar(550);unique" json:"slug"`
	Content         string    `gorm:"type:longtext" json:"content"`
	Image           *string   `gorm:"type:varchar(500)" json:"image,omitempty"`
	Alt             *string   `gorm:"type:varchar(255)" json:"alt,omitempty"`
	IsActive        bool      `gorm:"default:true" json:"is_active"`
	IsFeatured      bool      `gorm:"default:false" json:"is_featured"`
	Views           int       `gorm:"default:0" json:"views"`
	Country         string    `gorm:"type:varchar(10);default:'jo'" json:"country"`
	Keywords        *string   `gorm:"type:text" json:"keywords,omitempty"`
	MetaDescription *string   `gorm:"type:varchar(500)" json:"meta_description,omitempty"`
	AuthorID        *uint     `gorm:"index" json:"author_id,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`

	// Relationships
	Category    *Category `gorm:"foreignKey:CategoryID" json:"category,omitempty"`
	Author      *User     `gorm:"foreignKey:AuthorID" json:"author,omitempty"`
	Comments    []Comment `gorm:"polymorphic:Commentable" json:"comments,omitempty"`
	KeywordsRel []Keyword `gorm:"many2many:post_keyword" json:"keywords_rel,omitempty"`
	Files       []File    `gorm:"foreignKey:PostID" json:"files,omitempty"`

	// Computed
	ImageURL string `gorm:"-" json:"image_url,omitempty"`
}

// PostFilter is used to filter posts
type PostFilter struct {
	CategoryID *uint
	Search     string
	Featured   string
	IsActive   *bool
}

func (Post) TableName() string { return "posts" }

// PostKeyword is the pivot table
type PostKeyword struct {
	PostID    uint `gorm:"primaryKey"`
	KeywordID uint `gorm:"primaryKey"`
}

func (PostKeyword) TableName() string { return "post_keyword" }
