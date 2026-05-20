package models

import (
	"encoding/json"
	"time"
)

// SchoolClass represents a grade level
type SchoolClass struct {
	ID         uint      `gorm:"primaryKey" json:"id"`
	GradeName  string    `gorm:"column:grade_name;type:varchar(255)" json:"grade_name"`
	GradeLevel int       `gorm:"column:grade_level" json:"grade_level"`
	CountryID  *uint     `json:"country_id,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`

	// subjects.grade_level is FK to school_classes.id
	Subjects  []Subject  `gorm:"foreignKey:GradeLevel" json:"subjects,omitempty"`
	Semesters []Semester `gorm:"foreignKey:GradeLevel" json:"semesters,omitempty"`
}

func (SchoolClass) TableName() string { return "school_classes" }

// Subject represents a school subject (Math, English, etc.)
type Subject struct {
	ID            uint      `gorm:"primaryKey" json:"id"`
	SubjectName   string    `gorm:"column:subject_name;type:varchar(255)" json:"subject_name"`
	GradeLevel    uint      `gorm:"column:grade_level;not null;index" json:"grade_level"`
	ArticlesCount int64     `gorm:"-" json:"articles_count"`
	FilesCount    int64     `gorm:"-" json:"files_count"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`

	// grade_level is FK to school_classes.id
	SchoolClass *SchoolClass `gorm:"foreignKey:GradeLevel" json:"school_class,omitempty"`
}

func (Subject) TableName() string { return "subjects" }

// Semester represents an academic term
type Semester struct {
	ID           uint      `gorm:"primaryKey" json:"id"`
	SemesterName string    `gorm:"column:semester_name;type:varchar(255)" json:"semester_name"`
	GradeLevel   uint      `gorm:"column:grade_level;not null;index" json:"grade_level"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`

	// grade_level is FK to school_classes.id
	SchoolClass *SchoolClass `gorm:"foreignKey:GradeLevel" json:"school_class,omitempty"`
}

func (Semester) TableName() string { return "semesters" }

// Category represents a post category
type Category struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Name      string    `gorm:"type:varchar(255);not null" json:"name"`
	Slug      string    `gorm:"type:varchar(300);unique" json:"slug"`
	ParentID  *uint     `json:"parent_id,omitempty"`
	Icon      *string   `gorm:"type:varchar(500)" json:"icon,omitempty"`
	Image     *string   `gorm:"type:varchar(500)" json:"image,omitempty"`
	IsActive  bool      `gorm:"default:true" json:"is_active"`
	Country   string    `gorm:"type:varchar(10)" json:"country"`
	Depth     int       `gorm:"default:0" json:"depth"`
	IconImage *string   `gorm:"column:icon_image;type:varchar(500)" json:"icon_image,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	Posts []Post `gorm:"foreignKey:CategoryID" json:"posts,omitempty"`
}

func (Category) TableName() string { return "categories" }

// Keyword represents a tag/keyword used in articles and posts
type Keyword struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Keyword   string    `gorm:"column:keyword;type:varchar(255);unique;not null" json:"keyword"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	Articles []Article `gorm:"many2many:article_keyword;" json:"articles,omitempty"`
	Posts    []Post    `gorm:"many2many:post_keyword;" json:"posts,omitempty"`
}

func (Keyword) TableName() string { return "keywords" }

// File represents an attached document/file
type File struct {
	ID           uint      `gorm:"primaryKey" json:"id"`
	ArticleID    *uint     `gorm:"index" json:"article_id,omitempty"`
	PostID       *uint     `gorm:"index" json:"post_id,omitempty"`
	FilePath     string    `gorm:"type:varchar(500);not null" json:"file_path"`
	FileType     string    `gorm:"type:varchar(50)" json:"file_type"`
	FileCategory *string   `gorm:"type:varchar(100)" json:"file_category,omitempty"`
	FileName     string    `gorm:"type:varchar(255)" json:"file_name"`
	FileSize     int64     `json:"file_size"`
	MimeType     string    `gorm:"type:varchar(100)" json:"mime_type"`
	ViewCount    int       `gorm:"column:views_count;default:0" json:"view_count"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`

	Article *Article `gorm:"foreignKey:ArticleID" json:"article,omitempty"`
	Post    *Post    `gorm:"foreignKey:PostID" json:"post,omitempty"`
}

func (File) TableName() string { return "files" }

// Comment represents a polymorphic comment
const (
	CommentStatusPending  = "pending"
	CommentStatusApproved = "approved"
	CommentStatusRejected = "rejected"
)

