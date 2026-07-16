package models

import "time"

// SubscriptionPlan defines a paid plan. Initial MVP: one plan named
// "اشتراك المعلم للفصل الدراسي" with code teacher_semester.
type SubscriptionPlan struct {
	ID                uint      `gorm:"primaryKey" json:"id"`
	Code              string    `gorm:"type:varchar(80);uniqueIndex;not null" json:"code"`
	Name              string    `gorm:"type:varchar(255);not null" json:"name"`
	Description       string    `gorm:"type:text" json:"description"`
	TargetAudience    string    `gorm:"type:varchar(80);default:'teacher';index" json:"target_audience"`
	PriceJOD          float64   `gorm:"type:decimal(10,3);not null;default:25.000" json:"price_jod"`
	Currency          string    `gorm:"type:varchar(10);not null;default:'JOD'" json:"currency"`
	DurationDays      int       `gorm:"not null;default:150" json:"duration_days"`
	DeviceLimit       int       `gorm:"not null;default:2" json:"device_limit"`
	DownloadLimit     int       `gorm:"not null;default:300" json:"download_limit"`
	AIGenerationLimit int       `gorm:"not null;default:100" json:"ai_generation_limit"`
	ExportLimit       int       `gorm:"not null;default:100" json:"export_limit"`
	FeaturesJSON      string    `gorm:"type:json" json:"features_json"`
	PermissionsJSON   string    `gorm:"type:json" json:"permissions_json"`
	LimitsJSON        string    `gorm:"type:json" json:"limits_json"`
	SortOrder         int       `gorm:"not null;default:10;index" json:"sort_order"`
	IsActive          bool      `gorm:"not null;default:true;index" json:"is_active"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

func (SubscriptionPlan) TableName() string { return "subscription_plans" }

// TeacherProfile stores teacher-specific optional profile data.
type TeacherProfile struct {
	ID uint `gorm:"primaryKey" json:"id"`
	UserID uint `gorm:"not null;uniqueIndex" json:"user_id"`
	// Subject is the legacy single-subject field, kept for backward
	// compatibility with any code/reports that still read it directly. It is
	// always kept in sync with the first entry of Subjects.
	Subject string `gorm:"type:varchar(255)" json:"subject"`
	// Subjects stores up to 3 subjects as a JSON array string, e.g.
	// ["رياضيات","علوم","لغة عربية"]. This is the canonical field going
	// forward — teachers may subscribe to up to 3 subjects instead of one.
	Subjects  string    `gorm:"type:varchar(600)" json:"subjects"`
	School    string    `gorm:"type:varchar(255)" json:"school"`
	Phone     string    `gorm:"type:varchar(50)" json:"phone"`
	City      string    `gorm:"type:varchar(120)" json:"city"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	User *User `gorm:"foreignKey:UserID" json:"user,omitempty"`
}

func (TeacherProfile) TableName() string { return "teacher_profiles" }

// TeacherSubscription is the active or historical paid access record.
type TeacherSubscription struct {
	ID                uint       `gorm:"primaryKey" json:"id"`
	UserID            uint       `gorm:"not null;index" json:"user_id"`
	PlanID            uint       `gorm:"not null;index" json:"plan_id"`
	Status            string     `gorm:"type:varchar(30);not null;default:'active';index" json:"status"` // active, expired, cancelled
	StartsAt          time.Time  `gorm:"not null;index" json:"starts_at"`
	EndsAt            time.Time  `gorm:"not null;index" json:"ends_at"`
	PriceJOD          float64    `gorm:"type:decimal(10,3);not null;default:25.000" json:"price_jod"`
	DeviceLimit       int        `gorm:"not null;default:2" json:"device_limit"`
	DownloadLimit     int        `gorm:"not null;default:300" json:"download_limit"`
	AIGenerationLimit int        `gorm:"not null;default:100" json:"ai_generation_limit"`
	ExportLimit       int        `gorm:"not null;default:100" json:"export_limit"`
	ActivatedBy       *uint      `gorm:"index" json:"activated_by,omitempty"`
	CancelledAt       *time.Time `json:"cancelled_at,omitempty"`
	AdminNote         string     `gorm:"type:text" json:"admin_note,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`

	User *User             `gorm:"foreignKey:UserID" json:"user,omitempty"`
	Plan *SubscriptionPlan `gorm:"foreignKey:PlanID" json:"plan,omitempty"`
	// Profile carries what the teacher actually subscribed for (subjects, school,
	// city). Shares the user_id key with teacher_profiles; preloaded for admin views.
	Profile *TeacherProfile `gorm:"foreignKey:UserID;references:UserID" json:"profile,omitempty"`
}

