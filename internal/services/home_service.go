package services

import (
	"context"
	"time"

	"github.com/alemancenter/fiber-api/internal/database"
	"github.com/alemancenter/fiber-api/internal/models"
	"github.com/alemancenter/fiber-api/internal/repositories"
	"github.com/alemancenter/fiber-api/internal/utils"
	"github.com/alemancenter/fiber-api/pkg/logger"
	"go.uber.org/zap"
)

type HomeData struct {
	Articles      []models.Article     `json:"articles"`
	Posts         []models.Post        `json:"posts"`
	FeaturedPosts []models.Post        `json:"featured_posts"`
	Categories    []models.Category    `json:"categories"`
	Classes       []models.SchoolClass `json:"classes"`
	Settings      map[string]string    `json:"settings"`
}

type HomeService interface {
	GetHome(ctx context.Context, countryID database.CountryID) (*HomeData, error)
}

type homeService struct {
	articleRepo  repositories.ArticleRepository
	postRepo     repositories.PostRepository
	categoryRepo repositories.CategoryRepository
	gradeRepo    repositories.GradeRepository
	cache        CacheService
	settings     SettingService
}

func NewHomeService(
	articleRepo repositories.ArticleRepository,
	postRepo repositories.PostRepository,
	categoryRepo repositories.CategoryRepository,
	gradeRepo repositories.GradeRepository,
	cache CacheService,
	settings SettingService,
) HomeService {
	return &homeService{
		articleRepo:  articleRepo,
		postRepo:     postRepo,
		categoryRepo: categoryRepo,
		gradeRepo:    gradeRepo,
		cache:        cache,
		settings:     settings,
	}
}

func (s *homeService) GetHome(ctx context.Context, countryID database.CountryID) (*HomeData, error) {
	countryCode := database.CountryCode(countryID)
	cacheKey := database.Redis().Key("home", countryCode)

	var cached HomeData
	if s.cache != nil && s.cache.Get(cacheKey, &cached) {
		if err := s.attachSettings(ctx, countryID, &cached); err != nil {
			return nil, err
		}
		logger.Debug("cache hit:", zap.String("key", cacheKey))
		return &cached, nil
	}

	logger.Debug("cache miss:", zap.String("key", cacheKey))
	data := &HomeData{}

	// Get Articles
	statusPublished := 1
	articles, _, err := s.articleRepo.List(countryID, utils.Pagination{Page: 1, PerPage: 6}, &models.ArticleFilter{
		Status: &statusPublished,
	})
	if err != nil {
		return nil, MapError(err)
	}
	data.Articles = articles

	// Get Posts
	activeTrue := true
	postsFilter := &models.PostFilter{
		Search:   "",
		IsActive: &activeTrue,
	}
	// Need to check how ListPaginated filters by active, usually it's handled in the repo
	posts, _, err := s.postRepo.ListPaginated(countryID, postsFilter, 6, 0)
	if err != nil {
		return nil, MapError(err)
	}
	data.Posts = posts

	// Get Featured Posts
	featuredFilter := &models.PostFilter{
		Featured: "1", // Use "1" as repository checks for "1"
		IsActive: &activeTrue,
	}
	featuredPosts, _, err := s.postRepo.ListPaginated(countryID, featuredFilter, 6, 0)
	if err != nil {
		return nil, MapError(err)
	}
	data.FeaturedPosts = featuredPosts

	// Get Categories
	categories, err := s.categoryRepo.FindAllActive(countryID)
	if err != nil {
		return nil, MapError(err)
	}
	data.Categories = categories

	// Get School Classes
	classes, err := s.gradeRepo.ListSchoolClasses(countryID)
	if err != nil {
		return nil, MapError(err)
	}
	data.Classes = classes

	if s.cache != nil {
		cacheData := *data
		// Keep settings in their own country-scoped cache so settings updates
		// do not invalidate the heavier home page payload.
		cacheData.Settings = nil
		_ = s.cache.Set(cacheKey, &cacheData, 5*time.Minute)
	}

	if err := s.attachSettings(ctx, countryID, data); err != nil {
		return nil, err
	}

	return data, nil
}

func (s *homeService) attachSettings(ctx context.Context, countryID database.CountryID, data *HomeData) error {
	if s.settings == nil {
		data.Settings = map[string]string{}
		return nil
	}

	settings, err := s.settings.GetPublic(ctx, countryID)
	if err != nil {
		// Settings table may not exist yet on a fresh database — degrade gracefully
		logger.Warn("settings unavailable, returning empty map", zap.Error(err))
		data.Settings = map[string]string{}
		return nil
	}
	data.Settings = settings
	return nil
}
