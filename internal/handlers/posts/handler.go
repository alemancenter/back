package posts

import (
	"fmt"
	"mime"
	"strconv"
	"strings"

	"github.com/alemancenter/fiber-api/internal/database"
	"github.com/alemancenter/fiber-api/internal/models"
	"github.com/alemancenter/fiber-api/internal/repositories"
	"github.com/alemancenter/fiber-api/internal/services"
	"github.com/alemancenter/fiber-api/internal/utils"
	"github.com/alemancenter/fiber-api/pkg/logger"
	"github.com/gofiber/fiber/v2"
	"go.uber.org/zap"
)

var adminRoleNames = []string{"admin", "super_admin", "super-admin", "manager", "administrator", "root"}

func isAdminUser(user *models.User) bool {
	if user == nil {
		return false
	}
	if user.ID == 1 {
		return true
	}
	for _, role := range user.Roles {
		for _, name := range adminRoleNames {
			if strings.EqualFold(role.Name, name) {
				return true
			}
		}
	}
	return false
}

// Handler contains posts route handlers
type Handler struct {
	svc          services.PostService
	notification services.NotificationService
}

// New creates a new posts Handler
func New(svc services.PostService, notification ...services.NotificationService) *Handler {
	h := &Handler{svc: svc}
	if len(notification) > 0 {
		h.notification = notification[0]
	}
	return h
}

// List returns a paginated list of published posts
// @Summary List Public Posts
// @Description Returns a paginated list of active/published posts
// @Tags Posts
// @Produce json
// @Param X-Country-Id header string false "Country ID"
// @Param category_id query int false "Filter by category ID"
// @Param search query string false "Search query"
// @Param featured query string false "Filter by featured status (1 or 0)"
// @Param page query int false "Page number"
// @Param limit query int false "Items per page"
// @Success 200 {object} utils.APIResponse{data=[]models.Post}
// @Failure 500 {object} utils.APIResponse
// @Router /posts [get]
func (h *Handler) List(c *fiber.Ctx) error {
	countryID, _ := c.Locals("country_id").(database.CountryID)
	pag := utils.GetPagination(c)

	var catID *uint
	if cidStr := c.Query("category_id"); cidStr != "" {
		if id, err := strconv.ParseUint(cidStr, 10, 64); err == nil {
			parsedID := uint(id)
			catID = &parsedID
		}
	}

	activeOnly := true
	filter := &models.PostFilter{
		CategoryID: catID,
		Search:     c.Query("search"),
		Featured:   c.Query("featured"),
		IsActive:   &activeOnly,
	}

	postList, total, err := h.svc.List(countryID, filter, pag.PerPage, pag.Offset)
	if err != nil {
		return utils.InternalError(c)
	}

	return utils.Paginated(c, "success", newDashboardPostResponses(postList), pag.BuildMeta(total))
}

// Show returns a single post
// @Summary Get Post
// @Description Get a single active post by ID
// @Tags Posts
// @Produce json
// @Param X-Country-Id header string false "Country ID"
// @Param id path int true "Post ID"
// @Success 200 {object} utils.APIResponse{data=models.Post}
// @Failure 400 {object} utils.APIResponse
// @Failure 404 {object} utils.APIResponse
// @Failure 500 {object} utils.APIResponse
// @Router /posts/{id} [get]
func (h *Handler) Show(c *fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return utils.BadRequest(c, "معرف غير صحيح")
	}

	countryID, _ := c.Locals("country_id").(database.CountryID)

	post, err := h.svc.GetByID(countryID, id)
	if err != nil {
		if err == services.ErrNotFound {
			return utils.NotFound(c)
		}
		return utils.InternalError(c)
	}

	if !post.IsActive {
		return utils.NotFound(c)
	}

	go h.svc.IncrementView(countryID, uint64(post.ID))

	return utils.Success(c, "success", post)
}

// IncrementView increments the post view count
// @Summary Increment View Count
// @Description Increment the view counter for a specific post
// @Tags Posts
// @Produce json
// @Param X-Country-Id header string false "Country ID"
// @Param id path int true "Post ID"
// @Success 200 {object} utils.APIResponse
// @Failure 400 {object} utils.APIResponse
// @Failure 500 {object} utils.APIResponse
// @Router /posts/{id}/increment-view [post]
func (h *Handler) IncrementView(c *fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return utils.BadRequest(c, "معرف غير صحيح")
	}

	countryID, _ := c.Locals("country_id").(database.CountryID)

	if err := h.svc.IncrementView(countryID, id); err != nil {
		return utils.InternalError(c)
	}

	return utils.Success(c, "success", nil)
}