func (TeacherSubscription) TableName() string { return "teacher_subscriptions" }

// SubscriptionOrder represents a manual payment request awaiting admin review.
type SubscriptionOrder struct {
	ID               uint       `gorm:"primaryKey" json:"id"`
	UserID           uint       `gorm:"not null;index" json:"user_id"`
	PlanID           uint       `gorm:"not null;index" json:"plan_id"`
	Status           string     `gorm:"type:varchar(30);not null;default:'pending';index" json:"status"` // pending, approved, rejected
	AmountJOD        float64    `gorm:"type:decimal(10,3);not null;default:25.000" json:"amount_jod"`
	Currency         string     `gorm:"type:varchar(10);not null;default:'JOD'" json:"currency"`
	PaymentMethod    string     `gorm:"type:varchar(80);not null" json:"payment_method"`
	PayerName        string     `gorm:"type:varchar(255)" json:"payer_name"`
	Phone            string     `gorm:"type:varchar(50)" json:"phone"`
	PaymentRef       string     `gorm:"type:varchar(255)" json:"payment_reference"`
	PaymentProofURL  string     `gorm:"type:varchar(500)" json:"payment_proof_url"`
	PaymentProofPath string     `gorm:"type:varchar(1000)" json:"-"`
	AdminNote        string     `gorm:"type:text" json:"admin_note"`
	ReviewedBy       *uint      `gorm:"index" json:"reviewed_by,omitempty"`
	ReviewedAt       *time.Time `json:"reviewed_at,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`

	User *User             `gorm:"foreignKey:UserID" json:"user,omitempty"`
	Plan *SubscriptionPlan `gorm:"foreignKey:PlanID" json:"plan,omitempty"`
}

func (SubscriptionOrder) TableName() string { return "subscription_orders" }

// TeacherDevice helps reduce account sharing without locking a user to one IP.
type TeacherDevice struct {
	ID         uint       `gorm:"primaryKey" json:"id"`
	UserID     uint       `gorm:"not null;index" json:"user_id"`
	DeviceHash string     `gorm:"type:varchar(128);not null;index" json:"device_hash"`
	Label      string     `gorm:"type:varchar(255)" json:"label"`
	IPHash     string     `gorm:"type:varchar(128);index" json:"ip_hash"`
	UserAgent  string     `gorm:"type:varchar(500)" json:"user_agent"`
	IsActive   bool       `gorm:"not null;default:true;index" json:"is_active"`
	LastSeenAt *time.Time `json:"last_seen_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`

	User *User `gorm:"foreignKey:UserID" json:"user,omitempty"`
}

func (TeacherDevice) TableName() string { return "teacher_devices" }

