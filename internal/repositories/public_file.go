package repositories

import (
	"github.com/imanjo/fiber-api/internal/models"
	"gorm.io/gorm"
)

// PublicFileByID returns only ordinary attachments whose single parent is published.
// Premium-vault files have a separate subscription authorization path.
func PublicFileByID(db *gorm.DB, id uint64, kind string) (*models.File, error) {
	var file models.File
	if err := db.Preload("Article").Preload("Post").First(&file, id).Error; err != nil {
		return nil, err
	}
	if !IsPublicFile(&file, kind) {
		return nil, gorm.ErrRecordNotFound
	}
	return &file, nil
}

func IsPublicFile(file *models.File, kind string) bool {
	if file == nil || file.IsPremium || file.PremiumRequiresSubscription {
		return false
	}
	if file.ArticleID != nil && file.PostID == nil {
		return kind != "post" && file.Article != nil && file.Article.Status == 1
	}
	if file.PostID != nil && file.ArticleID == nil {
		return kind != "article" && file.Post != nil && file.Post.IsActive
	}
	return false
}
