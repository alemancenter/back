package repositories

import (
	"github.com/alemancenter/fiber-api/internal/database"
	"github.com/alemancenter/fiber-api/internal/models"
	"gorm.io/gorm"
)

// KeywordDTO represents a keyword with its item count
type KeywordDTO struct {
	ID         uint   `json:"id"`
	Keyword    string `json:"keyword"`
	ItemsCount int64  `json:"items_count"`
}

type KeywordRepository interface {
	FindByKeyword(countryID database.CountryID, keyword string) (*models.Keyword, error)
	ListKeywords(countryID database.CountryID, keywordType, search string, limit, offset int) ([]KeywordDTO, int64, error)
	ListArticlesByKeyword(countryID database.CountryID, keywordID uint, search, sort string, limit, offset int) ([]models.Article, int64, error)
	ListPostsByKeyword(countryID database.CountryID, keywordID uint, search, sort string, limit, offset int) ([]models.Post, int64, error)
}

type keywordRepository struct{}

func NewKeywordRepository() KeywordRepository {
	return &keywordRepository{}
}

func (r *keywordRepository) getDB(countryID database.CountryID) *gorm.DB {
	return database.DBForCountry(countryID)
}

func (r *keywordRepository) FindByKeyword(countryID database.CountryID, keyword string) (*models.Keyword, error) {
	var k models.Keyword
	err := r.getDB(countryID).Where("keyword = ?", keyword).First(&k).Error
	return &k, err
}

func (r *keywordRepository) ListKeywords(countryID database.CountryID, keywordType, search string, limit, offset int) ([]KeywordDTO, int64, error) {
	db := r.getDB(countryID).Model(&models.Keyword{})

	if search != "" {
		db = db.Where("keywords.keyword LIKE ?", "%"+search+"%")
	}

	var selectQuery string
	switch keywordType {
	case "article", "articles":
		selectQuery = "keywords.id, keywords.keyword, COUNT(articles.id) as items_count"
		db = db.Joins("JOIN article_keyword ON article_keyword.keyword_id = keywords.id").
			Joins("JOIN articles ON articles.id = article_keyword.article_id").
			Where("articles.status = 1").
			Group("keywords.id")
	case "post", "posts":
		selectQuery = "keywords.id, keywords.keyword, COUNT(posts.id) as items_count"
		db = db.Joins("JOIN post_keyword ON post_keyword.keyword_id = keywords.id").
			Joins("JOIN posts ON posts.id = post_keyword.post_id").
			Where("posts.is_active = ?", true).
			Group("keywords.id")
	default:
		// Both - just a basic count of any usage
		selectQuery = "keywords.id, keywords.keyword, (SELECT COUNT(*) FROM article_keyword WHERE article_keyword.keyword_id = keywords.id) + (SELECT COUNT(*) FROM post_keyword WHERE post_keyword.keyword_id = keywords.id) as items_count"
		db = db.Where("EXISTS (SELECT 1 FROM article_keyword WHERE article_keyword.keyword_id = keywords.id) OR EXISTS (SELECT 1 FROM post_keyword WHERE post_keyword.keyword_id = keywords.id)")
	}

	var total int64
	// count query needs to handle group by
	countDB := db.Session(&gorm.Session{})
	if keywordType == "article" || keywordType == "articles" || keywordType == "post" || keywordType == "posts" {
		err := countDB.Select("COUNT(DISTINCT keywords.id)").Count(&total).Error
		if err != nil {
			return nil, 0, err
		}
	} else {
		err := countDB.Count(&total).Error
		if err != nil {
			return nil, 0, err
		}
	}

	var keywords []KeywordDTO
	err := db.Select(selectQuery).Order("keywords.keyword ASC").Limit(limit).Offset(offset).Find(&keywords).Error

	return keywords, total, err
}

func (r *keywordRepository) ListArticlesByKeyword(countryID database.CountryID, keywordID uint, search, sort string, limit, offset int) ([]models.Article, int64, error) {
	db := r.getDB(countryID).Model(&models.Article{}).
		Joins("JOIN article_keyword ON article_keyword.article_id = articles.id").
		Where("article_keyword.keyword_id = ?", keywordID).
		Where("articles.status = 1")

	if search != "" {
		db = db.Where("(articles.title LIKE ? OR articles.content LIKE ?)", "%"+search+"%", "%"+search+"%")
	}

	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	if sort == "title" {
		db = db.Order("articles.title ASC")
	} else {
		db = db.Order("articles.id DESC")
	}

	var articles []models.Article
	err := db.Limit(limit).Offset(offset).Preload("Subject").Preload("Semester").Preload("SchoolClass").Preload("Files").Find(&articles).Error

	return articles, total, err
}

func (r *keywordRepository) ListPostsByKeyword(countryID database.CountryID, keywordID uint, search, sort string, limit, offset int) ([]models.Post, int64, error) {
	db := r.getDB(countryID).Model(&models.Post{}).
		Joins("JOIN post_keyword ON post_keyword.post_id = posts.id").
		Where("post_keyword.keyword_id = ?", keywordID).
		Where("posts.is_active = ?", true)

	if search != "" {
		db = db.Where("(posts.title LIKE ? OR posts.content LIKE ? OR posts.meta_description LIKE ?)", "%"+search+"%", "%"+search+"%", "%"+search+"%")
	}

	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	if sort == "title" {
		db = db.Order("posts.title ASC")
	} else {
		db = db.Order("posts.id DESC")
	}

	var posts []models.Post
	err := db.Limit(limit).Offset(offset).Preload("Category").Find(&posts).Error

	return posts, total, err
}
