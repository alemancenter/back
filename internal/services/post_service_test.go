package services

import (
	"errors"
	"testing"

	"github.com/alemancenter/fiber-api/internal/database"
	"github.com/alemancenter/fiber-api/internal/models"
	"github.com/alemancenter/fiber-api/internal/repositories"
	"github.com/alemancenter/fiber-api/internal/utils"
	"github.com/stretchr/testify/assert"
	"gorm.io/gorm"
)

// MockPostRepository is a mock implementation of repositories.PostRepository
type MockPostRepository struct {
	repositories.PostRepository // embed to satisfy interface

	ListPaginatedFunc func(countryID database.CountryID, filter *models.PostFilter, limit, offset int) ([]models.Post, int64, error)
	FindByIDFunc      func(countryID database.CountryID, id uint64) (*models.Post, error)
	ExistsBySlugFunc  func(countryID database.CountryID, slug string, excludeID uint64) bool
	IncrementViewFunc func(countryID database.CountryID, id uint64) error
	CreateFunc        func(countryID database.CountryID, post *models.Post) error
	UpdateFunc        func(countryID database.CountryID, post *models.Post) error
	DeleteFunc        func(countryID database.CountryID, id uint64) error
}

func (m *MockPostRepository) ListPaginated(countryID database.CountryID, filter *models.PostFilter, limit, offset int) ([]models.Post, int64, error) {
	if m.ListPaginatedFunc != nil {
		return m.ListPaginatedFunc(countryID, filter, limit, offset)
	}
	return nil, 0, nil
}

func (m *MockPostRepository) FindByID(countryID database.CountryID, id uint64) (*models.Post, error) {
	if m.FindByIDFunc != nil {
		return m.FindByIDFunc(countryID, id)
	}
	return nil, gorm.ErrRecordNotFound
}

func (m *MockPostRepository) ExistsBySlug(countryID database.CountryID, slug string, excludeID uint64) bool {
	if m.ExistsBySlugFunc != nil {
		return m.ExistsBySlugFunc(countryID, slug, excludeID)
	}
	return false
}

func (m *MockPostRepository) IncrementView(countryID database.CountryID, id uint64) error {
	if m.IncrementViewFunc != nil {
		return m.IncrementViewFunc(countryID, id)
	}
	return nil
}

func (m *MockPostRepository) Create(countryID database.CountryID, post *models.Post) error {
	if m.CreateFunc != nil {
		return m.CreateFunc(countryID, post)
	}
	return nil
}

func (m *MockPostRepository) Update(countryID database.CountryID, post *models.Post) error {
	if m.UpdateFunc != nil {
		return m.UpdateFunc(countryID, post)
	}
	return nil
}

func (m *MockPostRepository) Delete(countryID database.CountryID, id uint64) error {
	if m.DeleteFunc != nil {
		return m.DeleteFunc(countryID, id)
	}
	return nil
}

func TestPostService_GetByID(t *testing.T) {
	t.Setenv("JWT_SECRET", "test_secret_key_12345678901234567890")
	t.Setenv("DB_HOST_JO", "localhost")
	t.Setenv("DB_NAME_JO", "test_db")
	t.Setenv("DB_USER_JO", "root")
	t.Setenv("APP_URL", "http://localhost")
	t.Setenv("FRONTEND_URL", "http://localhost:3000")

	mockRepo := &MockPostRepository{}
	svc := NewPostService(mockRepo, nil)

	t.Run("Success", func(t *testing.T) {
		expectedPost := &models.Post{
			Title: "Test Post",
		}
		expectedPost.ID = 1

		mockRepo.FindByIDFunc = func(countryID database.CountryID, id uint64) (*models.Post, error) {
			assert.Equal(t, uint64(1), id)
			return expectedPost, nil
		}

		post, err := svc.GetByID(database.CountryJordan, 1)

		assert.NoError(t, err)
		assert.NotNil(t, post)
		assert.Equal(t, expectedPost.Title, post.Title)
	})

	t.Run("NotFound", func(t *testing.T) {
		mockRepo.FindByIDFunc = func(countryID database.CountryID, id uint64) (*models.Post, error) {
			return nil, gorm.ErrRecordNotFound
		}

		post, err := svc.GetByID(database.CountryJordan, 999)

		assert.Error(t, err)
		assert.Equal(t, ErrNotFound, err)
		assert.Nil(t, post)
	})
}

