package repositories

import (
	"github.com/imanjo/fiber-api/internal/models"
	"testing"
)

func TestPublicFilePolicy(t *testing.T) {
	id := uint(1)
	cases := []struct {
		name    string
		file    *models.File
		kind    string
		allowed bool
	}{
		{"nil", nil, "", false},
		{"orphan", &models.File{}, "", false},
		{"draft", &models.File{ArticleID: &id, Article: &models.Article{Status: 0}}, "", false},
		{"published", &models.File{ArticleID: &id, Article: &models.Article{Status: 1}}, "article", true},
		{"wrong kind", &models.File{ArticleID: &id, Article: &models.Article{Status: 1}}, "post", false},
		{"inactive post", &models.File{PostID: &id, Post: &models.Post{IsActive: false}}, "", false},
		{"active post", &models.File{PostID: &id, Post: &models.Post{IsActive: true}}, "post", true},
		{"ambiguous", &models.File{ArticleID: &id, PostID: &id, Article: &models.Article{Status: 1}}, "", false},
		{"premium", &models.File{ArticleID: &id, Article: &models.Article{Status: 1}, IsPremium: true}, "", false},
		{"subscription", &models.File{PostID: &id, Post: &models.Post{IsActive: true}, PremiumRequiresSubscription: true}, "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if IsPublicFile(c.file, c.kind) != c.allowed {
				t.Fatal("unexpected file authorization")
			}
		})
	}
}