// DashboardCreate creates a new post
// @Summary Create Post
// @Description Create a new post from the dashboard
// @Tags Posts
// @Accept json
// @Accept mpfd
// @Produce json
// @Security BearerAuth
// @Security FrontendKeyAuth
// @Param X-Country-Id header string false "Country ID"
// @Param request body services.CreatePostRequest true "Post data"
// @Success 201 {object} utils.APIResponse{data=models.Post}
// @Failure 400 {object} utils.APIResponse

type dashboardPostResponse struct {
	models.Post
	ViewsCount int `json:"views_count"`
	ViewCount  int `json:"view_count"`
}

func newDashboardPostResponse(post models.Post) dashboardPostResponse {
	views := post.Views
	if views < 0 {
		views = 0
	}
	return dashboardPostResponse{
		Post:       post,
		ViewsCount: views,
		ViewCount:  views,
	}
}

func newDashboardPostResponses(posts []models.Post) []dashboardPostResponse {
	items := make([]dashboardPostResponse, 0, len(posts))
	for _, post := range posts {
		items = append(items, newDashboardPostResponse(post))
	}
	return items
}

// @Failure 422 {object} utils.APIResponse
// @Failure 500 {object} utils.APIResponse
// DashboardList returns all posts for admins (including drafts/inactive).
// @Router /dashboard/posts [get]
func (h *Handler) DashboardList(c *fiber.Ctx) error {
	countryID, _ := c.Locals("country_id").(database.CountryID)
	pag := utils.GetPagination(c)

	var catID *uint
	if cidStr := c.Query("category_id"); cidStr != "" {
		if id, err := strconv.ParseUint(cidStr, 10, 64); err == nil {
			parsedID := uint(id)
			catID = &parsedID
		}
	}

	// Admins can pass is_active=0 to see drafts; omitting the param returns all posts.
	filter := &models.PostFilter{
		CategoryID: catID,
		Search:     c.Query("search"),
		Featured:   c.Query("featured"),
	}
	if raw := c.Query("is_active"); raw == "1" {
		t := true
		filter.IsActive = &t
	} else if raw == "0" {
		f := false
		filter.IsActive = &f
	}

	postList, total, err := h.svc.List(countryID, filter, pag.PerPage, pag.Offset)
	if err != nil {
		return utils.InternalError(c)
	}

	return utils.Paginated(c, "success", newDashboardPostResponses(postList), pag.BuildMeta(total))
}

// @Router /dashboard/posts [post]
func (h *Handler) DashboardCreate(c *fiber.Ctx) error {
	var req services.CreatePostRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.BadRequest(c, "بيانات غير صحيحة")
	}

	if errs := utils.Validate(req); errs != nil {
		return utils.ValidationError(c, errs)
	}

	countryID, _ := c.Locals("country_id").(database.CountryID)
	countryCode, _ := c.Locals("country_code").(string)
	user, _ := c.Locals("user").(*models.User)

	var userID *uint
	if user != nil {
		userID = &user.ID
	}

	// Handle image upload
	var imagePath string
	if img, err := c.FormFile("image"); err == nil {
		fileRepo := repositories.NewFileRepository()
		fileSvc := services.NewFileService(fileRepo)
		uploaded, err := fileSvc.UploadImage(img, "posts")
		if err == nil {
			imagePath = uploaded.Path
		}
	}

	post, err := h.svc.Create(countryID, countryCode, userID, &req, imagePath)
	if err != nil {
		return utils.InternalError(c, "فشل إنشاء المنشور")
	}

	// Handle attachments
	if form, err := c.MultipartForm(); err == nil {
		files := form.File["attachments[]"]
		files = append(files, form.File["attachments"]...)
		if len(files) > 0 {
			fileRepo := repositories.NewFileRepository()
			fileSvc := services.NewFileService(fileRepo)
			postID := post.ID
			fileCategory := "post_attachment"
			for _, file := range files {
				uploaded, uploadErr := fileSvc.UploadDocument(file, "posts/attachments")
				if uploadErr != nil {
					uploaded, uploadErr = fileSvc.UploadImage(file, "posts/attachments")
				}
				if uploadErr != nil {
					logger.Warn("post attachment upload failed",
						zap.String("filename", file.Filename),
						zap.Error(uploadErr),
					)
					continue
				}
				if _, recErr := fileSvc.CreateRecord(countryID, uploaded, nil, &postID, nil, &fileCategory); recErr != nil {
					logger.Warn("post attachment record creation failed",
						zap.String("path", uploaded.Path),
						zap.Error(recErr),
					)
				}
			}
		}
	}

	if h.notification != nil {
		includeIDs := []uint{}
		if userID != nil {
			includeIDs = append(includeIDs, *userID)
		}
		go func() {
			_ = h.notification.NotifyUsersWithPermissions(
				"App\\Notifications\\PostCreated",
				fmt.Sprintf("منشور جديد: %s", post.Title),
				fmt.Sprintf("تم إنشاء منشور جديد بعنوان \"%s\"", post.Title),
				fmt.Sprintf("/dashboard/posts/edit/%d", post.ID),
				[]string{"manage posts", "manage notifications"},
				includeIDs...,
			)
		}()
	}

	return utils.Created(c, "تم إنشاء المنشور بنجاح", post)
}

