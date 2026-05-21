package services

import (
	"fmt"
	"regexp"
	"time"

	"github.com/alemancenter/fiber-api/internal/database"
	"github.com/alemancenter/fiber-api/internal/models"
	"github.com/alemancenter/fiber-api/internal/repositories"
	"github.com/alemancenter/fiber-api/internal/utils"
)

type PostService interface {
	List(countryID database.CountryID, filter *models.PostFilter, limit, offset int) ([]models.Post, int64, error)
	GetByID(countryID database.CountryID, id uint64) (*models.Post, error)
	IncrementView(countryID database.CountryID, id uint64) error
	Create(countryID database.CountryID, countryCode string, userID *uint, req *CreatePostRequest, imagePath string) (*models.Post, error)
	Update(countryID database.CountryID, id uint64, req *UpdatePostRequest, callerID uint, callerIsAdmin bool) (*models.Post, error)
	Delete(countryID database.CountryID, id uint64, callerID uint, callerIsAdmin bool) error
}

type CreatePostRequest struct {
	CategoryID      uint   `json:"category_id" form:"category_id" validate:"required"`
	Title           string `json:"title" form:"title" validate:"required,min=3,max=500"`
	Content         string `json:"content" form:"content" validate:"required"`
	Alt             string `json:"alt" form:"alt" validate:"omitempty,max=255"`
	IsActive        bool   `json:"is_active" form:"is_active"`
	IsFeatured      bool   `json:"is_featured" form:"is_featured"`
	Keywords        string `json:"keywords" form:"keywords"`
	MetaDescription string `json:"meta_description" form:"meta_description" validate:"omitempty,max=500"`
}

type UpdatePostRequest struct {
	CategoryID      *uint   `json:"category_id" form:"category_id"`
	Title           string  `json:"title" form:"title"`
	Content         string  `json:"content" form:"content"`
	Alt             string  `json:"alt" form:"alt"`
	IsActive        *bool   `json:"is_active" form:"is_active"`
	IsFeatured      *bool   `json:"is_featured" form:"is_featured"`
	Keywords        string  `json:"keywords" form:"keywords"`
	MetaDescription string  `json:"meta_description" form:"meta_description"`
	ImagePath       *string `json:"image_path" form:"image_path"`
}

type postService struct {
	repo  repositories.PostRepository
	cache CacheService
}

var unsafePostTextBlocks = regexp.MustCompile(`(?is)<\s*(script|style|object|embed|iframe|link|meta)\b[^>]*>.*?<\s*/\s*(script|style|object|embed|iframe|link|meta)\s*>|<\s*(script|style|object|embed|iframe|link|meta)\b[^>]*\/?>`)

func NewPostService(repo repositories.PostRepository, cache CacheService) PostService {
	return &postService{repo: repo, cache: cache}
}

func applyPendingPostViews(countryID database.CountryID, posts []models.Post) []models.Post {
	if len(posts) == 0 {
		return posts
	}
	ids := make([]uint64, 0, len(posts))
	for _, post := range posts {
		ids = append(ids, uint64(post.ID))
	}
	pending := ViewCounter.PendingViews(countryID, "posts", ids)
	if len(pending) == 0 {
		return posts
	}
	for i := range posts {
		if extra := pending[uint64(posts[i].ID)]; extra > 0 {
			posts[i].Views += int(extra)
		}
	}
	return posts
}

func (s *postService) List(countryID database.CountryID, filter *models.PostFilter, limit, offset int) ([]models.Post, int64, error) {
	cacheKey := utils.CacheKey("posts:list", countryID, limit, offset, filter)

	var cached struct {
		Posts []models.Post `json:"posts"`
		Total int64         `json:"total"`
	}

	if s.cache != nil && s.cache.Get(cacheKey, &cached) {
		return applyPendingPostViews(countryID, cached.Posts), cached.Total, nil
	}

	posts, total, err := s.repo.ListPaginated(countryID, filter, limit, offset)
	if err != nil {
		return nil, 0, MapError(err)
	}

	if s.cache != nil {
		_ = s.cache.Set(cacheKey, struct {
			Posts []models.Post `json:"posts"`
			Total int64         `json:"total"`
		}{
			Posts: posts,
			Total: total,
		}, 5*time.Minute)
	}

	return applyPendingPostViews(countryID, posts), total, nil
}

func (s *postService) GetByID(countryID database.CountryID, id uint64) (*models.Post, error) {
	post, err := s.repo.FindByID(countryID, id)
	return post, MapError(err)
}

func (s *postService) IncrementView(countryID database.CountryID, id uint64) error {
	return ViewCounter.IncrementPostView(countryID, id)
}