func TestPostService_Create(t *testing.T) {
	t.Setenv("JWT_SECRET", "test_secret_key_12345678901234567890")
	t.Setenv("DB_HOST_JO", "localhost")
	t.Setenv("DB_NAME_JO", "test_db")
	t.Setenv("DB_USER_JO", "root")
	t.Setenv("APP_URL", "http://localhost")
	t.Setenv("FRONTEND_URL", "http://localhost:3000")

	mockRepo := &MockPostRepository{}
	svc := NewPostService(mockRepo, nil)

	t.Run("Success", func(t *testing.T) {
		req := &CreatePostRequest{
			Title:   "New Post",
			Content: "Post Content",
		}
		var authorID uint = 10

		mockRepo.ExistsBySlugFunc = func(countryID database.CountryID, slug string, excludeID uint64) bool {
			return false
		}

		mockRepo.CreateFunc = func(countryID database.CountryID, post *models.Post) error {
			assert.Equal(t, "New Post", post.Title)
			assert.Equal(t, "Post Content", post.Content)
			assert.Equal(t, utils.GenerateSlug("New Post"), post.Slug)
			assert.NotNil(t, post.Image)
			assert.Equal(t, "", *post.Image)
			post.ID = 5 // Simulate DB setting ID
			return nil
		}

		post, err := svc.Create(database.CountryJordan, "jo", &authorID, req, "")

		assert.NoError(t, err)
		assert.NotNil(t, post)
		assert.Equal(t, uint(5), post.ID)
		assert.Equal(t, "jo", post.Country)
	})

	t.Run("SanitizesPostFields", func(t *testing.T) {
		req := &CreatePostRequest{
			Title:           "<b>Safe Title</b><script>alert(1)</script>",
			Content:         `<p onclick="evil()">Hello</p><script>alert(1)</script><img src="javascript:alert(1)" onerror="bad()">`,
			Alt:             `<img onerror="bad()">Alt`,
			Keywords:        `math,<script>alert(1)</script>science`,
			MetaDescription: `<b>Useful description</b><script>alert(1)</script>`,
		}

		mockRepo.ExistsBySlugFunc = func(countryID database.CountryID, slug string, excludeID uint64) bool {
			return false
		}

		mockRepo.CreateFunc = func(countryID database.CountryID, post *models.Post) error {
			assert.NotContains(t, post.Title, "<")
			assert.NotContains(t, post.Title, ">")
			assert.NotContains(t, post.Title, "script")
			assert.NotContains(t, post.Title, "alert")
			assert.Equal(t, utils.GenerateSlug(post.Title), post.Slug)

			assert.NotContains(t, post.Content, "<script")
			assert.NotContains(t, post.Content, "onclick")
			assert.NotContains(t, post.Content, "onerror")
			assert.NotContains(t, post.Content, "javascript:")

			assert.NotNil(t, post.Alt)
			assert.NotContains(t, *post.Alt, "<")
			assert.NotContains(t, *post.Alt, "onerror")

			assert.NotNil(t, post.Keywords)
			assert.NotContains(t, *post.Keywords, "<")
			assert.NotContains(t, *post.Keywords, "alert")

			assert.NotNil(t, post.MetaDescription)
			assert.NotContains(t, *post.MetaDescription, "<")
			assert.NotContains(t, *post.MetaDescription, "alert")
			return nil
		}

		post, err := svc.Create(database.CountryJordan, "jo", nil, req, "")

		assert.NoError(t, err)
		assert.NotNil(t, post)
	})

	t.Run("DatabaseError", func(t *testing.T) {
		req := &CreatePostRequest{
			Title: "New Post",
		}

		expectedErr := errors.New("db connection error")
		mockRepo.CreateFunc = func(countryID database.CountryID, post *models.Post) error {
			return expectedErr
		}

		post, err := svc.Create(database.CountryJordan, "jo", nil, req, "")

		assert.Error(t, err)
		assert.Nil(t, post)
		assert.Equal(t, expectedErr, err) // because it's not a standard gorm err, MapError returns it as is
	})
}

func TestPostService_Update_SanitizesPostFields(t *testing.T) {
	t.Setenv("JWT_SECRET", "test_secret_key_12345678901234567890")
	t.Setenv("DB_HOST_JO", "localhost")
	t.Setenv("DB_NAME_JO", "test_db")
	t.Setenv("DB_USER_JO", "root")
	t.Setenv("APP_URL", "http://localhost")
	t.Setenv("FRONTEND_URL", "http://localhost:3000")

	mockRepo := &MockPostRepository{}
	svc := NewPostService(mockRepo, nil)
	imagePath := `<script>bad</script>posts/image.png`

	mockRepo.FindByIDFunc = func(countryID database.CountryID, id uint64) (*models.Post, error) {
		assert.Equal(t, uint64(1), id)
		return &models.Post{ID: 1, Title: "Old Post", Content: "Old Content"}, nil
	}
	mockRepo.ExistsBySlugFunc = func(countryID database.CountryID, slug string, excludeID uint64) bool {
		assert.Equal(t, uint64(1), excludeID)
		return false
	}
	mockRepo.UpdateFunc = func(countryID database.CountryID, post *models.Post) error {
		assert.NotContains(t, post.Title, "<")
		assert.NotContains(t, post.Title, ">")
		assert.NotContains(t, post.Title, "script")
		assert.NotContains(t, post.Title, "alert")
		assert.Equal(t, utils.GenerateSlug(post.Title), post.Slug)

		assert.NotContains(t, post.Content, "<script")
		assert.NotContains(t, post.Content, "onclick")
		assert.NotContains(t, post.Content, "onerror")
		assert.NotContains(t, post.Content, "javascript:")

		assert.NotNil(t, post.Alt)
		assert.NotContains(t, *post.Alt, "<")

		assert.NotNil(t, post.Keywords)
		assert.NotContains(t, *post.Keywords, "<")
		assert.NotContains(t, *post.Keywords, "alert")

		assert.NotNil(t, post.MetaDescription)
		assert.NotContains(t, *post.MetaDescription, "<")
		assert.NotContains(t, *post.MetaDescription, "alert")

		assert.NotNil(t, post.Image)
		assert.NotContains(t, *post.Image, "<")
		assert.NotContains(t, *post.Image, "bad")
		return nil
	}

	post, err := svc.Update(database.CountryJordan, 1, &UpdatePostRequest{
		Title:           "<b>Updated Title</b><script>alert(1)</script>",
		Content:         `<p onclick="evil()">Updated</p><script>alert(1)</script><img src="javascript:alert(1)" onerror="bad()">`,
		Alt:             "<b>Safe Alt</b>",
		Keywords:        "news,<script>alert(1)</script>education",
		MetaDescription: "<i>Updated description</i><script>alert(1)</script>",
		ImagePath:       &imagePath,
	}, 10, true)

	assert.NoError(t, err)
	assert.NotNil(t, post)
}
