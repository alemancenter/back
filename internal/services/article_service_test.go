package services

import (
	"errors"
	"testing"

	"github.com/imanjo/fiber-api/internal/database"
	"github.com/imanjo/fiber-api/internal/models"
	"github.com/imanjo/fiber-api/internal/repositories"
	"github.com/imanjo/fiber-api/internal/utils"
	"github.com/stretchr/testify/assert"
	"gorm.io/gorm"
)

// MockArticleRepository is a mock implementation of repositories.ArticleRepository
type MockArticleRepository struct {
	repositories.ArticleRepository // embed to satisfy interface

	ListFunc                 func(countryID database.CountryID, pag utils.Pagination, filters *models.ArticleFilter) ([]models.Article, int64, error)
	FindByIDFunc             func(countryID database.CountryID, id uint64) (*models.Article, error)
	FindByIDWithCommentsFunc func(countryID database.CountryID, id uint64) (*models.Article, error)
	IncrementViewFunc        func(countryID database.CountryID, id uint64) error
	CreateFunc               func(countryID database.CountryID, article *models.Article) error
	UpdateFunc               func(countryID database.CountryID, article *models.Article) error
	DeleteFunc               func(countryID database.CountryID, article *models.Article) error
	GetFileByIDFunc          func(countryID database.CountryID, fileID uint64) (*models.File, error)
	GetClassesFunc           func(countryID database.CountryID) ([]models.SchoolClass, error)
	GetAllSubjectsFunc       func(countryID database.CountryID) ([]models.Subject, error)
	GetAllSemestersFunc      func(countryID database.CountryID) ([]models.Semester, error)
	UpdateKeywordsFunc       func(countryID database.CountryID, articleID uint, keywordsStr string) error
}

func (m *MockArticleRepository) List(countryID database.CountryID, pag utils.Pagination, filters *models.ArticleFilter) ([]models.Article, int64, error) {
	if m.ListFunc != nil {
		return m.ListFunc(countryID, pag, filters)
	}
	return nil, 0, nil
}

func (m *MockArticleRepository) FindByID(countryID database.CountryID, id uint64) (*models.Article, error) {
	if m.FindByIDFunc != nil {
		return m.FindByIDFunc(countryID, id)
	}
	return nil, nil
}

func (m *MockArticleRepository) FindByIDWithComments(countryID database.CountryID, id uint64) (*models.Article, error) {
	if m.FindByIDWithCommentsFunc != nil {
		return m.FindByIDWithCommentsFunc(countryID, id)
	}
	if m.FindByIDFunc != nil {
		return m.FindByIDFunc(countryID, id)
	}
	return nil, gorm.ErrRecordNotFound
}

func (m *MockArticleRepository) IncrementViewCount(countryID database.CountryID, id uint64) error {
	if m.IncrementViewFunc != nil {
		return m.IncrementViewFunc(countryID, id)
	}
	return nil
}

func (m *MockArticleRepository) Create(countryID database.CountryID, article *models.Article) error {
	if m.CreateFunc != nil {
		return m.CreateFunc(countryID, article)
	}
	return nil
}

func (m *MockArticleRepository) Update(countryID database.CountryID, article *models.Article) error {
	if m.UpdateFunc != nil {
		return m.UpdateFunc(countryID, article)
	}
	return nil
}

func (m *MockArticleRepository) Delete(countryID database.CountryID, article *models.Article) error {
	if m.DeleteFunc != nil {
		return m.DeleteFunc(countryID, article)
	}
	return nil
}

func (m *MockArticleRepository) UpdateKeywords(countryID database.CountryID, articleID uint, keywordsStr string) error {
	if m.UpdateKeywordsFunc != nil {
		return m.UpdateKeywordsFunc(countryID, articleID, keywordsStr)
	}
	return nil
}

func (m *MockArticleRepository) GetFileByID(countryID database.CountryID, fileID uint64) (*models.File, error) {
	if m.GetFileByIDFunc != nil {
		return m.GetFileByIDFunc(countryID, fileID)
	}
	return nil, gorm.ErrRecordNotFound
}

func (m *MockArticleRepository) GetClasses(countryID database.CountryID) ([]models.SchoolClass, error) {
	if m.GetClassesFunc != nil {
		return m.GetClassesFunc(countryID)
	}
	return nil, nil
}

func (m *MockArticleRepository) GetAllSubjects(countryID database.CountryID) ([]models.Subject, error) {
	if m.GetAllSubjectsFunc != nil {
		return m.GetAllSubjectsFunc(countryID)
	}
	return nil, nil
}