// DashboardUpdate updates a post
// @Summary Update Post
// @Description Update an existing post from the dashboard
// @Tags Posts
// @Accept json
// @Accept mpfd
// @Produce json
// @Security BearerAuth
// @Security FrontendKeyAuth
// @Param X-Country-Id header string false "Country ID"
// @Param id path int true "Post ID"
// @Param request body services.UpdatePostRequest true "Post data"
// @Success 200 {object} utils.APIResponse{data=models.Post}
// @Failure 400 {object} utils.APIResponse
// @Failure 403 {object} utils.APIResponse
// @Failure 404 {object} utils.APIResponse
// @Failure 500 {object} utils.APIResponse
// @Router /dashboard/posts/{id} [put]
func (h *Handler) DashboardUpdate(c *fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return utils.BadRequest(c, "معرف غير صحيح")
	}

	countryID, _ := c.Locals("country_id").(database.CountryID)
	caller, _ := c.Locals("user").(*models.User)

	var req services.UpdatePostRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.BadRequest(c, "بيانات غير صحيحة")
	}

	callerID := uint(0)
	if caller != nil {
		callerID = caller.ID
	}

	// Handle new_image upload if present
	if img, err := c.FormFile("new_image"); err == nil {
		fileRepo := repositories.NewFileRepository()
		fileSvc := services.NewFileService(fileRepo)
		uploaded, err := fileSvc.UploadImage(img, "posts")
		if err == nil {
			req.ImagePath = &uploaded.Path
		}
	}

	post, err := h.svc.Update(countryID, id, &req, callerID, isAdminUser(caller))
	if err != nil {
		switch err {
		case services.ErrNotFound:
			return utils.NotFound(c)
		case services.ErrForbidden:
			return utils.Forbidden(c)
		default:
			return utils.InternalError(c, "فشل تحديث المنشور")
		}
	}

	// Handle attachments
	if form, err := c.MultipartForm(); err == nil {
		files := form.File["attachments[]"]
		files = append(files, form.File["attachments"]...)
		if len(files) > 0 {
			fileRepo := repositories.NewFileRepository()
			fileSvc := services.NewFileService(fileRepo)
			postID := post.ID
			fileCategory := "post_attachment"
			for _, file := range files {
				uploaded, uploadErr := fileSvc.UploadDocument(file, "posts/attachments")
				if uploadErr != nil {
					uploaded, uploadErr = fileSvc.UploadImage(file, "posts/attachments")
				}
				if uploadErr != nil {
					logger.Warn("post attachment upload failed",
						zap.String("filename", file.Filename),
						zap.Error(uploadErr),
					)
					continue
				}
				if _, recErr := fileSvc.CreateRecord(countryID, uploaded, nil, &postID, nil, &fileCategory); recErr != nil {
					logger.Warn("post attachment record creation failed",
						zap.String("path", uploaded.Path),
						zap.Error(recErr),
					)
				}
			}
		}
	}

	if h.notification != nil {
		includeIDs := []uint{}
		if callerID > 0 {
			includeIDs = append(includeIDs, callerID)
		}
		if post.AuthorID != nil {
			includeIDs = append(includeIDs, *post.AuthorID)
		}
		go func() {
			_ = h.notification.NotifyUsersWithPermissions(
				"App\\Notifications\\PostUpdated",
				"تم تحديث منشور",
				fmt.Sprintf("تم تحديث المنشور: %s", post.Title),
				fmt.Sprintf("/dashboard/posts/edit/%d", post.ID),
				[]string{"manage posts", "manage notifications"},
				includeIDs...,
			)
		}()
	}

	return utils.Success(c, "تم تحديث المنشور بنجاح", post)
}

