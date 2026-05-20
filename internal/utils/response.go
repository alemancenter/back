package utils

import (
	"reflect"

	"github.com/gofiber/fiber/v2"
)

// normalizeData converts a nil slice to an empty slice so JSON encodes it as []
// instead of null — prevents frontend .filter()/.map() crashes on empty lists.
func normalizeData(v interface{}) interface{} {
	if v == nil {
		return v
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Slice && rv.IsNil() {
		return reflect.MakeSlice(rv.Type(), 0, 0).Interface()
	}
	return v
}

// APIResponse is the standard JSON response envelope
type APIResponse struct {
	Success    bool            `json:"success"`
	Message    string          `json:"message"`
	Data       interface{}     `json:"data,omitempty"`
	Errors     interface{}     `json:"errors,omitempty"`
	Token      *string         `json:"token,omitempty"`
	Meta       *PaginationMeta `json:"meta,omitempty"`
	Pagination *PaginationMeta `json:"pagination,omitempty"`
}

// PaginationMeta contains pagination metadata
type PaginationMeta struct {
	CurrentPage int   `json:"current_page"`
	PerPage     int   `json:"per_page"`
	Total       int64 `json:"total"`
	LastPage    int   `json:"last_page"`
	From        int   `json:"from"`
	To          int   `json:"to"`
}

// Success sends a 200 success response
func Success(c *fiber.Ctx, message string, data interface{}) error {
	return c.Status(fiber.StatusOK).JSON(APIResponse{
		Success: true,
		Message: message,
		Data:    data,
	})
}

// Created sends a 201 created response
func Created(c *fiber.Ctx, message string, data interface{}) error {
	return c.Status(fiber.StatusCreated).JSON(APIResponse{
		Success: true,
		Message: message,
		Data:    data,
	})
}

// WithToken sends a success response with an auth token
func WithToken(c *fiber.Ctx, message string, data interface{}, token string) error {
	return c.Status(fiber.StatusOK).JSON(APIResponse{
		Success: true,
		Message: message,
		Data:    data,
		Token:   &token,
	})
}

// Paginated sends a paginated success response
func Paginated(c *fiber.Ctx, message string, data interface{}, meta PaginationMeta) error {
	return c.Status(fiber.StatusOK).JSON(APIResponse{
		Success:    true,
		Message:    message,
		Data:       normalizeData(data),
		Meta:       &meta,
		Pagination: &meta,
	})
}

// NoContent sends a 204 no content response
func NoContent(c *fiber.Ctx) error {
	return c.SendStatus(fiber.StatusNoContent)
}

// BadRequest sends a 400 bad request response
func BadRequest(c *fiber.Ctx, message string) error {
	return c.Status(fiber.StatusBadRequest).JSON(APIResponse{
		Success: false,
		Message: message,
	})
}

// Unauthorized sends a 401 unauthorized response
func Unauthorized(c *fiber.Ctx, message ...string) error {
	msg := "غير مصرح بالوصول"
	if len(message) > 0 {
		msg = message[0]
	}
	return c.Status(fiber.StatusUnauthorized).JSON(APIResponse{
		Success: false,
		Message: msg,
	})
}

// Forbidden sends a 403 forbidden response
func Forbidden(c *fiber.Ctx, message ...string) error {
	msg := "ليس لديك صلاحية للوصول لهذا المورد"
	if len(message) > 0 {
		msg = message[0]
	}
	return c.Status(fiber.StatusForbidden).JSON(APIResponse{
		Success: false,
		Message: msg,
	})
}

// NotFound sends a 404 not found response
func NotFound(c *fiber.Ctx, message ...string) error {
	msg := "المورد المطلوب غير موجود"
	if len(message) > 0 {
		msg = message[0]
	}
	return c.Status(fiber.StatusNotFound).JSON(APIResponse{
		Success: false,
		Message: msg,
	})
}

// ValidationError sends a 422 validation error response
func ValidationError(c *fiber.Ctx, errors interface{}) error {
	return c.Status(fiber.StatusUnprocessableEntity).JSON(APIResponse{
		Success: false,
		Message: "بيانات غير صحيحة",
		Errors:  errors,
	})
}

// TooManyRequests sends a 429 rate limit exceeded response
func TooManyRequests(c *fiber.Ctx) error {
	return c.Status(fiber.StatusTooManyRequests).JSON(APIResponse{
		Success: false,
		Message: "تم تجاوز الحد المسموح للطلبات",
	})
}

// InternalError sends a 500 internal server error response
func InternalError(c *fiber.Ctx, message ...string) error {
	msg := "حدث خطأ داخلي في الخادم"
	if len(message) > 0 {
		msg = message[0]
	}
	return c.Status(fiber.StatusInternalServerError).JSON(APIResponse{
		Success: false,
		Message: msg,
	})
}
