package models

import "time"

// ChatSession stores one support conversation. It supports guests and logged-in users.
type ChatSession struct {
	ID            uint          `gorm:"primaryKey" json:"id"`
	UserID        *uint         `gorm:"index" json:"user_id,omitempty"`
	GuestID       string        `gorm:"type:varchar(128);index" json:"guest_id"`
	CountryCode   string        `gorm:"type:varchar(10);default:jo;index" json:"country_code"`
	Status        string        `gorm:"type:varchar(30);default:open;index" json:"status"`
	LastIntent    string        `gorm:"type:varchar(80);index" json:"last_intent"`
	CurrentIntent string        `gorm:"type:varchar(80);index" json:"current_intent"`
	CurrentStep   string        `gorm:"type:varchar(80);index" json:"current_step"`
	ContextData   string        `gorm:"type:json" json:"context_data,omitempty"`
	CreatedAt     time.Time     `json:"created_at"`
	UpdatedAt     time.Time     `json:"updated_at"`
	ClosedAt      *time.Time    `json:"closed_at,omitempty"`
	Messages      []ChatMessage `gorm:"foreignKey:SessionID" json:"messages,omitempty"`
}

func (ChatSession) TableName() string { return "chat_sessions" }

// ChatMessage stores user and assistant messages with safety and source metadata.
type ChatMessage struct {
	ID         uint      `gorm:"primaryKey" json:"id"`
	SessionID  uint      `gorm:"index;not null" json:"session_id"`
	Role       string    `gorm:"type:varchar(30);not null;index" json:"role"`
	Message    string    `gorm:"type:text;not null" json:"message"`
	Intent     string    `gorm:"type:varchar(80);index" json:"intent"`
	Confidence float64   `gorm:"type:decimal(5,2);default:0" json:"confidence"`
	SourceType string    `gorm:"type:varchar(60)" json:"source_type"`
	Metadata   string    `gorm:"type:json;not null" json:"metadata,omitempty"`
	IPAddress  string    `gorm:"type:varchar(80);index" json:"ip_address,omitempty"`
	UserAgent  string    `gorm:"type:varchar(500)" json:"user_agent,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

func (ChatMessage) TableName() string { return "chat_messages" }

// ChatKnowledgeBase is the editable support knowledge base used before any AI fallback.
type ChatKnowledgeBase struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	Title       string    `gorm:"type:varchar(255);not null" json:"title"`
	Question    string    `gorm:"type:text;not null" json:"question"`
	Answer      string    `gorm:"type:text;not null" json:"answer"`
	Category    string    `gorm:"type:varchar(80);index" json:"category"`
	Keywords    string    `gorm:"type:text" json:"keywords"`
	CountryCode string    `gorm:"type:varchar(10);default:all;index" json:"country_code"`
	IsActive    bool      `gorm:"default:true;index" json:"is_active"`
	Priority    int       `gorm:"default:10;index" json:"priority"`
	CreatedBy   *uint     `gorm:"index" json:"created_by,omitempty"`
	UpdatedBy   *uint     `gorm:"index" json:"updated_by,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func (ChatKnowledgeBase) TableName() string { return "chat_knowledge_base" }

// ChatFeedback helps the admin identify useful and weak answers.
type ChatFeedback struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	MessageID uint      `gorm:"index;not null" json:"message_id"`
	Rating    string    `gorm:"type:varchar(30);not null;index" json:"rating"`
	Comment   string    `gorm:"type:text" json:"comment"`
	CreatedAt time.Time `json:"created_at"`
}

func (ChatFeedback) TableName() string { return "chat_feedback" }

// ChatAIUsage records guarded AI calls made by the chatbot for cost and safety review.
type ChatAIUsage struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	SessionID   uint      `gorm:"index" json:"session_id"`
	MessageID   uint      `gorm:"index" json:"message_id"`
	Provider    string    `gorm:"type:varchar(60);index" json:"provider"`
	Model       string    `gorm:"type:varchar(160);index" json:"model"`
	Intent      string    `gorm:"type:varchar(80);index" json:"intent"`
	Status      string    `gorm:"type:varchar(30);index" json:"status"`
	Reason      string    `gorm:"type:varchar(120)" json:"reason,omitempty"`
	Tokens      int       `gorm:"default:0" json:"tokens"`
	CountryCode string    `gorm:"type:varchar(10);index" json:"country_code"`
	CreatedAt   time.Time `json:"created_at"`
}

func (ChatAIUsage) TableName() string { return "chat_ai_usage" }
