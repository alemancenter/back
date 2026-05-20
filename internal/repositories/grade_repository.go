package repositories

import (
	"github.com/alemancenter/fiber-api/internal/database"
	"github.com/alemancenter/fiber-api/internal/models"
	"gorm.io/gorm"
)

type GradeRepository interface {
	// School Classes
	ListSchoolClasses(countryID database.CountryID) ([]models.SchoolClass, error)
	FindSchoolClassByID(countryID database.CountryID, id uint64) (*models.SchoolClass, error)
	CreateSchoolClass(countryID database.CountryID, class *models.SchoolClass) error
	UpdateSchoolClass(countryID database.CountryID, class *models.SchoolClass) error
	DeleteSchoolClass(countryID database.CountryID, id uint64) error
	CountSchoolClasses(countryID database.CountryID) (int64, error)
	ListSchoolClassesPaginated(countryID database.CountryID, limit, offset int) ([]models.SchoolClass, error)

	// Subjects
	ListSubjectsByClassID(countryID database.CountryID, classID uint64) ([]models.Subject, error)
	FindSubjectByID(countryID database.CountryID, id uint64) (*models.Subject, error)
	CreateSubject(countryID database.CountryID, subject *models.Subject) error
	UpdateSubject(countryID database.CountryID, subject *models.Subject) error
	DeleteSubject(countryID database.CountryID, id uint64) error
	CountSubjects(countryID database.CountryID) (int64, error)
	ListSubjectsPaginated(countryID database.CountryID, limit, offset int) ([]models.Subject, error)

	// Semesters
	ListSemestersByGradeLevel(countryID database.CountryID, gradeLevel uint) ([]models.Semester, error)
	FindSemesterByID(countryID database.CountryID, id uint64) (*models.Semester, error)
	CreateSemester(countryID database.CountryID, semester *models.Semester) error
	UpdateSemester(countryID database.CountryID, semester *models.Semester) error
	DeleteSemester(countryID database.CountryID, id uint64) error
	CountSemesters(countryID database.CountryID) (int64, error)
	ListSemestersPaginated(countryID database.CountryID, limit, offset int) ([]models.Semester, error)

	// Articles
	CountGradeArticles(countryID database.CountryID, subjectID uint64) (int64, error)
	ListGradeArticlesPaginated(countryID database.CountryID, subjectID uint64, limit, offset int) ([]models.Article, error)
}

type gradeRepository struct{}

func NewGradeRepository() GradeRepository {
	return &gradeRepository{}
}

func (r *gradeRepository) getDB(countryID database.CountryID) *gorm.DB {
	return database.DBForCountry(countryID)
}

// ── School Classes ──────────────────────────────────────────────────────────

func (r *gradeRepository) ListSchoolClasses(countryID database.CountryID) ([]models.SchoolClass, error) {
	var list []models.SchoolClass
	err := r.getDB(countryID).Order("grade_level ASC, grade_name ASC").Find(&list).Error
	return list, err
}

func (r *gradeRepository) FindSchoolClassByID(countryID database.CountryID, id uint64) (*models.SchoolClass, error) {
	var class models.SchoolClass
	err := r.getDB(countryID).Preload("Subjects").First(&class, id).Error
	if err == nil {
		err = r.hydrateSubjectCounts(countryID, class.Subjects)
	}
	return &class, err
}

func (r *gradeRepository) CreateSchoolClass(countryID database.CountryID, class *models.SchoolClass) error {
	return r.getDB(countryID).Create(class).Error
}

func (r *gradeRepository) UpdateSchoolClass(countryID database.CountryID, class *models.SchoolClass) error {
	return r.getDB(countryID).Save(class).Error
}

func (r *gradeRepository) DeleteSchoolClass(countryID database.CountryID, id uint64) error {
	return r.getDB(countryID).Delete(&models.SchoolClass{}, id).Error
}

func (r *gradeRepository) CountSchoolClasses(countryID database.CountryID) (int64, error) {
	var total int64
	err := r.getDB(countryID).Model(&models.SchoolClass{}).Count(&total).Error
	return total, err
}

func (r *gradeRepository) ListSchoolClassesPaginated(countryID database.CountryID, limit, offset int) ([]models.SchoolClass, error) {
	var classes []models.SchoolClass
	err := r.getDB(countryID).Order("grade_level ASC, grade_name ASC").Limit(limit).Offset(offset).Find(&classes).Error
	return classes, err
}

// ── Subjects ────────────────────────────────────────────────────────────────

func (r *gradeRepository) ListSubjectsByClassID(countryID database.CountryID, classID uint64) ([]models.Subject, error) {
	var list []models.Subject
	err := r.getDB(countryID).Where("grade_level = ?", classID).Order("subject_name ASC").Find(&list).Error
	if err == nil {
		err = r.hydrateSubjectCounts(countryID, list)
	}
	return list, err
}

func (r *gradeRepository) FindSubjectByID(countryID database.CountryID, id uint64) (*models.Subject, error) {
	var subject models.Subject
	err := r.getDB(countryID).First(&subject, id).Error
	return &subject, err
}

func (r *gradeRepository) CreateSubject(countryID database.CountryID, subject *models.Subject) error {
	return r.getDB(countryID).Create(subject).Error
}

