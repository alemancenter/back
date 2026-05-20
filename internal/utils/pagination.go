package utils

import (
	"strconv"

	"github.com/gofiber/fiber/v2"
)

// Pagination holds pagination parameters
type Pagination struct {
	Page    int
	PerPage int
	Offset  int
}

// GetPagination extracts pagination params from the request
func GetPagination(c *fiber.Ctx) Pagination {
	page, _ := strconv.Atoi(c.Query("page", "1"))
	perPage, _ := strconv.Atoi(c.Query("per_page", "15"))

	if page < 1 {
		page = 1
	}
	if perPage < 1 {
		perPage = 15
	} else if perPage > 500 {
		perPage = 500
	}

	return Pagination{
		Page:    page,
		PerPage: perPage,
		Offset:  (page - 1) * perPage,
	}
}

// BuildMeta constructs pagination metadata
func (p *Pagination) BuildMeta(total int64) PaginationMeta {
	lastPage := int(total) / p.PerPage
	if int(total)%p.PerPage != 0 {
		lastPage++
	}
	if lastPage == 0 {
		lastPage = 1
	}

	from := 0
	to := 0
	if total > 0 && p.Offset < int(total) {
		from = p.Offset + 1
		to = p.Offset + p.PerPage
		if to > int(total) {
			to = int(total)
		}
	}

	return PaginationMeta{
		CurrentPage: p.Page,
		PerPage:     p.PerPage,
		Total:       total,
		LastPage:    lastPage,
		From:        from,
		To:          to,
	}
}