func (m *MockArticleRepository) GetAllSemesters(countryID database.CountryID) ([]models.Semester, error) {
	if m.GetAllSemestersFunc != nil {
		return m.GetAllSemestersFunc(countryID)
	}
	return nil, nil
}

func TestArticleService_GetByID(t *testing.T) {
	t.Setenv("JWT_SECRET", "test_secret_key_12345678901234567890")
	t.Setenv("DB_HOST_JO", "localhost")
	t.Setenv("DB_NAME_JO", "test_db")
	t.Setenv("DB_USER_JO", "root")
	t.Setenv("APP_URL", "http://localhost")
	t.Setenv("FRONTEND_URL", "http://localhost:3000")

	mockRepo := &MockArticleRepository{}
	// FileService can be nil for this test as we don't use it in GetByID
	svc := NewArticleService(mockRepo, nil, nil)

	t.Run("Success", func(t *testing.T) {
		expectedArticle := &models.Article{
			Title: "Test Article",
		}
		expectedArticle.ID = 1

		mockRepo.FindByIDFunc = func(countryID database.CountryID, id uint64) (*models.Article, error) {
			assert.Equal(t, uint64(1), id)
			return expectedArticle, nil
		}

		// We just mock IncrementViewCount so it doesn't panic
		mockRepo.IncrementViewFunc = func(countryID database.CountryID, id uint64) error {
			return nil
		}

		article, err := svc.GetByID(database.CountryJordan, 1)

		assert.NoError(t, err)
		assert.NotNil(t, article)
		assert.Equal(t, expectedArticle.Title, article.Title)
	})

	t.Run("NotFound", func(t *testing.T) {
		mockRepo.FindByIDFunc = func(countryID database.CountryID, id uint64) (*models.Article, error) {
			return nil, gorm.ErrRecordNotFound
		}

		article, err := svc.GetByID(database.CountryJordan, 999)

		assert.Error(t, err)
		assert.Equal(t, gorm.ErrRecordNotFound, err)
		assert.Nil(t, article)
	})
}

func TestArticleService_UpdateArticle(t *testing.T) {
	t.Setenv("JWT_SECRET", "test_secret_key_12345678901234567890")
	t.Setenv("DB_HOST_JO", "localhost")
	t.Setenv("DB_NAME_JO", "test_db")
	t.Setenv("DB_USER_JO", "root")
	t.Setenv("APP_URL", "http://localhost")
	t.Setenv("FRONTEND_URL", "http://localhost:3000")

	mockRepo := &MockArticleRepository{}
	svc := NewArticleService(mockRepo, nil, nil)

	t.Run("Success", func(t *testing.T) {
		existingArticle := &models.Article{
			ID:    1,
			Title: "Old Title",
		}

		mockRepo.FindByIDFunc = func(countryID database.CountryID, id uint64) (*models.Article, error) {
			return existingArticle, nil
		}

		updateReq := &ArticleInput{
			Title: "Updated Title",
		}

		mockRepo.UpdateFunc = func(countryID database.CountryID, article *models.Article) error {
			assert.Equal(t, "Updated Title", article.Title)
			return nil
		}

		originalLogActivity := LogActivity
		defer func() { LogActivity = originalLogActivity }()
		LogActivity = func(action string, entityType string, entityID uint, userID uint) {}

		var authorID uint = 10
		article, err := svc.UpdateArticle(database.CountryJordan, 1, updateReq, &authorID)

		assert.NoError(t, err)
		assert.NotNil(t, article)
		assert.Equal(t, "Updated Title", article.Title)
	})

	t.Run("NotFound", func(t *testing.T) {
		mockRepo.FindByIDFunc = func(countryID database.CountryID, id uint64) (*models.Article, error) {
			return nil, gorm.ErrRecordNotFound
		}

		updateReq := &ArticleInput{Title: "Updated Title"}
		article, err := svc.UpdateArticle(database.CountryJordan, 99, updateReq, nil)

		assert.Error(t, err)
		assert.Equal(t, ErrNotFound, err)
		assert.Nil(t, article)
	})

}