func (r *gradeRepository) UpdateSubject(countryID database.CountryID, subject *models.Subject) error {
	return r.getDB(countryID).Save(subject).Error
}

func (r *gradeRepository) DeleteSubject(countryID database.CountryID, id uint64) error {
	return r.getDB(countryID).Delete(&models.Subject{}, id).Error
}

func (r *gradeRepository) CountSubjects(countryID database.CountryID) (int64, error) {
	var total int64
	err := r.getDB(countryID).Model(&models.Subject{}).Count(&total).Error
	return total, err
}

func (r *gradeRepository) ListSubjectsPaginated(countryID database.CountryID, limit, offset int) ([]models.Subject, error) {
	var subjects []models.Subject
	err := r.getDB(countryID).Preload("SchoolClass").Order("subject_name ASC").Limit(limit).Offset(offset).Find(&subjects).Error
	if err == nil {
		err = r.hydrateSubjectCounts(countryID, subjects)
	}
	return subjects, err
}

func (r *gradeRepository) hydrateSubjectCounts(countryID database.CountryID, subjects []models.Subject) error {
	if len(subjects) == 0 {
		return nil
	}

	subjectIDs := make([]uint, 0, len(subjects))
	for _, subject := range subjects {
		subjectIDs = append(subjectIDs, subject.ID)
	}

	type countRow struct {
		SubjectID uint  `gorm:"column:subject_id"`
		Count     int64 `gorm:"column:count"`
	}

	var articleRows []countRow
	db := r.getDB(countryID)
	if err := db.Model(&models.Article{}).
		Select("subject_id, COUNT(*) AS count").
		Where("subject_id IN ? AND status = ?", subjectIDs, 1).
		Group("subject_id").
		Scan(&articleRows).Error; err != nil {
		return err
	}

	var fileRows []countRow
	if err := db.Table("files").
		Select("articles.subject_id AS subject_id, COUNT(files.id) AS count").
		Joins("JOIN articles ON articles.id = files.article_id").
		Where("articles.subject_id IN ? AND articles.status = ?", subjectIDs, 1).
		Group("articles.subject_id").
		Scan(&fileRows).Error; err != nil {
		return err
	}

	articleCounts := make(map[uint]int64, len(articleRows))
	for _, row := range articleRows {
		articleCounts[row.SubjectID] = row.Count
	}

	fileCounts := make(map[uint]int64, len(fileRows))
	for _, row := range fileRows {
		fileCounts[row.SubjectID] = row.Count
	}

	for i := range subjects {
		subjects[i].ArticlesCount = articleCounts[subjects[i].ID]
		subjects[i].FilesCount = fileCounts[subjects[i].ID]
	}

	return nil
}

// ── Semesters ───────────────────────────────────────────────────────────────

func (r *gradeRepository) ListSemestersByGradeLevel(countryID database.CountryID, gradeLevel uint) ([]models.Semester, error) {
	var list []models.Semester
	err := r.getDB(countryID).Where("grade_level = ?", gradeLevel).Order("semester_name ASC").Find(&list).Error
	return list, err
}

func (r *gradeRepository) FindSemesterByID(countryID database.CountryID, id uint64) (*models.Semester, error) {
	var semester models.Semester
	err := r.getDB(countryID).First(&semester, id).Error
	return &semester, err
}

func (r *gradeRepository) CreateSemester(countryID database.CountryID, semester *models.Semester) error {
	return r.getDB(countryID).Create(semester).Error
}

func (r *gradeRepository) UpdateSemester(countryID database.CountryID, semester *models.Semester) error {
	return r.getDB(countryID).Save(semester).Error
}

func (r *gradeRepository) DeleteSemester(countryID database.CountryID, id uint64) error {
	return r.getDB(countryID).Delete(&models.Semester{}, id).Error
}

func (r *gradeRepository) CountSemesters(countryID database.CountryID) (int64, error) {
	var total int64
	err := r.getDB(countryID).Model(&models.Semester{}).Count(&total).Error
	return total, err
}

func (r *gradeRepository) ListSemestersPaginated(countryID database.CountryID, limit, offset int) ([]models.Semester, error) {
	var semesters []models.Semester
	err := r.getDB(countryID).Preload("SchoolClass").Order("semester_name ASC").Limit(limit).Offset(offset).Find(&semesters).Error
	return semesters, err
}

// ── Articles ────────────────────────────────────────────────────────────────

func (r *gradeRepository) CountGradeArticles(countryID database.CountryID, subjectID uint64) (int64, error) {
	var total int64
	err := r.getDB(countryID).Model(&models.Article{}).Where("subject_id = ? AND status = ?", subjectID, 1).Count(&total).Error
	return total, err
}

func (r *gradeRepository) ListGradeArticlesPaginated(countryID database.CountryID, subjectID uint64, limit, offset int) ([]models.Article, error) {
	var articles []models.Article
	err := r.getDB(countryID).Preload("Subject").Preload("Semester").Preload("Files").
		Where("subject_id = ? AND status = ?", subjectID, 1).
		Order("published_at DESC").
		Limit(limit).Offset(offset).
		Find(&articles).Error
	return articles, err
}