func (s *postService) uniqueSlug(countryID database.CountryID, base string, excludeID uint64) string {
	candidate := base
	for i := 1; ; i++ {
		if !s.repo.ExistsBySlug(countryID, candidate, excludeID) {
			return candidate
		}
		candidate = utils.NumberedSlug(base, i)
	}
}

func sanitizePostPlainText(value string) string {
	return utils.SanitizeInput(unsafePostTextBlocks.ReplaceAllString(value, ""))
}

func sanitizedOptionalText(value string) *string {
	sanitized := sanitizePostPlainText(value)
	if sanitized == "" {
		return nil
	}
	return &sanitized
}

func (s *postService) Create(countryID database.CountryID, countryCode string, userID *uint, req *CreatePostRequest, imagePath string) (*models.Post, error) {
	title := sanitizePostPlainText(req.Title)
	slug := s.uniqueSlug(countryID, utils.GenerateSlug(title), 0)
	post := &models.Post{
		Title:      title,
		Content:    utils.StripBlockedLinks(utils.SanitizeHTML(req.Content)),
		Slug:       slug,
		IsActive:   req.IsActive,
		IsFeatured: req.IsFeatured,
		Country:    countryCode,
	}

	if req.CategoryID > 0 {
		post.CategoryID = &req.CategoryID
	}
	if sanitized := sanitizedOptionalText(req.Alt); sanitized != nil {
		post.Alt = sanitized
	}
	if sanitized := sanitizedOptionalText(req.Keywords); sanitized != nil {
		post.Keywords = sanitized
	}
	if sanitized := sanitizedOptionalText(req.MetaDescription); sanitized != nil {
		post.MetaDescription = sanitized
	}
	if userID != nil {
		post.AuthorID = userID
	}
	post.Image = &imagePath

	if err := s.repo.Create(countryID, post); err != nil {
		return nil, MapError(err)
	}

	if req.Keywords != "" {
		if err := s.repo.UpdateKeywords(countryID, uint64(post.ID), req.Keywords); err != nil {
			fmt.Printf("failed to update keywords for post %d: %v\n", post.ID, err)
		}
	}

	if s.cache != nil {
		_ = s.cache.DeletePattern("posts:list:*")
	}

	return post, nil
}

func (s *postService) Update(countryID database.CountryID, id uint64, req *UpdatePostRequest, callerID uint, callerIsAdmin bool) (*models.Post, error) {
	post, err := s.repo.FindByID(countryID, id)
	if err != nil {
		return nil, MapError(err)
	}

	if !callerIsAdmin && callerID > 0 && post.AuthorID != nil && *post.AuthorID != callerID {
		return nil, ErrForbidden
	}

	if req.CategoryID != nil {
		post.CategoryID = req.CategoryID
	}
	if req.Title != "" {
		title := sanitizePostPlainText(req.Title)
		post.Title = title
		post.Slug = s.uniqueSlug(countryID, utils.GenerateSlug(title), id)
	}
	if req.Content != "" {
		post.Content = utils.StripBlockedLinks(utils.SanitizeHTML(req.Content))
	}
	if req.IsActive != nil {
		post.IsActive = *req.IsActive
	}
	if req.IsFeatured != nil {
		post.IsFeatured = *req.IsFeatured
	}
	if sanitized := sanitizedOptionalText(req.Alt); sanitized != nil {
		post.Alt = sanitized
	}
	if sanitized := sanitizedOptionalText(req.Keywords); sanitized != nil {
		post.Keywords = sanitized
	}
	if sanitized := sanitizedOptionalText(req.MetaDescription); sanitized != nil {
		post.MetaDescription = sanitized
	}
	if req.ImagePath != nil {
		post.Image = sanitizedOptionalText(*req.ImagePath)
	}

	if err := s.repo.Update(countryID, post); err != nil {
		return nil, MapError(err)
	}

	if req.Keywords != "" {
		if err := s.repo.UpdateKeywords(countryID, id, req.Keywords); err != nil {
			fmt.Printf("failed to update keywords for post %d: %v\n", id, err)
		}
	}

	if s.cache != nil {
		_ = s.cache.DeletePattern("posts:list:*")
	}

	return post, nil
}

func (s *postService) Delete(countryID database.CountryID, id uint64, callerID uint, callerIsAdmin bool) error {
	post, err := s.repo.FindByID(countryID, id)
	if err != nil {
		return MapError(err)
	}

	if !callerIsAdmin && callerID > 0 && post.AuthorID != nil && *post.AuthorID != callerID {
		return ErrForbidden
	}

	err = s.repo.Delete(countryID, id)
	if err == nil && s.cache != nil {
		_ = s.cache.DeletePattern("posts:list:*")
	}
	return MapError(err)
}