func TestArticleService_DeleteArticle(t *testing.T) {
	t.Setenv("JWT_SECRET", "test_secret_key_12345678901234567890")
	t.Setenv("DB_HOST_JO", "localhost")
	t.Setenv("DB_NAME_JO", "test_db")
	t.Setenv("DB_USER_JO", "root")
	t.Setenv("APP_URL", "http://localhost")
	t.Setenv("FRONTEND_URL", "http://localhost:3000")

	mockRepo := &MockArticleRepository{}
	svc := NewArticleService(mockRepo, nil, nil)

	t.Run("Success", func(t *testing.T) {
		existingArticle := &models.Article{
			ID:    1,
			Title: "To Delete",
		}

		mockRepo.FindByIDFunc = func(countryID database.CountryID, id uint64) (*models.Article, error) {
			return existingArticle, nil
		}

		mockRepo.DeleteFunc = func(countryID database.CountryID, article *models.Article) error {
			assert.Equal(t, uint(1), article.ID)
			return nil
		}

		originalLogActivity := LogActivity
		defer func() { LogActivity = originalLogActivity }()
		LogActivity = func(action string, entityType string, entityID uint, userID uint) {}

		var authorID uint = 10
		err := svc.DeleteArticle(database.CountryJordan, 1, &authorID)

		assert.NoError(t, err)
	})

	t.Run("NotFound", func(t *testing.T) {
		mockRepo.FindByIDFunc = func(countryID database.CountryID, id uint64) (*models.Article, error) {
			return nil, gorm.ErrRecordNotFound
		}

		err := svc.DeleteArticle(database.CountryJordan, 99, nil)

		assert.Error(t, err)
		assert.Equal(t, ErrNotFound, err)
	})
}

func TestArticleService_GetSignedDownloadToken(t *testing.T) {
	t.Setenv("JWT_SECRET", "test_secret_key_12345678901234567890")
	t.Setenv("DB_HOST_JO", "localhost")
	t.Setenv("DB_NAME_JO", "test_db")
	t.Setenv("DB_USER_JO", "root")
	t.Setenv("APP_URL", "http://localhost")
	t.Setenv("FRONTEND_URL", "http://localhost:3000")

	mockRepo := &MockArticleRepository{}
	svc := NewArticleService(mockRepo, nil, nil)

	t.Run("Success", func(t *testing.T) {
		mockRepo.GetFileByIDFunc = func(countryID database.CountryID, fileID uint64) (*models.File, error) {
			return &models.File{ID: 1, FilePath: "/test.pdf"}, nil
		}

		token, err := svc.GetSignedDownloadToken(database.CountryJordan, 1)

		assert.NoError(t, err)
		assert.NotEmpty(t, token)
	})

	t.Run("FileNotFound", func(t *testing.T) {
		mockRepo.GetFileByIDFunc = func(countryID database.CountryID, fileID uint64) (*models.File, error) {
			return nil, gorm.ErrRecordNotFound
		}

		token, err := svc.GetSignedDownloadToken(database.CountryJordan, 99)

		assert.Error(t, err)
		assert.Equal(t, ErrNotFound, err)
		assert.Empty(t, token)
	})
}

func TestArticleService_CreateArticle(t *testing.T) {
	t.Setenv("JWT_SECRET", "test_secret_key_12345678901234567890")
	t.Setenv("DB_HOST_JO", "localhost")
	t.Setenv("DB_NAME_JO", "test_db")
	t.Setenv("DB_USER_JO", "root")
	t.Setenv("APP_URL", "http://localhost")
	t.Setenv("FRONTEND_URL", "http://localhost:3000")

	mockRepo := &MockArticleRepository{}
	svc := NewArticleService(mockRepo, nil, nil)

	t.Run("Success", func(t *testing.T) {
		newArticle := &ArticleInput{
			Title: "New Article",
		}
		var authorID uint = 10

		mockRepo.CreateFunc = func(countryID database.CountryID, article *models.Article) error {
			assert.Equal(t, "New Article", article.Title)
			article.ID = 5 // Simulate DB setting ID
			return nil
		}

		// Save the original logger to restore later
		originalLogActivity := LogActivity
		defer func() { LogActivity = originalLogActivity }()

		// Mock LogActivity to prevent database calls during test
		LogActivity = func(action string, entityType string, entityID uint, userID uint) {}

		article, err := svc.CreateArticle(database.CountryJordan, newArticle, &authorID)

		assert.NoError(t, err)
		assert.NotNil(t, article)
		assert.Equal(t, uint(5), article.ID)
	})

	t.Run("DatabaseError", func(t *testing.T) {
		newArticle := &ArticleInput{
			Title: "New Article",
		}

		expectedErr := errors.New("db connection error")
		mockRepo.CreateFunc = func(countryID database.CountryID, article *models.Article) error {
			return expectedErr
		}

		article, err := svc.CreateArticle(database.CountryJordan, newArticle, nil)

		assert.Error(t, err)
		assert.Nil(t, article)
		assert.Equal(t, expectedErr, err)
	})

}

