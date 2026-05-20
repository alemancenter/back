package services

import (
	"testing"

	"github.com/alemancenter/fiber-api/internal/database"
	"github.com/alemancenter/fiber-api/internal/models"
	"github.com/alemancenter/fiber-api/internal/repositories"
	"github.com/stretchr/testify/assert"
	"gorm.io/gorm"
)

// MockGradeRepository is a mock implementation of repositories.GradeRepository
type MockGradeRepository struct {
	repositories.GradeRepository // embed to satisfy interface

	FindSchoolClassByIDFunc func(countryID database.CountryID, id uint64) (*models.SchoolClass, error)
	CreateSchoolClassFunc   func(countryID database.CountryID, class *models.SchoolClass) error

	FindSubjectByIDFunc func(countryID database.CountryID, id uint64) (*models.Subject, error)

	FindSemesterByIDFunc func(countryID database.CountryID, id uint64) (*models.Semester, error)
}

func (m *MockGradeRepository) FindSchoolClassByID(countryID database.CountryID, id uint64) (*models.SchoolClass, error) {
	if m.FindSchoolClassByIDFunc != nil {
		return m.FindSchoolClassByIDFunc(countryID, id)
	}
	return nil, nil
}

func (m *MockGradeRepository) CreateSchoolClass(countryID database.CountryID, class *models.SchoolClass) error {
	if m.CreateSchoolClassFunc != nil {
		return m.CreateSchoolClassFunc(countryID, class)
	}
	return nil
}

func (m *MockGradeRepository) FindSubjectByID(countryID database.CountryID, id uint64) (*models.Subject, error) {
	if m.FindSubjectByIDFunc != nil {
		return m.FindSubjectByIDFunc(countryID, id)
	}
	return nil, nil
}

func (m *MockGradeRepository) FindSemesterByID(countryID database.CountryID, id uint64) (*models.Semester, error) {
	if m.FindSemesterByIDFunc != nil {
		return m.FindSemesterByIDFunc(countryID, id)
	}
	return nil, nil
}

func TestGradeService_GetSchoolClass(t *testing.T) {
	t.Setenv("JWT_SECRET", "12345678901234567890123456789012") // For any underlying init

	repo := &MockGradeRepository{
		FindSchoolClassByIDFunc: func(countryID database.CountryID, id uint64) (*models.SchoolClass, error) {
			if id == 1 {
				return &models.SchoolClass{GradeName: "Class 1", GradeLevel: 1}, nil
			}
			return nil, gorm.ErrRecordNotFound
		},
	}

	svc := NewGradeService(repo, nil)

	// Test success
	class, err := svc.GetSchoolClass(database.CountryJordan, 1)
	assert.NoError(t, err)
	assert.NotNil(t, class)
	assert.Equal(t, "Class 1", class.GradeName)

	// Test not found mapping
	class, err = svc.GetSchoolClass(database.CountryJordan, 999)
	assert.Error(t, err)
	assert.Equal(t, ErrNotFound, err)
	assert.Nil(t, class)
}

func TestGradeService_CreateSchoolClass(t *testing.T) {
	t.Setenv("JWT_SECRET", "12345678901234567890123456789012")

	repo := &MockGradeRepository{
		CreateSchoolClassFunc: func(countryID database.CountryID, class *models.SchoolClass) error {
			class.ID = 1
			return nil
		},
	}

	svc := NewGradeService(repo, nil)

	req := &SchoolClassInput{
		GradeName:  "New Class",
		GradeLevel: 5,
	}

	class, err := svc.CreateSchoolClass(database.CountryJordan, req)
	assert.NoError(t, err)
	assert.NotNil(t, class)
	assert.Equal(t, "New Class", class.GradeName)
	assert.Equal(t, 5, class.GradeLevel)
}
