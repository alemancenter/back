package services

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/alemancenter/fiber-api/internal/database"
	"github.com/alemancenter/fiber-api/internal/models"
	"github.com/alemancenter/fiber-api/internal/repositories"
	"gorm.io/gorm"
)

var (
	ErrTeacherPlanNotFound    = errors.New("teacher subscription plan not found")
	ErrTeacherOrderNotPending = errors.New("subscription order is not pending")
	ErrTeacherDeviceLimit     = errors.New("teacher device limit reached")
)

const (
	TeacherSemesterPlanCode = "teacher_semester"
	TeacherProRoleName      = "Teacher Pro"
	// MaxTeacherSubjects caps how many subjects a single teacher subscription
	// may cover. Was previously hard-limited to exactly one subject.
	MaxTeacherSubjects = 3
)

// normalizeTeacherSubjects trims, dedupes (case-insensitive), and caps a
// requested subject list at MaxTeacherSubjects. If the list is empty it falls
// back to fallbackSingle (legacy single-subject callers/payloads).
func normalizeTeacherSubjects(list []string, fallbackSingle string) []string {
	seen := make(map[string]bool, MaxTeacherSubjects)
	result := make([]string, 0, MaxTeacherSubjects)
	add := func(raw string) {
		v := strings.TrimSpace(raw)
		if v == "" || len(result) >= MaxTeacherSubjects {
			return
		}
		key := strings.ToLower(v)
		if seen[key] {
			return
		}
		seen[key] = true
		result = append(result, v)
	}
	for _, s := range list {
		add(s)
	}
	if len(result) == 0 && strings.TrimSpace(fallbackSingle) != "" {
		add(fallbackSingle)
	}
	return result
}

// encodeTeacherSubjects serializes a subject list into the JSON string stored
// in TeacherProfile.Subjects.
func encodeTeacherSubjects(subjects []string) string {
	if len(subjects) == 0 {
		return "[]"
	}
	encoded, err := json.Marshal(subjects)
	if err != nil {
		return "[]"
	}
	return string(encoded)
}

// decodeTeacherSubjects parses TeacherProfile.Subjects back into a slice.
// Falls back to treating the raw value as a single legacy subject string if
// it isn't valid JSON (covers rows written before this field existed).
func decodeTeacherSubjects(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var list []string
	if err := json.Unmarshal([]byte(raw), &list); err == nil {
		return list
	}
	return []string{raw}
}

// teacherProfileSubjects returns the effective subject list for a profile,
// preferring the new multi-subject field and falling back to the legacy
// single Subject column for older rows.
func teacherProfileSubjects(profile *models.TeacherProfile) []string {
	if profile == nil {
		return nil
	}
	if strings.TrimSpace(profile.Subjects) != "" {
		if list := decodeTeacherSubjects(profile.Subjects); len(list) > 0 {
			return list
		}
	}
	if strings.TrimSpace(profile.Subject) != "" {
		return []string{strings.TrimSpace(profile.Subject)}
	}
	return nil
}

// teacherHasSubject reports whether any of the teacher's registered subjects
// matches the given subject (using the same loose substring comparison as
// subjectsMatch).
func teacherHasSubject(profile *models.TeacherProfile, subject string) bool {
	for _, s := range teacherProfileSubjects(profile) {
		if subjectsMatch(s, subject) {
			return true
		}
	}
	return false
}

var TeacherSemesterPermissions = []string{
	"teacher.subscription.access",
	"teacher.files.premium.download",
	"teacher.files.word_pdf.export",
	"teacher.ai.exam.generate",
	"teacher.ai.answer_key.generate",
	"teacher.ai.worksheet.generate",
	"teacher.ai.remedial_plan.generate",
	"teacher.library.access",
	"teacher.devices.manage",
	"teacher.usage.view",
}

var TeacherAdminPermissions = []string{
	"manage teacher subscriptions",
	"teacher.subscription.orders.review",
	"teacher.subscription.plans.view",
}

var TeacherSemesterFeatures = []string{
	"اختر حتى 3 مواد دراسية ضمن الاشتراك الواحد",
	"نماذج امتحانات حديثة ومتنوعة",
	"خطط فصلية وتحليل محتوى",
	"أوراق عمل وخطط علاجية",
	"ملفات Word/PDF قابلة للطباعة",
	"أدوات AI للمعلم",
	"مكتبة وسجل استخدام للمعلم",
	"جهازان موثقان",
	"Watermark لحماية الملفات المدفوعة",
}

type TeacherPlanDesign struct {
	Code             string         `json:"code"`
	Name             string         `json:"name"`
	PriceJOD         float64        `json:"price_jod"`
	Currency         string         `json:"currency"`
	DurationDays     int            `json:"duration_days"`
	Features         []string       `json:"features"`
	Permissions      []string       `json:"permissions"`
	Limits           map[string]int `json:"limits"`
	AdminPermissions []string       `json:"admin_permissions,omitempty"`
}

type TeacherAccessResult struct {
	HasActive    bool                        `json:"has_active"`
	Permissions  []string                    `json:"permissions"`
	Allowed      map[string]bool             `json:"allowed"`
	Limits       map[string]int              `json:"limits"`
	Usage        map[string]int64            `json:"usage"`
	Subscription *models.TeacherSubscription `json:"subscription,omitempty"`
}

type TeacherWorkspaceSummary struct {
	// Subject is deprecated (first entry of Subjects), kept for backward compatibility.
	Subject      string                      `json:"subject"`
	Subjects     []string                    `json:"subjects"`
	Subscription *models.TeacherSubscription `json:"subscription,omitempty"`
	Usage        map[string]int64            `json:"usage"`
	Limits       map[string]int              `json:"limits"`
	QuickLinks   []TeacherWorkspaceLink      `json:"quick_links"`
	Stats        map[string]int64            `json:"stats"`
}

type TeacherWorkspaceLink struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Href        string `json:"href"`
	Category    string `json:"category"`
}

type TeacherPremiumFileResponse struct {
	ID              uint   `json:"id"`
	FileName        string `json:"file_name"`
	FileType        string `json:"file_type"`
	FileCategory    string `json:"file_category"`
	FileSize        int64  `json:"file_size"`
	MimeType        string `json:"mime_type"`
	PremiumCategory string `json:"premium_category"`
	PremiumSubject  string `json:"premium_subject"`
	DownloadCount   int    `json:"download_count"`
	CreatedAt       string `json:"created_at"`
	ArticleTitle    string `json:"article_title,omitempty"`
	SubjectName     string `json:"subject_name,omitempty"`
	SemesterName    string `json:"semester_name,omitempty"`
}

type TeacherSaveLibraryRequest struct {
	ItemType   string `json:"item_type"`
	ItemID     *uint  `json:"item_id"`
	Title      string `json:"title"`
	SourceType string `json:"source_type"`
	Category   string `json:"category"`
	Country    string `json:"country"`
}

type TeacherPremiumVaultFileResponse struct {
	ID               uint             `json:"id"`
	Title            string           `json:"title"`
	Description      string           `json:"description"`
	Country          string           `json:"country"`
	GradeLevel       *uint            `json:"grade_level,omitempty"`
	GradeName        string           `json:"grade_name"`
	SubjectID        *uint            `json:"subject_id,omitempty"`
	SubjectName      string           `json:"subject_name"`
	SemesterID       *uint            `json:"semester_id,omitempty"`
	SemesterName     string           `json:"semester_name"`
	Category         string           `json:"category"`
	OriginalFilename string           `json:"original_filename"`
	FileSize         int64            `json:"file_size"`
	MimeType         string           `json:"mime_type"`
	FileType         string           `json:"file_type"`
	IsActive         bool             `json:"is_active"`
	DownloadCount    int              `json:"download_count"`
	CreatedAt        string           `json:"created_at"`
	WatermarkApplied bool             `json:"watermark_applied"`
	User             *TeacherUserMini `json:"user,omitempty"`
}

