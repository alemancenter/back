package services

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/alemancenter/fiber-api/internal/database"
	"github.com/alemancenter/fiber-api/internal/models"
	"github.com/alemancenter/fiber-api/internal/repositories"
)

const (
	staticDataTTL         = 24 * time.Hour
	academicCountsDataTTL = time.Minute
)

type GradeService interface {
	// School Classes
	ListSchoolClasses(countryID database.CountryID) ([]models.SchoolClass, error)
	GetSchoolClass(countryID database.CountryID, id uint64) (*models.SchoolClass, error)
	CreateSchoolClass(countryID database.CountryID, req *SchoolClassInput) (*models.SchoolClass, error)
	UpdateSchoolClass(countryID database.CountryID, id uint64, req *SchoolClassInput) (*models.SchoolClass, error)
	DeleteSchoolClass(countryID database.CountryID, id uint64) error
	ListSchoolClassesDashboard(countryID database.CountryID, limit, offset int) ([]models.SchoolClass, int64, error)
	InvalidateClassCache(countryID database.CountryID)

	// Subjects
	ListSubjects(countryID database.CountryID, classID uint64) ([]models.Subject, error)
	GetSubject(countryID database.CountryID, id uint64) (*models.Subject, error)
	CreateSubject(countryID database.CountryID, req *SubjectInput) (*models.Subject, error)
	UpdateSubject(countryID database.CountryID, id uint64, req *SubjectInput) (*models.Subject, error)
	DeleteSubject(countryID database.CountryID, id uint64) error
	ListSubjectsDashboard(countryID database.CountryID, limit, offset int) ([]models.Subject, int64, error)

	// Semesters
	ListSemesters(countryID database.CountryID, subjectID uint64) ([]models.Semester, *models.Subject, error)
	GetSemester(countryID database.CountryID, id uint64) (*models.Semester, error)
	CreateSemester(countryID database.CountryID, req *SemesterInput) (*models.Semester, error)
	UpdateSemester(countryID database.CountryID, id uint64, req *SemesterInput) (*models.Semester, error)
	DeleteSemester(countryID database.CountryID, id uint64) error
	ListSemestersDashboard(countryID database.CountryID, limit, offset int) ([]models.Semester, int64, error)

	// Meta / Filter
	FilterMeta(countryID database.CountryID) ([]models.SchoolClass, error)

	// Grade Articles
	ListGradeArticles(countryID database.CountryID, subjectID uint64, limit, offset int) ([]models.Article, int64, error)
}

type SchoolClassInput struct {
	GradeName  string `json:"grade_name" validate:"required,min=2,max=255"`
	GradeLevel int    `json:"grade_level"`
}

type SubjectInput struct {
	SubjectName string `json:"subject_name" validate:"required,min=2,max=255"`
	GradeLevel  uint   `json:"grade_level" validate:"required"`
}

type SemesterInput struct {
	SemesterName string `json:"semester_name" validate:"required,min=2,max=255"`
	GradeLevel   uint   `json:"grade_level"`
}

type SemestersResponse struct {
	Subject   *models.Subject   `json:"subject"`
	ClassID   uint              `json:"class_id"`
	Semesters []models.Semester `json:"semesters"`
}

type FilterMetaResponse struct {
	Classes []models.SchoolClass `json:"classes"`
}

type gradeService struct {
	repo  repositories.GradeRepository
	cache CacheService
}

func NewGradeService(repo repositories.GradeRepository, cache CacheService) GradeService {
	return &gradeService{repo: repo, cache: cache}
}

// InvalidateClassCache removes school class and filter caches for a country.
func (s *gradeService) InvalidateClassCache(countryID database.CountryID) {
	if s.cache == nil {
		return
	}
	_ = s.cache.DeletePattern("school-classes:*")
	_ = s.cache.DeletePattern("school-class:*")
	_ = s.cache.DeletePattern("filter:subjects:*")
	_ = s.cache.DeletePattern("filter:semesters:*")
	_ = s.cache.DeletePattern("filter:meta:*")
	_ = s.cache.DeletePattern("home:*")
}

// ── School Classes ──────────────────────────────────────────────────────────

func (s *gradeService) ListSchoolClasses(countryID database.CountryID) ([]models.SchoolClass, error) {
	key := "school-classes:list:" + database.CountryCode(countryID)

	var cached []models.SchoolClass
	if s.cache != nil && s.cache.Get(key, &cached) {
		return cached, nil
	}

	classes, err := s.repo.ListSchoolClasses(countryID)
	if err != nil {
		return nil, MapError(err)
	}

	if s.cache != nil {
		_ = s.cache.Set(key, classes, staticDataTTL)
	}

	return classes, nil
}