func IsValidCommentStatus(status string) bool {
	switch status {
	case CommentStatusPending, CommentStatusApproved, CommentStatusRejected:
		return true
	default:
		return false
	}
}

type Comment struct {
	ID              uint      `gorm:"primaryKey" json:"id"`
	Body            string    `gorm:"type:text;not null" json:"body"`
	UserID          uint      `gorm:"not null" json:"user_id"`
	CommentableID   uint      `gorm:"not null" json:"commentable_id"`
	CommentableType string    `gorm:"type:varchar(255);not null" json:"commentable_type"`
	Database        string    `gorm:"type:varchar(10);default:'jo'" json:"database"`
	Status          string    `gorm:"type:varchar(20);not null;default:'pending';index" json:"status"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`

	User      *User      `gorm:"foreignKey:UserID" json:"user,omitempty"`
	Reactions []Reaction `gorm:"foreignKey:CommentID" json:"reactions,omitempty"`
}

func (Comment) TableName() string { return "comments" }

// Reaction represents an emoji reaction on a comment
type Reaction struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	CommentID uint      `gorm:"not null;index" json:"comment_id"`
	UserID    uint      `gorm:"not null;index" json:"user_id"`
	Emoji     string    `gorm:"type:varchar(20)" json:"emoji"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	User    *User    `gorm:"foreignKey:UserID" json:"user,omitempty"`
	Comment *Comment `gorm:"foreignKey:CommentID" json:"comment,omitempty"`
}

func (Reaction) TableName() string { return "reactions" }

// Event represents a calendar event
type Event struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	Title       string    `gorm:"type:varchar(500);not null" json:"title"`
	Description *string   `gorm:"type:text" json:"description,omitempty"`
	EventDate   time.Time `gorm:"column:event_date" json:"event_date"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func (Event) TableName() string { return "events" }

// Setting represents an application setting key-value pair
type Setting struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Key       string    `gorm:"type:varchar(255);unique;not null" json:"key"`
	Value     *string   `gorm:"type:longtext" json:"value,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (Setting) TableName() string { return "settings" }

// Notification represents a user notification
type Notification struct {
	ID             string     `gorm:"type:char(36);primaryKey" json:"id"`
	Type           string     `gorm:"type:varchar(255);not null" json:"type"`
	NotifiableType string     `gorm:"type:varchar(255);not null" json:"-"`
	NotifiableID   uint       `gorm:"not null" json:"notifiable_id"`
	Data           json.RawMessage `gorm:"type:json" json:"data"`
	ReadAt         *time.Time `json:"read_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

func (Notification) TableName() string { return "notifications" }

// Message represents a direct message between users
// Conversation represents a private or public messaging thread.
type Conversation struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Title     *string   `gorm:"type:varchar(255)" json:"title,omitempty"`
	Type      string    `gorm:"type:enum('private','public');default:'private'" json:"type"`
	User1ID   uint      `gorm:"not null;index" json:"user1_id"`
	User2ID   uint      `gorm:"not null;index" json:"user2_id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	User1 *User `gorm:"foreignKey:User1ID" json:"user1,omitempty"`
	User2 *User `gorm:"foreignKey:User2ID" json:"user2,omitempty"`
}

func (Conversation) TableName() string { return "conversations" }

// Message represents a single message within a conversation.
type Message struct {
	ID             uint      `gorm:"primaryKey" json:"id"`
	ConversationID uint      `gorm:"not null;index" json:"conversation_id"`
	SenderID       uint      `gorm:"not null;index" json:"sender_id"`
	Subject        string    `gorm:"type:varchar(255)" json:"subject"`
	Body           string    `gorm:"type:text;not null" json:"body"`
	Read           bool      `gorm:"column:read;default:false" json:"read"`
	IsImportant    bool      `gorm:"default:false" json:"is_important"`
	IsDraft        bool      `gorm:"default:false" json:"is_draft"`
	IsDeleted      bool      `gorm:"default:false" json:"is_deleted"`
	IsChat         bool      `gorm:"default:false" json:"is_chat"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`

	Sender       *User         `gorm:"foreignKey:SenderID" json:"sender,omitempty"`
	Conversation *Conversation `gorm:"foreignKey:ConversationID" json:"conversation,omitempty"`

	// Recipient is populated at query time from the conversation, not stored in DB.
	Recipient *User `gorm:"-" json:"recipient,omitempty"`
}

func (Message) TableName() string { return "messages" }