type TeacherUserMini struct {
	ID    uint   `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

type TeacherAuditLogResponse struct {
	ID         uint             `json:"id"`
	ActorID    *uint            `json:"actor_id,omitempty"`
	UserID     *uint            `json:"user_id,omitempty"`
	EntityType string           `json:"entity_type"`
	EntityID   uint             `json:"entity_id"`
	Action     string           `json:"action"`
	Note       string           `json:"note"`
	IPHash     string           `json:"ip_hash"`
	CreatedAt  string           `json:"created_at"`
	Actor      *TeacherUserMini `json:"actor,omitempty"`
	User       *TeacherUserMini `json:"user,omitempty"`
}

type TeacherPremiumFileDetail struct {
	File      TeacherPremiumVaultFileResponse  `json:"file"`
	Downloads []TeacherPremiumDownloadResponse `json:"downloads"`
	AuditLogs []TeacherAuditLogResponse        `json:"audit_logs"`
}

type TeacherRenewSubscriptionRequest struct {
	EndsAt    string `json:"ends_at"`
	ExtraDays int    `json:"extra_days"`
	AdminNote string `json:"admin_note"`
}

type TeacherArchiveFileRequest struct {
	Reason string `json:"reason"`
}

type TeacherPremiumDownloadResponse struct {
	ID               uint             `json:"id"`
	UserID           uint             `json:"user_id"`
	SubscriptionID   uint             `json:"subscription_id"`
	PremiumFileID    *uint            `json:"premium_file_id,omitempty"`
	Country          string           `json:"country"`
	FileTitle        string           `json:"file_title"`
	OriginalFilename string           `json:"original_filename"`
	SubjectName      string           `json:"subject_name"`
	Category         string           `json:"category"`
	FileSize         int64            `json:"file_size"`
	MimeType         string           `json:"mime_type"`
	DownloadCode     string           `json:"download_code"`
	IPHash           string           `json:"ip_hash"`
	UserAgentHash    string           `json:"user_agent_hash"`
	CreatedAt        string           `json:"created_at"`
	WatermarkApplied bool             `json:"watermark_applied"`
	User             *TeacherUserMini `json:"user,omitempty"`
}

type TeacherPremiumVaultCreateRequest struct {
	Country      string `json:"country"`
	Title        string `json:"title"`
	Description  string `json:"description"`
	GradeLevel   *uint  `json:"grade_level"`
	GradeName    string `json:"grade_name"`
	SubjectID    *uint  `json:"subject_id"`
	SubjectName  string `json:"subject_name"`
	SemesterID   *uint  `json:"semester_id"`
	SemesterName string `json:"semester_name"`
	Category     string `json:"category"`
	FileType     string `json:"file_type"`
}

type TeacherPremiumFileAdminRequest struct {
	Country                     string `json:"country"`
	IsPremium                   bool   `json:"is_premium"`
	PremiumAudience             string `json:"premium_audience"`
	PremiumCategory             string `json:"premium_category"`
	PremiumSubject              string `json:"premium_subject"`
	PremiumRequiresSubscription bool   `json:"premium_requires_subscription"`
}

type TeacherCancelRequest struct {
	AdminNote string `json:"admin_note"`
}

type TeacherAdminDashboard struct {
	Stats        map[string]int64           `json:"stats"`
	Plan         TeacherPlanDesign          `json:"plan"`
	RecentOrders []models.SubscriptionOrder `json:"recent_orders"`
}

type CreateTeacherOrderRequest struct {
	// Subject is deprecated (kept for backward compatibility with older
	// clients). Subjects is the canonical field going forward and accepts up
	// to MaxTeacherSubjects entries.
	Subject         string   `json:"subject"`
	Subjects        []string `json:"subjects"`
	School          string `json:"school"`
	City            string `json:"city"`
	Phone           string `json:"phone"`
	PaymentMethod   string `json:"payment_method"`
	PayerName       string `json:"payer_name"`
	PaymentRef      string `json:"payment_reference"`
	PaymentProofURL string `json:"payment_proof_url"`
}

type TeacherOrderReviewRequest struct {
	AdminNote string `json:"admin_note"`
}

type TeacherOrderDetail struct {
	Order         *models.SubscriptionOrder `json:"order"`
	Profile       *models.TeacherProfile    `json:"profile,omitempty"`
	HasProof      bool                      `json:"has_proof"`
	ProofURL      string                    `json:"proof_url,omitempty"`
	ProofFilename string                    `json:"proof_filename,omitempty"`
}

type TeacherSubscriptionSummary struct {
	Plan         *models.SubscriptionPlan    `json:"plan"`
	Subscription *models.TeacherSubscription `json:"subscription,omitempty"`
	Profile      *models.TeacherProfile      `json:"profile,omitempty"`
	// Subjects is the decoded, ready-to-render subject list (up to
	// MaxTeacherSubjects) — a convenience over parsing Profile.Subjects JSON.
	Subjects       []string                   `json:"subjects"`
	Orders         []models.SubscriptionOrder `json:"orders"`
	Devices        []models.TeacherDevice      `json:"devices,omitempty"`
	Usage          map[string]int64            `json:"usage"`
	HasActive      bool                        `json:"has_active"`
	CanCreateOrder bool                        `json:"can_create_order"`
	PlanDesign     TeacherPlanDesign           `json:"plan_design"`
	Access         TeacherAccessResult         `json:"access"`
}

type TeacherAIGenerateRequest struct {
	ToolType string `json:"tool_type"`
	Title    string `json:"title"`
	Prompt   string `json:"prompt"`
	Grade    string `json:"grade"`
	Subject  string `json:"subject"`
	Semester string `json:"semester"`
}

type TeacherAIGenerateResponse struct {
	ID        uint   `json:"id"`
	ToolType  string `json:"tool_type"`
	Title     string `json:"title"`
	Output    string `json:"output"`
	Model     string `json:"model"`
	CreatedAt string `json:"created_at"`
}

type TeacherFinancialReport struct {
	TotalRevenueJOD      float64 `json:"total_revenue_jod"`
	CurrentMonthRevenue  float64 `json:"current_month_revenue_jod"`
	ApprovedOrders       int64   `json:"approved_orders"`
	PendingOrders        int64   `json:"pending_orders"`
	RejectedOrders       int64   `json:"rejected_orders"`
	ActiveSubscriptions  int64   `json:"active_subscriptions"`
	ExpiredSubscriptions int64   `json:"expired_subscriptions"`
}

type TeacherUsageAnalytics struct {
	TotalDownloads     int64               `json:"total_downloads"`
	TotalAIGenerations int64               `json:"total_ai_generations"`
	TotalPremiumFiles  int64               `json:"total_premium_files"`
	TotalTeachers      int64               `json:"total_teachers"`
	ActiveDevices      int64               `json:"active_devices"`
	TopSubjects        []TeacherMetricItem `json:"top_subjects"`
	TopCategories      []TeacherMetricItem `json:"top_categories"`
	TopDownloadedFiles []TeacherMetricItem `json:"top_downloaded_files"`
	MostActiveTeachers []TeacherMetricItem `json:"most_active_teachers"`
}

type TeacherMetricItem struct {
	Label string `json:"label"`
	Value int64  `json:"value"`
	Extra string `json:"extra,omitempty"`
}

type TeacherAdminDetail struct {
	User          *TeacherUserMini                 `json:"user,omitempty"`
	Profile       *models.TeacherProfile           `json:"profile,omitempty"`
	Subscription  *models.TeacherSubscription      `json:"subscription,omitempty"`
	Devices       []models.TeacherDevice           `json:"devices"`
	Downloads     []TeacherPremiumDownloadResponse `json:"downloads"`
	AIGenerations []models.TeacherAIGeneration     `json:"ai_generations"`
	Orders        []models.SubscriptionOrder       `json:"orders"`
	AuditLogs     []TeacherAuditLogResponse        `json:"audit_logs"`
}

type TeacherDeactivateDeviceRequest struct {
	UserID uint   `json:"user_id"`
	Note   string `json:"note"`
}

type TeacherSubscriptionService interface {
	EnsureDefaultPlan() (*models.SubscriptionPlan, error)
	EnsureTeacherProRole() (*models.Role, error)
	PublicPlan() (*models.SubscriptionPlan, error)
	Access(userID uint) (*TeacherAccessResult, error)
	PlanDesign() TeacherPlanDesign
	MySummary(userID uint) (*TeacherSubscriptionSummary, error)
	CreateOrder(user *models.User, req CreateTeacherOrderRequest) (*models.SubscriptionOrder, error)
	ListOrders(status string, limit, offset int) ([]models.SubscriptionOrder, int64, error)
	AdminListAIGenerations(userID uint, limit, offset int) ([]models.TeacherAIGeneration, int64, error)
	AdminUpdatePremiumVaultFile(countryID database.CountryID, fileID uint, req TeacherPremiumVaultCreateRequest, isActive *bool, adminID uint) (*TeacherPremiumVaultFileResponse, error)
	AdminCreatePremiumVaultFile(countryID database.CountryID, req TeacherPremiumVaultCreateRequest, privatePath, storedFilename, originalFilename, mimeType string, fileSize int64, adminID uint) (*TeacherPremiumVaultFileResponse, error)
	AdminListPremiumVaultFiles(countryID database.CountryID, search, active, category, subject string, limit, offset int) ([]TeacherPremiumVaultFileResponse, int64, error)
	RecordPremiumVaultDownload(userID uint, subscriptionID uint, countryID database.CountryID, file *models.TeacherPremiumFile, ip string, userAgent string) (*models.TeacherPremiumDownload, error)
	GetPremiumVaultFileForDownload(userID uint, countryID database.CountryID, fileID uint) (*models.TeacherPremiumFile, *models.TeacherSubscription, error)
	PremiumVaultFiles(userID uint, countryID database.CountryID, category, query string, limit, offset int) ([]TeacherPremiumVaultFileResponse, int64, error)
	AdminDisablePremiumFile(countryID database.CountryID, fileID uint) (*TeacherPremiumFileResponse, error)
	AdminUpdatePremiumFile(countryID database.CountryID, fileID uint, req TeacherPremiumFileAdminRequest) (*TeacherPremiumFileResponse, error)
	AdminListPremiumFiles(countryID database.CountryID, search, premium, category, subject string, limit, offset int) ([]TeacherPremiumFileResponse, int64, error)
	AdminRemoveTeacherMembership(userID uint, adminID uint, req TeacherCancelRequest) error
	AdminCancelSubscription(subscriptionID uint, adminID uint, req TeacherCancelRequest) (*models.TeacherSubscription, error)
	AdminListPremiumDownloads(userID uint, limit, offset int) ([]TeacherPremiumDownloadResponse, int64, error)
	AdminListDevices(userID uint, active string, limit, offset int) ([]models.TeacherDevice, int64, error)
	AdminListTeachers(search string, limit, offset int) ([]models.TeacherProfile, int64, error)
	AdminListSubscriptions(status, search string, limit, offset int) ([]models.TeacherSubscription, int64, error)
	AdminDashboard() (*TeacherAdminDashboard, error)
	ListUserOrders(userID uint, limit int) ([]models.SubscriptionOrder, error)
	ApproveOrder(orderID uint, adminID uint, req TeacherOrderReviewRequest) (*models.TeacherSubscription, error)
	RejectOrder(orderID uint, adminID uint, req TeacherOrderReviewRequest) error
	RegisterDevice(userID uint, ip, userAgent, label string) error
	ListDevices(userID uint) ([]models.TeacherDevice, error)
	DeactivateDevice(userID, deviceID uint) error
	AIGenerations(userID uint, limit, offset int) ([]models.TeacherAIGeneration, int64, error)
	Downloads(userID uint, limit, offset int) ([]TeacherPremiumDownloadResponse, int64, error)
	Library(userID uint, limit, offset int) ([]models.TeacherLibraryItem, int64, error)
	SaveLibraryItem(userID uint, req TeacherSaveLibraryRequest) (*models.TeacherLibraryItem, error)
	PremiumFiles(userID uint, countryID database.CountryID, category, query string, limit, offset int) ([]TeacherPremiumFileResponse, int64, error)
	Workspace(userID uint) (*TeacherWorkspaceSummary, error)
	CreateAudit(actorID *uint, userID *uint, entityType string, entityID uint, action, note, ip string) error
	AdminListAuditLogs(entityType string, entityID uint, limit, offset int) ([]TeacherAuditLogResponse, int64, error)
	RunExpiryMaintenance() (map[string]int64, error)
	AdminRenewSubscription(subscriptionID uint, adminID uint, req TeacherRenewSubscriptionRequest, ip string) (*models.TeacherSubscription, error)
	AdminArchivePremiumVaultFile(countryID database.CountryID, fileID uint, req TeacherArchiveFileRequest, adminID uint, ip string) (*TeacherPremiumVaultFileResponse, error)
	AdminGetPremiumVaultFileDetail(countryID database.CountryID, fileID uint) (*TeacherPremiumFileDetail, error)
	ReactivateSubscription(subscriptionID uint, adminID uint, req TeacherRenewSubscriptionRequest, ip string) (*models.TeacherSubscription, error)
	GenerateTeacherAI(userID uint, req TeacherAIGenerateRequest) (*TeacherAIGenerateResponse, error)
	ExportTeacherAI(userID uint, generationID uint, format string) (string, string, string, error)
	TeacherNotifications(userID uint, limit, offset int) ([]models.TeacherNotification, int64, error)
	PaymentSettings() ([]models.TeacherPaymentSetting, error)
	EnsureDefaultPaymentSettings() error
	AdminFinancialReport() (*TeacherFinancialReport, error)
	AdminUsageAnalytics() (*TeacherUsageAnalytics, error)
	AdminTeacherDetail(userID uint) (*TeacherAdminDetail, error)
	AdminDeactivateTeacherDevice(deviceID uint, adminID uint, req TeacherDeactivateDeviceRequest, ip string) error
	AdminGetOrderDetail(orderID uint) (*TeacherOrderDetail, error)
	AdminOrderProofPath(orderID uint) (string, string, string, error)
	UpdateDownloadWatermark(downloadID uint, applied bool, text string, path string) error
}

type teacherSubscriptionService struct {
	repo repositories.TeacherSubscriptionRepository
	// ai is optional (may be nil in tests/older wiring). When present,
	// GenerateTeacherAI tries it first and only falls back to the static
	// template if the call errors or the service isn't configured.
	ai AIService
}

func NewTeacherSubscriptionService(repo repositories.TeacherSubscriptionRepository, ai AIService) TeacherSubscriptionService {
	return &teacherSubscriptionService{repo: repo, ai: ai}
}

func (s *teacherSubscriptionService) EnsureTeacherProRole() (*models.Role, error) {
	db := s.repo.DB()

	permissionIDs := make([]uint, 0, len(TeacherSemesterPermissions))
	for _, permissionName := range TeacherSemesterPermissions {
		permission := models.Permission{Name: permissionName, GuardName: "api"}
		if err := db.Where("name = ?", permissionName).FirstOrCreate(&permission, models.Permission{Name: permissionName, GuardName: "api"}).Error; err != nil {
			return nil, err
		}
		permissionIDs = append(permissionIDs, permission.ID)
	}

	role := models.Role{Name: TeacherProRoleName, GuardName: "api"}
	if err := db.Where("name = ?", TeacherProRoleName).FirstOrCreate(&role, models.Role{Name: TeacherProRoleName, GuardName: "api"}).Error; err != nil {
		return nil, err
	}

	for _, permissionID := range permissionIDs {
		if err := db.Exec(
			"INSERT IGNORE INTO role_has_permissions (permission_id, role_id) VALUES (?, ?)",
			permissionID, role.ID,
		).Error; err != nil {
			return nil, err
		}
	}

	return &role, nil
}

func (s *teacherSubscriptionService) assignTeacherProRole(userID uint) error {
	role, err := s.EnsureTeacherProRole()
	if err != nil {
		return err
	}

	if err := s.repo.DB().Exec(
		"INSERT IGNORE INTO model_has_roles (role_id, model_type, model_id) VALUES (?, ?, ?)",
		role.ID, modelTypeUser, userID,
	).Error; err != nil {
		return err
	}

	InvalidateUserCache(userID)
	return nil
}

func (s *teacherSubscriptionService) PlanDesign() TeacherPlanDesign {
	return TeacherPlanDesign{
		Code:             TeacherSemesterPlanCode,
		Name:             "اشتراك المعلم للفصل الدراسي",
		PriceJOD:         25,
		Currency:         "JOD",
		DurationDays:     150,
		Features:         TeacherSemesterFeatures,
		Permissions:      TeacherSemesterPermissions,
		AdminPermissions: TeacherAdminPermissions,
		Limits: map[string]int{
			"devices":           2,
			"premium_downloads": 300,
			"ai_generations":    100,
			"exports":           100,
			"subjects":          MaxTeacherSubjects,
		},
	}
}

func (s *teacherSubscriptionService) Access(userID uint) (*TeacherAccessResult, error) {
	sub, err := s.repo.GetCurrentSubscription(userID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return &TeacherAccessResult{
				HasActive:   false,
				Permissions: []string{},
				Allowed:     map[string]bool{},
				Limits:      map[string]int{},
				Usage:       map[string]int64{"downloads": 0, "ai_generations": 0},
			}, nil
		}
		return nil, err
	}

	usage := map[string]int64{"downloads": 0, "ai_generations": 0}
	if c, err := s.repo.CountDownloads(sub.ID); err == nil {
		usage["downloads"] = c
	}
	if c, err := s.repo.CountAIGenerations(sub.ID); err == nil {
		usage["ai_generations"] = c
	}

	permissions := parseStringList(sub.Plan)
	if len(permissions) == 0 {
		permissions = TeacherSemesterPermissions
	}
	allowed := map[string]bool{}
	for _, permission := range permissions {
		allowed[permission] = true
	}

	return &TeacherAccessResult{
		HasActive:   true,
		Permissions: permissions,
		Allowed:     allowed,
		Limits: map[string]int{
			"devices":           sub.DeviceLimit,
			"premium_downloads": sub.DownloadLimit,
			"ai_generations":    sub.AIGenerationLimit,
			"exports":           sub.ExportLimit,
		},
		Usage:        usage,
		Subscription: sub,
	}, nil
}

func (s *teacherSubscriptionService) EnsureDefaultPlan() (*models.SubscriptionPlan, error) {
	plan, err := s.repo.UpsertDefaultPlan()
	if err != nil {
		return nil, err
	}
	_, _ = s.EnsureTeacherProRole()
	return plan, nil
}

func (s *teacherSubscriptionService) PublicPlan() (*models.SubscriptionPlan, error) {
	plan, err := s.repo.FirstActivePlan()
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return s.repo.UpsertDefaultPlan()
	}
	return plan, err
}

func (s *teacherSubscriptionService) MySummary(userID uint) (*TeacherSubscriptionSummary, error) {
	plan, err := s.PublicPlan()
	if err != nil {
		return nil, err
	}
	sub, err := s.repo.GetCurrentSubscription(userID)
	hasActive := err == nil && sub != nil && sub.ID > 0
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	profile, _ := s.repo.GetProfile(userID)
	orders, _ := s.repo.ListUserOrders(userID, 10)
	devices, _ := s.repo.ListDevices(userID)

	usage := map[string]int64{"downloads": 0, "ai_generations": 0}
	if hasActive {
		if c, err := s.repo.CountDownloads(sub.ID); err == nil {
			usage["downloads"] = c
		}
		if c, err := s.repo.CountAIGenerations(sub.ID); err == nil {
			usage["ai_generations"] = c
		}
	}

	canCreateOrder := true
	for _, order := range orders {
		if order.Status == "pending" {
			canCreateOrder = false
			break
		}
	}

	access, _ := s.Access(userID)
	if access == nil {
		access = &TeacherAccessResult{
			HasActive:   false,
			Permissions: []string{},
			Allowed:     map[string]bool{},
			Limits:      map[string]int{},
			Usage:       usage,
		}
	}

	return &TeacherSubscriptionSummary{
		Plan:           plan,
		Subscription:   sub,
		Profile:        profile,
		Subjects:       teacherProfileSubjects(profile),
		Orders:         orders,
		Devices:        devices,
		Usage:          usage,
		HasActive:      hasActive,
		CanCreateOrder: canCreateOrder && !hasActive,
		PlanDesign:     s.PlanDesign(),
		Access:         *access,
	}, nil
}

func (s *teacherSubscriptionService) CreateOrder(user *models.User, req CreateTeacherOrderRequest) (*models.SubscriptionOrder, error) {
	subjects := normalizeTeacherSubjects(req.Subjects, req.Subject)
	if len(subjects) == 0 {
		return nil, errors.New("teacher subject is required")
	}
	plan, err := s.PublicPlan()
	if err != nil {
		return nil, err
	}
	profile := &models.TeacherProfile{
		UserID:   user.ID,
		Subject:  subjects[0],
		Subjects: encodeTeacherSubjects(subjects),
		School:   strings.TrimSpace(req.School),
		City:     strings.TrimSpace(req.City),
		Phone:    strings.TrimSpace(req.Phone),
	}
	if err := s.repo.CreateOrUpdateProfile(profile); err != nil {
		return nil, err
	}

	method := strings.TrimSpace(req.PaymentMethod)
	if method == "" {
		method = "manual"
	}
	payer := strings.TrimSpace(req.PayerName)
	if payer == "" {
		payer = user.Name
	}

	order := &models.SubscriptionOrder{
		UserID:           user.ID,
		PlanID:           plan.ID,
		Status:           "pending",
		AmountJOD:        plan.PriceJOD,
		Currency:         plan.Currency,
		PaymentMethod:    method,
		PayerName:        payer,
		Phone:            strings.TrimSpace(req.Phone),
		PaymentRef:       strings.TrimSpace(req.PaymentRef),
		PaymentProofURL:  strings.TrimSpace(req.PaymentProofURL),
		PaymentProofPath: proofPathFromPrivateURL(req.PaymentProofURL),
	}
	if err := s.repo.CreateOrder(order); err != nil {
		return nil, err
	}
	_ = s.repo.CreateTeacherNotification(&models.TeacherNotification{
		Type:    "admin_new_subscription_order",
		Title:   "طلب اشتراك معلم جديد",
		Message: fmt.Sprintf("طلب جديد من %s لمواد: %s", user.Name, strings.Join(subjects, "، ")),
		URL:     "/dashboard/teacher-subscriptions/orders",
	})
	return s.repo.GetOrder(order.ID)
}

func proofPathFromPrivateURL(value string) string {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "private://") {
		return strings.TrimPrefix(value, "private://")
	}
	return ""
}

func (s *teacherSubscriptionService) AdminGetOrderDetail(orderID uint) (*TeacherOrderDetail, error) {
	order, err := s.repo.GetOrder(orderID)
	if err != nil {
		return nil, err
	}
	profile, _ := s.repo.GetProfile(order.UserID)
	proofPath := strings.TrimSpace(order.PaymentProofPath)
	if proofPath == "" {
		proofPath = proofPathFromPrivateURL(order.PaymentProofURL)
	}
	hasProof := false
	proofFilename := ""
	if proofPath != "" {
		if info, err := os.Stat(proofPath); err == nil && !info.IsDir() {
			hasProof = true
			proofFilename = filepath.Base(proofPath)
		}
	}
	return &TeacherOrderDetail{
		Order:         order,
		Profile:       profile,
		HasProof:      hasProof,
		ProofURL:      fmt.Sprintf("/dashboard/teacher-subscriptions/orders/%d/proof", order.ID),
		ProofFilename: proofFilename,
	}, nil
}

func (s *teacherSubscriptionService) AdminOrderProofPath(orderID uint) (string, string, string, error) {
	order, err := s.repo.GetOrder(orderID)
	if err != nil {
		return "", "", "", err
	}
	proofPath := strings.TrimSpace(order.PaymentProofPath)
	if proofPath == "" {
		proofPath = proofPathFromPrivateURL(order.PaymentProofURL)
	}
	if proofPath == "" {
		return "", "", "", gorm.ErrRecordNotFound
	}
	info, err := os.Stat(proofPath)
	if err != nil || info.IsDir() {
		return "", "", "", gorm.ErrRecordNotFound
	}
	contentType := "application/octet-stream"
	ext := strings.ToLower(filepath.Ext(proofPath))
	switch ext {
	case ".jpg", ".jpeg":
		contentType = "image/jpeg"
	case ".png":
		contentType = "image/png"
	case ".webp":
		contentType = "image/webp"
	case ".pdf":
		contentType = "application/pdf"
	}
	return proofPath, filepath.Base(proofPath), contentType, nil
}

func (s *teacherSubscriptionService) ListOrders(status string, limit, offset int) ([]models.SubscriptionOrder, int64, error) {
	return s.repo.ListOrders(status, limit, offset)
}

func (s *teacherSubscriptionService) ListUserOrders(userID uint, limit int) ([]models.SubscriptionOrder, error) {
	return s.repo.ListUserOrders(userID, limit)
}

func (s *teacherSubscriptionService) AdminDashboard() (*TeacherAdminDashboard, error) {
	stats, err := s.repo.AdminStats()
	if err != nil {
		return nil, err
	}
	orders, _, _ := s.repo.ListOrders("", 8, 0)
	return &TeacherAdminDashboard{
		Stats:        stats,
		Plan:         s.PlanDesign(),
		RecentOrders: orders,
	}, nil
}

func (s *teacherSubscriptionService) AdminListSubscriptions(status, search string, limit, offset int) ([]models.TeacherSubscription, int64, error) {
	return s.repo.AdminListSubscriptions(status, search, limit, offset)
}

func (s *teacherSubscriptionService) AdminListTeachers(search string, limit, offset int) ([]models.TeacherProfile, int64, error) {
	return s.repo.AdminListTeachers(search, limit, offset)
}

func (s *teacherSubscriptionService) AdminListDevices(userID uint, active string, limit, offset int) ([]models.TeacherDevice, int64, error) {
	return s.repo.AdminListDevices(userID, active, limit, offset)
}

func (s *teacherSubscriptionService) AdminListPremiumDownloads(userID uint, limit, offset int) ([]TeacherPremiumDownloadResponse, int64, error) {
	items, total, err := s.repo.AdminListPremiumDownloads(userID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	return mapTeacherPremiumDownloads(items), total, nil
}

func (s *teacherSubscriptionService) AdminListAIGenerations(userID uint, limit, offset int) ([]models.TeacherAIGeneration, int64, error) {
	return s.repo.AdminListAIGenerations(userID, limit, offset)
}

func (s *teacherSubscriptionService) PremiumVaultFiles(userID uint, countryID database.CountryID, category, query string, limit, offset int) ([]TeacherPremiumVaultFileResponse, int64, error) {
	access, err := s.Access(userID)
	if err != nil {
		return nil, 0, err
	}
	if !access.HasActive || !access.Allowed["teacher.files.premium.download"] {
		return []TeacherPremiumVaultFileResponse{}, 0, nil
	}

	profile, _ := s.repo.GetProfile(userID)
	subjects := teacherProfileSubjects(profile)

	files, total, err := s.repo.ListTeacherPremiumFiles(countryID, subjects, strings.TrimSpace(category), strings.TrimSpace(query), limit, offset)
	if err != nil {
		return nil, 0, err
	}
	return mapTeacherPremiumVaultFiles(files), total, nil
}

func (s *teacherSubscriptionService) GetPremiumVaultFileForDownload(userID uint, countryID database.CountryID, fileID uint) (*models.TeacherPremiumFile, *models.TeacherSubscription, error) {
	access, err := s.Access(userID)
	if err != nil {
		return nil, nil, err
	}
	if !access.HasActive || !access.Allowed["teacher.files.premium.download"] || access.Subscription == nil {
		return nil, nil, ErrTeacherPlanNotFound
	}

	file, err := s.repo.GetTeacherPremiumFile(countryID, fileID)
	if err != nil {
		return nil, nil, err
	}

	profile, _ := s.repo.GetProfile(userID)
	teacherSubjects := teacherProfileSubjects(profile)
	// Preserve the old lenient behaviour (allow when the teacher has no
	// subject on file yet), but otherwise require a match against ANY of the
	// teacher's up-to-3 registered subjects.
	if len(teacherSubjects) > 0 && !teacherHasSubject(profile, file.SubjectName) {
		return nil, nil, ErrTeacherPlanNotFound
	}

	if access.Limits["premium_downloads"] > 0 && access.Usage["downloads"] >= int64(access.Limits["premium_downloads"]) {
		return nil, nil, ErrTeacherDeviceLimit
	}

	return file, access.Subscription, nil
}

func (s *teacherSubscriptionService) UpdateDownloadWatermark(downloadID uint, applied bool, text string, path string) error {
	return s.repo.UpdateTeacherPremiumDownloadWatermark(downloadID, applied, text, path)
}

func (s *teacherSubscriptionService) RecordPremiumVaultDownload(userID uint, subscriptionID uint, countryID database.CountryID, file *models.TeacherPremiumFile, ip string, userAgent string) (*models.TeacherPremiumDownload, error) {
	if file == nil {
		return nil, ErrTeacherPlanNotFound
	}

	code := fmt.Sprintf("TPF-%d-%d-%d", userID, file.ID, time.Now().UnixNano())
	country := file.Country
	if strings.TrimSpace(country) == "" {
		country = database.CountryCode(countryID)
	}

	download := &models.TeacherPremiumDownload{
		UserID:           userID,
		SubscriptionID:   subscriptionID,
		PremiumFileID:    &file.ID,
		Country:          country,
		FileTitle:        file.Title,
		OriginalFilename: file.OriginalFilename,
		SubjectName:      file.SubjectName,
		Category:         file.Category,
		FileSize:         file.FileSize,
		MimeType:         file.MimeType,
		DownloadCode:     code,
		IPHash:           hashValue(ip),
		UserAgentHash:    hashValue(userAgent),
	}

	if err := s.repo.CreateTeacherPremiumDownload(download); err != nil {
		return nil, err
	}
	if err := s.repo.IncrementTeacherPremiumFileDownload(countryID, file.ID); err != nil {
		return nil, err
	}
	return download, nil
}

func (s *teacherSubscriptionService) AdminListPremiumVaultFiles(countryID database.CountryID, search, active, category, subject string, limit, offset int) ([]TeacherPremiumVaultFileResponse, int64, error) {
	files, total, err := s.repo.AdminListTeacherPremiumFiles(countryID, strings.TrimSpace(search), strings.TrimSpace(active), strings.TrimSpace(category), strings.TrimSpace(subject), limit, offset)
	if err != nil {
		return nil, 0, err
	}
	return mapTeacherPremiumVaultFiles(files), total, nil
}

func (s *teacherSubscriptionService) AdminCreatePremiumVaultFile(countryID database.CountryID, req TeacherPremiumVaultCreateRequest, privatePath, storedFilename, originalFilename, mimeType string, fileSize int64, adminID uint) (*TeacherPremiumVaultFileResponse, error) {
	category := strings.TrimSpace(req.Category)
	if category == "" {
		category = "exam"
	}
	title := strings.TrimSpace(req.Title)
	if title == "" {
		title = originalFilename
	}
	subject := strings.TrimSpace(req.SubjectName)
	if subject == "" {
		return nil, errors.New("subject is required")
	}
	country := strings.TrimSpace(req.Country)
	if country == "" {
		country = database.CountryCode(countryID)
	}

	file := &models.TeacherPremiumFile{
		Title:                       title,
		Description:                 strings.TrimSpace(req.Description),
		Country:                     country,
		GradeLevel:                  req.GradeLevel,
		GradeName:                   strings.TrimSpace(req.GradeName),
		SubjectID:                   req.SubjectID,
		SubjectName:                 subject,
		SemesterID:                  req.SemesterID,
		SemesterName:                strings.TrimSpace(req.SemesterName),
		Category:                    category,
		OriginalFilename:            originalFilename,
		StoredFilename:              storedFilename,
		PrivatePath:                 privatePath,
		FileSize:                    fileSize,
		MimeType:                    mimeType,
		FileType:                    normalizeFileType(req.FileType, originalFilename),
		IsActive:                    true,
		RequiresTeacherSubscription: true,
		CreatedBy:                   &adminID,
		UpdatedBy:                   &adminID,
	}
	if err := s.repo.AdminCreateTeacherPremiumFile(countryID, file); err != nil {
		return nil, err
	}
	s.notifyTeachersAboutPremiumFile(file)
	items := mapTeacherPremiumVaultFiles([]models.TeacherPremiumFile{*file})
	return &items[0], nil
}

func (s *teacherSubscriptionService) AdminUpdatePremiumVaultFile(countryID database.CountryID, fileID uint, req TeacherPremiumVaultCreateRequest, isActive *bool, adminID uint) (*TeacherPremiumVaultFileResponse, error) {
	values := map[string]interface{}{
		"title":         strings.TrimSpace(req.Title),
		"description":   strings.TrimSpace(req.Description),
		"grade_level":   req.GradeLevel,
		"grade_name":    strings.TrimSpace(req.GradeName),
		"subject_id":    req.SubjectID,
		"subject_name":  strings.TrimSpace(req.SubjectName),
		"semester_id":   req.SemesterID,
		"semester_name": strings.TrimSpace(req.SemesterName),
		"category":      strings.TrimSpace(req.Category),
		"file_type":     strings.TrimSpace(req.FileType),
		"updated_by":    adminID,
	}
	if values["category"] == "" {
		values["category"] = "exam"
	}
	if isActive != nil {
		values["is_active"] = *isActive
	}
	file, err := s.repo.AdminUpdateTeacherPremiumFile(countryID, fileID, values)
	if err != nil {
		return nil, err
	}
	items := mapTeacherPremiumVaultFiles([]models.TeacherPremiumFile{*file})
	return &items[0], nil
}

func (s *teacherSubscriptionService) CreateAudit(actorID *uint, userID *uint, entityType string, entityID uint, action, note, ip string) error {
	return s.repo.CreateAuditLog(&models.TeacherAuditLog{
		ActorID:    actorID,
		UserID:     userID,
		EntityType: entityType,
		EntityID:   entityID,
		Action:     action,
		Note:       note,
		IPHash:     hashValue(ip),
	})
}

func (s *teacherSubscriptionService) AdminListAuditLogs(entityType string, entityID uint, limit, offset int) ([]TeacherAuditLogResponse, int64, error) {
	items, total, err := s.repo.ListAuditLogs(entityType, entityID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	return mapTeacherAuditLogs(items), total, nil
}

func (s *teacherSubscriptionService) AdminGetPremiumVaultFileDetail(countryID database.CountryID, fileID uint) (*TeacherPremiumFileDetail, error) {
	file, err := s.repo.GetTeacherPremiumFileAdmin(countryID, fileID)
	if err != nil {
		return nil, err
	}
	fileItems := mapTeacherPremiumVaultFiles([]models.TeacherPremiumFile{*file})
	if len(fileItems) == 0 {
		return nil, gorm.ErrRecordNotFound
	}
	downloads, _, _ := s.repo.AdminListPremiumDownloads(0, 20, 0)
	fileDownloads := make([]models.TeacherPremiumDownload, 0)
	for _, item := range downloads {
		if item.PremiumFileID != nil && *item.PremiumFileID == file.ID {
			fileDownloads = append(fileDownloads, item)
		}
	}
	audits, _, _ := s.repo.ListAuditLogs("teacher_premium_file", file.ID, 20, 0)
	return &TeacherPremiumFileDetail{
		File:      fileItems[0],
		Downloads: mapTeacherPremiumDownloads(fileDownloads),
		AuditLogs: mapTeacherAuditLogs(audits),
	}, nil
}

func (s *teacherSubscriptionService) AdminArchivePremiumVaultFile(countryID database.CountryID, fileID uint, req TeacherArchiveFileRequest, adminID uint, ip string) (*TeacherPremiumVaultFileResponse, error) {
	reason := strings.TrimSpace(req.Reason)
	if reason == "" {
		reason = "Archived by admin"
	}
	file, err := s.repo.ArchiveTeacherPremiumFile(countryID, fileID, reason)
	if err != nil {
		return nil, err
	}
	actorID := adminID
	_ = s.CreateAudit(&actorID, nil, "teacher_premium_file", fileID, "archive", reason, ip)
	items := mapTeacherPremiumVaultFiles([]models.TeacherPremiumFile{*file})
	if len(items) == 0 {
		return nil, gorm.ErrRecordNotFound
	}
	return &items[0], nil
}

func (s *teacherSubscriptionService) AdminRenewSubscription(subscriptionID uint, adminID uint, req TeacherRenewSubscriptionRequest, ip string) (*models.TeacherSubscription, error) {
	newEndsAt := time.Now().AddDate(0, 0, 150)
	if strings.TrimSpace(req.EndsAt) != "" {
		parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(req.EndsAt))
		if err == nil {
			newEndsAt = parsed
		}
	}
	if req.ExtraDays > 0 {
		newEndsAt = time.Now().AddDate(0, 0, req.ExtraDays)
	}
	sub, err := s.repo.RenewSubscription(subscriptionID, newEndsAt, adminID, req.AdminNote)
	if err != nil {
		return nil, err
	}
	_ = s.assignTeacherProRole(sub.UserID)
	InvalidateUserCache(sub.UserID)
	actorID := adminID
	userID := sub.UserID
	_ = s.CreateAudit(&actorID, &userID, "teacher_subscription", sub.ID, "renew", req.AdminNote, ip)
	return sub, nil
}

func (s *teacherSubscriptionService) RunExpiryMaintenance() (map[string]int64, error) {
	now := time.Now()
	expired, err := s.repo.ExpireOverdueSubscriptions(now)
	if err != nil {
		return nil, err
	}
	stats := map[string]int64{"expired": expired, "notices": 0}
	// Lightweight notice generation uses existing active subscriptions list window.
	active, _, err := s.repo.AdminListSubscriptions("active", "", 500, 0)
	if err == nil {
		for _, sub := range active {
			days := int(time.Until(sub.EndsAt).Hours() / 24)
			var noticeType string
			if days <= 14 && days > 7 {
				noticeType = "before_14_days"
			} else if days <= 7 && days >= 0 {
				noticeType = "before_7_days"
			}
			if noticeType != "" {
				err := s.repo.CreateExpiryNotificationIfMissing(&models.TeacherExpiryNotification{
					UserID:         sub.UserID,
					SubscriptionID: sub.ID,
					NoticeType:     noticeType,
					Status:         "pending",
					Message:        fmt.Sprintf("اقترب انتهاء اشتراك المعلم خلال %d يوم", days),
				})
				if err == nil {
					stats["notices"]++
				}
			}
		}
	}
	return stats, nil
}

func mapTeacherAuditLogs(items []models.TeacherAuditLog) []TeacherAuditLogResponse {
	results := make([]TeacherAuditLogResponse, 0, len(items))
	for _, item := range items {
		var actor *TeacherUserMini
		if item.Actor != nil {
			actor = &TeacherUserMini{ID: item.Actor.ID, Name: item.Actor.Name, Email: item.Actor.Email}
		}
		var user *TeacherUserMini
		if item.User != nil {
			user = &TeacherUserMini{ID: item.User.ID, Name: item.User.Name, Email: item.User.Email}
		}
		results = append(results, TeacherAuditLogResponse{
			ID:         item.ID,
			ActorID:    item.ActorID,
			UserID:     item.UserID,
			EntityType: item.EntityType,
			EntityID:   item.EntityID,
			Action:     item.Action,
			Note:       item.Note,
			IPHash:     item.IPHash,
			CreatedAt:  item.CreatedAt.Format(time.RFC3339),
			Actor:      actor,
			User:       user,
		})
	}
	return results
}

func mapTeacherPremiumVaultFiles(files []models.TeacherPremiumFile) []TeacherPremiumVaultFileResponse {
	items := make([]TeacherPremiumVaultFileResponse, 0, len(files))
	for _, file := range files {
		items = append(items, TeacherPremiumVaultFileResponse{
			ID:               file.ID,
			Title:            file.Title,
			Description:      file.Description,
			Country:          file.Country,
			GradeLevel:       file.GradeLevel,
			GradeName:        file.GradeName,
			SubjectID:        file.SubjectID,
			SubjectName:      file.SubjectName,
			SemesterID:       file.SemesterID,
			SemesterName:     file.SemesterName,
			Category:         file.Category,
			OriginalFilename: file.OriginalFilename,
			FileSize:         file.FileSize,
			MimeType:         file.MimeType,
			FileType:         file.FileType,
			IsActive:         file.IsActive,
			DownloadCount:    file.DownloadCount,
			CreatedAt:        file.CreatedAt.Format(time.RFC3339),
		})
	}
	return items
}

func (s *teacherSubscriptionService) AdminFinancialReport() (*TeacherFinancialReport, error) {
	db := s.repo.DB()
	report := &TeacherFinancialReport{}

	_ = db.Model(&models.SubscriptionOrder{}).Where("status = ?", "approved").Select("COALESCE(SUM(amount_jod),0)").Scan(&report.TotalRevenueJOD).Error
	monthStart := time.Now().AddDate(0, 0, -time.Now().Day()+1)
	_ = db.Model(&models.SubscriptionOrder{}).Where("status = ? AND created_at >= ?", "approved", monthStart).Select("COALESCE(SUM(amount_jod),0)").Scan(&report.CurrentMonthRevenue).Error
	_ = db.Model(&models.SubscriptionOrder{}).Where("status = ?", "approved").Count(&report.ApprovedOrders).Error
	_ = db.Model(&models.SubscriptionOrder{}).Where("status = ?", "pending").Count(&report.PendingOrders).Error
	_ = db.Model(&models.SubscriptionOrder{}).Where("status = ?", "rejected").Count(&report.RejectedOrders).Error
	_ = db.Model(&models.TeacherSubscription{}).Where("status = ?", "active").Count(&report.ActiveSubscriptions).Error
	_ = db.Model(&models.TeacherSubscription{}).Where("status = ?", "expired").Count(&report.ExpiredSubscriptions).Error

	return report, nil
}

func (s *teacherSubscriptionService) AdminUsageAnalytics() (*TeacherUsageAnalytics, error) {
	db := s.repo.DB()
	analytics := &TeacherUsageAnalytics{}
	_ = db.Model(&models.TeacherPremiumDownload{}).Count(&analytics.TotalDownloads).Error
	_ = db.Model(&models.TeacherAIGeneration{}).Count(&analytics.TotalAIGenerations).Error
	_ = db.Model(&models.TeacherPremiumFile{}).Where("archived_at IS NULL").Count(&analytics.TotalPremiumFiles).Error
	_ = db.Model(&models.TeacherProfile{}).Count(&analytics.TotalTeachers).Error
	_ = db.Model(&models.TeacherDevice{}).Where("is_active = ?", true).Count(&analytics.ActiveDevices).Error

	analytics.TopSubjects = s.metricQuery("SELECT subject_name AS label, COUNT(*) AS value FROM teacher_premium_downloads GROUP BY subject_name ORDER BY value DESC LIMIT 8")
	analytics.TopCategories = s.metricQuery("SELECT category AS label, COUNT(*) AS value FROM teacher_premium_downloads GROUP BY category ORDER BY value DESC LIMIT 8")
	analytics.TopDownloadedFiles = s.metricQuery("SELECT file_title AS label, COUNT(*) AS value FROM teacher_premium_downloads GROUP BY file_title ORDER BY value DESC LIMIT 8")
	analytics.MostActiveTeachers = s.metricQuery("SELECT COALESCE(u.name, CONCAT('User #', d.user_id)) AS label, COUNT(*) AS value FROM teacher_premium_downloads d LEFT JOIN users u ON u.id = d.user_id GROUP BY d.user_id, u.name ORDER BY value DESC LIMIT 8")

	return analytics, nil
}

func (s *teacherSubscriptionService) metricQuery(sql string) []TeacherMetricItem {
	var items []TeacherMetricItem
	_ = s.repo.DB().Raw(sql).Scan(&items).Error
	return items
}

func (s *teacherSubscriptionService) AdminTeacherDetail(userID uint) (*TeacherAdminDetail, error) {
	db := s.repo.DB()
	var user models.User
	_ = db.First(&user, userID).Error

	profile, _ := s.repo.GetProfile(userID)
	sub, _ := s.repo.GetCurrentSubscription(userID)
	devices, _, _ := s.repo.AdminListDevices(userID, "", 50, 0)
	downloads, _, _ := s.repo.AdminListPremiumDownloads(userID, 50, 0)
	ai, _, _ := s.repo.AdminListAIGenerations(userID, 50, 0)
	orders, _ := s.repo.ListUserOrders(userID, 20)
	audits, _, _ := s.repo.ListAuditLogs("", 0, 50, 0)

	userMini := &TeacherUserMini{ID: user.ID, Name: user.Name, Email: user.Email}
	filteredAudits := make([]models.TeacherAuditLog, 0)
	for _, audit := range audits {
		if audit.UserID != nil && *audit.UserID == userID {
			filteredAudits = append(filteredAudits, audit)
		}
	}

	return &TeacherAdminDetail{
		User:          userMini,
		Profile:       profile,
		Subscription:  sub,
		Devices:       devices,
		Downloads:     mapTeacherPremiumDownloads(downloads),
		AIGenerations: ai,
		Orders:        orders,
		AuditLogs:     mapTeacherAuditLogs(filteredAudits),
	}, nil
}

func (s *teacherSubscriptionService) AdminDeactivateTeacherDevice(deviceID uint, adminID uint, req TeacherDeactivateDeviceRequest, ip string) error {
	if req.UserID == 0 {
		var device models.TeacherDevice
		if err := s.repo.DB().First(&device, deviceID).Error; err != nil {
			return err
		}
		req.UserID = device.UserID
	}
	if err := s.repo.DeactivateDevice(req.UserID, deviceID); err != nil {
		return err
	}
	actorID := adminID
	userID := req.UserID
	_ = s.CreateAudit(&actorID, &userID, "teacher_device", deviceID, "deactivate", req.Note, ip)
	_ = s.repo.CreateTeacherNotification(&models.TeacherNotification{
		UserID:  &userID,
		Type:    "device_deactivated",
		Title:   "تم تعطيل جهاز من حسابك",
		Message: "قامت الإدارة بتعطيل أحد الأجهزة المرتبطة باشتراكك.",
		URL:     "/dashboard/teacher/devices",
	})
	return nil
}

func (s *teacherSubscriptionService) ReactivateSubscription(subscriptionID uint, adminID uint, req TeacherRenewSubscriptionRequest, ip string) (*models.TeacherSubscription, error) {
	if req.ExtraDays <= 0 && strings.TrimSpace(req.EndsAt) == "" {
		req.ExtraDays = 150
	}
	sub, err := s.AdminRenewSubscription(subscriptionID, adminID, req, ip)
	if err != nil {
		return nil, err
	}
	actorID := adminID
	userID := sub.UserID
	_ = s.CreateAudit(&actorID, &userID, "teacher_subscription", sub.ID, "reactivate", req.AdminNote, ip)
	_ = s.repo.CreateTeacherNotification(&models.TeacherNotification{
		UserID:  &sub.UserID,
		Type:    "subscription_reactivated",
		Title:   "تمت إعادة تفعيل اشتراكك",
		Message: "تمت إعادة تفعيل اشتراك المعلم للفصل الدراسي ويمكنك استخدام خدمات Teacher Pro.",
		URL:     "/dashboard/teacher",
	})
	return sub, nil
}

func (s *teacherSubscriptionService) GenerateTeacherAI(userID uint, req TeacherAIGenerateRequest) (*TeacherAIGenerateResponse, error) {
	access, err := s.Access(userID)
	if err != nil {
		return nil, err
	}
	if !access.HasActive || access.Subscription == nil {
		return nil, ErrTeacherPlanNotFound
	}
	if access.Limits["ai_generations"] > 0 && access.Usage["ai_generations"] >= int64(access.Limits["ai_generations"]) {
		return nil, ErrTeacherDeviceLimit
	}

	tool := strings.TrimSpace(req.ToolType)
	if tool == "" {
		tool = "exam"
	}
	title := strings.TrimSpace(req.Title)
	if title == "" {
		title = "مخرج AI للمعلم"
	}
	profile, _ := s.repo.GetProfile(userID)
	subjects := teacherProfileSubjects(profile)
	if len(subjects) == 0 {
		return nil, ErrTeacherPlanNotFound
	}
	// Teacher may now hold up to MaxTeacherSubjects subjects; let them pick
	// which one this generation is for, defaulting to the first and
	// ignoring any subject that isn't actually part of their subscription.
	subject := strings.TrimSpace(req.Subject)
	if subject == "" || !teacherHasSubject(profile, subject) {
		subject = subjects[0]
	}

	grade := strings.TrimSpace(req.Grade)
	semester := strings.TrimSpace(req.Semester)
	promptText := strings.TrimSpace(req.Prompt)

	// Try real AI-generated content first; gracefully fall back to the
	// static template on any failure (missing API key, timeout, provider
	// error) so this feature never breaks the teacher's workflow.
	output, model, aiErr := s.generateRealTeacherAIOutput(tool, title, promptText, grade, subject, semester)
	if aiErr != nil {
		output = buildTeacherAIOutput(tool, title, promptText, grade, subject, semester)
		model = "alemancenter-template-v1"
	}

	item := &models.TeacherAIGeneration{
		UserID:         userID,
		SubscriptionID: access.Subscription.ID,
		ToolType:       tool,
		Title:          title,
		Model:          model,
		Prompt:         promptText,
		Output:         output,
	}
	if err := s.repo.CreateTeacherAIGeneration(item); err != nil {
		return nil, err
	}
	return &TeacherAIGenerateResponse{
		ID:        item.ID,
		ToolType:  item.ToolType,
		Title:     item.Title,
		Output:    item.Output,
		Model:     item.Model,
		CreatedAt: item.CreatedAt.Format(time.RFC3339),
	}, nil
}

func (s *teacherSubscriptionService) ExportTeacherAI(userID uint, generationID uint, format string) (string, string, string, error) {
	item, err := s.repo.GetTeacherAIGeneration(userID, generationID)
	if err != nil {
		return "", "", "", err
	}

	format = strings.ToLower(strings.TrimSpace(format))
	if format == "" {
		format = "word"
	}

	dir := filepath.Join("storage", "private", "teacher-ai-exports", fmt.Sprintf("user-%d", userID))
	if err := os.MkdirAll(dir, 0750); err != nil {
		return "", "", "", err
	}

	name := fmt.Sprintf("teacher-ai-%d.%s", item.ID, "doc")
	contentType := "application/msword"
	body := "<html><head><meta charset=\"utf-8\"></head><body dir=\"rtl\"><h1>" + html.EscapeString(item.Title) + "</h1><pre style=\"font-family:Arial;white-space:pre-wrap\">" + html.EscapeString(item.Output) + "</pre></body></html>"

	if format == "pdf" {
		// Minimal protected export placeholder until real PDF renderer is attached.
		name = fmt.Sprintf("teacher-ai-%d.pdf.html", item.ID)
		contentType = "text/html; charset=utf-8"
		body = "<html><head><meta charset=\"utf-8\"></head><body dir=\"rtl\"><h1>نسخة PDF جاهزة للطباعة</h1><pre style=\"font-family:Arial;white-space:pre-wrap\">" + html.EscapeString(item.Output) + "</pre></body></html>"
	}

	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0640); err != nil {
		return "", "", "", err
	}
	_ = s.repo.IncrementAIGenerationExport(item.ID)
	return path, name, contentType, nil
}

func (s *teacherSubscriptionService) TeacherNotifications(userID uint, limit, offset int) ([]models.TeacherNotification, int64, error) {
	return s.repo.ListTeacherNotifications(userID, limit, offset)
}

func (s *teacherSubscriptionService) PaymentSettings() ([]models.TeacherPaymentSetting, error) {
	return s.repo.ListPaymentSettings()
}

func (s *teacherSubscriptionService) EnsureDefaultPaymentSettings() error {
	defaults := []models.TeacherPaymentSetting{
		{Provider: "manual_bank", DisplayName: "تحويل بنكي يدوي", Instructions: "حوّل قيمة الاشتراك ثم ارفع إثبات الدفع من صفحة الاشتراك.", IsActive: true, SortOrder: 1},
		{Provider: "cliq_manual", DisplayName: "CliQ يدوي", Instructions: "أرسل التحويل عبر CliQ ثم أدخل رقم العملية وارفع صورة الإثبات.", IsActive: true, SortOrder: 2},
	}
	for _, item := range defaults {
		if err := s.repo.UpsertPaymentSetting(&item); err != nil {
			return err
		}
	}
	return nil
}

func (s *teacherSubscriptionService) notifyTeachersAboutPremiumFile(file *models.TeacherPremiumFile) {
	if file == nil {
		return
	}
	var profiles []models.TeacherProfile
	like := "%" + strings.TrimSpace(file.SubjectName) + "%"
	if strings.TrimSpace(file.SubjectName) == "" {
		return
	}
	// Match against BOTH the legacy single `subject` column and the new
	// multi-subject `subjects` JSON column (a teacher may now hold up to 3
	// subjects, and the new file's subject could be any one of them).
	if err := s.repo.DB().Where(
		"subject LIKE ? OR ? LIKE CONCAT('%', subject, '%') OR subjects LIKE ?",
		like, file.SubjectName, like,
	).Find(&profiles).Error; err != nil {
		return
	}
	for _, profile := range profiles {
		userID := profile.UserID
		_ = s.repo.CreateTeacherNotification(&models.TeacherNotification{
			UserID:  &userID,
			Type:    "premium_file_added",
			Title:   "تمت إضافة ملف جديد لمادتك",
			Message: fmt.Sprintf("تمت إضافة ملف Premium جديد: %s", file.Title),
			URL:     "/dashboard/teacher/files",
		})
	}
	_ = s.repo.CreateTeacherNotification(&models.TeacherNotification{
		Type:    "admin_premium_file_added",
		Title:   "تم رفع ملف Premium جديد",
		Message: fmt.Sprintf("تم رفع ملف %s لمادة %s", file.Title, file.SubjectName),
		URL:     "/dashboard/teacher-subscriptions/premium-files",
	})
}

var teacherAIHTMLTagRe = regexp.MustCompile(`<[^>]+>`)

func stripHTMLTagsForTeacherAI(value string) string {
	return strings.TrimSpace(teacherAIHTMLTagRe.ReplaceAllString(value, " "))
}

// teacherAIToolLabel returns the Arabic display label for a teacher AI tool type.
func teacherAIToolLabel(tool string) string {
	switch tool {
	case "answer_key":
		return "نموذج إجابة"
	case "worksheet":
		return "ورقة عمل"
	case "remedial_plan":
		return "خطة علاجية"
	case "content_analysis":
		return "تحليل محتوى"
	default:
		return "نموذج امتحان"
	}
}

// teacherAIInstruction returns the generation instruction injected as
// "curriculum context" so the shared SEO/content AI engine produces content
// shaped like the requested teacher tool instead of a generic blog article.
func teacherAIInstruction(tool string) string {
	switch tool {
	case "answer_key":
		return "أنشئ نموذج إجابة (Answer Key) دقيق ومفصل لامتحان في هذه المادة، يشمل الإجابة الصحيحة المتوقعة لكل سؤال مع معيار تصحيح مختصر وملاحظات حول الأخطاء الشائعة. لا تكتب مقالاً عاماً؛ اكتب نموذج إجابة مرقّماً وواضحاً."
	case "worksheet":
		return "أنشئ ورقة عمل تعليمية مرقّمة ومتدرجة الصعوبة تشمل: أسئلة تهيئة، تدريبات فردية، نشاطاً تطبيقياً، وتقويماً ختامياً، مناسبة لمستوى الصف والفصل المحددين. لا تكتب مقالاً عاماً؛ اكتب ورقة عمل عملية بصيغة تمارين."
	case "remedial_plan":
		return "أنشئ خطة علاجية تفصيلية لمعالجة الفاقد التعليمي تشمل: الأهداف، اختباراً تشخيصياً مقترحاً، إجراءات أسبوعية مرقّمة، وأساليب قياس التحسن. لا تكتب مقالاً عاماً؛ اكتب خطة عملية للمعلم."
	case "content_analysis":
		return "أنشئ تحليل محتوى تربوي شامل يوضح المفاهيم الرئيسية، المهارات المستهدفة (تذكر/فهم/تطبيق/تحليل)، ونواتج التعلم المتوقعة لهذا الموضوع. لا تكتب مقالاً عاماً؛ اكتب تحليلاً تربوياً مبوباً."
	default:
		return "أنشئ نموذج امتحان متكامل يشمل: أسئلة اختيار من متعدد، أسئلة صح أو خطأ، وأسئلة مقالية قصيرة مرقّمة، مناسبة تماماً لمستوى الصف والمادة والفصل الدراسي المحددين. لا تكتب مقالاً عاماً؛ اكتب أسئلة امتحان فعلية."
	}
}

// generateRealTeacherAIOutput asks the shared AI content engine (used
// elsewhere for SEO article generation) to produce real, context-aware
// educational content for a teacher tool request. It reuses
// GenerateSEOArticleWithContext rather than a bespoke prompt pipeline, so it
// inherits the same provider/model fallback chain already battle-tested for
// SEO generation. Returns an error if the AI service isn't configured or the
// call fails — callers should fall back to buildTeacherAIOutput in that case.
func (s *teacherSubscriptionService) generateRealTeacherAIOutput(tool, title, prompt, grade, subject, semester string) (string, string, error) {
	if s.ai == nil {
		return "", "", errors.New("teacher AI: ai service not configured")
	}

	label := teacherAIToolLabel(tool)
	instruction := teacherAIInstruction(tool)
	if strings.TrimSpace(prompt) != "" {
		instruction = instruction + "\nملاحظات إضافية من المعلم: " + strings.TrimSpace(prompt)
	}

	safeTitle := strings.TrimSpace(title)
	if safeTitle == "" {
		safeTitle = label
	}
	aiTitle := fmt.Sprintf("%s: %s", label, safeTitle)

	article, err := s.ai.GenerateSEOArticleWithContext(aiTitle, "article", SEOGenerationContext{
		SubjectName:       subject,
		GradeName:         grade,
		SemesterName:      semester,
		CurriculumContext: instruction,
	})
	if err != nil {
		return "", "", err
	}
	if article == nil {
		return "", "", errors.New("teacher AI: empty response")
	}

	content := strings.TrimSpace(article.Content)
	if content == "" {
		content = stripHTMLTagsForTeacherAI(article.ContentHTML)
	}
	if content == "" {
		return "", "", errors.New("teacher AI: response had no usable content")
	}

	header := fmt.Sprintf(
		"%s: %s\nالمادة: %s\nالصف: %s\nالفصل: %s\n\n",
		label, safeTitle,
		firstNonEmpty(subject, "المادة المختارة"),
		firstNonEmpty(grade, "الصف المختار"),
		firstNonEmpty(semester, "الفصل الدراسي"),
	)
	output := header + content

	if len(article.FAQ) > 0 {
		faqLines := make([]string, 0, len(article.FAQ))
		for i, item := range article.FAQ {
			faqLines = append(faqLines, fmt.Sprintf("%d. %s\n%s", i+1, item.Question, item.Answer))
		}
		output += "\n\nأسئلة/بنود إضافية:\n" + strings.Join(faqLines, "\n\n")
	}

	return output, "together-ai", nil
}

func buildTeacherAIOutput(tool, title, prompt, grade, subject, semester string) string {
	if subject == "" {
		subject = "المادة المختارة"
	}
	if grade == "" {
		grade = "الصف المختار"
	}
	if semester == "" {
		semester = "الفصل الدراسي"
	}

	switch tool {
	case "answer_key":
		return fmt.Sprintf("نموذج إجابة: %s\nالمادة: %s\nالصف: %s\nالفصل: %s\n\n1. الإجابة الأولى مع معيار التصحيح.\n2. الإجابة الثانية مع توزيع العلامات.\n3. ملاحظات للمعلم حول الأخطاء الشائعة.\n\nملاحظات إضافية:\n%s", title, subject, grade, semester, prompt)
	case "worksheet":
		return fmt.Sprintf("ورقة عمل: %s\nالمادة: %s\nالصف: %s\nالفصل: %s\n\nأولًا: أسئلة تهيئة.\nثانيًا: تدريب فردي.\nثالثًا: نشاط تطبيقي.\nرابعًا: تقويم ختامي.\n\nتعليمات المعلم:\n%s", title, subject, grade, semester, prompt)
	case "remedial_plan":
		return fmt.Sprintf("خطة علاجية: %s\nالمادة: %s\nالصف: %s\nالفصل: %s\n\nالأهداف:\n- معالجة الفاقد التعليمي.\n- رفع مستوى الطلبة الضعاف.\n\nالإجراءات:\n1. اختبار تشخيصي.\n2. تقسيم الطلبة حسب المستوى.\n3. أنشطة علاجية قصيرة.\n4. قياس التحسن أسبوعيًا.\n\nملاحظات:\n%s", title, subject, grade, semester, prompt)
	case "content_analysis":
		return fmt.Sprintf("تحليل محتوى: %s\nالمادة: %s\nالصف: %s\nالفصل: %s\n\nالمفاهيم الرئيسية:\n- مفهوم 1\n- مفهوم 2\n\nالمهارات:\n- تذكر\n- فهم\n- تطبيق\n\nنواتج التعلم:\n- يوضح الطالب المفهوم.\n- يطبق الطالب المهارة.\n\nملاحظات:\n%s", title, subject, grade, semester, prompt)
	default:
		return fmt.Sprintf("نموذج امتحان: %s\nالمادة: %s\nالصف: %s\nالفصل: %s\n\nالسؤال الأول: اختر الإجابة الصحيحة.\nالسؤال الثاني: أجب بصح أو خطأ.\nالسؤال الثالث: أسئلة مقالية قصيرة.\nالسؤال الرابع: تطبيق عملي/تحليلي.\n\nتعليمات خاصة:\n%s", title, subject, grade, semester, prompt)
	}
}

func BuildTeacherWatermarkText(userID uint, downloadCode string) string {
	return fmt.Sprintf("Alemancenter Teacher Pro | User:%d | Code:%s | %s", userID, downloadCode, time.Now().Format("2006-01-02"))
}

func subjectsMatch(userSubject, fileSubject string) bool {
	userSubject = strings.TrimSpace(strings.ToLower(userSubject))
	fileSubject = strings.TrimSpace(strings.ToLower(fileSubject))
	if userSubject == "" || fileSubject == "" {
		return true
	}
	return strings.Contains(fileSubject, userSubject) || strings.Contains(userSubject, fileSubject)
}

func normalizeFileType(fileType, filename string) string {
	fileType = strings.TrimSpace(fileType)
	if fileType != "" {
		return fileType
	}
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(filename)), ".")
	if ext == "" {
		return "file"
	}
	return ext
}

func (s *teacherSubscriptionService) AdminListPremiumFiles(countryID database.CountryID, search, premium, category, subject string, limit, offset int) ([]TeacherPremiumFileResponse, int64, error) {
	files, total, err := s.repo.AdminListFilesForPremium(countryID, strings.TrimSpace(search), strings.TrimSpace(premium), strings.TrimSpace(category), strings.TrimSpace(subject), limit, offset)
	if err != nil {
		return nil, 0, err
	}
	return mapTeacherPremiumFiles(files), total, nil
}

func (s *teacherSubscriptionService) AdminUpdatePremiumFile(countryID database.CountryID, fileID uint, req TeacherPremiumFileAdminRequest) (*TeacherPremiumFileResponse, error) {
	audience := strings.TrimSpace(req.PremiumAudience)
	if audience == "" {
		audience = "teacher"
	}

	values := map[string]interface{}{
		"is_premium":                    req.IsPremium,
		"premium_audience":              audience,
		"premium_category":              strings.TrimSpace(req.PremiumCategory),
		"premium_subject":               strings.TrimSpace(req.PremiumSubject),
		"premium_requires_subscription": req.PremiumRequiresSubscription,
	}

	if req.IsPremium {
		values["premium_audience"] = "teacher"
		if strings.TrimSpace(req.PremiumCategory) == "" {
			values["premium_category"] = "exam"
		}
		values["premium_requires_subscription"] = true
	}

	file, err := s.repo.AdminUpdateFilePremium(countryID, fileID, values)
	if err != nil {
		return nil, err
	}

	items := mapTeacherPremiumFiles([]models.File{*file})
	if len(items) == 0 {
		return nil, gorm.ErrRecordNotFound
	}
	return &items[0], nil
}

func (s *teacherSubscriptionService) AdminDisablePremiumFile(countryID database.CountryID, fileID uint) (*TeacherPremiumFileResponse, error) {
	file, err := s.repo.AdminUpdateFilePremium(countryID, fileID, map[string]interface{}{
		"is_premium":                    false,
		"premium_audience":              "",
		"premium_category":              "",
		"premium_subject":               "",
		"premium_requires_subscription": false,
	})
	if err != nil {
		return nil, err
	}

	items := mapTeacherPremiumFiles([]models.File{*file})
	if len(items) == 0 {
		return nil, gorm.ErrRecordNotFound
	}
	return &items[0], nil
}

func (s *teacherSubscriptionService) AdminCancelSubscription(subscriptionID uint, adminID uint, req TeacherCancelRequest) (*models.TeacherSubscription, error) {
	subscription, err := s.repo.GetSubscriptionByID(subscriptionID)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	subscription.Status = "cancelled"
	subscription.CancelledAt = &now
	if strings.TrimSpace(req.AdminNote) != "" {
		subscription.AdminNote = strings.TrimSpace(req.AdminNote)
	}
	if err := s.repo.UpdateSubscription(subscription); err != nil {
		return nil, err
	}

	// If the user has no other active subscription, remove Teacher Pro permissions immediately.
	if active, err := s.repo.GetCurrentSubscription(subscription.UserID); err == nil && active != nil && active.ID != subscription.ID {
		// Keep Teacher Pro because another active subscription exists.
		InvalidateUserCache(subscription.UserID)
	} else {
		_ = s.removeTeacherProRole(subscription.UserID)
		for _, permissionName := range TeacherSemesterPermissions {
			_ = s.repo.DB().Exec(`
				DELETE mhp FROM model_has_permissions mhp
				INNER JOIN permissions p ON p.id = mhp.permission_id
				WHERE mhp.model_id = ? AND p.name = ?
			`, subscription.UserID, permissionName).Error
		}
		_ = s.repo.DeactivateAllDevices(subscription.UserID)
		InvalidateUserCache(subscription.UserID)
	}

	return subscription, nil
}

func (s *teacherSubscriptionService) AdminRemoveTeacherMembership(userID uint, adminID uint, req TeacherCancelRequest) error {
	return s.revokeTeacherAccessHard(userID, req.AdminNote)
}

func (s *teacherSubscriptionService) removeTeacherProRole(userID uint) error {
	role, err := s.EnsureTeacherProRole()
	if err != nil {
		return err
	}
	return s.repo.DB().Exec(
		"DELETE FROM model_has_roles WHERE role_id = ? AND model_id = ?",
		role.ID, userID,
	).Error
}

func (s *teacherSubscriptionService) revokeTeacherAccessHard(userID uint, adminNote string) error {
	note := strings.TrimSpace(adminNote)
	if note == "" {
		note = "Teacher Pro access revoked by admin"
	}

	if err := s.repo.CancelActiveSubscriptionsForUser(userID, note); err != nil {
		return err
	}

	_ = s.repo.DeactivateAllDevices(userID)
	_ = s.repo.DeleteTeacherProfile(userID)
	_ = s.removeTeacherProRole(userID)

	// Remove any direct teacher permissions that may have been granted manually.
	db := s.repo.DB()
	for _, permissionName := range TeacherSemesterPermissions {
		if err := db.Exec(`
			DELETE mhp FROM model_has_permissions mhp
			INNER JOIN permissions p ON p.id = mhp.permission_id
			WHERE mhp.model_id = ? AND p.name = ?
		`, userID, permissionName).Error; err != nil {
			return err
		}
	}

	InvalidateUserCache(userID)
	return nil
}

func (s *teacherSubscriptionService) ApproveOrder(orderID uint, adminID uint, req TeacherOrderReviewRequest) (*models.TeacherSubscription, error) {
	order, err := s.repo.GetOrder(orderID)
	if err != nil {
		return nil, err
	}
	if order.Status != "pending" {
		return nil, ErrTeacherOrderNotPending
	}
	plan := order.Plan
	if plan == nil {
		plan, err = s.PublicPlan()
		if err != nil {
			return nil, err
		}
	}

	now := time.Now()
	reviewer := adminID
	order.Status = "approved"
	order.ReviewedBy = &reviewer
	order.ReviewedAt = &now
	order.AdminNote = strings.TrimSpace(req.AdminNote)
	if err := s.repo.UpdateOrder(order); err != nil {
		return nil, err
	}

	sub := &models.TeacherSubscription{
		UserID:            order.UserID,
		PlanID:            plan.ID,
		Status:            "active",
		StartsAt:          now,
		EndsAt:            now.AddDate(0, 0, plan.DurationDays),
		PriceJOD:          order.AmountJOD,
		DeviceLimit:       plan.DeviceLimit,
		DownloadLimit:     plan.DownloadLimit,
		AIGenerationLimit: plan.AIGenerationLimit,
		ExportLimit:       plan.ExportLimit,
		ActivatedBy:       &reviewer,
		AdminNote:         order.AdminNote,
	}
	if err := s.repo.CreateSubscription(sub); err != nil {
		return nil, err
	}
	if err := s.assignTeacherProRole(order.UserID); err != nil {
		return nil, err
	}
	_ = s.repo.CreateTeacherNotification(&models.TeacherNotification{UserID: &order.UserID, Type: "subscription_approved", Title: "تم قبول اشتراكك", Message: "تم تفعيل اشتراك المعلم للفصل الدراسي.", URL: "/dashboard/teacher"})
	return sub, nil
}

func (s *teacherSubscriptionService) RejectOrder(orderID uint, adminID uint, req TeacherOrderReviewRequest) error {
	order, err := s.repo.GetOrder(orderID)
	if err != nil {
		return err
	}
	if order.Status != "pending" {
		return ErrTeacherOrderNotPending
	}
	now := time.Now()
	reviewer := adminID
	order.Status = "rejected"
	order.ReviewedBy = &reviewer
	order.ReviewedAt = &now
	order.AdminNote = strings.TrimSpace(req.AdminNote)
	err = s.repo.UpdateOrder(order)
	if err == nil {
		_ = s.repo.CreateTeacherNotification(&models.TeacherNotification{UserID: &order.UserID, Type: "subscription_rejected", Title: "تم رفض طلب الاشتراك", Message: order.AdminNote, URL: "/dashboard/teacher/subscription"})
	}
	return err
}

func (s *teacherSubscriptionService) RegisterDevice(userID uint, ip, userAgent, label string) error {
	sub, err := s.repo.GetCurrentSubscription(userID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}

	deviceHash := hashValue(userAgent + "|" + label)
	if deviceHash == "" {
		deviceHash = hashValue(userAgent)
	}
	count, err := s.repo.CountActiveDevices(userID)
	if err != nil {
		return err
	}

	devices, _ := s.repo.ListDevices(userID)
	exists := false
	for _, d := range devices {
		if d.DeviceHash == deviceHash && d.IsActive {
			exists = true
			break
		}
	}
	if !exists && count >= int64(sub.DeviceLimit) {
		return ErrTeacherDeviceLimit
	}

	return s.repo.UpsertDevice(&models.TeacherDevice{
		UserID:     userID,
		DeviceHash: deviceHash,
		IPHash:     hashValue(ip),
		UserAgent:  trimString(userAgent, 500),
		Label:      trimString(label, 255),
		IsActive:   true,
	})
}

func (s *teacherSubscriptionService) ListDevices(userID uint) ([]models.TeacherDevice, error) {
	return s.repo.ListDevices(userID)
}

func (s *teacherSubscriptionService) DeactivateDevice(userID, deviceID uint) error {
	return s.repo.DeactivateDevice(userID, deviceID)
}

func parseStringList(plan *models.SubscriptionPlan) []string {
	if plan == nil || strings.TrimSpace(plan.PermissionsJSON) == "" {
		return nil
	}
	var values []string
	if err := json.Unmarshal([]byte(plan.PermissionsJSON), &values); err != nil {
		return nil
	}
	return values
}

func (s *teacherSubscriptionService) Workspace(userID uint) (*TeacherWorkspaceSummary, error) {
	access, err := s.Access(userID)
	if err != nil {
		return nil, err
	}
	profile, _ := s.repo.GetProfile(userID)
	subjects := teacherProfileSubjects(profile)
	subject := ""
	if len(subjects) > 0 {
		subject = subjects[0]
	}

	libraryCount := int64(0)
	if _, total, err := s.repo.ListLibraryItems(userID, 1, 0); err == nil {
		libraryCount = total
	}
	downloads := access.Usage["downloads"]
	ai := access.Usage["ai_generations"]

	return &TeacherWorkspaceSummary{
		Subject:      subject,
		Subjects:     subjects,
		Subscription: access.Subscription,
		Usage:        access.Usage,
		Limits:       access.Limits,
		Stats: map[string]int64{
			"library_items":  libraryCount,
			"downloads":      downloads,
			"ai_generations": ai,
		},
		QuickLinks: []TeacherWorkspaceLink{
			{Title: "ملفات المعلم", Description: "كل ملفات Premium الخاصة بمادتك", Href: "/dashboard/teacher/files", Category: "files"},
			{Title: "نماذج الامتحانات", Description: "اختبارات ونماذج إجابة منظمة", Href: "/dashboard/teacher/exams", Category: "exam"},
			{Title: "الخطط وتحليل المحتوى", Description: "خطط فصلية وتحليل محتوى وخطط علاجية", Href: "/dashboard/teacher/plans", Category: "plan"},
			{Title: "أوراق العمل", Description: "أوراق عمل ومراجعات وأنشطة صفية", Href: "/dashboard/teacher/worksheets", Category: "worksheet"},
			{Title: "مكتبتي", Description: "ملفاتك المحفوظة ومخرجاتك القادمة", Href: "/dashboard/teacher/library", Category: "library"},
			{Title: "أدوات المعلم الذكية", Description: "إنشاء اختبار، نموذج إجابة، ورقة عمل، خطة علاجية", Href: "/dashboard/teacher/ai-tools", Category: "ai"},
		},
	}, nil
}

func (s *teacherSubscriptionService) PremiumFiles(userID uint, countryID database.CountryID, category, query string, limit, offset int) ([]TeacherPremiumFileResponse, int64, error) {
	vaultFiles, total, err := s.PremiumVaultFiles(userID, countryID, category, query, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	items := make([]TeacherPremiumFileResponse, 0, len(vaultFiles))
	for _, file := range vaultFiles {
		items = append(items, TeacherPremiumFileResponse{
			ID:              file.ID,
			FileName:        file.Title,
			FileType:        file.FileType,
			FileCategory:    file.Category,
			FileSize:        file.FileSize,
			MimeType:        file.MimeType,
			PremiumCategory: file.Category,
			PremiumSubject:  file.SubjectName,
			DownloadCount:   file.DownloadCount,
			CreatedAt:       file.CreatedAt,
			ArticleTitle:    file.Description,
			SubjectName:     file.SubjectName,
			SemesterName:    file.SemesterName,
		})
	}
	return items, total, nil
}

func mapTeacherPremiumFiles(files []models.File) []TeacherPremiumFileResponse {
	items := make([]TeacherPremiumFileResponse, 0, len(files))
	for _, file := range files {
		category := ""
		if file.FileCategory != nil {
			category = *file.FileCategory
		}
		item := TeacherPremiumFileResponse{
			ID:              file.ID,
			FileName:        file.FileName,
			FileType:        file.FileType,
			FileCategory:    category,
			FileSize:        file.FileSize,
			MimeType:        file.MimeType,
			PremiumCategory: file.PremiumCategory,
			PremiumSubject:  file.PremiumSubject,
			DownloadCount:   file.DownloadCount,
			CreatedAt:       file.CreatedAt.Format(time.RFC3339),
		}
		if file.Article != nil {
			item.ArticleTitle = file.Article.Title
			if file.Article.Subject != nil {
				item.SubjectName = file.Article.Subject.SubjectName
			}
			if file.Article.Semester != nil {
				item.SemesterName = file.Article.Semester.SemesterName
			}
		}
		items = append(items, item)
	}
	return items
}

func (s *teacherSubscriptionService) SaveLibraryItem(userID uint, req TeacherSaveLibraryRequest) (*models.TeacherLibraryItem, error) {
	itemType := strings.TrimSpace(req.ItemType)
	if itemType == "" {
		itemType = "file"
	}
	if req.ItemID != nil {
		if existing, err := s.repo.FindLibraryItem(userID, itemType, req.ItemID); err == nil && existing != nil && existing.ID > 0 {
			return existing, nil
		}
	}

	title := strings.TrimSpace(req.Title)
	if title == "" {
		title = "عنصر محفوظ"
	}

	item := &models.TeacherLibraryItem{
		UserID:     userID,
		ItemType:   itemType,
		ItemID:     req.ItemID,
		Title:      title,
		SourceType: strings.TrimSpace(req.SourceType),
		Category:   strings.TrimSpace(req.Category),
		Country:    strings.TrimSpace(req.Country),
	}
	if item.SourceType == "" {
		item.SourceType = "premium_file"
	}
	if item.Country == "" {
		item.Country = "jo"
	}
	if err := s.repo.CreateLibraryItem(item); err != nil {
		return nil, err
	}
	return item, nil
}

func (s *teacherSubscriptionService) Library(userID uint, limit, offset int) ([]models.TeacherLibraryItem, int64, error) {
	return s.repo.ListLibraryItems(userID, limit, offset)
}

func (s *teacherSubscriptionService) Downloads(userID uint, limit, offset int) ([]TeacherPremiumDownloadResponse, int64, error) {
	items, total, err := s.repo.ListPremiumDownloads(userID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	return mapTeacherPremiumDownloads(items), total, nil
}

func (s *teacherSubscriptionService) AIGenerations(userID uint, limit, offset int) ([]models.TeacherAIGeneration, int64, error) {
	return s.repo.ListAIGenerations(userID, limit, offset)
}

func mapTeacherPremiumDownloads(items []models.TeacherPremiumDownload) []TeacherPremiumDownloadResponse {
	results := make([]TeacherPremiumDownloadResponse, 0, len(items))
	for _, item := range items {
		var user *TeacherUserMini
		if item.User != nil {
			user = &TeacherUserMini{
				ID:    item.User.ID,
				Name:  item.User.Name,
				Email: item.User.Email,
			}
		}

		results = append(results, TeacherPremiumDownloadResponse{
			ID:               item.ID,
			UserID:           item.UserID,
			SubscriptionID:   item.SubscriptionID,
			PremiumFileID:    item.PremiumFileID,
			Country:          item.Country,
			FileTitle:        item.FileTitle,
			OriginalFilename: item.OriginalFilename,
			SubjectName:      item.SubjectName,
			Category:         item.Category,
			FileSize:         item.FileSize,
			MimeType:         item.MimeType,
			DownloadCode:     item.DownloadCode,
			IPHash:           item.IPHash,
			UserAgentHash:    item.UserAgentHash,
			CreatedAt:        item.CreatedAt.Format(time.RFC3339),
			WatermarkApplied: item.WatermarkApplied,
			User:             user,
		})
	}
	return results
}

func hashValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func trimString(v string, max int) string {
	v = strings.TrimSpace(v)
	if max > 0 && len(v) > max {
		return v[:max]
	}
	return v
}
