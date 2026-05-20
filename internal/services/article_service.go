package services

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/alemancenter/fiber-api/internal/database"
	"github.com/alemancenter/fiber-api/internal/models"
	"github.com/alemancenter/fiber-api/internal/repositories"
	"github.com/alemancenter/fiber-api/internal/utils"
)

type ArticleInput struct {
	Title           string `json:"title" form:"title" validate:"required,min=3,max=500"`
	Content         string `json:"content" form:"content" validate:"required"`
	GradeLevel      string `json:"grade_level" form:"grade_level"`
	SubjectID       *uint  `json:"subject_id" form:"subject_id"`
	SemesterID      *uint  `json:"semester_id" form:"semester_id"`
	MetaDescription string `json:"meta_description" form:"meta_description" validate:"omitempty,max=500"`
	Keywords        string `json:"keywords" form:"keywords"`
	Status          *int8  `json:"status" form:"status" validate:"omitempty,oneof=0 1"`
}

type ArticleDashboardCreateData struct {
	Classes   []models.SchoolClass `json:"classes"`
	Subjects  []models.Subject     `json:"subjects"`
	Semesters []models.Semester    `json:"semesters"`
}

type ArticleDashboardEditData struct {
	Data      *models.Article      `json:"data"`
	Classes   []models.SchoolClass `json:"classes"`
	Subjects  []models.Subject     `json:"subjects"`
	Semesters []models.Semester    `json:"semesters"`
}

type ArticleDashboardStats struct {
	Total     int64 `json:"total"`
	Published int64 `json:"published"`
	Drafts    int64 `json:"drafts"`
	Views     int64 `json:"views"`
}

type ArticleService interface {
	List(countryID database.CountryID, pag utils.Pagination, filter *models.ArticleFilter) ([]models.Article, int64, error)
	GetByID(countryID database.CountryID, id uint64) (*models.Article, error)
	GetByGradeLevel(countryID database.CountryID, gradeLevel string, pag utils.Pagination) ([]models.Article, int64, error)
	GetByKeyword(countryID database.CountryID, keyword string, pag utils.Pagination) ([]models.Article, int64, error)
	GetFileForDownload(countryID database.CountryID, id uint64) (*models.File, string, error)
	GetSignedDownloadToken(countryID database.CountryID, fileID uint64) (string, error)
	GetFileBySignedToken(token string) (*models.File, string, error)

	// Dashboard methods
	GetDashboardCreateData(countryID database.CountryID) (*ArticleDashboardCreateData, error)
	GetDashboardEditData(countryID database.CountryID, id uint64) (*ArticleDashboardEditData, error)
	CreateArticle(countryID database.CountryID, req *ArticleInput, authorID *uint) (*models.Article, error)
	UpdateArticle(countryID database.CountryID, id uint64, req *ArticleInput, authorID *uint) (*models.Article, error)
	DeleteArticle(countryID database.CountryID, id uint64, authorID *uint) error
	SetArticleStatus(countryID database.CountryID, id uint64, status int8) (*models.Article, error)
	GetDashboardStats(countryID database.CountryID) (*ArticleDashboardStats, error)
}

type articleService struct {
	repo    repositories.ArticleRepository
	fileSvc *FileService
	cache   CacheService
}

func NewArticleService(repo repositories.ArticleRepository, fileSvc *FileService, cache CacheService) ArticleService {
	return &articleService{
		repo:    repo,
		fileSvc: fileSvc,
		cache:   cache,
	}
}

func (s *articleService) List(countryID database.CountryID, pag utils.Pagination, filter *models.ArticleFilter) ([]models.Article, int64, error) {
	cacheKey := utils.CacheKey("articles:list", countryID, pag.Page, pag.PerPage, filter)

	var cached struct {
		Articles []models.Article `json:"articles"`
		Total    int64            `json:"total"`
	}

	if s.cache != nil && s.cache.Get(cacheKey, &cached) {
		return cached.Articles, cached.Total, nil
	}

	articles, total, err := s.repo.List(countryID, pag, filter)
	if err != nil {
		return nil, 0, MapError(err)
	}

	if s.cache != nil {
		_ = s.cache.Set(cacheKey, struct {
			Articles []models.Article `json:"articles"`
			Total    int64            `json:"total"`
		}{
			Articles: articles,
			Total:    total,
		}, 5*time.Minute)
	}

	return articles, total, nil
}

func (s *articleService) GetByID(countryID database.CountryID, id uint64) (*models.Article, error) {
	article, err := s.repo.FindByIDWithComments(countryID, id)
	if err != nil {
		return nil, MapError(err)
	}
	go func() {
		_ = ViewCounter.IncrementArticleView(countryID, id)
	}()
	return article, nil
}

