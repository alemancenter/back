package articles

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alemancenter/fiber-api/internal/database"
	"github.com/alemancenter/fiber-api/internal/models"
	"github.com/alemancenter/fiber-api/internal/services"
	"github.com/alemancenter/fiber-api/internal/utils"
	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"
)

// MockArticleService is a mock implementation of services.ArticleService
type MockArticleService struct {
	ListFunc                   func(countryID database.CountryID, pag utils.Pagination, filter *models.ArticleFilter) ([]models.Article, int64, error)
	GetByIDFunc                func(countryID database.CountryID, id uint64) (*models.Article, error)
	GetByGradeLevelFunc        func(countryID database.CountryID, gradeLevel string, pag utils.Pagination) ([]models.Article, int64, error)
	GetByKeywordFunc           func(countryID database.CountryID, keyword string, pag utils.Pagination) ([]models.Article, int64, error)
	GetFileForDownloadFunc     func(countryID database.CountryID, id uint64) (*models.File, string, error)
	GetSignedDownloadTokenFunc func(countryID database.CountryID, fileID uint64) (string, error)
	GetFileBySignedTokenFunc   func(token string) (*models.File, string, error)
	GetDashboardCreateDataFunc func(countryID database.CountryID) (*services.ArticleDashboardCreateData, error)
	GetDashboardEditDataFunc   func(countryID database.CountryID, id uint64) (*services.ArticleDashboardEditData, error)
	CreateArticleFunc          func(countryID database.CountryID, req *services.ArticleInput, authorID *uint) (*models.Article, error)
	UpdateArticleFunc          func(countryID database.CountryID, id uint64, req *services.ArticleInput, authorID *uint) (*models.Article, error)
	DeleteArticleFunc          func(countryID database.CountryID, id uint64, authorID *uint) error
	SetArticleStatusFunc       func(countryID database.CountryID, id uint64, status int8) (*models.Article, error)
	GetDashboardStatsFunc      func(countryID database.CountryID) (*services.ArticleDashboardStats, error)
}

func (m *MockArticleService) List(countryID database.CountryID, pag utils.Pagination, filter *models.ArticleFilter) ([]models.Article, int64, error) {
	if m.ListFunc != nil {
		return m.ListFunc(countryID, pag, filter)
	}
	return nil, 0, nil
}
func (m *MockArticleService) GetByID(countryID database.CountryID, id uint64) (*models.Article, error) {
	if m.GetByIDFunc != nil {
		return m.GetByIDFunc(countryID, id)
	}
	return nil, nil
}
func (m *MockArticleService) GetByGradeLevel(countryID database.CountryID, gradeLevel string, pag utils.Pagination) ([]models.Article, int64, error) {
	return nil, 0, nil
}
func (m *MockArticleService) GetByKeyword(countryID database.CountryID, keyword string, pag utils.Pagination) ([]models.Article, int64, error) {
	return nil, 0, nil
}
func (m *MockArticleService) GetFileForDownload(countryID database.CountryID, id uint64) (*models.File, string, error) {
	return nil, "", nil
}
func (m *MockArticleService) GetSignedDownloadToken(countryID database.CountryID, fileID uint64) (string, error) {
	if m.GetSignedDownloadTokenFunc != nil {
		return m.GetSignedDownloadTokenFunc(countryID, fileID)
	}
	return "", nil
}
func (m *MockArticleService) GetFileBySignedToken(token string) (*models.File, string, error) {
	return nil, "", nil
}
func (m *MockArticleService) GetDashboardCreateData(countryID database.CountryID) (*services.ArticleDashboardCreateData, error) {
	return nil, nil
}
func (m *MockArticleService) GetDashboardEditData(countryID database.CountryID, id uint64) (*services.ArticleDashboardEditData, error) {
	return nil, nil
}
func (m *MockArticleService) CreateArticle(countryID database.CountryID, req *services.ArticleInput, authorID *uint) (*models.Article, error) {
	if m.CreateArticleFunc != nil {
		return m.CreateArticleFunc(countryID, req, authorID)
	}
	return nil, nil
}
func (m *MockArticleService) UpdateArticle(countryID database.CountryID, id uint64, req *services.ArticleInput, authorID *uint) (*models.Article, error) {
	if m.UpdateArticleFunc != nil {
		return m.UpdateArticleFunc(countryID, id, req, authorID)
	}
	return nil, nil
}
func (m *MockArticleService) DeleteArticle(countryID database.CountryID, id uint64, authorID *uint) error {
	if m.DeleteArticleFunc != nil {
		return m.DeleteArticleFunc(countryID, id, authorID)
	}
	return nil
}
func (m *MockArticleService) SetArticleStatus(countryID database.CountryID, id uint64, status int8) (*models.Article, error) {
	return nil, nil
}
func (m *MockArticleService) GetDashboardStats(countryID database.CountryID) (*services.ArticleDashboardStats, error) {
	return nil, nil
}

