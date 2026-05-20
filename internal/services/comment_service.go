package services

import (
	"github.com/alemancenter/fiber-api/internal/database"
	"github.com/alemancenter/fiber-api/internal/models"
	"github.com/alemancenter/fiber-api/internal/repositories"
	"github.com/alemancenter/fiber-api/internal/utils"
)

type CommentService interface {
	List(dbCode string, commentableType string, commentableID string, status string, search string, limit, offset int) ([]models.Comment, int64, error)
	ListPublic(dbCode string, commentableType string, commentableID string, limit, offset int) ([]models.Comment, int64, error)
	Create(dbCode string, userID uint, req *CreateCommentRequest) (*models.Comment, error)
	Approve(dbCode string, id uint64) (*models.Comment, error)
	Reject(dbCode string, id uint64) (*models.Comment, error)
	Delete(dbCode string, id uint64) error
	DeleteMany(dbCode string, ids []uint64) (int64, error)
	CreateReaction(countryID database.CountryID, userID uint, req *ReactionRequest) (*models.Reaction, error)
	DeleteReaction(countryID database.CountryID, commentID uint64, userID uint) error
	GetReactions(countryID database.CountryID, commentID uint64) ([]models.Reaction, error)
}

type CreateCommentRequest struct {
	Body            string `json:"body" validate:"required,min=1"`
	CommentableID   uint   `json:"commentable_id" validate:"required"`
	CommentableType string `json:"commentable_type" validate:"required"`
}

type ReactionRequest struct {
	CommentID uint   `json:"comment_id" validate:"required"`
	Emoji     string `json:"emoji" validate:"required,max=20"`
}

type BulkDeleteCommentsRequest struct {
	IDs []uint64 `json:"ids" validate:"required,min=1,max=500,dive,required"`
}

type commentService struct {
	repo repositories.CommentRepository
}

func NewCommentService(repo repositories.CommentRepository) CommentService {
	return &commentService{repo: repo}
}

func (s *commentService) List(dbCode string, commentableType string, commentableID string, status string, search string, limit, offset int) ([]models.Comment, int64, error) {
	if status != "" && !models.IsValidCommentStatus(status) {
		return []models.Comment{}, 0, nil
	}
	return s.repo.ListPaginated(dbCode, commentableType, commentableID, status, search, limit, offset)
}

func (s *commentService) ListPublic(dbCode string, commentableType string, commentableID string, limit, offset int) ([]models.Comment, int64, error) {
	return s.repo.ListPaginated(dbCode, commentableType, commentableID, models.CommentStatusApproved, "", limit, offset)
}

func (s *commentService) Create(dbCode string, userID uint, req *CreateCommentRequest) (*models.Comment, error) {
	comment := &models.Comment{
		Body:            utils.SanitizeInput(req.Body),
		UserID:          userID,
		CommentableID:   req.CommentableID,
		CommentableType: req.CommentableType,
		Database:        dbCode,
		Status:          models.CommentStatusPending,
	}

	if err := s.repo.CreateComment(dbCode, comment); err != nil {
		return nil, MapError(err)
	}

	return comment, nil
}

func (s *commentService) Approve(dbCode string, id uint64) (*models.Comment, error) {
	return MapErr1(s.repo.UpdateCommentStatus(dbCode, id, models.CommentStatusApproved))
}

func (s *commentService) Reject(dbCode string, id uint64) (*models.Comment, error) {
	return MapErr1(s.repo.UpdateCommentStatus(dbCode, id, models.CommentStatusRejected))
}

func (s *commentService) Delete(dbCode string, id uint64) error {
	return MapError(s.repo.DeleteComment(dbCode, id))
}

func (s *commentService) DeleteMany(dbCode string, ids []uint64) (int64, error) {
	uniqueIDs := uniqueUint64(ids)
	if len(uniqueIDs) == 0 {
		return 0, nil
	}
	deleted, err := s.repo.DeleteComments(dbCode, uniqueIDs)
	return deleted, MapError(err)
}

func (s *commentService) CreateReaction(countryID database.CountryID, userID uint, req *ReactionRequest) (*models.Reaction, error) {
	if !s.repo.IsApprovedComment(countryID, uint64(req.CommentID)) {
		return nil, ErrForbidden
	}

	reaction := &models.Reaction{
		CommentID: req.CommentID,
		UserID:    userID,
		Emoji:     req.Emoji,
	}

	if err := s.repo.UpsertReaction(countryID, reaction); err != nil {
		return nil, MapError(err)
	}

	return reaction, nil
}

func (s *commentService) DeleteReaction(countryID database.CountryID, commentID uint64, userID uint) error {
	return s.repo.DeleteReaction(countryID, commentID, userID)
}

func (s *commentService) GetReactions(countryID database.CountryID, commentID uint64) ([]models.Reaction, error) {
	return s.repo.GetReactions(countryID, commentID)
}

func uniqueUint64(values []uint64) []uint64 {
	seen := make(map[uint64]struct{}, len(values))
	result := make([]uint64, 0, len(values))
	for _, value := range values {
		if value == 0 {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}
