package services

import (
	"github.com/alemancenter/fiber-api/internal/database"
	"github.com/alemancenter/fiber-api/internal/models"
	"github.com/alemancenter/fiber-api/internal/repositories"
)

type KeywordService interface {
	GetKeywords(countryID database.CountryID, keywordType, search string, limit, offset int) ([]repositories.KeywordDTO, int64, error)
	GetKeywordContent(countryID database.CountryID, keyword string, search, sort string, limit, offset int) (*models.Keyword, []models.Article, int64, []models.Post, int64, error)
}

type keywordService struct {
	repo repositories.KeywordRepository
}

func NewKeywordService(repo repositories.KeywordRepository) KeywordService {
	return &keywordService{repo: repo}
}

func (s *keywordService) GetKeywords(countryID database.CountryID, keywordType, search string, limit, offset int) ([]repositories.KeywordDTO, int64, error) {
	return s.repo.ListKeywords(countryID, keywordType, search, limit, offset)
}

func (s *keywordService) GetKeywordContent(countryID database.CountryID, keyword string, search, sort string, limit, offset int) (*models.Keyword, []models.Article, int64, []models.Post, int64, error) {
	kw, err := s.repo.FindByKeyword(countryID, keyword)
	if err != nil {
		return nil, nil, 0, nil, 0, MapError(err)
	}

	articles, artTotal, err := s.repo.ListArticlesByKeyword(countryID, kw.ID, search, sort, limit, offset)
	if err != nil {
		return nil, nil, 0, nil, 0, MapError(err)
	}

	posts, postTotal, err := s.repo.ListPostsByKeyword(countryID, kw.ID, search, sort, limit, offset)
	if err != nil {
		return nil, nil, 0, nil, 0, MapError(err)
	}

	return kw, articles, artTotal, posts, postTotal, nil
}
