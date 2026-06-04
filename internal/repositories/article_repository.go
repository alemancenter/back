package repositories

import (
	"github.com/alemancenter/fiber-api/internal/database"
	"github.com/alemancenter/fiber-api/internal/models"
	"github.com/alemancenter/fiber-api/internal/utils"
	"gorm.io/gorm"
)

type ArticleRepository interface {
	List(countryID database.CountryID, pag utils.Pagination, filter *models.ArticleFilter) ([]models.Article, int64, error)
	FindByID(countryID database.CountryID, id uint64) (*models.Article, error)
	FindByIDWithComments(countryID database.CountryID, id uint64) (*models.Article, error)
	FindByGradeLevel(countryID database.CountryID, gradeLevel string, pag utils.Pagination) ([]models.Article, int64, error)
	FindByKeyword(countryID database.CountryID, keyword string, pag utils.Pagination) ([]models.Article, int64, error)
	Create(countryID database.CountryID, article *models.Article) error
	Update(countryID database.CountryID, article *models.Article) error
	Delete(countryID database.CountryID, article *models.Article) error
	GetFileByID(countryID database.CountryID, id uint64) (*models.File, error)
	IncrementViewCount(countryID database.CountryID, articleID uint64) error
	IncrementFileViewCount(countryID database.CountryID, fileID uint64) error
	GetClasses(countryID database.CountryID) ([]models.SchoolClass, error)
	GetSubjectsByClass(countryID database.CountryID, classID uint) ([]models.Subject, error)
	GetSubjectByID(countryID database.CountryID, subjectID uint) (*models.Subject, error)
	GetSemestersByClass(countryID database.CountryID, classID uint) ([]models.Semester, error)
	GetStats(countryID database.CountryID) (total, published, drafts, views int64, err error)
	UpdateKeywords(countryID database.CountryID, articleID uint, keywordsStr string) error
}

// fileCategoryAliases maps each canonical slug to all values that may be stored
// in the database (new Go-dashboard values + old Laravel-migrated values).
var fileCategoryAliases = map[string][]string{
	"plans":   {"study_plan", "study-plan", "plan", "plans"},
	"papers":  {"worksheet", "worksheets", "papers"},
	"tests":   {"exam", "exams", "test", "tests"},
	"books":   {"book", "books"},
	"records": {"record", "records"},
}

// resolveFileCategories returns all known database values for a given slug.
// Falls back to a slice containing the original value when the slug is unknown.
func resolveFileCategories(slug string) []string {
	if aliases, ok := fileCategoryAliases[slug]; ok {
		return aliases
	}
	// Also check if the slug is itself a known alias (e.g. "study_plan" passed directly)
	for _, aliases := range fileCategoryAliases {
		for _, a := range aliases {
			if a == slug {
				return aliases
			}
		}
	}
	return []string{slug}
}

type articleRepository struct{}

func NewArticleRepository() ArticleRepository {
	return &articleRepository{}
}

func (r *articleRepository) GetDB(countryID database.CountryID) *gorm.DB {
	return database.DBForCountry(countryID)
}

// allowedArticleOrders is an allowlist of safe ORDER BY expressions for articles.
var allowedArticleOrders = map[string]bool{
	"created_at DESC":  true,
	"created_at ASC":   true,
	"visit_count DESC": true,
	"visit_count ASC":  true,
	"title ASC":        true,
	"title DESC":       true,
}

func (r *articleRepository) List(countryID database.CountryID, pag utils.Pagination, filter *models.ArticleFilter) ([]models.Article, int64, error) {
	db := r.GetDB(countryID)
	var articles []models.Article
	var total int64

	query := db.Model(&models.Article{}).Preload("Subject").Preload("Semester").Preload("Files")

	if filter != nil {
		if filter.Status != nil {
			query = query.Where("status = ?", *filter.Status)
		}

		if filter.GradeLevel != "" {
			query = query.Where("grade_level = ?", filter.GradeLevel)
		}
		if filter.SubjectID != nil {
			query = query.Where("subject_id = ?", *filter.SubjectID)
		}
		if filter.SemesterID != nil {
			query = query.Where("semester_id = ?", *filter.SemesterID)
		}
		if filter.FileCategory != "" {
			cats := resolveFileCategories(filter.FileCategory)
			query = query.Where(
				"EXISTS (SELECT 1 FROM files WHERE files.article_id = articles.id AND files.file_category IN ?)",
				cats,
			)
		}
		if filter.Query != "" {
			query = query.Where("title LIKE ?", "%"+filter.Query+"%")
		}

		if filter.Order != "" && allowedArticleOrders[filter.Order] {
			query = query.Order(filter.Order)
		} else {
			query = query.Order("created_at DESC")
		}
	} else {
		query = query.Order("created_at DESC")
	}

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	err := query.Limit(pag.PerPage).Offset(pag.Offset).Find(&articles).Error
	return articles, total, err
}

func (r *articleRepository) FindByID(countryID database.CountryID, id uint64) (*models.Article, error) {
	db := r.GetDB(countryID)
	var article models.Article
	// Dashboard edits don't need Comments — omit to avoid the nested join on large datasets.
	err := db.Preload("Subject").Preload("Semester").Preload("Files").Preload("KeywordsRel").
		First(&article, id).Error
	return &article, err
}

func (r *articleRepository) FindByIDWithComments(countryID database.CountryID, id uint64) (*models.Article, error) {
	db := r.GetDB(countryID)
	var article models.Article
	err := db.Preload("Subject").Preload("Semester").Preload("Files").
		Preload("Comments", "status = ?", models.CommentStatusApproved).
		Preload("Comments.User").Preload("KeywordsRel").
		First(&article, id).Error
	return &article, err
}