func (s *articleService) GetByGradeLevel(countryID database.CountryID, gradeLevel string, pag utils.Pagination) ([]models.Article, int64, error) {
	articles, total, err := s.repo.FindByGradeLevel(countryID, gradeLevel, pag)
	return articles, total, MapError(err)
}

func (s *articleService) GetByKeyword(countryID database.CountryID, keyword string, pag utils.Pagination) ([]models.Article, int64, error) {
	articles, total, err := s.repo.FindByKeyword(countryID, keyword, pag)
	return articles, total, MapError(err)
}

func (s *articleService) GetFileForDownload(countryID database.CountryID, id uint64) (*models.File, string, error) {
	file, err := s.repo.GetFileByID(countryID, id)
	if err != nil {
		return nil, "", MapError(err)
	}

	var absPath string
	if s.fileSvc != nil {
		absPath = s.fileSvc.GetAbsPath(file.FilePath)
	} else {
		absPath = file.FilePath
	}

	// Increment view count async
	go func() {
		_ = ViewCounter.IncrementFileView(countryID, id)
	}()

	return file, absPath, nil
}

// GetSignedDownloadToken generates a short-lived (15 min) token that authorises
// downloading the given file without exposing the raw file path.
func (s *articleService) GetSignedDownloadToken(countryID database.CountryID, fileID uint64) (string, error) {
	// Verify file exists before issuing token
	if _, err := s.repo.GetFileByID(countryID, fileID); err != nil {
		return "", MapError(err)
	}
	jwtSvc := NewJWTService()
	return jwtSvc.GenerateDownloadToken(fileID, uint(countryID))
}

// GetFileBySignedToken validates a signed download token and returns the file + abs path.
func (s *articleService) GetFileBySignedToken(token string) (*models.File, string, error) {
	jwtSvc := NewJWTService()
	claims, err := jwtSvc.ValidateDownloadToken(token)
	if err != nil {
		return nil, "", MapError(err)
	}

	countryID := database.CountryID(claims.CountryID)
	file, err := s.repo.GetFileByID(countryID, claims.FileID)
	if err != nil {
		return nil, "", MapError(err)
	}

	var absPath string
	if s.fileSvc != nil {
		absPath = s.fileSvc.GetAbsPath(file.FilePath)
	} else {
		absPath = file.FilePath
	}

	go func() {
		_ = ViewCounter.IncrementFileView(countryID, claims.FileID)
	}()

	return file, absPath, nil
}

func (s *articleService) GetDashboardCreateData(countryID database.CountryID) (*ArticleDashboardCreateData, error) {
	classes, err := s.repo.GetClasses(countryID)
	if err != nil {
		return nil, MapError(err)
	}
	return &ArticleDashboardCreateData{
		Classes:   classes,
		Subjects:  []models.Subject{},
		Semesters: []models.Semester{},
	}, nil
}

func (s *articleService) GetDashboardEditData(countryID database.CountryID, id uint64) (*ArticleDashboardEditData, error) {
	article, err := s.repo.FindByID(countryID, id)
	if err != nil {
		return nil, MapError(err)
	}

	classID := articleClassID(article)
	if classID == 0 && article.SubjectID != nil {
		if subject, err := s.repo.GetSubjectByID(countryID, *article.SubjectID); err == nil {
			classID = subject.GradeLevel
		}
	}

	var (
		classes   []models.SchoolClass
		subjects  []models.Subject
		semesters []models.Semester
		classErr  error
		wg        sync.WaitGroup
	)

	wg.Add(1)
	go func() {
		defer wg.Done()
		classes, classErr = s.repo.GetClasses(countryID)
	}()

	if classID > 0 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			subjects, _ = s.repo.GetSubjectsByClass(countryID, classID)
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			semesters, _ = s.repo.GetSemestersByClass(countryID, classID)
		}()
	}

	wg.Wait()

	if classErr != nil {
		return nil, classErr
	}

	return &ArticleDashboardEditData{
		Data:      article,
		Classes:   classes,
		Subjects:  subjects,
		Semesters: semesters,
	}, nil
}