// TeacherPremiumFile is a private protected file for Teacher Pro subscribers.
// It is intentionally separated from public article/post attachments.
type TeacherPremiumFile struct {
	ID                          uint       `gorm:"primaryKey" json:"id"`
	Title                       string     `gorm:"type:varchar(500);not null;index" json:"title"`
	Description                 string     `gorm:"type:text" json:"description"`
	Country                     string     `gorm:"type:varchar(10);default:'jo';index" json:"country"`
	GradeLevel                  *uint      `gorm:"index" json:"grade_level,omitempty"`
	GradeName                   string     `gorm:"type:varchar(255)" json:"grade_name"`
	SubjectID                   *uint      `gorm:"index" json:"subject_id,omitempty"`
	SubjectName                 string     `gorm:"type:varchar(255);not null;index" json:"subject_name"`
	SemesterID                  *uint      `gorm:"index" json:"semester_id,omitempty"`
	SemesterName                string     `gorm:"type:varchar(255)" json:"semester_name"`
	Category                    string     `gorm:"type:varchar(80);not null;index" json:"category"`
	OriginalFilename            string     `gorm:"type:varchar(500);not null" json:"original_filename"`
	StoredFilename              string     `gorm:"type:varchar(500);not null" json:"stored_filename"`
	PrivatePath                 string     `gorm:"type:varchar(1000);not null" json:"-"`
	FileSize                    int64      `json:"file_size"`
	MimeType                    string     `gorm:"type:varchar(150)" json:"mime_type"`
	FileType                    string     `gorm:"type:varchar(50)" json:"file_type"`
	IsActive                    bool       `gorm:"not null;default:true;index" json:"is_active"`
	ArchivedAt                  *time.Time `gorm:"index" json:"archived_at,omitempty"`
	ArchiveReason               string     `gorm:"type:text" json:"archive_reason,omitempty"`
	WatermarkEnabled            bool       `gorm:"not null;default:true" json:"watermark_enabled"`
	DownloadCount               int        `gorm:"default:0" json:"download_count"`
	RequiresTeacherSubscription bool       `gorm:"not null;default:true;index" json:"requires_teacher_subscription"`
	CreatedBy                   *uint      `gorm:"index" json:"created_by,omitempty"`
	UpdatedBy                   *uint      `gorm:"index" json:"updated_by,omitempty"`
	CreatedAt                   time.Time  `json:"created_at"`
	UpdatedAt                   time.Time  `json:"updated_at"`
}

func (TeacherPremiumFile) TableName() string { return "teacher_premium_files" }

// TeacherLibraryItem stores saved premium files and generated materials in a teacher's personal library.
type TeacherLibraryItem struct {
	ID         uint      `gorm:"primaryKey" json:"id"`
	UserID     uint      `gorm:"not null;index" json:"user_id"`
	ItemType   string    `gorm:"type:varchar(50);not null;index" json:"item_type"` // file, ai_generation
	ItemID     *uint     `gorm:"index" json:"item_id,omitempty"`
	Title      string    `gorm:"type:varchar(500);not null" json:"title"`
	SourceType string    `gorm:"type:varchar(80);default:'premium_file';index" json:"source_type"`
	Category   string    `gorm:"type:varchar(80);default:'';index" json:"category"`
	Country    string    `gorm:"type:varchar(10);default:'jo';index" json:"country"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`

	User *User `gorm:"foreignKey:UserID" json:"user,omitempty"`
}

func (TeacherLibraryItem) TableName() string { return "teacher_library_items" }

// TeacherPremiumDownload logs future premium file downloads.
type TeacherPremiumDownload struct {
	ID               uint      `gorm:"primaryKey" json:"id"`
	UserID           uint      `gorm:"not null;index" json:"user_id"`
	SubscriptionID   uint      `gorm:"not null;index" json:"subscription_id"`
	FileID           *uint     `gorm:"index" json:"file_id,omitempty"`
	PremiumFileID    *uint     `gorm:"index" json:"premium_file_id,omitempty"`
	Country          string    `gorm:"type:varchar(10);default:'jo';index" json:"country"`
	FileTitle        string    `gorm:"type:varchar(500)" json:"file_title"`
	OriginalFilename string    `gorm:"type:varchar(500)" json:"original_filename"`
	SubjectName      string    `gorm:"type:varchar(255);index" json:"subject_name"`
	Category         string    `gorm:"type:varchar(80);index" json:"category"`
	FileSize         int64     `json:"file_size"`
	MimeType         string    `gorm:"type:varchar(150)" json:"mime_type"`
	DownloadCode     string    `gorm:"type:varchar(120);index" json:"download_code"`
	IPHash           string    `gorm:"type:varchar(128);index" json:"ip_hash"`
	UserAgentHash    string    `gorm:"type:varchar(128);index" json:"user_agent_hash"`
	WatermarkApplied bool      `gorm:"not null;default:false;index" json:"watermark_applied"`
	WatermarkText    string    `gorm:"type:text" json:"watermark_text,omitempty"`
	WatermarkedPath  string    `gorm:"type:varchar(1000)" json:"-"`
	CreatedAt        time.Time `json:"created_at"`

	User *User `gorm:"foreignKey:UserID" json:"user,omitempty"`
}

func (TeacherPremiumDownload) TableName() string { return "teacher_premium_downloads" }