// DashboardToggleStatus toggles post active status
// @Summary Toggle Post Status
// @Description Toggle the is_active status of a post
// @Tags Posts
// @Produce json
// @Security BearerAuth
// @Security FrontendKeyAuth
// @Param X-Country-Id header string false "Country ID"
// @Param id path int true "Post ID"
// @Success 200 {object} utils.APIResponse{data=models.Post}
// @Failure 400 {object} utils.APIResponse
// @Failure 403 {object} utils.APIResponse
// @Failure 404 {object} utils.APIResponse
// @Failure 500 {object} utils.APIResponse
// @Router /dashboard/posts/{id}/toggle-status [post]
func (h *Handler) DashboardToggleStatus(c *fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return utils.BadRequest(c, "معرف غير صحيح")
	}

	countryID, _ := c.Locals("country_id").(database.CountryID)
	caller, _ := c.Locals("user").(*models.User)

	post, err := h.svc.GetByID(countryID, id)
	if err != nil {
		return utils.NotFound(c)
	}

	callerID := uint(0)
	if caller != nil {
		callerID = caller.ID
	}

	newStatus := !post.IsActive
	updatedPost, err := h.svc.Update(countryID, id, &services.UpdatePostRequest{
		IsActive: &newStatus,
	}, callerID, isAdminUser(caller))
	if err != nil {
		switch err {
		case services.ErrForbidden:
			return utils.Forbidden(c)
		default:
			return utils.InternalError(c, "فشل تحديث حالة المنشور")
		}
	}

	if h.notification != nil {
		includeIDs := []uint{}
		if callerID > 0 {
			includeIDs = append(includeIDs, callerID)
		}
		if updatedPost.AuthorID != nil {
			includeIDs = append(includeIDs, *updatedPost.AuthorID)
		}
		statusLabel := "تم إلغاء تفعيل"
		if newStatus {
			statusLabel = "تم تفعيل"
		}
		go func() {
			_ = h.notification.NotifyUsersWithPermissions(
				"App\\Notifications\\PostStatusChanged",
				"تغيير حالة منشور",
				fmt.Sprintf("%s المنشور: %s", statusLabel, updatedPost.Title),
				fmt.Sprintf("/dashboard/posts/edit/%d", updatedPost.ID),
				[]string{"manage posts", "manage notifications"},
				includeIDs...,
			)
		}()
	}

	return utils.Success(c, "تم تحديث حالة المنشور", updatedPost)
}

// DashboardDelete deletes a post
// @Summary Delete Post
// @Description Delete a post by ID
// @Tags Posts
// @Produce json
// @Security BearerAuth
// @Security FrontendKeyAuth
// @Param X-Country-Id header string false "Country ID"
// @Param id path int true "Post ID"
// @Success 200 {object} utils.APIResponse
// @Failure 400 {object} utils.APIResponse
// @Failure 403 {object} utils.APIResponse
// @Failure 404 {object} utils.APIResponse
// @Failure 500 {object} utils.APIResponse
// @Router /dashboard/posts/{id} [delete]
func (h *Handler) DashboardDelete(c *fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return utils.BadRequest(c, "معرف غير صحيح")
	}

	countryID, _ := c.Locals("country_id").(database.CountryID)
	caller, _ := c.Locals("user").(*models.User)

	callerID := uint(0)
	if caller != nil {
		callerID = caller.ID
	}

	post, _ := h.svc.GetByID(countryID, id)

	if err := h.svc.Delete(countryID, id, callerID, isAdminUser(caller)); err != nil {
		switch err {
		case services.ErrNotFound:
			return utils.NotFound(c)
		case services.ErrForbidden:
			return utils.Forbidden(c)
		default:
			return utils.InternalError(c, "فشل حذف المنشور")
		}
	}

	if h.notification != nil && post != nil {
		includeIDs := []uint{}
		if callerID > 0 {
			includeIDs = append(includeIDs, callerID)
		}
		if post.AuthorID != nil {
			includeIDs = append(includeIDs, *post.AuthorID)
		}
		go func(title string) {
			_ = h.notification.NotifyUsersWithPermissions(
				"App\\Notifications\\PostDeleted",
				"تم حذف منشور",
				fmt.Sprintf("تم حذف المنشور: %s", title),
				"/dashboard/posts",
				[]string{"manage posts", "manage notifications"},
				includeIDs...,
			)
		}(post.Title)
	}

	return utils.Success(c, "تم حذف المنشور بنجاح", nil)
}

// GetDownloadToken returns a short-lived signed token for a post file download.
func (h *Handler) GetDownloadToken(c *fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return utils.BadRequest(c, "معرف غير صحيح")
	}

	countryID, _ := c.Locals("country_id").(database.CountryID)

	token, err := h.svc.GetSignedDownloadToken(countryID, id)
	if err != nil {
		if err == services.ErrNotFound {
			return utils.NotFound(c)
		}
		return utils.InternalError(c)
	}

	return utils.Success(c, "success", fiber.Map{"token": token})
}

// DownloadFileSigned validates a signed token and serves the post file.
func (h *Handler) DownloadFileSigned(c *fiber.Ctx) error {
	token := c.Query("token")
	if token == "" {
		return utils.BadRequest(c, "رمز التحميل مطلوب")
	}

	file, absPath, err := h.svc.GetFileBySignedToken(token)
	if err != nil {
		return utils.Unauthorized(c, "رمز التحميل غير صالح أو منتهي الصلاحية")
	}

	disposition := mime.FormatMediaType("attachment", map[string]string{"filename": file.FileName})
	c.Set("Content-Disposition", disposition)
	c.Set("Content-Type", file.MimeType)
	return c.SendFile(absPath)
}