func (s *articleService) CreateArticle(countryID database.CountryID, req *ArticleInput, authorID *uint) (*models.Article, error) {
	article := &models.Article{
		Title:   utils.SanitizeInput(req.Title),
		Content: utils.SanitizeHTML(req.Content),
	}

	if req.Status != nil {
		article.Status = *req.Status
	}

	if req.GradeLevel != "" {
		article.GradeLevel = &req.GradeLevel
	}
	if req.SubjectID != nil && *req.SubjectID > 0 {
		article.SubjectID = req.SubjectID
	}
	if req.SemesterID != nil && *req.SemesterID > 0 {
		article.SemesterID = req.SemesterID
	}
	if req.MetaDescription != "" {
		article.MetaDescription = &req.MetaDescription
	}

	if authorID != nil {
		article.AuthorID = authorID
	}

	err := s.repo.Create(countryID, article)
	if err != nil {
		return nil, MapError(err)
	}

	// Handle Keywords using KeywordsRel many-to-many relationship
	if req.Keywords != "" {
		if err := s.repo.UpdateKeywords(countryID, article.ID, req.Keywords); err != nil {
			// Log the error but don't fail the article creation
			fmt.Printf("failed to update keywords for article %d: %v\n", article.ID, err)
		}
	}

	if s.cache != nil {
		_ = s.cache.DeletePattern("articles:list:*")
	}

	if authorID != nil {
		LogActivity("أنشأ مقالة: "+article.Title, "Article", article.ID, *authorID)
	}
	return article, nil
}

func (s *articleService) UpdateArticle(countryID database.CountryID, id uint64, req *ArticleInput, authorID *uint) (*models.Article, error) {
	article, err := s.repo.FindByID(countryID, id)
	if err != nil {
		return nil, MapError(err)
	}

	if req.Title != "" {
		article.Title = utils.SanitizeInput(req.Title)
	}
	if req.Content != "" {
		article.Content = utils.SanitizeHTML(req.Content)
	}
	if req.GradeLevel != "" {
		article.GradeLevel = &req.GradeLevel
	}
	if req.SubjectID != nil {
		article.SubjectID = req.SubjectID
	}
	if req.SemesterID != nil {
		article.SemesterID = req.SemesterID
	}
	if req.MetaDescription != "" {
		article.MetaDescription = &req.MetaDescription
	}

	// TODO: Handle Keywords using KeywordsRel many-to-many relationship

	if req.Status != nil {
		article.Status = *req.Status
	}

	err = s.repo.Update(countryID, article)
	if err != nil {
		return nil, MapError(err)
	}

	// Handle Keywords using KeywordsRel many-to-many relationship
	if req.Keywords != "" {
		if err := s.repo.UpdateKeywords(countryID, article.ID, req.Keywords); err != nil {
			// Log the error but don't fail the article update
			fmt.Printf("failed to update keywords for article %d: %v\n", article.ID, err)
		}
	}

	if s.cache != nil {
		_ = s.cache.DeletePattern("articles:list:*")
	}

	if authorID != nil {
		LogActivity("حدّث مقالة: "+article.Title, "Article", article.ID, *authorID)
	}

	return article, nil
}

func (s *articleService) DeleteArticle(countryID database.CountryID, id uint64, authorID *uint) error {
	article, err := s.repo.FindByID(countryID, id)
	if err != nil {
		return MapError(err)
	}

	err = s.repo.Delete(countryID, article)
	if err == nil {
		if s.cache != nil {
			_ = s.cache.DeletePattern("articles:list:*")
		}
		if authorID != nil {
			LogActivity("حذف مقالة: "+article.Title, "Article", article.ID, *authorID)
		}
	}

	return MapError(err)
}

func (s *articleService) SetArticleStatus(countryID database.CountryID, id uint64, status int8) (*models.Article, error) {
	article, err := s.repo.FindByID(countryID, id)
	if err != nil {
		return nil, MapError(err)
	}

	article.Status = status
	if status == 1 && article.PublishedAt == nil {
		now := time.Now()
		article.PublishedAt = &now
	} else if status == 0 {
		article.PublishedAt = nil
	}
	err = s.repo.Update(countryID, article)

	if err == nil && s.cache != nil {
		_ = s.cache.DeletePattern("articles:list:*")
	}

	return article, MapError(err)
}

func (s *articleService) GetDashboardStats(countryID database.CountryID) (*ArticleDashboardStats, error) {
	ctx := context.Background()
	key := fmt.Sprintf("article_stats:%d", countryID)
	return GetOrSet(ctx, key, time.Hour, func() (*ArticleDashboardStats, error) {
		total, published, drafts, views, err := s.repo.GetStats(countryID)
		if err != nil {
			return nil, MapError(err)
		}
		return &ArticleDashboardStats{
			Total:     total,
			Published: published,
			Drafts:    drafts,
			Views:     views,
		}, nil
	})
}

func articleClassID(article *models.Article) uint {
	if article == nil || article.GradeLevel == nil || *article.GradeLevel == "" {
		return 0
	}

	id, err := strconv.ParseUint(*article.GradeLevel, 10, 64)
	if err != nil {
		return 0
	}

	return uint(id)
}
