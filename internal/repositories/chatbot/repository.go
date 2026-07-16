package chatbot

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"sync"

	"github.com/alemancenter/fiber-api/internal/database"
	"github.com/alemancenter/fiber-api/internal/models"
	"github.com/alemancenter/fiber-api/pkg/logger"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

type ContentResult struct {
	ID          uint   `json:"id"`
	Title       string `json:"title"`
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`
	URL         string `json:"url"`
	Grade       string `json:"grade,omitempty"`
	Subject     string `json:"subject,omitempty"`
	Semester    string `json:"semester,omitempty"`
	Category    string `json:"category,omitempty"`
	Score       int    `json:"score,omitempty"`
}

type Repository interface {
	FindOrCreateSession(countryID database.CountryID, userID *uint, guestID string) (*models.ChatSession, error)
	UpdateSessionState(countryID database.CountryID, sessionID uint, intent, step string, context map[string]interface{}) error
	CreateMessage(countryID database.CountryID, message *models.ChatMessage) error
	FindKnowledge(countryID database.CountryID, countryCode, query, intent string, limit int) ([]models.ChatKnowledgeBase, error)
	SearchContent(countryID database.CountryID, query string, limit int) ([]ContentResult, error)
	CreateFeedback(countryID database.CountryID, feedback *models.ChatFeedback) error
	ListSessions(countryID database.CountryID, limit int) ([]models.ChatSession, error)
	ListSessionsPaginated(countryID database.CountryID, limit, offset int) ([]models.ChatSession, int64, error)
	GetSessionWithMessages(countryID database.CountryID, sessionID uint) (*models.ChatSession, error)
	GetSessionsWithMessages(countryID database.CountryID, ids []uint) ([]models.ChatSession, error)
	DeleteSessions(countryID database.CountryID, ids []uint) (int64, error)
	ListKnowledge(countryID database.CountryID, countryCode string, limit int) ([]models.ChatKnowledgeBase, error)
	CreateKnowledge(countryID database.CountryID, item *models.ChatKnowledgeBase) error
	UpdateKnowledge(countryID database.CountryID, item *models.ChatKnowledgeBase) error
	DeleteKnowledge(countryID database.CountryID, id uint) error
	CreateAIUsage(countryID database.CountryID, usage *models.ChatAIUsage) error
}

type repository struct {
	migrated sync.Map
}

func NewRepository() Repository { return &repository{} }

func (r *repository) db(countryID database.CountryID) *gorm.DB {
	db := database.DBForCountry(countryID)
	r.ensureSchema(countryID, db)
	return db
}

func (r *repository) ensureSchema(countryID database.CountryID, db *gorm.DB) {
	if db == nil {
		return
	}
	key := fmt.Sprintf("%p:%s", db, database.CountryCode(countryID))
	if _, ok := r.migrated.Load(key); ok {
		return
	}
	if err := db.AutoMigrate(
		&models.ChatSession{},
		&models.ChatMessage{},
		&models.ChatKnowledgeBase{},
		&models.ChatFeedback{},
		&models.ChatAIUsage{},
	); err != nil {
		logger.Warn("chatbot auto-migrate failed", zap.String("country", database.CountryCode(countryID)), zap.Error(err))
		return
	}
	seedDefaultKnowledge(db, database.CountryCode(countryID))
	r.migrated.Store(key, true)
}

func normalizeJSON(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "{}"
	}
	if !json.Valid([]byte(value)) {
		return "{}"
	}
	return value
}

func (r *repository) FindOrCreateSession(countryID database.CountryID, userID *uint, guestID string) (*models.ChatSession, error) {
	db := r.db(countryID)
	var session models.ChatSession
	q := db.Where("status = ?", "open")
	if userID != nil && *userID > 0 {
		q = q.Where("user_id = ?", *userID)
	} else {
		q = q.Where("guest_id = ?", guestID)
	}
	if err := q.Order("updated_at DESC").First(&session).Error; err == nil {
		return &session, nil
	}
	session = models.ChatSession{UserID: userID, GuestID: guestID, CountryCode: database.CountryCode(countryID), Status: "open", ContextData: "{}"}
	if err := db.Create(&session).Error; err != nil {
		return nil, err
	}
	return &session, nil
}

func (r *repository) UpdateSessionState(countryID database.CountryID, sessionID uint, intent, step string, context map[string]interface{}) error {
	if sessionID == 0 {
		return nil
	}
	if context == nil {
		context = map[string]interface{}{}
	}
	contextJSON, _ := json.Marshal(context)
	updates := map[string]interface{}{
		"last_intent":    intent,
		"current_intent": intent,
		"current_step":   step,
		"context_data":   normalizeJSON(string(contextJSON)),
	}
	return r.db(countryID).Model(&models.ChatSession{}).Where("id = ?", sessionID).Updates(updates).Error
}

func (r *repository) CreateMessage(countryID database.CountryID, message *models.ChatMessage) error {
	message.Metadata = normalizeJSON(message.Metadata)
	return r.db(countryID).Create(message).Error
}

func (r *repository) FindKnowledge(countryID database.CountryID, countryCode, query, intent string, limit int) ([]models.ChatKnowledgeBase, error) {
	if limit <= 0 || limit > 10 {
		limit = 5
	}
	q := "%" + strings.TrimSpace(query) + "%"
	intentLike := "%" + strings.TrimSpace(intent) + "%"
	items := make([]models.ChatKnowledgeBase, 0)
	db := r.db(countryID).Model(&models.ChatKnowledgeBase{}).
		Where("is_active = ?", true).
		Where("country_code = ? OR country_code = ? OR country_code = ''", countryCode, "all")
	if strings.TrimSpace(query) != "" {
		db = db.Where("question LIKE ? OR title LIKE ? OR keywords LIKE ? OR category LIKE ? OR category LIKE ?", q, q, q, q, intentLike)
	}
	err := db.Order("priority ASC, updated_at DESC").Limit(limit).Find(&items).Error
	return items, err
}

func (r *repository) SearchContent(countryID database.CountryID, query string, limit int) ([]ContentResult, error) {
	if limit <= 0 || limit > 12 {
		limit = 8
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return []ContentResult{}, nil
	}

	terms := searchTerms(query)
	expandedTerms := expandSearchTerms(terms)
	cc := database.CountryCode(countryID)
	db := r.db(countryID)

	// Collect a wide candidate pool, then rank by relevance and trim to `limit`.
	// Capping the pool at `limit` *before* scoring (the old behaviour) discarded
	// the truly relevant item whenever more popular loose matches came first.
	const collectCap = 60
	results := make([]ContentResult, 0, collectCap)
	seen := map[string]bool{}

	appendResult := func(item ContentResult) {
		key := item.Type + ":" + uintToString(item.ID)
		if seen[key] || len(results) >= collectCap {
			return
		}
		// Score against the expanded terms so synonyms (امتحان↔اختبار, الثاني↔2)
		// still credit a match.
		item.Score = scoreContentResult(query, expandedTerms, item)
		seen[key] = true
		results = append(results, item)
	}

	// Files first: users usually ask for امتحان/اختبار/نموذج/ملف.
	var files []models.File
	fileDB := db.Model(&models.File{}).
		Preload("Article").
		Preload("Article.Subject").
		Preload("Article.Semester").
		Preload("Post").
		Select("id, article_id, post_id, file_name, file_category, created_at, download_count, view_count, views_count")
	fileDB = applySmartMatch(fileDB, []string{"file_name", "file_category"}, expandedTerms, true)
	if err := fileDB.Order("download_count DESC, view_count DESC, views_count DESC, created_at DESC").Limit(collectCap).Find(&files).Error; err == nil {
		for _, f := range files {
			desc := ""
			if f.FileCategory != nil {
				desc = *f.FileCategory
			}
			item := ContentResult{ID: f.ID, Title: f.FileName, Type: "file", Description: desc, URL: "/download/" + uintToString(f.ID), Category: desc}
			if f.Article != nil {
				item.Grade = gradeNameFromArticle(f.Article)
				if f.Article.Subject != nil {
					item.Subject = f.Article.Subject.SubjectName
				}
				if f.Article.Semester != nil {
					item.Semester = f.Article.Semester.SemesterName
				}
				if item.Description == "" && f.Article.MetaDescription != nil {
					item.Description = *f.Article.MetaDescription
				}
			}
			if f.Post != nil && item.Description == "" && f.Post.MetaDescription != nil {
				item.Description = *f.Post.MetaDescription
			}
			appendResult(item)
		}
	}

	// Articles with joined subject/semester names.
	if len(results) < collectCap {
		var articles []models.Article
		articleDB := db.Model(&models.Article{}).
			Preload("Subject").
			Preload("Semester").
			Select("id,title,meta_description,grade_level,subject_id,semester_id,visit_count,created_at").
			Where("status = ?", 1)
		articleDB = applySmartMatch(articleDB, []string{"title", "meta_description", "content"}, expandedTerms, true)
		if err := articleDB.Order("visit_count DESC, created_at DESC").Limit(collectCap).Find(&articles).Error; err == nil {
			for _, a := range articles {
				desc := ""
				if a.MetaDescription != nil {
					desc = *a.MetaDescription
				}
				item := ContentResult{ID: a.ID, Title: a.Title, Type: "article", Description: desc, URL: "/" + cc + "/lesson/articles/" + uintToString(a.ID), Grade: gradeNameFromArticle(&a)}
				if a.Subject != nil {
					item.Subject = a.Subject.SubjectName
				}
				if a.Semester != nil {
					item.Semester = a.Semester.SemesterName
				}
				appendResult(item)
			}
		}
	}

	// Posts with keywords/category-like content.
	if len(results) < collectCap {
		var posts []models.Post
		postDB := db.Model(&models.Post{}).
			Select("id,title,meta_description,keywords,country,views,created_at").
			Where("is_active = ?", true).
			Where("country = ? OR country = '' OR country IS NULL", string(cc))
		postDB = applySmartMatch(postDB, []string{"title", "meta_description", "keywords", "content"}, expandedTerms, true)
		if err := postDB.Order("views DESC, created_at DESC").Limit(collectCap).Find(&posts).Error; err == nil {
			for _, p := range posts {
				desc := ""
				if p.MetaDescription != nil {
					desc = *p.MetaDescription
				}
				appendResult(ContentResult{ID: p.ID, Title: p.Title, Type: "post", Description: desc, URL: "/" + cc + "/posts/" + uintToString(p.ID)})
			}
		}
	}

	// If the first pass is too narrow, retry with strict=false and fewer core terms.
	if len(results) == 0 && len(expandedTerms) > 1 {
		core := coreSearchTerms(expandedTerms)
		if len(core) > 0 && len(core) < len(expandedTerms) {
			var articles []models.Article
			articleDB := db.Model(&models.Article{}).
				Preload("Subject").
				Preload("Semester").
				Select("id,title,meta_description,grade_level,subject_id,semester_id,visit_count,created_at").
				Where("status = ?", 1)
			articleDB = applySmartMatch(articleDB, []string{"title", "meta_description", "content"}, core, false)
			if err := articleDB.Order("visit_count DESC, created_at DESC").Limit(limit).Find(&articles).Error; err == nil {
				for _, a := range articles {
					desc := ""
					if a.MetaDescription != nil {
						desc = *a.MetaDescription
					}
					item := ContentResult{ID: a.ID, Title: a.Title, Type: "article", Description: desc, URL: "/" + cc + "/lesson/articles/" + uintToString(a.ID), Grade: gradeNameFromArticle(&a)}
					if a.Subject != nil {
						item.Subject = a.Subject.SubjectName
					}
					if a.Semester != nil {
						item.Semester = a.Semester.SemesterName
					}
					appendResult(item)
				}
			}
		}
	}

	sortContentResults(results)

	// Relevance gate: with OR matching the pool includes loose matches (e.g. items
	// that only share "الصف"). Keep the strongest results and drop anything far
	// below the best match, so answers stay precise while still tolerating
	// near-miss wording (امتحان بدل اختبار، الفصل الدراسي بدل الفصل).
	if len(results) > 0 {
		best := results[0].Score
		threshold := best * 2 / 5 // 40% of the top score
		gated := results[:0:0]
		for i, r := range results {
			if i < 3 || r.Score >= threshold {
				gated = append(gated, r)
			}
		}
		results = gated
	}

	if len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}

func gradeNameFromArticle(a *models.Article) string {
	if a == nil || a.GradeLevel == nil {
		return ""
	}
	return strings.TrimSpace(*a.GradeLevel)
}

// searchTerms extracts meaningful normalized tokens from a query.
func searchTerms(query string) []string {
	query = normalizeSearchText(query)
	if query == "" {
		return nil
	}
	stop := map[string]bool{
		"عن": true, "في": true, "من": true, "الى": true, "إلى": true, "او": true, "أو": true,
		"و": true, "ال": true, "ل": true, "ابحث": true, "بحث": true, "اريد": true, "أريد": true,
		"عايز": true, "بدي": true, "لو": true, "سمحت": true, "ممكن": true, "ملف": true, "ملفات": true,
		"للصف": true, "صف": true,
	}
	terms := make([]string, 0, 10)
	seen := map[string]bool{}
	for _, token := range strings.Fields(query) {
		token = strings.TrimSpace(token)
		if token == "" || stop[token] {
			continue
		}
		stem := stemArabic(token)
		if len([]rune(stem)) < 2 || seen[stem] {
			continue
		}
		seen[stem] = true
		terms = append(terms, stem)
		if len(terms) >= 10 {
			break
		}
	}
	if len(terms) == 0 {
		return []string{query}
	}
	return terms
}

func normalizeSearchText(query string) string {
	query = strings.TrimSpace(query)
	query = strings.ReplaceAll(query, "أ", "ا")
	query = strings.ReplaceAll(query, "إ", "ا")
	query = strings.ReplaceAll(query, "آ", "ا")
	query = strings.ReplaceAll(query, "ة", "ه")
	query = strings.ReplaceAll(query, "ى", "ي")
	query = strings.ReplaceAll(query, "ؤ", "و")
	query = strings.ReplaceAll(query, "ئ", "ي")
	query = strings.ReplaceAll(query, "نموذج2", "نموذج 2")
	query = strings.ReplaceAll(query, "نموذج1", "نموذج 1")
	query = regexp.MustCompile(`[^\p{Arabic}\p{Latin}\p{N}\s]+`).ReplaceAllString(query, " ")
	return strings.Join(strings.Fields(query), " ")
}

func expandSearchTerms(terms []string) []string {
	out := make([]string, 0, len(terms)*2)
	seen := map[string]bool{}
	add := func(v string) {
		v = strings.TrimSpace(stemArabic(normalizeSearchText(v)))
		if v == "" || seen[v] {
			return
		}
		seen[v] = true
		out = append(out, v)
	}
	for _, term := range terms {
		add(term)
		switch term {
		case "امتحان", "امتحانات", "اختبار", "اختبارات":
			add("اختبار")
			add("امتحان")
			add("نموذج")
		case "نهائي", "نهاي":
			add("نهائي")
			add("نهايه")
			add("نهاية")
		case "فيزيا", "فزيا", "فيزياء":
			add("فيزياء")
			add("فيزيا")
		case "اسلاميه", "اسلامي", "دين":
			add("اسلاميه")
			add("دين")
		case "عربي", "عربية", "عربه":
			add("عربي")
			add("عربية")
		case "انجليزي", "انكليزي", "english":
			add("انجليزي")
			add("انكليزي")
		case "رياضيات":
			add("رياضيات")
			add("رياض")
		case "ثاني", "الثاني":
			add("ثاني")
			add("2")
		case "اول", "الاول":
			add("اول")
			add("1")
		case "تاسع", "التاسع":
			add("تاسع")
			add("9")
		case "ثامن", "الثامن":
			add("ثامن")
			add("8")
		}
	}
	return out
}

func coreSearchTerms(terms []string) []string {
	out := []string{}
	for _, term := range terms {
		if len([]rune(term)) < 3 {
			continue
		}
		if term == "الصف" || term == "فصل" || term == "ملف" {
			continue
		}
		out = append(out, term)
		if len(out) >= 5 {
			break
		}
	}
	return out
}

func stemArabic(token string) string {
	token = normalizeSearchText(token)
	r := []rune(token)
	if len(r) > 4 && r[0] == 'ا' && r[1] == 'ل' {
		r = r[2:]
	}
	if len(r) > 5 {
		suffix := string(r[len(r)-2:])
		if suffix == "ات" || suffix == "ون" || suffix == "ين" {
			r = r[:len(r)-2]
		}
	}
	return string(r)
}

// applySmartMatch uses AND between terms. Every term must appear in one of the selected columns.
func applySmartMatch(db *gorm.DB, columns []string, terms []string, loose bool) *gorm.DB {
	if len(terms) == 0 || len(columns) == 0 {
		return db
	}
	if loose {
		parts := make([]string, 0, len(terms)*len(columns))
		args := make([]interface{}, 0, len(terms)*len(columns))
		for _, term := range terms {
			like := "%" + term + "%"
			for _, col := range columns {
				parts = append(parts, col+" LIKE ?")
				args = append(args, like)
			}
		}
		return db.Where("("+strings.Join(parts, " OR ")+")", args...)
	}
	for _, term := range terms {
		like := "%" + term + "%"
		parts := make([]string, 0, len(columns))
		args := make([]interface{}, 0, len(columns))
		for _, col := range columns {
			parts = append(parts, col+" LIKE ?")
			args = append(args, like)
		}
		db = db.Where("("+strings.Join(parts, " OR ")+")", args...)
	}
	return db
}

func scoreContentResult(query string, terms []string, item ContentResult) int {
	score := 0
	haystack := normalizeSearchText(strings.Join([]string{item.Title, item.Description, item.Grade, item.Subject, item.Semester, item.Category}, " "))
	title := normalizeSearchText(item.Title)
	for _, term := range terms {
		if strings.Contains(title, term) {
			score += 12
		}
		if strings.Contains(haystack, term) {
			score += 4
		}
	}
	if item.Type == "file" {
		score += 8
	}
	if strings.Contains(normalizeSearchText(query), "نموذج 2") && strings.Contains(haystack, "نموذج 2") {
		score += 20
	}
	if strings.Contains(normalizeSearchText(query), "نهائي") && strings.Contains(haystack, "نهائي") {
		score += 15
	}
	return score
}

func sortContentResults(items []ContentResult) {
	for i := 0; i < len(items); i++ {
		for j := i + 1; j < len(items); j++ {
			if items[j].Score > items[i].Score {
				items[i], items[j] = items[j], items[i]
			}
		}
	}
}

func uintToString(id uint) string {
	const digits = "0123456789"
	if id == 0 {
		return "0"
	}
	buf := make([]byte, 0, 12)
	for id > 0 {
		buf = append(buf, digits[id%10])
		id /= 10
	}
	for i, j := 0, len(buf)-1; i < j; i, j = i+1, j-1 {
		buf[i], buf[j] = buf[j], buf[i]
	}
	return string(buf)
}

func (r *repository) CreateFeedback(countryID database.CountryID, feedback *models.ChatFeedback) error {
	return r.db(countryID).Create(feedback).Error
}

func (r *repository) ListSessions(countryID database.CountryID, limit int) ([]models.ChatSession, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	var sessions []models.ChatSession
	err := r.db(countryID).Preload("Messages", func(db *gorm.DB) *gorm.DB { return db.Order("created_at ASC").Limit(8) }).Order("updated_at DESC").Limit(limit).Find(&sessions).Error
	return sessions, err
}

// ListSessionsPaginated returns a page of sessions plus the total count, so the
// dashboard can offer real pagination (the older ListSessions caps at 100).
func (r *repository) ListSessionsPaginated(countryID database.CountryID, limit, offset int) ([]models.ChatSession, int64, error) {
	if limit <= 0 {
		limit = 50
	} else if limit > 500 {
		limit = 500
	}
	if offset < 0 {
		offset = 0
	}
	db := r.db(countryID)
	var total int64
	if err := db.Model(&models.ChatSession{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var sessions []models.ChatSession
	err := db.
		Preload("Messages", func(d *gorm.DB) *gorm.DB { return d.Order("created_at ASC").Limit(8) }).
		Order("updated_at DESC").
		Limit(limit).
		Offset(offset).
		Find(&sessions).Error
	return sessions, total, err
}

// GetSessionsWithMessages fetches multiple sessions with their FULL message
// history, ordered oldest-first — used for the single-request training export
// so the dashboard never has to fan out one request per session.
func (r *repository) GetSessionsWithMessages(countryID database.CountryID, ids []uint) ([]models.ChatSession, error) {
	if len(ids) == 0 {
		return []models.ChatSession{}, nil
	}
	var sessions []models.ChatSession
	err := r.db(countryID).
		Preload("Messages", func(d *gorm.DB) *gorm.DB { return d.Order("created_at ASC") }).
		Where("id IN ?", ids).
		Order("id ASC").
		Find(&sessions).Error
	return sessions, err
}

// DeleteSessions removes the given sessions and their messages, returning the
// number of sessions deleted. Runs in a transaction so a session is never left
// without its messages (or vice versa).
func (r *repository) DeleteSessions(countryID database.CountryID, ids []uint) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	db := r.db(countryID)
	var deleted int64
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("session_id IN ?", ids).Delete(&models.ChatMessage{}).Error; err != nil {
			return err
		}
		res := tx.Where("id IN ?", ids).Delete(&models.ChatSession{})
		if res.Error != nil {
			return res.Error
		}
		deleted = res.RowsAffected
		return nil
	})
	return deleted, err
}

func (r *repository) GetSessionWithMessages(countryID database.CountryID, sessionID uint) (*models.ChatSession, error) {
	var session models.ChatSession
	err := r.db(countryID).
		Preload("Messages", func(db *gorm.DB) *gorm.DB { return db.Order("created_at ASC") }).
		First(&session, sessionID).Error
	if err != nil {
		return nil, err
	}
	return &session, nil
}

func (r *repository) ListKnowledge(countryID database.CountryID, countryCode string, limit int) ([]models.ChatKnowledgeBase, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	var items []models.ChatKnowledgeBase
	db := r.db(countryID).Order("priority ASC, updated_at DESC").Limit(limit)
	if countryCode != "" {
		db = db.Where("country_code = ? OR country_code = ?", countryCode, "all")
	}
	err := db.Find(&items).Error
	return items, err
}

func (r *repository) CreateKnowledge(countryID database.CountryID, item *models.ChatKnowledgeBase) error {
	return r.db(countryID).Create(item).Error
}
func (r *repository) UpdateKnowledge(countryID database.CountryID, item *models.ChatKnowledgeBase) error {
	return r.db(countryID).Save(item).Error
}
func (r *repository) DeleteKnowledge(countryID database.CountryID, id uint) error {
	return r.db(countryID).Delete(&models.ChatKnowledgeBase{}, id).Error
}

func seedDefaultKnowledge(db *gorm.DB, countryCode string) {
	defaults := []models.ChatKnowledgeBase{
		// الحساب وتسجيل الدخول
		{Title: "مشكلة تسجيل الدخول", Question: "لا أستطيع تسجيل الدخول", Answer: "لحل مشكلة تسجيل الدخول: تأكد من كتابة البريد وكلمة المرور بدون مسافات زائدة، ثم جرّب مرة أخرى. إذا ظهرت رسالة أن البريد غير مفعّل، فعّل البريد أولًا. وإذا نسيت كلمة المرور، استخدم خيار استعادة كلمة المرور من صفحة تسجيل الدخول.", Category: "auth_login_problem", Keywords: "دخول,login,تسجيل الدخول,password,كلمة المرور,حساب,لا أستطيع الدخول", CountryCode: "all", IsActive: true, Priority: 10},
		{Title: "كلمة المرور غير صحيحة", Question: "تظهر رسالة كلمة المرور غير صحيحة", Answer: "إذا ظهرت رسالة أن كلمة المرور غير صحيحة، اكتب البريد وكلمة المرور يدويًا بدون نسخ ولصق، وتأكد من لغة لوحة المفاتيح. إذا استمرت المشكلة، استخدم استعادة كلمة المرور ولا تنشئ حسابًا جديدًا بنفس البريد.", Category: "auth_login_problem", Keywords: "كلمة المرور غير صحيحة,wrong password,invalid password,خطأ كلمة المرور", CountryCode: "all", IsActive: true, Priority: 11},
		{Title: "استعادة كلمة المرور", Question: "نسيت كلمة المرور", Answer: "افتح صفحة تسجيل الدخول واضغط على خيار استعادة كلمة المرور، ثم أدخل البريد المسجل في الموقع. إذا لم تصل الرسالة خلال دقائق، افحص البريد غير الهام وتأكد أن البريد مكتوب بشكل صحيح.", Category: "password_reset_problem", Keywords: "نسيت,كلمة المرور,password reset,forgot password,استعادة كلمة المرور,اعادة تعيين", CountryCode: "all", IsActive: true, Priority: 12},
		{Title: "لا تصل رسالة استعادة كلمة المرور", Question: "لم تصلني رسالة استعادة كلمة المرور", Answer: "افحص مجلد البريد غير الهام أو الرسائل الترويجية أولًا. إذا لم تجد الرسالة، تأكد أنك تستخدم نفس البريد المسجل في الموقع. بعد ذلك جرّب طلب الاستعادة مرة واحدة فقط وانتظر عدة دقائق قبل إعادة المحاولة.", Category: "password_reset_problem", Keywords: "استعادة كلمة المرور لا تصل,reset email,forgot password email,رسالة كلمة المرور", CountryCode: "all", IsActive: true, Priority: 13},
		{Title: "إنشاء حساب جديد", Question: "كيف أنشئ حساب جديد", Answer: "لإنشاء حساب جديد، افتح صفحة التسجيل، أدخل اسمك وبريدًا صحيحًا وكلمة مرور قوية، ثم فعّل الحساب من رسالة التحقق التي تصلك على البريد. بعد التفعيل تستطيع تسجيل الدخول واستخدام التحميلات المتاحة.", Category: "auth_register_problem", Keywords: "تسجيل,انشاء حساب,إنشاء حساب,register,signup,حساب جديد", CountryCode: "all", IsActive: true, Priority: 14},
		{Title: "البريد مستخدم سابقًا", Question: "يظهر أن البريد مستخدم سابقًا", Answer: "إذا ظهر أن البريد مستخدم سابقًا، فهذا يعني أن لديك حسابًا بالفعل. استخدم تسجيل الدخول أو استعادة كلمة المرور بدل إنشاء حساب جديد. إذا كنت لا تملك هذا الحساب، تواصل مع الإدارة واذكر البريد المستخدم.", Category: "auth_register_problem", Keywords: "البريد مستخدم,email already exists,حساب موجود,مستخدم سابقا", CountryCode: "all", IsActive: true, Priority: 15},

		// التفعيل والبريد
		{Title: "تفعيل البريد الإلكتروني", Question: "لم تصلني رسالة تفعيل البريد", Answer: "إذا لم تصلك رسالة التفعيل، افحص البريد غير الهام أولًا، وتأكد أن البريد داخل الحساب مكتوب بشكل صحيح. بعد ذلك استخدم زر إعادة إرسال التفعيل من صفحة الحساب. إذا بقيت المشكلة، أرسل للإدارة البريد المستخدم ووقت المحاولة.", Category: "email_verification_problem", Keywords: "تفعيل,بريد,email,verify,verification,رسالة التفعيل,لا يصلني,لم تصلني,غير مفعل", CountryCode: "all", IsActive: true, Priority: 16},
		{Title: "رسالة أو كود لا يصل", Question: "لا يصلني كود أو رسالة", Answer: "إذا لم تصلك الرسالة أو الكود، افحص البريد غير الهام والرسائل الترويجية أولًا. تأكد من كتابة البريد بشكل صحيح، ثم جرّب إعادة الإرسال من صفحة الحساب. إذا تكررت المشكلة، أرسل للإدارة البريد المستخدم ووقت آخر محاولة.", Category: "email_verification_problem", Keywords: "كود,رمز,رسالة,لا يصلني,لم يصلني,لم تصلني,تفعيل,بريد,email,code", CountryCode: "all", IsActive: true, Priority: 17},
		{Title: "البريد غير مفعل", Question: "البريد غير مفعل", Answer: "عندما يظهر أن البريد غير مفعّل، يجب فتح رسالة التحقق المرسلة إلى بريدك والضغط على رابط التفعيل. إذا لم تجد الرسالة، استخدم إعادة إرسال التفعيل ثم افحص البريد غير الهام.", Category: "email_verification_problem", Keywords: "البريد غير مفعل,الحساب غير مفعل,email not verified,verify email", CountryCode: "all", IsActive: true, Priority: 18},
		{Title: "كتبت البريد خطأ", Question: "كتبت البريد الإلكتروني بشكل خاطئ", Answer: "إذا كتبت البريد بشكل خاطئ ولم تستطع التفعيل، حاول تعديل البريد من صفحة الحساب إن كان الخيار متاحًا. إذا لم تستطع الدخول أو التعديل، تواصل مع الإدارة واذكر البريد الخاطئ والبريد الصحيح.", Category: "email_verification_problem", Keywords: "بريد خاطئ,غير صحيح,تعديل البريد,email typo,wrong email", CountryCode: "all", IsActive: true, Priority: 19},

		// التحميل والملفات
		{Title: "مشكلة تحميل الملفات", Question: "لا أستطيع تحميل ملف", Answer: "لحل مشكلة تحميل الملفات: تأكد أنك مسجل الدخول وأن بريدك مفعّل، ثم افتح صفحة الملف الأصلية من جديد واضغط تحميل. لا تستخدم رابط تحميل قديم أو منسوخ لأنه قد تنتهي صلاحيته.", Category: "download_problem", Keywords: "تحميل,download,file,pdf,ملف,رابط,تنزيل,لا أستطيع تحميل", CountryCode: "all", IsActive: true, Priority: 20},
		{Title: "سجلت الدخول لكن التحميل لا يعمل", Question: "سجلت الدخول لكن التحميل لا يعمل", Answer: "إذا كنت مسجل الدخول ولا يعمل التحميل، حدّث الصفحة أولًا، ثم افتح الملف من صفحته الأصلية وليس من رابط قديم. تأكد أيضًا أن البريد مفعّل. إذا ظهرت رسالة خطأ، أرسل للإدارة رابط الصفحة ونص الرسالة.", Category: "download_problem", Keywords: "سجلت الدخول,التحميل لا يعمل,download not working,logged in,لا يحمل", CountryCode: "all", IsActive: true, Priority: 21},
		{Title: "رابط التحميل منتهي", Question: "رابط التحميل لا يعمل أو منتهي", Answer: "روابط التحميل المؤقتة قد تنتهي صلاحيتها للحماية. افتح صفحة الملف الأصلية داخل الموقع واضغط تحميل من جديد للحصول على رابط صالح.", Category: "download_problem", Keywords: "رابط منتهي,expired link,signed url,رابط التحميل,لا يفتح", CountryCode: "all", IsActive: true, Priority: 22},
		{Title: "الملف لا يفتح بعد التحميل", Question: "الملف لا يفتح بعد التحميل", Answer: "إذا تم تحميل الملف لكنه لا يفتح، تأكد أن التحميل اكتمل بالكامل، ثم جرّب فتحه ببرنامج PDF أو تطبيق ملفات مناسب. إذا كان حجم الملف صفرًا أو ناقصًا، أعد التحميل من صفحة الملف الأصلية.", Category: "download_problem", Keywords: "الملف لا يفتح,pdf لا يفتح,حجم الملف,ملف تالف,corrupt file", CountryCode: "all", IsActive: true, Priority: 23},
		{Title: "صفحة التحميل تطلب تسجيل الدخول", Question: "صفحة التحميل تطلب تسجيل الدخول", Answer: "بعض الملفات تحتاج تسجيل دخول لحماية الروابط وتنظيم التحميل. سجّل الدخول أولًا، فعّل البريد إذا طُلب منك ذلك، ثم ارجع إلى صفحة الملف واضغط تحميل.", Category: "download_problem", Keywords: "يطلب تسجيل الدخول,login required,تحميل يحتاج دخول,صلاحية التحميل", CountryCode: "all", IsActive: true, Priority: 24},

		{Title: "أين أجد الملف بعد التحميل", Question: "عملت تحميل للملف ولكن لا أعرف أين أجده", Answer: "إذا ضغطت تحميل ولم تعرف أين ذهب الملف، افتح مجلد التنزيلات Downloads في الهاتف أو الكمبيوتر. في الهاتف افتح تطبيق الملفات أو مدير الملفات، وفي الكمبيوتر افتح File Explorer ثم Downloads. إذا لم يظهر الملف، أعد التحميل من صفحة الملف الأصلية وتأكد أن المتصفح لم يمنع التنزيل.", Category: "download_location", Keywords: "وين بتنزل,وين بكون,وين الاقيه,التنزيلات,downloads,عملت تحميل,لا أجده,لا اعرف وين", CountryCode: "all", IsActive: true, Priority: 24},
		{Title: "ملف تعليمي يحتاج تفعيل البريد", Question: "أريد ملف أو امتحان لكن يطلب تفعيل البريد", Answer: "إذا كان الملف أو الامتحان يحتاج تفعيل البريد، فعّل حسابك أولًا حتى تستطيع الوصول للتحميل. افتح صفحة تسجيل الدخول، ثم اضغط إعادة إرسال التفعيل إذا ظهر الخيار، وافحص Inbox وSpam/Junk. بعد التفعيل ارجع للبحث عن الملف المطلوب.", Category: "email_verification_problem", Keywords: "امتحان,ملف,دين,يطلب تفعيل البريد,يريد تفعيل البريد,غير مفعل,لا استطيع بسبب التفعيل", CountryCode: "all", IsActive: true, Priority: 18},
		{Title: "لا أملك صلاحية التحميل", Question: "تظهر رسالة لا تملك صلاحية التحميل", Answer: "إذا ظهرت رسالة عدم وجود صلاحية، تأكد أولًا من تسجيل الدخول وتفعيل البريد. إذا كانت الصفحة مخصصة لفئة أو دولة أو عضوية معينة، استخدم صفحة التواصل وأرسل رابط الملف حتى تتم مراجعة الصلاحية.", Category: "permission_problem", Keywords: "لا تملك صلاحية,permission,forbidden,403,غير مصرح,صلاحية التحميل", CountryCode: "all", IsActive: true, Priority: 25},
		{Title: "الملف غير موجود", Question: "الملف غير موجود أو الرابط خاطئ", Answer: "إذا ظهر أن الملف غير موجود، قد يكون الرابط قديمًا أو تم نقل الملف. استخدم البحث داخل الموقع باسم الملف أو الصف والمادة، وإذا لم تجده أرسل رابط الصفحة للإدارة لمراجعته.", Category: "file_not_found", Keywords: "غير موجود,404,file not found,رابط خاطئ,تم حذف الملف", CountryCode: "all", IsActive: true, Priority: 26},

		{Title: "رابط الامتحان لا يفتح", Question: "رابط الامتحان لا يفتح أو يعطي عدم الوصول", Answer: "إذا ظهر عدم الوصول أو المرفق غير موجود عند تحميل الامتحان، افتح صفحة الملف الأصلية واضغط تحميل من جديد. إذا بقي الخطأ، ابحث باسم الامتحان أو أرسل رابط الصفحة للإدارة حتى يتم فحص المرفق.", Category: "file_not_found", Keywords: "رابط الامتحان لا يفتح,عدم الوصول,المرفق غير موجود,ما بطلعلي,مو راضي يفتح,لا يفتح", CountryCode: "all", IsActive: true, Priority: 27},

		// البحث والمحتوى التعليمي
		{Title: "البحث عن ملف تعليمي", Question: "أريد البحث عن ملف تعليمي", Answer: "للبحث بشكل أدق، اكتب الصف والمادة والفصل ونوع الملف. مثال: رياضيات الصف التاسع الفصل الأول اختبار، أو ملخص علوم الصف السابع. كلما كان السؤال أوضح ظهرت نتائج أفضل.", Category: "search_content", Keywords: "بحث,ملف تعليمي,ابحث,أريد ملف,ملخص,اختبار,ورقة عمل", CountryCode: "all", IsActive: true, Priority: 30},
		{Title: "تصفح الصفوف التعليمية", Question: "أريد تصفح الصفوف التعليمية", Answer: "افتح صفحة الصفوف الخاصة بالأردن، ثم اختر الصف المطلوب وبعدها المادة والفصل. إذا كنت تبحث عن ملف محدد، اكتب نوع الملف مع المادة والصف والفصل للحصول على نتيجة أدق.", Category: "open_classes", Keywords: "تصفح الصفوف,عرض الصفوف التعليمية,فتح الصفوف,الصفوف التعليمية,أريد تصفح الصفوف,اعرض الصفوف", CountryCode: "all", IsActive: true, Priority: 29},
		{Title: "فتح البحث بكلمات محددة", Question: "فتح البحث بهذه الكلمات", Answer: "استخدم صفحة البحث واكتب كلمات قصيرة وواضحة. أفضل صيغة هي: نوع الملف + المادة + الصف + الفصل. إذا كانت الكلمات كافية، يمكن فتح البحث مباشرة بنفس العبارة.", Category: "open_search", Keywords: "فتح البحث,افتح البحث,فتح البحث بهذه الكلمات,ابحث بهذه الكلمات,صفحة البحث", CountryCode: "all", IsActive: true, Priority: 29},
		{Title: "البحث عن كتاب مدرسي", Question: "أريد كتاب الصف لمادة معينة", Answer: "للبحث عن كتاب، اكتب: كتاب + المادة + الصف + الفصل إن وجد. مثال: كتاب اللغة العربية الصف الرابع الفصل الأول. إذا لم تظهر نتيجة دقيقة، افتح البحث بنفس الكلمات أو أرسل طلب إضافة ملف للإدارة.", Category: "search_content", Keywords: "كتاب,كتب,كتاب الطالب,كتاب التمارين,دليل المعلم,كراسة,كتاب الصف", CountryCode: "all", IsActive: true, Priority: 30},
		{Title: "البحث عن اختبار", Question: "أريد اختبار أو امتحان", Answer: "اكتب اسم المادة والصف والفصل مع كلمة اختبار أو امتحان، مثل: اختبار رياضيات الصف العاشر الفصل الثاني. سأحاول إظهار أقرب النتائج من داخل الموقع.", Category: "search_content", Keywords: "اختبار,امتحان,نماذج امتحانات,exam,test,اختبارات", CountryCode: "all", IsActive: true, Priority: 31},

		{Title: "نماذج امتحان حاسوب صف ثامن فصل ثاني", Question: "أريد نماذج امتحان حاسوب نهائي صف ثامن فصل ثاني", Answer: "للبحث عن نماذج امتحان حاسوب للصف الثامن الفصل الثاني، استخدم كلمات البحث: حاسوب الصف الثامن الفصل الثاني امتحان نهائي. إذا لم تظهر نتيجة دقيقة، افتح صفحة البحث أو أرسل طلب إضافة ملف للإدارة مع الصف والمادة والفصل.", Category: "search_content", Keywords: "حاسوب,صف ثامن,الصف الثامن,فصل ثاني,الفصل الثاني,امتحان نهائي,نماذج امتحان,كمبيوتر", CountryCode: "all", IsActive: true, Priority: 31},
		{Title: "خطة نمو مهني تربية فنية", Question: "أريد خطة نمو مهني تربية فنية", Answer: "للبحث عن خطة نمو مهني لمادة التربية الفنية، استخدم عبارة: خطة نمو مهني تربية فنية. إذا لم تظهر نتيجة مناسبة، أرسل طلب إضافة ملف للإدارة وحدد المادة ونوع الملف المطلوب.", Category: "search_content", Keywords: "خطة نمو مهني,تربية فنية,خطة,نمو مهني,فنية,ملف", CountryCode: "all", IsActive: true, Priority: 32},
		{Title: "امتحان ثقافة مالية صف ثامن فصل ثاني", Question: "أريد امتحان ثقافة مالية الصف الثامن الفصل الثاني", Answer: "للبحث عن امتحان ثقافة مالية للصف الثامن الفصل الثاني، اكتب: امتحان ثقافة مالية الصف الثامن الفصل الثاني. إذا كان الرابط لا يفتح أو يظهر المرفق غير موجود، أرسل رابط الصفحة للإدارة لمراجعة المرفق.", Category: "search_content", Keywords: "ثقافة مالية,الصف الثامن,صف ثامن,الفصل الثاني,امتحان نهائي,اختبار", CountryCode: "all", IsActive: true, Priority: 33},
		{Title: "البحث عن ملخص", Question: "أريد ملخص درس أو مادة", Answer: "اكتب اسم المادة والصف والفصل أو اسم الدرس، مثل: ملخص علوم الصف الثامن الفصل الأول. إذا لم تظهر نتيجة مناسبة، جرّب استخدام كلمات أبسط أو اسم المادة فقط.", Category: "search_content", Keywords: "ملخص,تلخيص,شرح درس,مراجعة,summary,درس", CountryCode: "all", IsActive: true, Priority: 32},
		{Title: "البحث حسب الصف", Question: "كيف أجد ملفات صف معين", Answer: "افتح صفحة الصفوف ثم اختر الصف المطلوب، وبعدها اختر المادة أو الفصل إن كان متاحًا. يمكنك أيضًا كتابة اسم الصف مباشرة في البحث مثل: الصف التاسع رياضيات.", Category: "find_grade", Keywords: "صف,الصف,grade,صفوف,الصف التاسع,الصف العاشر", CountryCode: "all", IsActive: true, Priority: 33},
		{Title: "البحث حسب المادة", Question: "كيف أجد ملفات مادة معينة", Answer: "اكتب اسم المادة مع الصف والفصل للحصول على نتائج أدق. مثال: اللغة العربية الصف السابع الفصل الأول، أو رياضيات الصف التاسع اختبار.", Category: "find_subject", Keywords: "مادة,subject,رياضيات,علوم,لغة عربية,انجليزي,تربية اسلامية", CountryCode: "all", IsActive: true, Priority: 34},
		{Title: "البحث حسب الفصل", Question: "أريد ملفات الفصل الأول أو الثاني", Answer: "اذكر الفصل مع الصف والمادة، مثل: رياضيات الصف التاسع الفصل الأول. إذا لم تعرف اسم الملف، استخدم كلمات عامة مثل اختبار، ملخص، أوراق عمل.", Category: "find_semester", Keywords: "فصل,الفصل الأول,الفصل الثاني,semester,ترم,الفصل الدراسي", CountryCode: "all", IsActive: true, Priority: 35},
		{Title: "لا أجد الملف المطلوب", Question: "لم أجد الملف الذي أبحث عنه", Answer: "جرّب البحث باسم المادة والصف بدل اسم الملف الكامل، وتأكد من اختيار الدولة الصحيحة. إذا لم يظهر الملف، أرسل طلبًا للإدارة يتضمن الصف والمادة والفصل ونوع الملف المطلوب.", Category: "search_content", Keywords: "لا أجد,لم أجد,غير موجود,لا تظهر نتائج,ما لقيت", CountryCode: "all", IsActive: true, Priority: 36},

		// الدولة والمنهج
		{Title: "اختيار الدولة أو المنهج", Question: "كيف أغير الدولة أو المنهج", Answer: "إذا كانت الملفات لا تطابق منهجك، تأكد من اختيار الدولة الصحيحة من الموقع. المحتوى قد يختلف بين الأردن والسعودية ومصر وفلسطين، لذلك اختيار الدولة يساعد في عرض الصفوف والمواد المناسبة.", Category: "country_or_curriculum", Keywords: "دولة,منهج,country,jo,sa,eg,ps,الأردن,السعودية,مصر,فلسطين", CountryCode: "all", IsActive: true, Priority: 40},
		{Title: "الملف لا يناسب دولتي", Question: "الملف لا يناسب المنهج", Answer: "تأكد من اختيار الدولة الصحيحة قبل البحث. إذا كان الملف ظاهرًا في دولة غير مناسبة أو يحمل تصنيفًا خاطئًا، أرسل رابط الملف للإدارة ليتم مراجعته وتصحيحه.", Category: "country_or_curriculum", Keywords: "لا يناسب المنهج,منهج خطأ,دولة خطأ,تصنيف خاطئ", CountryCode: "all", IsActive: true, Priority: 41},

		// مشاكل الموقع والواجهة
		{Title: "الصفحة لا تفتح", Question: "الصفحة لا تفتح أو تظهر رسالة خطأ", Answer: "حدّث الصفحة أولًا، ثم جرّب فتحها من متصفح آخر أو امسح ذاكرة التخزين المؤقت. إذا استمرت المشكلة، أرسل للإدارة رابط الصفحة ورسالة الخطأ الظاهرة.", Category: "site_error", Keywords: "الصفحة لا تفتح,خطأ,500,502,404,مشكلة في الصفحة,لا يعمل", CountryCode: "all", IsActive: true, Priority: 50},
		{Title: "الموقع بطيء", Question: "الموقع بطيء عندي", Answer: "جرّب تحديث الصفحة والتحقق من اتصال الإنترنت. إذا كان البطء في صفحة معينة فقط، أرسل رابط الصفحة للإدارة. بعض الصفحات قد تحتاج وقتًا أطول إذا كانت تحتوي ملفات أو نتائج بحث كثيرة.", Category: "site_error", Keywords: "بطيء,slow,تحميل الصفحة,يتأخر,لا يستجيب", CountryCode: "all", IsActive: true, Priority: 51},
		{Title: "مشكلة في الجوال", Question: "الموقع لا يعمل بشكل جيد على الهاتف", Answer: "جرّب تحديث الصفحة أو استخدام متصفح حديث مثل Chrome أو Safari. إذا كانت المشكلة في زر أو صفحة محددة، أرسل نوع الجهاز ورابط الصفحة للإدارة حتى يتم فحصها.", Category: "site_error", Keywords: "هاتف,جوال,mobile,اندرويد,ايفون,لا يعمل على الهاتف", CountryCode: "all", IsActive: true, Priority: 52},
		{Title: "الإعلانات أو النوافذ تغطي المحتوى", Question: "يوجد إعلان أو نافذة تغطي المحتوى", Answer: "إذا ظهر إعلان أو عنصر يغطي المحتوى، جرّب إغلاقه أو تحديث الصفحة. إذا تكررت المشكلة، أرسل صورة للشاشة ورابط الصفحة للإدارة حتى يتم تعديل موضع العنصر.", Category: "site_error", Keywords: "إعلان,اعلان,ads,نافذة,يغطي المحتوى,مزعج", CountryCode: "all", IsActive: true, Priority: 53},

		// الحساب الشخصي والخصوصية
		{Title: "تعديل بيانات الحساب", Question: "كيف أعدل بيانات حسابي", Answer: "ادخل إلى حسابك ثم افتح صفحة الملف الشخصي أو الإعدادات إن كانت متاحة. عدّل البيانات المطلوبة واحفظ التغييرات. إذا لم يظهر خيار التعديل، تواصل مع الإدارة مع ذكر البيانات المطلوب تعديلها.", Category: "profile_problem", Keywords: "تعديل حساب,بياناتي,الملف الشخصي,profile,تغيير الاسم,تغيير البريد", CountryCode: "all", IsActive: true, Priority: 60},
		{Title: "حماية الحساب", Question: "كيف أحافظ على أمان حسابي", Answer: "استخدم كلمة مرور قوية ولا تشاركها مع أحد. لا ترسل كود التفعيل أو رابط الاستعادة لأي شخص. إذا شعرت أن حسابك مستخدم من شخص آخر، غيّر كلمة المرور فورًا وتواصل مع الإدارة.", Category: "account_security", Keywords: "أمان الحساب,حماية,اختراق,كلمة مرور قوية,security", CountryCode: "all", IsActive: true, Priority: 61},
		{Title: "حذف الحساب", Question: "أريد حذف حسابي", Answer: "لطلب حذف الحساب أو البيانات، استخدم صفحة التواصل واكتب البريد المرتبط بالحساب وطلبك بوضوح. قد تحتاج الإدارة للتحقق من ملكية الحساب قبل تنفيذ الطلب.", Category: "privacy_request", Keywords: "حذف الحساب,حذف بياناتي,privacy,delete account,بيانات شخصية", CountryCode: "all", IsActive: true, Priority: 62},

		// التواصل والإبلاغ
		{Title: "التواصل مع الإدارة", Question: "كيف أتواصل مع الإدارة", Answer: "استخدم صفحة التواصل داخل الموقع، واكتب المشكلة بوضوح مع البريد المستخدم ورابط الصفحة ورسالة الخطأ إن وجدت. كلما كانت التفاصيل أوضح، كان حل المشكلة أسرع.", Category: "contact_support", Keywords: "تواصل,إدارة,ادارة,دعم,contact,support,مراسلة", CountryCode: "all", IsActive: true, Priority: 70},
		{Title: "الإبلاغ عن ملف خاطئ", Question: "أريد الإبلاغ عن ملف خاطئ", Answer: "أرسل رابط الملف ووضح سبب المشكلة: ملف لا يفتح، تصنيف خاطئ، محتوى غير مناسب، أو ملف لا يخص الصف/المادة. ستتم مراجعة البلاغ من الإدارة.", Category: "report_content", Keywords: "إبلاغ,ملف خاطئ,تصنيف خاطئ,محتوى خاطئ,report,بلاغ", CountryCode: "all", IsActive: true, Priority: 71},
		{Title: "طلب إضافة ملف", Question: "أريد طلب إضافة ملف أو درس", Answer: "يمكنك إرسال طلبك للإدارة مع تحديد الدولة والصف والمادة والفصل ونوع الملف المطلوب. مثال: الأردن، الصف التاسع، رياضيات، الفصل الأول، اختبار نهائي.", Category: "request_content", Keywords: "طلب ملف,إضافة ملف,أريد درس,طلب درس,missing content,محتوى ناقص", CountryCode: "all", IsActive: true, Priority: 72},
		{Title: "مشكلة لم تُحل", Question: "ما زالت المشكلة موجودة", Answer: "إذا جرّبت الخطوات السابقة وما زالت المشكلة موجودة، أرسل للإدارة: البريد المستخدم، رابط الصفحة، وقت حدوث المشكلة، صورة أو نص رسالة الخطأ، ونوع الجهاز أو المتصفح المستخدم.", Category: "contact_support", Keywords: "ما زالت المشكلة,لم تنحل,لا تزال,تواصل مع الدعم,help", CountryCode: "all", IsActive: true, Priority: 73},

		// تدريب موسع لصيغ المستخدمين الواقعية
		{Title: "التحميل من متصفح فيسبوك أو إنستغرام", Question: "أفتح الموقع من فيسبوك ولا أستطيع تحميل الملف", Answer: "متصفح Facebook أو Instagram الداخلي قد يمنع تحميل الملفات. انسخ رابط الصفحة وافتحه في Chrome أو Safari، ثم سجّل الدخول واضغط تحميل من صفحة الملف الأصلية. إذا بقي الخطأ، أرسل رابط الصفحة ونوع الجهاز للإدارة.", Category: "download_problem", Keywords: "متصفح فيسبوك,داخل فيسبوك,داخل الفيسبوك,متصفح انستغرام,داخل انستغرام,in-app browser,facebook browser,instagram browser,لا يحمل من فيسبوك", CountryCode: "all", IsActive: true, Priority: 20},
		{Title: "زر التحميل لا يستجيب", Question: "زر التحميل لا يضغط أو العداد لا يعمل", Answer: "إذا كان زر التحميل لا يستجيب، حدّث الصفحة، تأكد من تسجيل الدخول وتفعيل البريد، ثم جرّب متصفحًا آخر. لا تضغط الزر عدة مرات متتالية. إذا بقيت المشكلة، أرسل رابط الصفحة وصورة للخطأ للإدارة.", Category: "download_problem", Keywords: "زر التحميل,زر تحميل,لا يستجيب,ما بضغط,العداد لا يعمل,العداد واقف,تحميل واقف,لا يحمل,ما بنزل", CountryCode: "all", IsActive: true, Priority: 20},
		{Title: "تحميل من الهاتف", Question: "لا أستطيع تحميل الملف من الهاتف", Answer: "على الهاتف استخدم متصفح Chrome أو Safari، وليس متصفح التطبيقات الداخلية. بعد الضغط على تحميل، افتح تطبيق الملفات أو مجلد Downloads. إذا لم يظهر الملف، أعد التحميل من صفحة الملف الأصلية.", Category: "download_problem", Keywords: "تحميل من الهاتف,تحميل من الجوال,اندرويد,ايفون,ios,android,الهاتف لا يحمل,الجوال لا يحمل", CountryCode: "all", IsActive: true, Priority: 21},
		{Title: "التحميل يبدأ ثم يتوقف", Question: "التحميل يبدأ ثم يتوقف قبل اكتمال الملف", Answer: "إذا بدأ التحميل ثم توقف، تحقق من اتصال الإنترنت، أعد فتح صفحة الملف الأصلية، ولا تستخدم رابط تحميل قديم. جرّب متصفحًا آخر، وإذا كان حجم الملف صفرًا أو ناقصًا أبلغ الإدارة برابط الصفحة.", Category: "download_problem", Keywords: "التحميل يتوقف,تحميل ناقص,حجم الملف صفر,ملف ناقص,download interrupted,download incomplete", CountryCode: "all", IsActive: true, Priority: 22},
		{Title: "خطأ بعد الضغط على تحميل", Question: "تظهر رسالة خطأ بعد الضغط على تحميل", Answer: "إذا ظهرت رسالة خطأ بعد الضغط على تحميل، لا تستخدم الرابط القديم. افتح صفحة الملف الأصلية واضغط تحميل مرة أخرى. إذا بقي الخطأ، أرسل نص الرسالة ورابط الصفحة للإدارة.", Category: "download_problem", Keywords: "رسالة خطأ بعد التحميل,download error,خطأ تحميل,فشل التحميل,error download", CountryCode: "all", IsActive: true, Priority: 23},
		{Title: "رابط التحميل يفتح صفحة بيضاء", Question: "رابط التحميل يفتح صفحة بيضاء", Answer: "إذا فتح رابط التحميل صفحة بيضاء، فقد يكون الرابط منتهي الصلاحية أو المتصفح منع التنزيل. ارجع إلى صفحة الملف الأصلية واضغط تحميل من جديد، وجرّب Chrome أو Safari.", Category: "file_not_found", Keywords: "صفحة بيضاء,رابط التحميل صفحة بيضاء,blank page,رابط لا يظهر,الرابط فارغ", CountryCode: "all", IsActive: true, Priority: 26},
		{Title: "المرفق غير موجود", Question: "تظهر رسالة المرفق غير موجود", Answer: "رسالة المرفق غير موجود تعني غالبًا أن الملف نُقل أو الرابط قديم. ابحث باسم الملف داخل الموقع، وإذا لم تجده أرسل رابط الصفحة للإدارة لفحص المرفق.", Category: "file_not_found", Keywords: "المرفق غير موجود,attachment not found,الملف غير مرفق,مرفق محذوف", CountryCode: "all", IsActive: true, Priority: 26},
		{Title: "عدم الوصول للملف", Question: "تظهر رسالة عدم الوصول عند التحميل", Answer: "إذا ظهرت رسالة عدم الوصول، تأكد أنك مسجل الدخول وأن بريدك مفعّل. إذا كانت الصفحة تحتاج صلاحية خاصة أو الدولة غير صحيحة، أرسل رابط الملف للإدارة لمراجعة الصلاحية.", Category: "permission_problem", Keywords: "عدم الوصول,لا يوجد وصول,access denied,غير مصرح,لا تملك صلاحية,403", CountryCode: "all", IsActive: true, Priority: 25},
		{Title: "الحساب يحتاج تفعيل قبل التحميل", Question: "لا أستطيع التحميل لأن الحساب غير مفعّل", Answer: "فعّل البريد أولًا: سجّل الدخول، اضغط إعادة إرسال التفعيل إن ظهر الخيار، وافحص Inbox وSpam/Junk. بعد التفعيل ارجع إلى صفحة الملف واضغط تحميل من جديد.", Category: "email_verification_problem", Keywords: "الحساب غير مفعل قبل التحميل,تفعيل قبل التحميل,تحميل يحتاج تفعيل,غير مفعل لا يحمل", CountryCode: "all", IsActive: true, Priority: 18},
		{Title: "كتبت البريد أو الإيميل خطأ", Question: "كتبت الإيميل غلط ولا تصل رسالة التفعيل", Answer: "إذا كان البريد مكتوبًا خطأ، حاول تعديله من الحساب إن استطعت. إذا لم تستطع الدخول أو التعديل، تواصل مع الإدارة واكتب البريد الخاطئ والبريد الصحيح المطلوب اعتماده.", Category: "email_verification_problem", Keywords: "كتبت الايميل غلط,كتبت البريد خطأ,الايميل خطأ,البريد خطأ,تصحيح البريد,wrong email,email typo", CountryCode: "all", IsActive: true, Priority: 16},
		{Title: "لا أستطيع فتح البريد القديم", Question: "لا أستطيع الوصول إلى بريدي القديم", Answer: "إذا فقدت الوصول للبريد القديم، حاول استعادته من مزود البريد أولًا. إذا لم تنجح، تواصل مع الإدارة واذكر البريد القديم والجديد واسم الحساب، وقد تحتاج الإدارة للتحقق من ملكيتك للحساب.", Category: "email_verification_problem", Keywords: "لا استطيع الوصول الى بريدي,بريدي قديم,فقدت البريد,نسيت البريد,لا افتح البريد,تغيير البريد", CountryCode: "all", IsActive: true, Priority: 17},
		{Title: "صندوق البريد ممتلئ", Question: "صندوق البريد ممتلئ ولا تصل الرسائل", Answer: "إذا كان صندوق البريد ممتلئًا، احذف بعض الرسائل أو أفرغ المهملات ثم أعد إرسال التفعيل. انتظر عدة دقائق وافحص Spam/Junk والرسائل الترويجية.", Category: "email_verification_problem", Keywords: "صندوق البريد ممتلئ,البريد ممتلئ,inbox full,mailbox full,لا تصل الرسائل بسبب الامتلاء", CountryCode: "all", IsActive: true, Priority: 17},
		{Title: "رسالة التفعيل في البريد غير الهام", Question: "أين أبحث عن رسالة التفعيل؟", Answer: "افحص Inbox ثم Spam/Junk ثم Promotions أو الرسائل الترويجية. ابحث داخل البريد عن اسم الموقع أو كلمة تفعيل. إذا لم تجدها، أعد إرسال التفعيل مرة واحدة وانتظر من 2 إلى 5 دقائق.", Category: "email_verification_problem", Keywords: "اين رسالة التفعيل,spam,junk,promotions,البريد غير الهام,الرسائل الترويجية", CountryCode: "all", IsActive: true, Priority: 17},
		{Title: "رسالة استعادة كلمة المرور لا تصل", Question: "طلبت استعادة كلمة المرور ولم تصل الرسالة", Answer: "تأكد أنك تستخدم البريد المرتبط بالحساب، وافحص Spam/Junk. لا تطلب الاستعادة مرات كثيرة متتالية. إذا لم تصل الرسالة، تواصل مع الإدارة واذكر البريد ووقت المحاولة.", Category: "password_reset_problem", Keywords: "استعادة كلمة المرور لا تصل,reset لا يصل,forgot password email,لم تصل رسالة الاستعادة", CountryCode: "all", IsActive: true, Priority: 12},
		{Title: "تسجيل الدخول بالبريد بدل Google أو Facebook", Question: "أريد الدخول بالبريد بدل فيسبوك أو جوجل", Answer: "إذا كان حسابك مرتبطًا ببريد، افتح صفحة تسجيل الدخول واستخدم البريد وكلمة المرور. إذا لا تعرف كلمة المرور، استخدم استعادة كلمة المرور للبريد نفسه.", Category: "social_login_problem", Keywords: "دخول بالبريد بدل فيسبوك,دخول بالبريد بدل جوجل,google login problem,facebook login problem,الدخول الاجتماعي", CountryCode: "all", IsActive: true, Priority: 14},
		{Title: "نافذة Google أو Facebook لا تفتح", Question: "زر Google أو Facebook لا يفتح", Answer: "إذا لم تفتح نافذة Google أو Facebook، اسمح بالنوافذ المنبثقة وملفات الارتباط، وجرّب Chrome أو Safari. إذا بقيت المشكلة، استخدم الدخول بالبريد أو تواصل مع الإدارة.", Category: "social_login_problem", Keywords: "زر جوجل لا يفتح,زر فيسبوك لا يفتح,نافذة جوجل لا تفتح,نافذة فيسبوك لا تفتح,popup blocked", CountryCode: "all", IsActive: true, Priority: 14},
		{Title: "نتائج البحث غير مناسبة", Question: "نتائج البحث غير مناسبة لما أريد", Answer: "اكتب البحث بصيغة واضحة: نوع الملف + المادة + الصف + الفصل. مثال: امتحان اللغة العربية الصف الأول الفصل الثاني. إذا بقيت النتائج غير مناسبة، أرسل طلب إضافة ملف للإدارة.", Category: "search_content", Keywords: "نتائج البحث غير مناسبة,نتائج غلط,نتائج غير دقيقة,لا تظهر نتائج,ما في نتائج,ما لقيت", CountryCode: "all", IsActive: true, Priority: 30},
		{Title: "لا أعرف ماذا أكتب في البحث", Question: "كيف أبحث عن ملف؟", Answer: "اكتب أربع معلومات إن أمكن: نوع الملف، المادة، الصف، الفصل. مثال: ملخص علوم الصف السابع الفصل الأول. الكلمات القصيرة والواضحة تعطي نتائج أفضل من الجمل الطويلة.", Category: "search_content", Keywords: "كيف ابحث,كيف أبحث,ماذا اكتب في البحث,طريقة البحث,صيغة البحث", CountryCode: "all", IsActive: true, Priority: 30},
		{Title: "اختيار الدولة الصحيحة قبل البحث", Question: "الملفات لا تطابق منهجي", Answer: "تأكد من اختيار الدولة أو المنهج الصحيح قبل البحث؛ المحتوى يختلف بين الدول. إذا وجدت ملفًا بتصنيف خاطئ، أرسل رابط الملف للإدارة ليتم تصحيحه.", Category: "country_or_curriculum", Keywords: "المنهج غلط,الدولة غلط,لا يطابق منهجي,ملفات دولة ثانية,اختيار الدولة", CountryCode: "all", IsActive: true, Priority: 40},
		{Title: "الموقع بطيء في صفحة معينة", Question: "صفحة معينة بطيئة جدًا", Answer: "إذا كان البطء في صفحة واحدة فقط، حدّث الصفحة ثم جرّب متصفحًا آخر. إذا تكرر البطء، أرسل رابط الصفحة ونوع الجهاز للإدارة حتى يتم فحصها.", Category: "site_error", Keywords: "صفحة بطيئة,الموقع بطيء,تحميل بطيء,slow page,يتأخر", CountryCode: "all", IsActive: true, Priority: 51},
		{Title: "زر أو عنصر لا يعمل في الهاتف", Question: "زر في الموقع لا يعمل على الهاتف", Answer: "جرّب فتح الصفحة من Chrome أو Safari وتحديثها. إذا كان زرًا محددًا لا يعمل، أرسل اسم الزر ورابط الصفحة ونوع الجهاز للإدارة.", Category: "site_error", Keywords: "زر لا يعمل,الهاتف لا يعمل,الجوال لا يعمل,button not working,mobile issue", CountryCode: "all", IsActive: true, Priority: 52},
		{Title: "إعلان يغطي زر التحميل", Question: "إعلان يغطي زر التحميل أو المحتوى", Answer: "إذا كان إعلان أو عنصر يغطي زر التحميل، حدّث الصفحة أو أغلق الإعلان إن أمكن. إذا تكرر، أرسل صورة للشاشة ورابط الصفحة للإدارة لتعديل الموضع.", Category: "site_error", Keywords: "اعلان يغطي زر التحميل,إعلان يغطي المحتوى,ads cover,نافذة تغطي,العنصر يغطي", CountryCode: "all", IsActive: true, Priority: 53},
		{Title: "تعديل بيانات الحساب الأساسية", Question: "أريد تعديل اسمي أو بيانات حسابي", Answer: "افتح الملف الشخصي أو إعدادات الحساب وعدّل البيانات المتاحة. إذا لم يظهر خيار التعديل، تواصل مع الإدارة واذكر البريد والبيانات المطلوب تعديلها.", Category: "profile_problem", Keywords: "تعديل الاسم,تعديل بياناتي,تعديل الحساب,profile edit,تغيير بيانات الحساب", CountryCode: "all", IsActive: true, Priority: 60},
		{Title: "إكمال بيانات الحساب للتحميل", Question: "يطلب مني إكمال بيانات الحساب قبل التحميل", Answer: "بعض التحميلات تحتاج إكمال بيانات الحساب مثل الدولة أو الجنس أو معلومات أساسية. افتح الملف الشخصي، أكمل البيانات المطلوبة، ثم ارجع لصفحة الملف واضغط تحميل.", Category: "profile_problem", Keywords: "إكمال بيانات الحساب,اكمال البيانات,profile completion,لا يحمل قبل اكمال البيانات", CountryCode: "all", IsActive: true, Priority: 60},
		{Title: "الإبلاغ عن محتوى لا يخص المادة", Question: "الملف لا يخص الصف أو المادة", Answer: "إذا كان الملف لا يخص الصف أو المادة، أرسل رابط الصفحة وحدد التصنيف الصحيح المقترح. ستتم مراجعة البلاغ من الإدارة.", Category: "report_content", Keywords: "لا يخص المادة,لا يخص الصف,تصنيف خاطئ,محتوى خاطئ,ملف غير مناسب", CountryCode: "all", IsActive: true, Priority: 71},
		{Title: "طلب إضافة امتحان أو ملخص", Question: "أريد إضافة امتحان أو ملخص غير موجود", Answer: "أرسل طلبك للإدارة مع الدولة والصف والمادة والفصل ونوع الملف. مثال: الأردن، الصف الأول، اللغة العربية، الفصل الثاني، امتحان نهائي.", Category: "request_content", Keywords: "اضافة امتحان,إضافة ملخص,طلب امتحان,طلب ملخص,ملف غير موجود,missing file", CountryCode: "all", IsActive: true, Priority: 72},
		{Title: "ما المعلومات التي أرسلها للدعم", Question: "ماذا أكتب للإدارة حتى تحل المشكلة؟", Answer: "اكتب للإدارة: البريد المستخدم، رابط الصفحة، وصف المشكلة، نص رسالة الخطأ، نوع الجهاز والمتصفح، وصورة شاشة إن وجدت. هذه التفاصيل تختصر وقت الحل.", Category: "contact_support", Keywords: "ماذا ارسل للدعم,تفاصيل الدعم,معلومات للإدارة,كيف اشرح المشكلة,ما المعلومات المطلوبة", CountryCode: "all", IsActive: true, Priority: 70},

		// ردود عامة آمنة
		{Title: "ما الذي يستطيع المساعد فعله", Question: "ماذا تستطيع أن تفعل", Answer: "أستطيع مساعدتك في تسجيل الدخول، تفعيل البريد، مشاكل التحميل، البحث عن الملفات التعليمية، اختيار الصف والمادة والفصل، والإرشاد للتواصل مع الإدارة عند الحاجة.", Category: "general_question", Keywords: "ماذا تستطيع,مساعدة,بوت,مساعد,help,ماذا تفعل", CountryCode: "all", IsActive: true, Priority: 90},
		{Title: "طريقة السؤال الصحيحة", Question: "كيف أسأل بطريقة صحيحة", Answer: "اكتب المشكلة بجملة واضحة، واذكر الصف والمادة والفصل إذا كنت تبحث عن ملف. مثال: لا أستطيع تحميل ملف، أو أريد اختبار رياضيات الصف التاسع الفصل الأول.", Category: "general_question", Keywords: "كيف أسأل,طريقة السؤال,مساعدة,شرح", CountryCode: "all", IsActive: true, Priority: 91},
	}

	for _, item := range defaults {
		var existing models.ChatKnowledgeBase
		err := db.Where("question = ? AND country_code = ?", item.Question, item.CountryCode).First(&existing).Error
		if err == nil {
			updates := map[string]interface{}{
				"title":        item.Title,
				"answer":       item.Answer,
				"category":     item.Category,
				"keywords":     item.Keywords,
				"is_active":    item.IsActive,
				"priority":     item.Priority,
				"country_code": item.CountryCode,
			}
			if err := db.Model(&existing).Updates(updates).Error; err != nil {
				logger.Warn("chatbot default knowledge update failed", zap.String("country", countryCode), zap.String("category", item.Category), zap.Error(err))
			}
			continue
		}
		if err != nil && err != gorm.ErrRecordNotFound {
			logger.Warn("chatbot default knowledge lookup failed", zap.String("country", countryCode), zap.String("category", item.Category), zap.Error(err))
			continue
		}
		if err := db.Create(&item).Error; err != nil {
			logger.Warn("chatbot default knowledge seed failed", zap.String("country", countryCode), zap.String("category", item.Category), zap.Error(err))
		}
	}
}

func (r *repository) CreateAIUsage(countryID database.CountryID, usage *models.ChatAIUsage) error {
	if usage == nil {
		return nil
	}
	if usage.CountryCode == "" {
		usage.CountryCode = database.CountryCode(countryID)
	}
	return r.db(countryID).Create(usage).Error
}
