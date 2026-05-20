package repositories

import (
	"github.com/alemancenter/fiber-api/internal/database"
	"github.com/alemancenter/fiber-api/internal/models"
	"github.com/alemancenter/fiber-api/internal/utils"
	"gorm.io/gorm"
)

type PostRepository interface {
	ListPaginated(countryID database.CountryID, filter *models.PostFilter, limit, offset int) ([]models.Post, int64, error)
	FindByID(countryID database.CountryID, id uint64) (*models.Post, error)
	ExistsBySlug(countryID database.CountryID, slug string, excludeID uint64) bool
	IncrementView(countryID database.CountryID, id uint64) error
	Create(countryID database.CountryID, post *models.Post) error
	Update(countryID database.CountryID, post *models.Post) error
	Delete(countryID database.CountryID, id uint64) error
	UpdateKeywords(countryID database.CountryID, postID uint64, keywordsStr string) error
}

type postRepository struct{}

func NewPostRepository() PostRepository {
	return &postRepository{}
}

func (r *postRepository) getDB(countryID database.CountryID) *gorm.DB {
	return database.DBForCountry(countryID)
}

func hydratePostComputedFields(post *models.Post) {
	if post == nil || post.Image == nil || *post.Image == "" {
		return
	}
	post.ImageURL = *post.Image
}

func (r *postRepository) ListPaginated(countryID database.CountryID, filter *models.PostFilter, limit, offset int) ([]models.Post, int64, error) {
	db := r.getDB(countryID)
	var postList []models.Post
	var total int64

	query := db.Model(&models.Post{}).Preload("Category").Preload("Author")

	if filter != nil {
		if filter.IsActive != nil {
			query = query.Where("is_active = ?", *filter.IsActive)
		}
		if filter.CategoryID != nil {
			query = query.Where("category_id = ?", *filter.CategoryID)
		}
		if filter.Search != "" {
			// Search title only — content is a large TEXT column and scanning it causes
			// a full-table scan even with indexes. Add a FULLTEXT index on content and
			// switch to MATCH() AGAINST() if full-content search is required.
			query = query.Where("title LIKE ?", "%"+filter.Search+"%")
		}
		if filter.Featured == "1" {
			query = query.Where("is_featured = ?", true)
		}
	}

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	err := query.Order("created_at DESC").Limit(limit).Offset(offset).Find(&postList).Error
	if err == nil {
		for i := range postList {
			hydratePostComputedFields(&postList[i])
		}
	}
	return postList, total, err
}

func (r *postRepository) FindByID(countryID database.CountryID, id uint64) (*models.Post, error) {
	db := r.getDB(countryID)
	var post models.Post
	err := db.Preload("Category").Preload("Author").
		Preload("Comments", "status = ?", models.CommentStatusApproved).
		Preload("Comments.User").
		Preload("KeywordsRel").Preload("Files").
		Where("id = ?", id).
		First(&post).Error
	if err != nil {
		return nil, err
	}
	hydratePostComputedFields(&post)
	return &post, nil
}

func (r *postRepository) ExistsBySlug(countryID database.CountryID, slug string, excludeID uint64) bool {
	db := r.getDB(countryID)
	var count int64
	q := db.Model(&models.Post{}).Where("slug = ?", slug)
	if excludeID > 0 {
		q = q.Where("id != ?", excludeID)
	}
	q.Count(&count)
	return count > 0
}

func (r *postRepository) IncrementView(countryID database.CountryID, id uint64) error {
	db := r.getDB(countryID)
	return db.Model(&models.Post{}).Where("id = ?", id).
		UpdateColumn("views", gorm.Expr("views + 1")).Error
}

func (r *postRepository) Create(countryID database.CountryID, post *models.Post) error {
	db := r.getDB(countryID)
	return db.Create(post).Error
}

func (r *postRepository) Update(countryID database.CountryID, post *models.Post) error {
	db := r.getDB(countryID)
	return db.Save(post).Error
}

func (r *postRepository) Delete(countryID database.CountryID, id uint64) error {
	db := r.getDB(countryID)
	return db.Delete(&models.Post{}, id).Error
}

func (r *postRepository) UpdateKeywords(countryID database.CountryID, postID uint64, keywordsStr string) error {
	db := r.getDB(countryID)

	keywordList := utils.SplitKeywords(keywordsStr)
	if len(keywordList) == 0 {
		return db.Model(&models.Post{ID: uint(postID)}).Association("KeywordsRel").Clear()
	}

	var keywords []models.Keyword
	for _, kw := range keywordList {
		var keyword models.Keyword
		if err := db.Where("keyword = ?", kw).FirstOrCreate(&keyword, models.Keyword{Keyword: kw}).Error; err == nil {
			keywords = append(keywords, keyword)
		}
	}

	return db.Model(&models.Post{ID: uint(postID)}).Association("KeywordsRel").Replace(keywords)
}