func setupApp() (*fiber.App, *MockArticleService) {
	app := fiber.New()
	mockSvc := &MockArticleService{}
	h := New(mockSvc, nil)

	// Middleware to set country_id
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("country_id", database.CountryJordan)
		return c.Next()
	})

	api := app.Group("/api")
	api.Post("/dashboard/articles", h.DashboardCreate)
	api.Put("/dashboard/articles/:id", h.DashboardUpdate)
	api.Delete("/dashboard/articles/:id", h.DashboardDelete)
	api.Get("/articles/file/:id/download-url", h.GetDownloadToken)

	return app, mockSvc
}

func TestHandler_DashboardCreate(t *testing.T) {
	app, mockSvc := setupApp()

	t.Run("Success", func(t *testing.T) {
		mockSvc.CreateArticleFunc = func(countryID database.CountryID, req *services.ArticleInput, authorID *uint) (*models.Article, error) {
			assert.Equal(t, "Test Article", req.Title)
			return &models.Article{ID: 1, Title: "Test Article"}, nil
		}

		reqBody := services.ArticleInput{
			Title:   "Test Article",
			Content: "<p>Content</p>",
		}
		body, _ := json.Marshal(reqBody)

		req := httptest.NewRequest(http.MethodPost, "/api/dashboard/articles", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")

		resp, err := app.Test(req)
		assert.NoError(t, err)
		assert.Equal(t, http.StatusCreated, resp.StatusCode)
	})

	t.Run("ValidationError", func(t *testing.T) {
		reqBody := services.ArticleInput{
			Title: "", // Empty title to trigger validation error
		}
		body, _ := json.Marshal(reqBody)

		req := httptest.NewRequest(http.MethodPost, "/api/dashboard/articles", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")

		resp, err := app.Test(req)
		assert.NoError(t, err)
		assert.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)
	})
}

func TestHandler_DashboardUpdate(t *testing.T) {
	app, mockSvc := setupApp()

	t.Run("Success", func(t *testing.T) {
		mockSvc.UpdateArticleFunc = func(countryID database.CountryID, id uint64, req *services.ArticleInput, authorID *uint) (*models.Article, error) {
			assert.Equal(t, uint64(1), id)
			assert.Equal(t, "Updated Article", req.Title)
			return &models.Article{ID: 1, Title: "Updated Article"}, nil
		}

		reqBody := services.ArticleInput{
			Title: "Updated Article",
		}
		body, _ := json.Marshal(reqBody)

		req := httptest.NewRequest(http.MethodPut, "/api/dashboard/articles/1", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")

		resp, err := app.Test(req)
		assert.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})
}

func TestHandler_DashboardDelete(t *testing.T) {
	app, mockSvc := setupApp()

	t.Run("Success", func(t *testing.T) {
		mockSvc.DeleteArticleFunc = func(countryID database.CountryID, id uint64, authorID *uint) error {
			assert.Equal(t, uint64(1), id)
			return nil
		}

		req := httptest.NewRequest(http.MethodDelete, "/api/dashboard/articles/1", nil)
		resp, err := app.Test(req)

		assert.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})
}

func TestHandler_GetDownloadToken(t *testing.T) {
	app, mockSvc := setupApp()

	t.Run("Success", func(t *testing.T) {
		mockSvc.GetSignedDownloadTokenFunc = func(countryID database.CountryID, fileID uint64) (string, error) {
			return "fake-jwt-token", nil
		}

		req := httptest.NewRequest(http.MethodGet, "/api/articles/file/1/download-url", nil)
		resp, err := app.Test(req)

		assert.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})
}
