package services

import (
	"context"
	"time"

	"github.com/alemancenter/fiber-api/internal/database"
	"github.com/alemancenter/fiber-api/internal/models"
	"github.com/alemancenter/fiber-api/internal/repositories"
	"github.com/alemancenter/fiber-api/internal/utils"
)

const categoriesCacheTTL = 15 * time.Minute

type CategoryService interface {
	GetActiveCategories(countryID database.CountryID) ([]models.Category, error)
	GetByID(countryID database.CountryID, id uint64) (*models.Category, error)
	ListDashboard(countryID database.CountryID, search string, isActiveStr string, limit, offset int) ([]models.Category, int64, error)
	Create(countryID database.CountryID, req *CreateCategoryRequest) (*models.Category, error)
	Update(countryID database.CountryID, id uint64, req *UpdateCategoryRequest) (*models.Category, error)
	Delete(countryID database.CountryID, id uint64) error
	BulkDelete(countryID database.CountryID, ids []uint) error
	UpdateStatus(countryID database.CountryID, ids []uint, isActive bool) error
}

type CreateCategoryRequest struct {
	Name      string `json:"name" validate:"required,min=2,max=255"`
	Slug      string `json:"slug"`
	Icon      string `json:"icon"`
	Image     string `json:"image"`
	IconImage string `json:"icon_image"`
	ParentID  *uint  `json:"parent_id"`
	IsActive  *bool  `json:"is_active"`
}

type UpdateCategoryRequest struct {
	Name      string `json:"name"`
	Slug      string `json:"slug"`
	IsActive  *bool  `json:"is_active"`
	ParentID  *uint  `json:"parent_id"`
	Icon      string `json:"icon"`
	Image     string `json:"image"`
	IconImage string `json:"icon_image"`
	Depth     *int   `json:"depth"`
}

type categoryService struct {
	repo  repositories.CategoryRepository
	cache CacheService
}

func NewCategoryService(repo repositories.CategoryRepository, cache CacheService) CategoryService {
	return &categoryService{repo: repo, cache: cache}
}

func categoriesKey(countryID database.CountryID) string {
	return database.Redis().CountryKey(database.CountryCode(countryID), "categories")
}

func (s *categoryService) invalidateCache(countryID database.CountryID) {
	InvalidateCache(categoriesKey(countryID))
}

func (s *categoryService) GetActiveCategories(countryID database.CountryID) ([]models.Category, error) {
	cats, err := GetOrSet(context.Background(), categoriesKey(countryID), categoriesCacheTTL, func() ([]models.Category, error) {
		return s.repo.FindAllActive(countryID)
	})
	return cats, MapError(err)
}

func (s *categoryService) GetByID(countryID database.CountryID, id uint64) (*models.Category, error) {
	category, err := s.repo.FindByID(countryID, id)
	return category, MapError(err)
}

func (s *categoryService) ListDashboard(countryID database.CountryID, search string, isActiveStr string, limit, offset int) ([]models.Category, int64, error) {
	var isActive *bool
	if isActiveStr != "" {
		active := isActiveStr == "true" || isActiveStr == "1"
		isActive = &active
	}
	cats, total, err := s.repo.ListPaginated(countryID, search, isActive, limit, offset)
	return cats, total, MapError(err)
}

func (s *categoryService) Create(countryID database.CountryID, req *CreateCategoryRequest) (*models.Category, error) {
	slug := req.Slug
	if slug == "" {
		slug = utils.GenerateSlug(req.Name)
	}

	isActive := true
	if req.IsActive != nil {
		isActive = *req.IsActive
	}

	category := &models.Category{
		Name:     req.Name,
		Slug:     slug,
		IsActive: isActive,
		Country:  database.CountryCode(countryID),
	}
	if req.Icon != "" {
		category.Icon = &req.Icon
	}
	if req.Image != "" {
		category.Image = &req.Image
	}
	if req.IconImage != "" {
		category.IconImage = &req.IconImage
	}
	if req.ParentID != nil {
		category.ParentID = req.ParentID
	}

	if err := s.repo.Create(countryID, category); err != nil {
		return nil, MapError(err)
	}

	s.invalidateCache(countryID)
	return category, nil
}

func (s *categoryService) Update(countryID database.CountryID, id uint64, req *UpdateCategoryRequest) (*models.Category, error) {
	category, err := s.repo.FindByID(countryID, id)
	if err != nil {
		return nil, MapError(err)
	}

	if req.Name != "" {
		category.Name = req.Name
	}
	if req.Slug != "" {
		category.Slug = req.Slug
	}
	if req.IsActive != nil {
		category.IsActive = *req.IsActive
	}
	if req.ParentID != nil {
		category.ParentID = req.ParentID
	}
	if req.Icon != "" {
		category.Icon = &req.Icon
	}
	if req.Image != "" {
		category.Image = &req.Image
	}
	if req.IconImage != "" {
		category.IconImage = &req.IconImage
	}
	if req.Depth != nil {
		category.Depth = *req.Depth
	}

	if err := s.repo.Update(countryID, category); err != nil {
		return nil, MapError(err)
	}
	s.invalidateCache(countryID)

	return category, nil
}

func (s *categoryService) Delete(countryID database.CountryID, id uint64) error {
	if err := s.repo.Delete(countryID, id); err != nil {
		return MapError(err)
	}
	s.invalidateCache(countryID)
	return nil
}

func (s *categoryService) BulkDelete(countryID database.CountryID, ids []uint) error {
	if len(ids) == 0 {
		return nil
	}
	if err := s.repo.BulkDelete(countryID, ids); err != nil {
		return MapError(err)
	}
	s.invalidateCache(countryID)
	return nil
}

func (s *categoryService) UpdateStatus(countryID database.CountryID, ids []uint, isActive bool) error {
	if len(ids) == 0 {
		return nil
	}
	if err := s.repo.UpdateStatus(countryID, ids, isActive); err != nil {
		return MapError(err)
	}
	s.invalidateCache(countryID)
	return nil
}
