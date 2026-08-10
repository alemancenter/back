package services

import (
	"fmt"
	"sync"
	"time"

	"github.com/imanjo/fiber-api/internal/database"
	"github.com/imanjo/fiber-api/internal/models"
	"github.com/imanjo/fiber-api/internal/repositories"
	"github.com/imanjo/fiber-api/internal/utils"
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
	sitemap SitemapService
}

func NewArticleService(repo repositories.ArticleRepository, fileSvc *FileService, cache CacheService, sitemap ...SitemapService) ArticleService {
	service := &articleService{
		repo:    repo,
		fileSvc: fileSvc,
		cache:   cache,
	}
	if len(sitemap) > 0 {
		service.sitemap = sitemap[0]
	}
	return service
}

func (s *articleService) scheduleSitemapRefresh(countryID database.CountryID) {
	if s.sitemap != nil {
		s.sitemap.ScheduleGenerate(database.CountryCode(countryID))
	}
}

func applyPendingArticleViews(countryID database.CountryID, articles []models.Article) []models.Article {
	if len(articles) == 0 {
		return articles
	}
	ids := make([]uint64, 0, len(articles))
	for _, article := range articles {
		ids = append(ids, uint64(article.ID))
	}
	pending := ViewCounter.PendingViews(countryID, "articles", ids)
	if len(pending) == 0 {
		return articles
	}
	for i := range articles {
		if extra := pending[uint64(articles[i].ID)]; extra > 0 {
			articles[i].VisitCount += int(extra)
		}
	}
	return articles
}

func (s *articleService) List(countryID database.CountryID, pag utils.Pagination, filter *models.ArticleFilter) ([]models.Article, int64, error) {
	cacheKey := utils.CacheKey("articles:list", countryID, pag.Page, pag.PerPage, filter)

	var cached struct {
		Articles []models.Article `json:"articles"`
		Total    int64            `json:"total"`
	}

	if s.cache != nil && s.cache.Get(cacheKey, &cached) {
		return applyPendingArticleViews(countryID, cached.Articles), cached.Total, nil
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

	return applyPendingArticleViews(countryID, articles), total, nil
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

	// Count a real download immediately. Page views are tracked separately by /files/:id/increment-view.
	go func() {
		_ = IncrementFileDownload(countryID, id)
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
		_ = IncrementFileDownload(countryID, claims.FileID)
	}()

	return file, absPath, nil
}

// GetDashboardCreateData used to return Subjects/Semesters as hardcoded empty slices
// (`Subjects: []models.Subject{}, Semesters: []models.Semester{}`), regardless of country —
// the dashboard's article-create form has a class → subject → semester cascade that filters
// client-side, so an empty payload meant no subject ever appeared no matter which class was
// selected. Fetching every subject/semester across all classes (not scoped to any single
// one) fixes this — the client already filters down to the selected class itself.
func (s *articleService) GetDashboardCreateData(countryID database.CountryID) (*ArticleDashboardCreateData, error) {
	var (
		classes                           []models.SchoolClass
		subjects                          []models.Subject
		semesters                         []models.Semester
		classErr, subjectErr, semesterErr error
		wg                                sync.WaitGroup
	)

	wg.Add(3)
	go func() {
		defer wg.Done()
		classes, classErr = s.repo.GetClasses(countryID)
	}()
	go func() {
		defer wg.Done()
		subjects, subjectErr = s.repo.GetAllSubjects(countryID)
	}()
	go func() {
		defer wg.Done()
		semesters, semesterErr = s.repo.GetAllSemesters(countryID)
	}()
	wg.Wait()

	if classErr != nil {
		return nil, MapError(classErr)
	}
	if subjectErr != nil {
		return nil, MapError(subjectErr)
	}
	if semesterErr != nil {
		return nil, MapError(semesterErr)
	}

	return &ArticleDashboardCreateData{
		Classes:   classes,
		Subjects:  subjects,
		Semesters: semesters,
	}, nil
}

func (s *articleService) GetDashboardEditData(countryID database.CountryID, id uint64) (*ArticleDashboardEditData, error) {
	article, err := s.repo.FindByID(countryID, id)
	if err != nil {
		return nil, MapError(err)
	}

	var (
		classes                           []models.SchoolClass
		subjects                          []models.Subject
		semesters                         []models.Semester
		classErr, subjectErr, semesterErr error
		wg                                sync.WaitGroup
	)

	// All subjects/semesters across every class (not just the article's current one) — an
	// admin editing an article can reclassify it to a different class entirely, and the
	// class/subject/semester selects cascade client-side, so they need the full lists to
	// filter against. Matches GetDashboardCreateData for the same reason.
	wg.Add(3)
	go func() {
		defer wg.Done()
		classes, classErr = s.repo.GetClasses(countryID)
	}()
	go func() {
		defer wg.Done()
		subjects, subjectErr = s.repo.GetAllSubjects(countryID)
	}()
	go func() {
		defer wg.Done()
		semesters, semesterErr = s.repo.GetAllSemesters(countryID)
	}()
	wg.Wait()

	if classErr != nil {
		return nil, MapError(classErr)
	}
	if subjectErr != nil {
		return nil, MapError(subjectErr)
	}
	if semesterErr != nil {
		return nil, MapError(semesterErr)
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
		// Same fix as post_service.go's UpdateKeywords calls: this was passing the raw request
		// value straight into stored Keyword rows with no sanitization at all.
		if err := s.repo.UpdateKeywords(countryID, article.ID, utils.SanitizeInput(req.Keywords)); err != nil {
			// Log the error but don't fail the article creation
			fmt.Printf("failed to update keywords for article %d: %v\n", article.ID, err)
		}
	}

	if s.cache != nil {
		_ = s.cache.DeletePattern("articles:list:*")
	}
	s.scheduleSitemapRefresh(countryID)

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
		if err := s.repo.UpdateKeywords(countryID, article.ID, utils.SanitizeInput(req.Keywords)); err != nil {
			// Log the error but don't fail the article update
			fmt.Printf("failed to update keywords for article %d: %v\n", article.ID, err)
		}
	}

	if s.cache != nil {
		_ = s.cache.DeletePattern("articles:list:*")
	}
	s.scheduleSitemapRefresh(countryID)

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
		s.scheduleSitemapRefresh(countryID)
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
	if err == nil {
		s.scheduleSitemapRefresh(countryID)
	}

	return article, MapError(err)
}

func (s *articleService) GetDashboardStats(countryID database.CountryID) (*ArticleDashboardStats, error) {
	total, published, drafts, views, err := s.repo.GetStats(countryID)
	if err != nil {
		return nil, MapError(err)
	}
	views += ViewCounter.PendingTotalViews(countryID, "articles")
	return &ArticleDashboardStats{
		Total:     total,
		Published: published,
		Drafts:    drafts,
		Views:     views,
	}, nil
}