// Regression guard for the bug where GetDashboardCreateData used to return
// Subjects/Semesters as hardcoded empty slices regardless of country — the dashboard's
// class → subject → semester cascade had nothing to filter against. The fix fetches every
// subject/semester across all classes (not scoped to any single one), so the key assertion
// here is that the result spans more than one grade level, not just that it's non-empty.
func TestArticleService_GetDashboardCreateData(t *testing.T) {
	t.Setenv("JWT_SECRET", "test_secret_key_12345678901234567890")
	t.Setenv("DB_HOST_JO", "localhost")
	t.Setenv("DB_NAME_JO", "test_db")
	t.Setenv("DB_USER_JO", "root")
	t.Setenv("APP_URL", "http://localhost")
	t.Setenv("FRONTEND_URL", "http://localhost:3000")

	mockRepo := &MockArticleRepository{}
	svc := NewArticleService(mockRepo, nil, nil)

	t.Run("Success returns subjects and semesters across every class", func(t *testing.T) {
		mockRepo.GetClassesFunc = func(countryID database.CountryID) ([]models.SchoolClass, error) {
			return []models.SchoolClass{
				{ID: 1, GradeName: "الصف الأول", GradeLevel: 1},
				{ID: 2, GradeName: "الصف الثاني", GradeLevel: 2},
			}, nil
		}
		mockRepo.GetAllSubjectsFunc = func(countryID database.CountryID) ([]models.Subject, error) {
			return []models.Subject{
				{ID: 10, SubjectName: "اللغة العربية", GradeLevel: 1},
				{ID: 20, SubjectName: "الرياضيات", GradeLevel: 2},
			}, nil
		}
		mockRepo.GetAllSemestersFunc = func(countryID database.CountryID) ([]models.Semester, error) {
			return []models.Semester{
				{ID: 100, SemesterName: "الفصل الأول", GradeLevel: 1},
				{ID: 200, SemesterName: "الفصل الأول", GradeLevel: 2},
			}, nil
		}

		data, err := svc.GetDashboardCreateData(database.CountryJordan)

		assert.NoError(t, err)
		assert.NotNil(t, data)
		assert.Len(t, data.Classes, 2)
		assert.Len(t, data.Subjects, 2)
		assert.Len(t, data.Semesters, 2)

		gradeLevels := map[uint]bool{}
		for _, s := range data.Subjects {
			gradeLevels[s.GradeLevel] = true
		}
		assert.Len(t, gradeLevels, 2, "subjects must span more than one grade level, not be scoped to a single class")
	})

	t.Run("ClassesError propagates", func(t *testing.T) {
		expectedErr := errors.New("classes query failed")
		mockRepo.GetClassesFunc = func(countryID database.CountryID) ([]models.SchoolClass, error) {
			return nil, expectedErr
		}
		mockRepo.GetAllSubjectsFunc = func(countryID database.CountryID) ([]models.Subject, error) { return nil, nil }
		mockRepo.GetAllSemestersFunc = func(countryID database.CountryID) ([]models.Semester, error) { return nil, nil }

		data, err := svc.GetDashboardCreateData(database.CountryJordan)

		assert.Error(t, err)
		assert.Equal(t, expectedErr, err)
		assert.Nil(t, data)
	})

	t.Run("SubjectsError propagates", func(t *testing.T) {
		expectedErr := errors.New("subjects query failed")
		mockRepo.GetClassesFunc = func(countryID database.CountryID) ([]models.SchoolClass, error) { return nil, nil }
		mockRepo.GetAllSubjectsFunc = func(countryID database.CountryID) ([]models.Subject, error) {
			return nil, expectedErr
		}
		mockRepo.GetAllSemestersFunc = func(countryID database.CountryID) ([]models.Semester, error) { return nil, nil }

		data, err := svc.GetDashboardCreateData(database.CountryJordan)

		assert.Error(t, err)
		assert.Equal(t, expectedErr, err)
		assert.Nil(t, data)
	})

	t.Run("SemestersError propagates", func(t *testing.T) {
		expectedErr := errors.New("semesters query failed")
		mockRepo.GetClassesFunc = func(countryID database.CountryID) ([]models.SchoolClass, error) { return nil, nil }
		mockRepo.GetAllSubjectsFunc = func(countryID database.CountryID) ([]models.Subject, error) { return nil, nil }
		mockRepo.GetAllSemestersFunc = func(countryID database.CountryID) ([]models.Semester, error) {
			return nil, expectedErr
		}

		data, err := svc.GetDashboardCreateData(database.CountryJordan)

		assert.Error(t, err)
		assert.Equal(t, expectedErr, err)
		assert.Nil(t, data)
	})
}

