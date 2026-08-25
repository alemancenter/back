package services

import (
	"sync"
	"testing"

	"github.com/imanjo/fiber-api/internal/database"
	"github.com/imanjo/fiber-api/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type sitemapScheduleSpy struct {
	mu    sync.Mutex
	codes []string
}

func (s *sitemapScheduleSpy) GetStatus(string) map[string]SitemapInfo { return nil }
func (s *sitemapScheduleSpy) GenerateAll(string) []error              { return nil }
func (s *sitemapScheduleSpy) Delete(string, string) error             { return nil }
func (s *sitemapScheduleSpy) ScheduleGenerate(code string) {
	s.mu.Lock()
	s.codes = append(s.codes, code)
	s.mu.Unlock()
}

func (s *sitemapScheduleSpy) scheduledCodes() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.codes...)
}

func TestArticleWritesScheduleSitemapRefresh(t *testing.T) {
	repo := &MockArticleRepository{}
	spy := &sitemapScheduleSpy{}
	service := NewArticleService(repo, nil, nil, spy)

	repo.CreateFunc = func(_ database.CountryID, article *models.Article) error {
		article.ID = 7
		return nil
	}
	_, _, err := service.CreateArticle(database.CountrySaudi, &ArticleInput{Title: "Article"}, nil)
	require.NoError(t, err)

	repo.FindByIDFunc = func(_ database.CountryID, _ uint64) (*models.Article, error) {
		return &models.Article{ID: 7, Title: "Article"}, nil
	}
	repo.UpdateFunc = func(_ database.CountryID, _ *models.Article) error { return nil }
	_, _, err = service.UpdateArticle(database.CountrySaudi, 7, &ArticleInput{Title: "Updated article"}, nil)
	require.NoError(t, err)

	assert.Equal(t, []string{"sa", "sa"}, spy.scheduledCodes())
}

func TestPostWritesScheduleSitemapRefresh(t *testing.T) {
	repo := &MockPostRepository{}
	spy := &sitemapScheduleSpy{}
	service := NewPostService(repo, nil, nil, spy)

	repo.ExistsBySlugFunc = func(database.CountryID, string, uint64) bool { return false }
	repo.CreateFunc = func(_ database.CountryID, post *models.Post) error {
		post.ID = 9
		return nil
	}
	_, _, err := service.Create(database.CountryEgypt, "eg", nil, &CreatePostRequest{Title: "Post", Content: "Content"}, "")
	require.NoError(t, err)

	repo.FindByIDFunc = func(_ database.CountryID, _ uint64) (*models.Post, error) {
		return &models.Post{ID: 9, Title: "Post"}, nil
	}
	repo.UpdateFunc = func(_ database.CountryID, _ *models.Post) error { return nil }
	_, _, err = service.Update(database.CountryEgypt, 9, &UpdatePostRequest{Title: "Updated post"}, 1, true)
	require.NoError(t, err)

	assert.Equal(t, []string{"eg", "eg"}, spy.scheduledCodes())
}
