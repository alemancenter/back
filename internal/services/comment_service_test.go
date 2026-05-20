package services

import (
	"testing"

	"github.com/alemancenter/fiber-api/internal/database"
	"github.com/alemancenter/fiber-api/internal/models"
	"github.com/alemancenter/fiber-api/internal/repositories"
	"github.com/stretchr/testify/assert"
	"gorm.io/gorm"
)

type MockCommentRepository struct {
	repositories.CommentRepository

	ListPaginatedFunc       func(dbCode string, commentableType string, commentableID string, status string, search string, limit, offset int) ([]models.Comment, int64, error)
	CreateCommentFunc       func(dbCode string, comment *models.Comment) error
	UpdateCommentStatusFunc func(dbCode string, id uint64, status string) (*models.Comment, error)
	DeleteCommentsFunc      func(dbCode string, ids []uint64) (int64, error)
	IsApprovedCommentFunc   func(countryID database.CountryID, commentID uint64) bool
	UpsertReactionFunc      func(countryID database.CountryID, reaction *models.Reaction) error
}

func (m *MockCommentRepository) ListPaginated(dbCode string, commentableType string, commentableID string, status string, search string, limit, offset int) ([]models.Comment, int64, error) {
	if m.ListPaginatedFunc != nil {
		return m.ListPaginatedFunc(dbCode, commentableType, commentableID, status, search, limit, offset)
	}
	return nil, 0, nil
}

func (m *MockCommentRepository) CreateComment(dbCode string, comment *models.Comment) error {
	if m.CreateCommentFunc != nil {
		return m.CreateCommentFunc(dbCode, comment)
	}
	return nil
}

func (m *MockCommentRepository) UpdateCommentStatus(dbCode string, id uint64, status string) (*models.Comment, error) {
	if m.UpdateCommentStatusFunc != nil {
		return m.UpdateCommentStatusFunc(dbCode, id, status)
	}
	return nil, gorm.ErrRecordNotFound
}

func (m *MockCommentRepository) DeleteComments(dbCode string, ids []uint64) (int64, error) {
	if m.DeleteCommentsFunc != nil {
		return m.DeleteCommentsFunc(dbCode, ids)
	}
	return 0, nil
}

func (m *MockCommentRepository) IsApprovedComment(countryID database.CountryID, commentID uint64) bool {
	if m.IsApprovedCommentFunc != nil {
		return m.IsApprovedCommentFunc(countryID, commentID)
	}
	return false
}

func (m *MockCommentRepository) UpsertReaction(countryID database.CountryID, reaction *models.Reaction) error {
	if m.UpsertReactionFunc != nil {
		return m.UpsertReactionFunc(countryID, reaction)
	}
	return nil
}

func TestCommentService_CreateStoresPendingComment(t *testing.T) {
	mockRepo := &MockCommentRepository{
		CreateCommentFunc: func(dbCode string, comment *models.Comment) error {
			assert.Equal(t, "jo", dbCode)
			assert.Equal(t, models.CommentStatusPending, comment.Status)
			assert.Equal(t, "alert(1) nice", comment.Body)
			assert.Equal(t, uint(42), comment.UserID)
			assert.Equal(t, uint(7), comment.CommentableID)
			assert.Equal(t, "App\\Models\\Post", comment.CommentableType)
			assert.Equal(t, "jo", comment.Database)
			return nil
		},
	}

	svc := NewCommentService(mockRepo)
	comment, err := svc.Create("jo", 42, &CreateCommentRequest{
		Body:            "<script>alert(1)</script> nice",
		CommentableID:   7,
		CommentableType: "App\\Models\\Post",
	})

	assert.NoError(t, err)
	assert.Equal(t, models.CommentStatusPending, comment.Status)
}

func TestCommentService_ListPublicFiltersApprovedOnly(t *testing.T) {
	mockRepo := &MockCommentRepository{
		ListPaginatedFunc: func(dbCode string, commentableType string, commentableID string, status string, search string, limit, offset int) ([]models.Comment, int64, error) {
			assert.Equal(t, "jo", dbCode)
			assert.Equal(t, "App\\Models\\Article", commentableType)
			assert.Equal(t, "12", commentableID)
			assert.Equal(t, models.CommentStatusApproved, status)
			assert.Empty(t, search)
			return []models.Comment{{ID: 1, Status: models.CommentStatusApproved}}, 1, nil
		},
	}

	svc := NewCommentService(mockRepo)
	comments, total, err := svc.ListPublic("jo", "App\\Models\\Article", "12", 20, 0)

	assert.NoError(t, err)
	assert.Equal(t, int64(1), total)
	assert.Len(t, comments, 1)
	assert.Equal(t, models.CommentStatusApproved, comments[0].Status)
}

func TestCommentService_ApproveRejectMapStatus(t *testing.T) {
	var statuses []string
	mockRepo := &MockCommentRepository{
		UpdateCommentStatusFunc: func(dbCode string, id uint64, status string) (*models.Comment, error) {
			statuses = append(statuses, status)
			return &models.Comment{ID: uint(id), Status: status}, nil
		},
	}

	svc := NewCommentService(mockRepo)
	approved, err := svc.Approve("jo", 5)
	assert.NoError(t, err)
	assert.Equal(t, models.CommentStatusApproved, approved.Status)

	rejected, err := svc.Reject("jo", 5)
	assert.NoError(t, err)
	assert.Equal(t, models.CommentStatusRejected, rejected.Status)
	assert.Equal(t, []string{models.CommentStatusApproved, models.CommentStatusRejected}, statuses)
}

func TestCommentService_ApproveMapsNotFound(t *testing.T) {
	mockRepo := &MockCommentRepository{
		UpdateCommentStatusFunc: func(dbCode string, id uint64, status string) (*models.Comment, error) {
			return nil, gorm.ErrRecordNotFound
		},
	}

	svc := NewCommentService(mockRepo)
	comment, err := svc.Approve("jo", 404)

	assert.Nil(t, comment)
	assert.Equal(t, ErrNotFound, err)
}

func TestCommentService_DeleteManyDeduplicatesAndSkipsZero(t *testing.T) {
	mockRepo := &MockCommentRepository{
		DeleteCommentsFunc: func(dbCode string, ids []uint64) (int64, error) {
			assert.Equal(t, "jo", dbCode)
			assert.Equal(t, []uint64{10, 12, 14}, ids)
			return 3, nil
		},
	}

	svc := NewCommentService(mockRepo)
	deleted, err := svc.DeleteMany("jo", []uint64{10, 0, 12, 10, 14})

	assert.NoError(t, err)
	assert.Equal(t, int64(3), deleted)
}

func TestCommentService_CreateReactionRequiresApprovedComment(t *testing.T) {
	mockRepo := &MockCommentRepository{
		IsApprovedCommentFunc: func(countryID database.CountryID, commentID uint64) bool {
			assert.Equal(t, database.CountryID(1), countryID)
			assert.Equal(t, uint64(99), commentID)
			return false
		},
		UpsertReactionFunc: func(countryID database.CountryID, reaction *models.Reaction) error {
			t.Fatal("reaction should not be stored for non-approved comments")
			return nil
		},
	}

	svc := NewCommentService(mockRepo)
	reaction, err := svc.CreateReaction(1, 42, &ReactionRequest{CommentID: 99, Emoji: "like"})

	assert.Nil(t, reaction)
	assert.Equal(t, ErrForbidden, err)
}