func (r *articleRepository) FindByGradeLevel(countryID database.CountryID, gradeLevel string, pag utils.Pagination) ([]models.Article, int64, error) {
	db := r.GetDB(countryID)
	var articles []models.Article
	var total int64

	query := db.Model(&models.Article{}).Where("grade_level = ? AND status = ?", gradeLevel, 1)
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	err := query.Preload("Subject").Preload("Semester").
		Order("created_at DESC").
		Limit(pag.PerPage).Offset(pag.Offset).
		Find(&articles).Error
	return articles, total, err
}

func (r *articleRepository) FindByKeyword(countryID database.CountryID, keyword string, pag utils.Pagination) ([]models.Article, int64, error) {
	db := r.GetDB(countryID)
	var articles []models.Article
	var total int64

	var kw models.Keyword
	if err := db.Where("keyword = ?", keyword).First(&kw).Error; err != nil {
		return nil, 0, err // Not found or error
	}

	subQuery := db.Table("article_keyword").Select("article_id").Where("keyword_id = ?", kw.ID)
	query := db.Model(&models.Article{}).Where("id IN (?) AND status = ?", subQuery, 1)

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	err := query.Preload("Subject").Preload("Semester").
		Order("created_at DESC").
		Limit(pag.PerPage).Offset(pag.Offset).
		Find(&articles).Error

	return articles, total, err
}

func (r *articleRepository) Create(countryID database.CountryID, article *models.Article) error {
	return r.GetDB(countryID).Create(article).Error
}

func (r *articleRepository) Update(countryID database.CountryID, article *models.Article) error {
	return r.GetDB(countryID).Save(article).Error
}

func (r *articleRepository) Delete(countryID database.CountryID, article *models.Article) error {
	return r.GetDB(countryID).Delete(article).Error
}

func (r *articleRepository) GetFileByID(countryID database.CountryID, id uint64) (*models.File, error) {
	db := r.GetDB(countryID)
	var file models.File
	err := db.First(&file, id).Error
	return &file, err
}

func (r *articleRepository) IncrementViewCount(countryID database.CountryID, articleID uint64) error {
	db := r.GetDB(countryID)
	return db.Model(&models.Article{}).Where("id = ?", articleID).UpdateColumn("visit_count", gorm.Expr("visit_count + 1")).Error
}

func (r *articleRepository) IncrementFileViewCount(countryID database.CountryID, fileID uint64) error {
	db := r.GetDB(countryID)
	if db == nil {
		return nil
	}
	updated := false
	for _, col := range []string{"view_count", "views_count"} {
		if !db.Migrator().HasColumn(&models.File{}, col) {
			continue
		}
		if err := db.Exec("UPDATE files SET "+col+" = LEAST(COALESCE("+col+", 0) + 1, 2147483647) WHERE id = ?", fileID).Error; err != nil {
			return err
		}
		updated = true
	}
	if !updated {
		return nil
	}
	return nil
}

func (r *articleRepository) GetClasses(countryID database.CountryID) ([]models.SchoolClass, error) {
	var classes []models.SchoolClass
	err := r.GetDB(countryID).Order("grade_level ASC, grade_name ASC").Find(&classes).Error
	return classes, err
}

func (r *articleRepository) GetSubjectsByClass(countryID database.CountryID, classID uint) ([]models.Subject, error) {
	var subjects []models.Subject
	err := r.GetDB(countryID).Where("grade_level = ?", classID).Order("subject_name ASC").Find(&subjects).Error
	return subjects, err
}

func (r *articleRepository) GetSubjectByID(countryID database.CountryID, subjectID uint) (*models.Subject, error) {
	var subject models.Subject
	err := r.GetDB(countryID).First(&subject, subjectID).Error
	return &subject, err
}

func (r *articleRepository) GetSemestersByClass(countryID database.CountryID, classID uint) ([]models.Semester, error) {
	var semesters []models.Semester
	err := r.GetDB(countryID).Where("grade_level = ?", classID).Order("semester_name ASC").Find(&semesters).Error
	return semesters, err
}

func (r *articleRepository) GetStats(countryID database.CountryID) (total, published, drafts, views int64, err error) {
	db := r.GetDB(countryID)

	type statsRow struct {
		Total     int64
		Published int64
		Drafts    int64
		Views     int64
	}
	var row statsRow
	err = db.Model(&models.Article{}).Select(
		"COUNT(*) AS total, " +
			"SUM(CASE WHEN status = 1 THEN 1 ELSE 0 END) AS published, " +
			"SUM(CASE WHEN status = 0 THEN 1 ELSE 0 END) AS drafts, " +
			"COALESCE(SUM(visit_count), 0) AS views",
	).Scan(&row).Error
	return row.Total, row.Published, row.Drafts, row.Views, err
}

func (r *articleRepository) UpdateKeywords(countryID database.CountryID, articleID uint, keywordsStr string) error {
	db := r.GetDB(countryID)

	// Split and trim keywords
	keywordList := utils.SplitKeywords(keywordsStr)
	if len(keywordList) == 0 {
		// Clear existing associations
		return db.Model(&models.Article{ID: articleID}).Association("KeywordsRel").Clear()
	}

	var keywords []models.Keyword
	for _, kw := range keywordList {
		var keyword models.Keyword
		// Find or create keyword
		if err := db.Where("keyword = ?", kw).FirstOrCreate(&keyword, models.Keyword{Keyword: kw}).Error; err == nil {
			keywords = append(keywords, keyword)
		}
	}

	// Replace associations
	return db.Model(&models.Article{ID: articleID}).Association("KeywordsRel").Replace(keywords)
}
