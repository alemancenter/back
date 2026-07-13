package roles

import (
	"strconv"

	"github.com/alemancenter/fiber-api/internal/models"
	"github.com/alemancenter/fiber-api/internal/services"
	"github.com/alemancenter/fiber-api/internal/utils"
	"github.com/gofiber/fiber/v2"
)

func callerID(c *fiber.Ctx) uint {
	if u, ok := c.Locals("user").(*models.User); ok && u != nil {
		return u.ID
	}
	return 0
}

// Handler contains roles and permissions route handlers
type Handler struct {
	svc services.RoleService
}

// New creates a new roles Handler
func New(svc services.RoleService) *Handler {
	return &Handler{
		svc: svc,
	}
}

// ListRoles returns all roles
// @Summary List Roles
// @Description Returns a list of all roles
// @Tags Roles & Permissions
// @Produce json
// @Security BearerAuth
// @Security FrontendKeyAuth
// @Success 200 {object} utils.APIResponse{data=[]models.Role}
// @Failure 500 {object} utils.APIResponse
// @Router /dashboard/roles [get]
func (h *Handler) ListRoles(c *fiber.Ctx) error {
	roles, err := h.svc.ListRoles()
	if err != nil {
		return utils.InternalError(c)
	}
	return utils.Success(c, "success", roles)
}

// GetRole returns a single role
// @Summary Get Role
// @Description Get role details by ID
// @Tags Roles & Permissions
// @Produce json
// @Security BearerAuth
// @Security FrontendKeyAuth
// @Param id path int true "Role ID"
// @Success 200 {object} utils.APIResponse{data=models.Role}
// @Failure 400 {object} utils.APIResponse
// @Failure 404 {object} utils.APIResponse
// @Router /dashboard/roles/{id} [get]
func (h *Handler) GetRole(c *fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return utils.BadRequest(c, "معرف غير صحيح")
	}

	role, err := h.svc.GetRole(id)
	if err != nil {
		return utils.NotFound(c)
	}

	return utils.Success(c, "success", role)
}

type CreateRoleRequest struct {
	Name        string `json:"name" validate:"required,min=2,max=125"`
	Permissions []uint `json:"permissions"`
}

// CreateRole creates a new role
// @Summary Create Role
// @Description Create a new role with associated permissions
// @Tags Roles & Permissions
// @Accept json
// @Produce json
// @Security BearerAuth
// @Security FrontendKeyAuth
// @Param request body CreateRoleRequest true "Role data"
// @Success 201 {object} utils.APIResponse{data=models.Role}
// @Failure 400 {object} utils.APIResponse
// @Failure 422 {object} utils.APIResponse
// @Failure 500 {object} utils.APIResponse
// @Router /dashboard/roles [post]
func (h *Handler) CreateRole(c *fiber.Ctx) error {
	var req CreateRoleRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.BadRequest(c, "بيانات غير صحيحة")
	}

	if errs := utils.Validate(req); errs != nil {
		return utils.ValidationError(c, errs)
	}

	role, err := h.svc.CreateRole(callerID(c), req.Name, req.Permissions)
	if err != nil {
		if err == services.ErrForbidden {
			return utils.Forbidden(c, "هذه العملية تتطلب صلاحية Super Admin")
		}
		if err.Error() == "اسم الدور مستخدم بالفعل" {
			return utils.ValidationError(c, map[string]string{"name": err.Error()})
		}
		return utils.InternalError(c, "فشل إنشاء الدور")
	}

	return utils.Created(c, "تم إنشاء الدور بنجاح", role)
}

type UpdateRoleRequest struct {
	Name        string `json:"name" validate:"omitempty,min=2,max=125"`
	Permissions []uint `json:"permissions"`
}

// UpdateRole updates a role
// @Summary Update Role
// @Description Update an existing role
// @Tags Roles & Permissions
// @Accept json
// @Produce json
// @Security BearerAuth
// @Security FrontendKeyAuth
// @Param id path int true "Role ID"
// @Param request body UpdateRoleRequest true "Role update data"
// @Success 200 {object} utils.APIResponse{data=models.Role}
// @Failure 400 {object} utils.APIResponse
// @Failure 404 {object} utils.APIResponse
// @Router /dashboard/roles/{id} [put]
func (h *Handler) UpdateRole(c *fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return utils.BadRequest(c, "معرف غير صحيح")
	}

	var req UpdateRoleRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.BadRequest(c, "بيانات غير صحيحة")
	}

	role, err := h.svc.UpdateRole(callerID(c), id, req.Name, req.Permissions)
	if err != nil {
		switch err {
		case services.ErrForbidden:
			return utils.Forbidden(c, "هذه العملية تتطلب صلاحية Super Admin")
		case services.ErrNotFound:
			return utils.NotFound(c)
		default:
			return utils.InternalError(c, "فشل تحديث الدور")
		}
	}

	return utils.Success(c, "تم تحديث الدور بنجاح", role)
}