func (s *gradeService) GetSchoolClass(countryID database.CountryID, id uint64) (*models.SchoolClass, error) {
	key := "school-class:v2:" + database.CountryCode(countryID) + ":" + strconv.FormatUint(id, 10)

	var cached models.SchoolClass
	if s.cache != nil && s.cache.Get(key, &cached) {
		return &cached, nil
	}

	class, err := s.repo.FindSchoolClassByID(countryID, id)
	if err != nil {
		return nil, MapError(err)
	}

	if s.cache != nil {
		_ = s.cache.Set(key, class, staticDataTTL)
	}

	return class, nil
}

func (s *gradeService) CreateSchoolClass(countryID database.CountryID, req *SchoolClassInput) (*models.SchoolClass, error) {
	class := &models.SchoolClass{
		GradeName:  req.GradeName,
		GradeLevel: req.GradeLevel,
	}

	if err := s.repo.CreateSchoolClass(countryID, class); err != nil {
		return nil, MapError(err)
	}
	s.InvalidateClassCache(countryID)
	return class, nil
}

func (s *gradeService) UpdateSchoolClass(countryID database.CountryID, id uint64, req *SchoolClassInput) (*models.SchoolClass, error) {
	class, err := s.repo.FindSchoolClassByID(countryID, id)
	if err != nil {
		return nil, MapError(err)
	}

	if req.GradeName != "" {
		class.GradeName = req.GradeName
	}
	if req.GradeLevel > 0 {
		class.GradeLevel = req.GradeLevel
	}

	if err := s.repo.UpdateSchoolClass(countryID, class); err != nil {
		return nil, MapError(err)
	}

	s.InvalidateClassCache(countryID)
	return class, nil
}

func (s *gradeService) DeleteSchoolClass(countryID database.CountryID, id uint64) error {
	if err := s.repo.DeleteSchoolClass(countryID, id); err != nil {
		return MapError(err)
	}
	s.InvalidateClassCache(countryID)
	return nil
}

func (s *gradeService) ListSchoolClassesDashboard(countryID database.CountryID, limit, offset int) ([]models.SchoolClass, int64, error) {
	total, err := s.repo.CountSchoolClasses(countryID)
	if err != nil {
		return nil, 0, MapError(err)
	}

	classes, err := s.repo.ListSchoolClassesPaginated(countryID, limit, offset)
	return classes, total, MapError(err)
}

// ── Subjects ────────────────────────────────────────────────────────────────

func (s *gradeService) ListSubjects(countryID database.CountryID, classID uint64) ([]models.Subject, error) {
	key := "filter:subjects:v2:" + database.CountryCode(countryID) + ":" + strconv.FormatUint(classID, 10)

	var cached []models.Subject
	if s.cache != nil && s.cache.Get(key, &cached) {
		return cached, nil
	}

	subjects, err := s.repo.ListSubjectsByClassID(countryID, classID)
	if err != nil {
		return nil, MapError(err)
	}

	if s.cache != nil {
		_ = s.cache.Set(key, subjects, staticDataTTL)
	}

	return subjects, nil
}

func (s *gradeService) CreateSubject(countryID database.CountryID, req *SubjectInput) (*models.Subject, error) {
	subject := &models.Subject{
		SubjectName: req.SubjectName,
		GradeLevel:  req.GradeLevel,
	}

	if err := s.repo.CreateSubject(countryID, subject); err != nil {
		return nil, MapError(err)
	}

	s.InvalidateClassCache(countryID)
	return subject, nil
}

func (s *gradeService) GetSubject(countryID database.CountryID, id uint64) (*models.Subject, error) {
	subject, err := s.repo.FindSubjectByID(countryID, id)
	return subject, MapError(err)
}

func (s *gradeService) UpdateSubject(countryID database.CountryID, id uint64, req *SubjectInput) (*models.Subject, error) {
	subject, err := s.repo.FindSubjectByID(countryID, id)
	if err != nil {
		return nil, MapError(err)
	}

	subject.SubjectName = req.SubjectName
	subject.GradeLevel = req.GradeLevel

	if err := s.repo.UpdateSubject(countryID, subject); err != nil {
		return nil, MapError(err)
	}

	s.InvalidateClassCache(countryID)
	return subject, nil
}

func (s *gradeService) DeleteSubject(countryID database.CountryID, id uint64) error {
	if _, err := s.repo.FindSubjectByID(countryID, id); err != nil {
		return MapError(err)
	}

	if err := s.repo.DeleteSubject(countryID, id); err != nil {
		return MapError(err)
	}

	s.InvalidateClassCache(countryID)
	return nil
}

func (s *gradeService) ListSubjectsDashboard(countryID database.CountryID, limit, offset int) ([]models.Subject, int64, error) {
	total, err := s.repo.CountSubjects(countryID)
	if err != nil {
		return nil, 0, MapError(err)
	}

	subjects, err := s.repo.ListSubjectsPaginated(countryID, limit, offset)
	return subjects, total, MapError(err)
}