// TeacherAIGeneration logs future AI tool usage for limits.
type TeacherAIGeneration struct {
	ID             uint      `gorm:"primaryKey" json:"id"`
	UserID         uint      `gorm:"not null;index" json:"user_id"`
	SubscriptionID uint      `gorm:"not null;index" json:"subscription_id"`
	ToolType       string    `gorm:"type:varchar(80);not null;index" json:"tool_type"`
	Title          string    `gorm:"type:varchar(255)" json:"title"`
	Model          string    `gorm:"type:varchar(120)" json:"model"`
	Prompt         string    `gorm:"type:text" json:"prompt"`
	Output         string    `gorm:"type:longtext" json:"output"`
	ExportCount    int       `gorm:"default:0" json:"export_count"`
	CreatedAt      time.Time `json:"created_at"`

	User *User `gorm:"foreignKey:UserID" json:"user,omitempty"`
}

func (TeacherAIGeneration) TableName() string { return "teacher_ai_generations" }

// TeacherAuditLog records sensitive admin/member actions for paid teacher product.
type TeacherAuditLog struct {
	ID         uint      `gorm:"primaryKey" json:"id"`
	ActorID    *uint     `gorm:"index" json:"actor_id,omitempty"`
	UserID     *uint     `gorm:"index" json:"user_id,omitempty"`
	EntityType string    `gorm:"type:varchar(120);index" json:"entity_type"`
	EntityID   uint      `gorm:"index" json:"entity_id"`
	Action     string    `gorm:"type:varchar(120);index" json:"action"`
	Note       string    `gorm:"type:text" json:"note"`
	IPHash     string    `gorm:"type:varchar(128);index" json:"ip_hash"`
	CreatedAt  time.Time `json:"created_at"`

	Actor *User `gorm:"foreignKey:ActorID" json:"actor,omitempty"`
	User  *User `gorm:"foreignKey:UserID" json:"user,omitempty"`
}

func (TeacherAuditLog) TableName() string { return "teacher_audit_logs" }

// TeacherExpiryNotification stores renewal/expiry notices to avoid duplicate reminders.
type TeacherExpiryNotification struct {
	ID             uint      `gorm:"primaryKey" json:"id"`
	UserID         uint      `gorm:"not null;index" json:"user_id"`
	SubscriptionID uint      `gorm:"not null;index" json:"subscription_id"`
	NoticeType     string    `gorm:"type:varchar(50);not null;index" json:"notice_type"` // before_14_days, before_7_days, expired
	Status         string    `gorm:"type:varchar(30);not null;default:'pending';index" json:"status"`
	Message        string    `gorm:"type:text" json:"message"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`

	User         *User                `gorm:"foreignKey:UserID" json:"user,omitempty"`
	Subscription *TeacherSubscription `gorm:"foreignKey:SubscriptionID" json:"subscription,omitempty"`
}

func (TeacherExpiryNotification) TableName() string { return "teacher_expiry_notifications" }

// TeacherNotification stores teacher/admin notices for the paid teacher product.
type TeacherNotification struct {
	ID        uint       `gorm:"primaryKey" json:"id"`
	UserID    *uint      `gorm:"index" json:"user_id,omitempty"`
	Type      string     `gorm:"type:varchar(80);not null;index" json:"type"`
	Title     string     `gorm:"type:varchar(255);not null" json:"title"`
	Message   string     `gorm:"type:text" json:"message"`
	URL       string     `gorm:"type:varchar(500)" json:"url"`
	ReadAt    *time.Time `json:"read_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`

	User *User `gorm:"foreignKey:UserID" json:"user,omitempty"`
}

func (TeacherNotification) TableName() string { return "teacher_notifications" }

// TeacherPaymentSetting stores manual/electronic payment instructions.
type TeacherPaymentSetting struct {
	ID           uint      `gorm:"primaryKey" json:"id"`
	Provider     string    `gorm:"type:varchar(80);not null;index" json:"provider"`
	DisplayName  string    `gorm:"type:varchar(255);not null" json:"display_name"`
	Instructions string    `gorm:"type:text" json:"instructions"`
	IsActive     bool      `gorm:"not null;default:true;index" json:"is_active"`
	SortOrder    int       `gorm:"default:0" json:"sort_order"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func (TeacherPaymentSetting) TableName() string { return "teacher_payment_settings" }