// Regression guard for the bug where GetDashboardEditData scoped Subjects/Semesters to only
// the article's own class — an admin reclassifying an article to a different class saw 0
// subjects for it. The fix (matching GetDashboardCreateData) fetches every subject/semester
// across all classes, so the key assertion is that the result includes subjects outside the
// edited article's own grade level, not just that it's non-empty.
func TestArticleService_GetDashboardEditData(t *testing.T) {
	t.Setenv("JWT_SECRET", "test_secret_key_12345678901234567890")
	t.Setenv("DB_HOST_JO", "localhost")
	t.Setenv("DB_NAME_JO", "test_db")
	t.Setenv("DB_USER_JO", "root")
	t.Setenv("APP_URL", "http://localhost")
	t.Setenv("FRONTEND_URL", "http://localhost:3000")

	mockRepo := &MockArticleRepository{}
	svc := NewArticleService(mockRepo, nil, nil)

	t.Run("Success includes subjects outside the article's own class", func(t *testing.T) {
		// The article being edited belongs to subject 10 (grade level 1) — the bug this
		// guards against scoped the returned Subjects/Semesters lists down to exactly that
		// article's own class, so the fixed behavior must return grade level 2 as well.
		ownSubjectID := uint(10)
		const ownGradeLevel = 1
		const otherGradeLevel = 2
		existingArticle := &models.Article{ID: 1, Title: "Existing Article", SubjectID: &ownSubjectID}

		mockRepo.FindByIDFunc = func(countryID database.CountryID, id uint64) (*models.Article, error) {
			return existingArticle, nil
		}
		mockRepo.GetClassesFunc = func(countryID database.CountryID) ([]models.SchoolClass, error) {
			return []models.SchoolClass{{ID: 1, GradeLevel: ownGradeLevel}, {ID: 2, GradeLevel: otherGradeLevel}}, nil
		}
		mockRepo.GetAllSubjectsFunc = func(countryID database.CountryID) ([]models.Subject, error) {
			return []models.Subject{
				{ID: ownSubjectID, SubjectName: "اللغة العربية", GradeLevel: ownGradeLevel},
				{ID: 20, SubjectName: "الرياضيات", GradeLevel: otherGradeLevel},
			}, nil
		}
		mockRepo.GetAllSemestersFunc = func(countryID database.CountryID) ([]models.Semester, error) {
			return []models.Semester{
				{ID: 100, SemesterName: "الفصل الأول", GradeLevel: ownGradeLevel},
				{ID: 200, SemesterName: "الفصل الأول", GradeLevel: otherGradeLevel},
			}, nil
		}

		data, err := svc.GetDashboardEditData(database.CountryJordan, 1)

		assert.NoError(t, err)
		assert.NotNil(t, data)
		assert.Equal(t, existingArticle, data.Data)
		assert.Len(t, data.Subjects, 2)
		assert.Len(t, data.Semesters, 2)

		foundOutsideOwnClass := false
		for _, s := range data.Subjects {
			if s.GradeLevel == otherGradeLevel {
				foundOutsideOwnClass = true
			}
		}
		assert.True(t, foundOutsideOwnClass, "subjects must include grades other than the article's own — not scoped to a single class")
	})

	t.Run("ArticleNotFound", func(t *testing.T) {
		mockRepo.FindByIDFunc = func(countryID database.CountryID, id uint64) (*models.Article, error) {
			return nil, gorm.ErrRecordNotFound
		}

		data, err := svc.GetDashboardEditData(database.CountryJordan, 999)

		assert.Error(t, err)
		assert.Equal(t, ErrNotFound, err)
		assert.Nil(t, data)
	})

	t.Run("SubjectsError propagates", func(t *testing.T) {
		existingArticle := &models.Article{ID: 1, Title: "Existing Article"}
		mockRepo.FindByIDFunc = func(countryID database.CountryID, id uint64) (*models.Article, error) {
			return existingArticle, nil
		}
		mockRepo.GetClassesFunc = func(countryID database.CountryID) ([]models.SchoolClass, error) { return nil, nil }
		expectedErr := errors.New("subjects query failed")
		mockRepo.GetAllSubjectsFunc = func(countryID database.CountryID) ([]models.Subject, error) {
			return nil, expectedErr
		}
		mockRepo.GetAllSemestersFunc = func(countryID database.CountryID) ([]models.Semester, error) { return nil, nil }

		data, err := svc.GetDashboardEditData(database.CountryJordan, 1)

		assert.Error(t, err)
		assert.Equal(t, expectedErr, err)
		assert.Nil(t, data)
	})
}