// ── Semesters ───────────────────────────────────────────────────────────────

func (s *gradeService) ListSemesters(countryID database.CountryID, subjectID uint64) ([]models.Semester, *models.Subject, error) {
	subject, err := s.repo.FindSubjectByID(countryID, subjectID)
	if err != nil {
		return nil, nil, MapError(err)
	}

	key := "filter:semesters:" + database.CountryCode(countryID) + ":" + strconv.FormatUint(subjectID, 10)

	var cached []models.Semester
	if s.cache != nil && s.cache.Get(key, &cached) {
		return cached, subject, nil
	}

	semesters, err := s.repo.ListSemestersByGradeLevel(countryID, subject.GradeLevel)
	if err != nil {
		return nil, nil, MapError(err)
	}

	if s.cache != nil {
		_ = s.cache.Set(key, semesters, staticDataTTL)
	}

	return semesters, subject, nil
}

func (s *gradeService) GetSemester(countryID database.CountryID, id uint64) (*models.Semester, error) {
	semester, err := s.repo.FindSemesterByID(countryID, id)
	return semester, MapError(err)
}

func (s *gradeService) CreateSemester(countryID database.CountryID, req *SemesterInput) (*models.Semester, error) {
	semester := &models.Semester{
		SemesterName: req.SemesterName,
		GradeLevel:   req.GradeLevel,
	}

	if err := s.repo.CreateSemester(countryID, semester); err != nil {
		return nil, MapError(err)
	}

	s.InvalidateClassCache(countryID)
	return semester, nil
}

func (s *gradeService) UpdateSemester(countryID database.CountryID, id uint64, req *SemesterInput) (*models.Semester, error) {
	semester, err := s.repo.FindSemesterByID(countryID, id)
	if err != nil {
		return nil, MapError(err)
	}

	if req.SemesterName != "" {
		semester.SemesterName = req.SemesterName
	}
	if req.GradeLevel > 0 {
		semester.GradeLevel = req.GradeLevel
	}

	if err := s.repo.UpdateSemester(countryID, semester); err != nil {
		return nil, MapError(err)
	}

	s.InvalidateClassCache(countryID)
	return semester, nil
}

func (s *gradeService) DeleteSemester(countryID database.CountryID, id uint64) error {
	if _, err := s.repo.FindSemesterByID(countryID, id); err != nil {
		return MapError(err)
	}

	if err := s.repo.DeleteSemester(countryID, id); err != nil {
		return MapError(err)
	}

	s.InvalidateClassCache(countryID)
	return nil
}

func (s *gradeService) ListSemestersDashboard(countryID database.CountryID, limit, offset int) ([]models.Semester, int64, error) {
	total, err := s.repo.CountSemesters(countryID)
	if err != nil {
		return nil, 0, MapError(err)
	}

	semesters, err := s.repo.ListSemestersPaginated(countryID, limit, offset)
	return semesters, total, MapError(err)
}

// ── Meta / Filter ───────────────────────────────────────────────────────────

type filterResult struct {
	Classes []models.SchoolClass `json:"classes"`
}

func (s *gradeService) FilterMeta(countryID database.CountryID) ([]models.SchoolClass, error) {
	key := "filter:meta:" + database.CountryCode(countryID)

	var cached filterResult
	if s.cache != nil && s.cache.Get(key, &cached) {
		return cached.Classes, nil
	}

	classes, err := s.repo.ListSchoolClasses(countryID)
	if err != nil {
		return nil, MapError(err)
	}

	if s.cache != nil {
		_ = s.cache.Set(key, filterResult{Classes: classes}, staticDataTTL)
	}

	return classes, nil
}

// ── Grade Articles ──────────────────────────────────────────────────────────

func (s *gradeService) ListGradeArticles(countryID database.CountryID, subjectID uint64, limit, offset int) ([]models.Article, int64, error) {
	key := database.Redis().CountryKey(database.CountryCode(countryID), "grade-articles", fmt.Sprintf("sub%d-l%d-o%d", subjectID, limit, offset))

	type listResult struct {
		Articles []models.Article `json:"articles"`
		Total    int64            `json:"total"`
	}

	res, err := GetOrSet(context.Background(), key, 5*time.Minute, func() (listResult, error) {
		total, err := s.repo.CountGradeArticles(countryID, subjectID)
		if err != nil {
			return listResult{}, err
		}

		articles, err := s.repo.ListGradeArticlesPaginated(countryID, subjectID, limit, offset)
		return listResult{Articles: articles, Total: total}, err
	})

	return res.Articles, res.Total, MapError(err)
}
