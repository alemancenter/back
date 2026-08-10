package keywords

import (
	"net/url"

	"github.com/imanjo/fiber-api/internal/database"
	"github.com/imanjo/fiber-api/internal/repositories"
	"github.com/imanjo/fiber-api/internal/services"
	"github.com/imanjo/fiber-api/internal/utils"
	"github.com/gofiber/fiber/v2"
)

type Handler struct {
	svc services.KeywordService
}

func New(svc services.KeywordService) *Handler {
	return &Handler{svc: svc}
}

// Index returns keywords with pagination
// @Summary List Keywords
// @Description Returns a paginated list of keywords used in articles and/or posts
// @Tags Keywords
// @Produce json
// @Param X-Country-Id header string false "Country ID"
// @Param q query string false "Search query"
// @Param type query string false "Type: all, article, post"
// @Param page query int false "Page number"
// @Param limit query int false "Items per page"
// @Success 200 {object} utils.APIResponse{data=map[string]interface{}}
// @Failure 500 {object} utils.APIResponse
// @Router /keywords [get]
func (h *Handler) Index(c *fiber.Ctx) error {
	countryID, ok := c.Locals("country_id").(database.CountryID)
	if !ok || countryID == 0 {
		// See Show() below — same missing fallback caused this endpoint to silently return
		// Jordan's keywords for every country regardless of X-Country-Id.
		countryID = database.CountryIDFromHeader(c.Get("X-Country-Id"))
	}

	search := c.Query("q", "")
	keywordType := c.Query("type", "all")
	pag := utils.GetPagination(c)

	var articleKeywords []repositories.KeywordDTO
	var postKeywords []repositories.KeywordDTO
	var err error
	var totalArticles, totalPosts int64

	if keywordType == "all" || keywordType == "article" || keywordType == "articles" {
		articleKeywords, totalArticles, err = h.svc.GetKeywords(countryID, "articles", search, pag.PerPage, pag.Offset)
		if err != nil {
			return utils.InternalError(c)
		}
	}

	if keywordType == "all" || keywordType == "post" || keywordType == "posts" {
		postKeywords, totalPosts, err = h.svc.GetKeywords(countryID, "posts", search, pag.PerPage, pag.Offset)
		if err != nil {
			return utils.InternalError(c)
		}
	}

	res := fiber.Map{
		"database": database.CountryCode(countryID),
		"query":    search,
		"per_page": pag.PerPage,
	}

	if keywordType == "all" || keywordType == "article" || keywordType == "articles" {
		res["article_keywords"] = fiber.Map{
			"data": articleKeywords,
			"meta": pag.BuildMeta(totalArticles),
		}
	} else {
		res["article_keywords"] = nil
	}

	if keywordType == "all" || keywordType == "post" || keywordType == "posts" {
		res["post_keywords"] = fiber.Map{
			"data": postKeywords,
			"meta": pag.BuildMeta(totalPosts),
		}
	} else {
		res["post_keywords"] = nil
	}

	c.Set("Cache-Control", "public, max-age=600, stale-while-revalidate=120")
	return utils.Success(c, "success", res)
}

// Show returns articles and posts for a keyword
// @Summary Get Keyword Content
// @Description Returns a list of articles and posts associated with a specific keyword
// @Tags Keywords
// @Produce json
// @Param X-Country-Id header string false "Country ID"
// @Param keyword path string true "The exact keyword text"
// @Param q query string false "Search query inside keyword results"
// @Param sort query string false "Sort order (latest, popular)"
// @Param page query int false "Page number"
// @Param limit query int false "Items per page"
// @Success 200 {object} utils.APIResponse{data=map[string]interface{}}
// @Failure 404 {object} utils.APIResponse
// @Router /keywords/{keyword} [get]
func (h *Handler) Show(c *fiber.Ctx) error {
	keyword := c.Params("keyword")
	countryID, ok := c.Locals("country_id").(database.CountryID)
	if !ok || countryID == 0 {
		// Locals("country_id") isn't guaranteed to be set on every route (see home/handler.go
		// for the same fallback) — without this, a request that reaches here without it fell
		// through to CountryID's zero value, which Manager.Get() silently maps to Jordan
		// regardless of the caller's actual X-Country-Id header.
		countryID = database.CountryIDFromHeader(c.Get("X-Country-Id"))
	}

	search := c.Query("q", "")
	sort := c.Query("sort", "latest")
	pag := utils.GetPagination(c)

	// c.Params() is expected to already be percent-decoded (UnescapePath is enabled in
	// cmd/server/main.go's fiber.Config), but an exact-match lookup on a multi-word Arabic
	// keyword was confirmed failing in production for a keyword verified byte-for-byte present
	// in the database (found via /keywords listing, compared codepoint-by-codepoint against
	// what this handler receives). Rather than leave that dependent on exactly one layer of
	// the request pipeline decoding correctly, try the value as received first and fall back
	// to an explicit unescape — safe either way, since re-unescaping an already-decoded string
	// with no remaining %XX sequences is a no-op.
	kw, articles, artTotal, posts, postTotal, err := h.svc.GetKeywordContent(countryID, keyword, search, sort, pag.PerPage, pag.Offset)
	if err != nil {
		if decoded, decodeErr := url.QueryUnescape(keyword); decodeErr == nil && decoded != keyword {
			kw, articles, artTotal, posts, postTotal, err = h.svc.GetKeywordContent(countryID, decoded, search, sort, pag.PerPage, pag.Offset)
		}
	}
	if err != nil {
		return utils.NotFound(c) // Keyword not found
	}

	// Calculate og_image
	var ogImage *string
	if len(articles) > 0 && len(articles[0].Files) > 0 {
		for _, f := range articles[0].Files {
			if f.FileType == "image" {
				ogImage = &f.FilePath
				break
			}
		}
	}
	if ogImage == nil && len(posts) > 0 && posts[0].Image != nil {
		ogImage = posts[0].Image
	}

	return utils.Success(c, "success", fiber.Map{
		"database": database.CountryCode(countryID),
		"keyword":  kw,
		"filters": fiber.Map{
			"q":        search,
			"sort":     sort,
			"per_page": pag.PerPage,
		},
		"articles": fiber.Map{
			"data": articles,
			"meta": pag.BuildMeta(artTotal),
		},
		"posts": fiber.Map{
			"data": posts,
			"meta": pag.BuildMeta(postTotal),
		},
		"og_image": ogImage,
	})
}