// DeleteRole deletes a role
// @Summary Delete Role
// @Description Delete a role by ID
// @Tags Roles & Permissions
// @Produce json
// @Security BearerAuth
// @Security FrontendKeyAuth
// @Param id path int true "Role ID"
// @Success 200 {object} utils.APIResponse
// @Failure 400 {object} utils.APIResponse
// @Failure 500 {object} utils.APIResponse
// @Router /dashboard/roles/{id} [delete]
func (h *Handler) DeleteRole(c *fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return utils.BadRequest(c, "معرف غير صحيح")
	}

	if err := h.svc.DeleteRole(callerID(c), id); err != nil {
		if err == services.ErrForbidden {
			return utils.Forbidden(c, "هذه العملية تتطلب صلاحية Super Admin")
		}
		return utils.InternalError(c)
	}

	return utils.Success(c, "تم حذف الدور بنجاح", nil)
}

// ListPermissions returns all permissions
// @Summary List Permissions
// @Description Returns a list of all permissions
// @Tags Roles & Permissions
// @Produce json
// @Security BearerAuth
// @Security FrontendKeyAuth
// @Success 200 {object} utils.APIResponse{data=[]models.Permission}
// @Failure 500 {object} utils.APIResponse
// @Router /dashboard/permissions [get]
func (h *Handler) ListPermissions(c *fiber.Ctx) error {
	permissions, err := h.svc.ListPermissions()
	if err != nil {
		return utils.InternalError(c)
	}
	return utils.Success(c, "success", permissions)
}

type CreatePermissionRequest struct {
	Name string `json:"name" validate:"required,min=2,max=125"`
}

// CreatePermission creates a new permission
// @Summary Create Permission
// @Description Create a new system permission
// @Tags Roles & Permissions
// @Accept json
// @Produce json
// @Security BearerAuth
// @Security FrontendKeyAuth
// @Param request body CreatePermissionRequest true "Permission data"
// @Success 201 {object} utils.APIResponse{data=models.Permission}
// @Failure 400 {object} utils.APIResponse
// @Failure 422 {object} utils.APIResponse
// @Failure 500 {object} utils.APIResponse
// @Router /dashboard/permissions [post]
func (h *Handler) CreatePermission(c *fiber.Ctx) error {
	var req CreatePermissionRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.BadRequest(c, "بيانات غير صحيحة")
	}

	if errs := utils.Validate(req); errs != nil {
		return utils.ValidationError(c, errs)
	}

	permission, err := h.svc.CreatePermission(callerID(c), req.Name)
	if err != nil {
		if err == services.ErrForbidden {
			return utils.Forbidden(c, "هذه العملية تتطلب صلاحية Super Admin")
		}
		return utils.InternalError(c, "فشل إنشاء الصلاحية")
	}

	return utils.Created(c, "تم إنشاء الصلاحية بنجاح", permission)
}

type UpdatePermissionRequest struct {
	Name string `json:"name" validate:"required,min=2,max=125"`
}

// UpdatePermission updates a permission
// @Summary Update Permission
// @Description Update an existing permission by ID
// @Tags Roles & Permissions
// @Accept json
// @Produce json
// @Security BearerAuth
// @Security FrontendKeyAuth
// @Param id path int true "Permission ID"
// @Param request body UpdatePermissionRequest true "Permission update data"
// @Success 200 {object} utils.APIResponse
// @Failure 400 {object} utils.APIResponse
// @Failure 500 {object} utils.APIResponse
// @Router /dashboard/permissions/{id} [put]
func (h *Handler) UpdatePermission(c *fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return utils.BadRequest(c, "معرف غير صحيح")
	}

	var req UpdatePermissionRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.BadRequest(c, "بيانات غير صحيحة")
	}

	if err := h.svc.UpdatePermission(callerID(c), id, req.Name); err != nil {
		if err == services.ErrForbidden {
			return utils.Forbidden(c, "هذه العملية تتطلب صلاحية Super Admin")
		}
		return utils.InternalError(c, "فشل تحديث الصلاحية")
	}

	return utils.Success(c, "تم تحديث الصلاحية بنجاح", nil)
}

// DeletePermission deletes a permission
// @Summary Delete Permission
// @Description Delete a permission by ID
// @Tags Roles & Permissions
// @Produce json
// @Security BearerAuth
// @Security FrontendKeyAuth
// @Param id path int true "Permission ID"
// @Success 200 {object} utils.APIResponse
// @Failure 400 {object} utils.APIResponse
// @Failure 500 {object} utils.APIResponse
// @Router /dashboard/permissions/{id} [delete]
func (h *Handler) DeletePermission(c *fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return utils.BadRequest(c, "معرف غير صحيح")
	}

	if err := h.svc.DeletePermission(callerID(c), id); err != nil {
		if err == services.ErrForbidden {
			return utils.Forbidden(c, "هذه العملية تتطلب صلاحية Super Admin")
		}
		return utils.InternalError(c, "فشل حذف الصلاحية")
	}

	return utils.Success(c, "تم حذف الصلاحية بنجاح", nil)
}
