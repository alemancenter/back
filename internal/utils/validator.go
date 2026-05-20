package utils

import (
	"reflect"
	"regexp"
	"strings"

	"github.com/go-playground/validator/v10"
	"github.com/microcosm-cc/bluemonday"
)

// richTextPolicy is a permissive bluemonday policy for WYSIWYG-editor content.
// It keeps standard formatting, tables, images, links, and media iframes while
// stripping scripts, event handlers, and other dangerous constructs.
var richTextPolicy = func() *bluemonday.Policy {
	p := bluemonday.UGCPolicy()
	// Allow iframes from trusted video/map hosts only
	p.AllowElements("iframe")
	p.AllowAttrs("src").Matching(regexp.MustCompile(
		`^https://(www\.)?(youtube(-nocookie)?\.com|player\.vimeo\.com|maps\.google\.com)/`,
	)).OnElements("iframe")
	p.AllowAttrs("width", "height", "frameborder", "allowfullscreen", "allow").OnElements("iframe")
	// Allow dir attribute for RTL text
	p.AllowAttrs("dir").OnElements("p", "div", "span", "h1", "h2", "h3", "h4", "h5", "h6", "td", "th")
	// Allow inline styles for editor-generated content (colour, font-size, etc.)
	p.AllowStyles("color", "background-color", "font-size", "font-weight", "text-align",
		"text-decoration", "font-style").Globally()
	return p
}()

// SanitizeHTML sanitizes rich HTML content from a WYSIWYG editor.
// It keeps safe formatting tags and attributes while stripping scripts and event handlers.
func SanitizeHTML(html string) string {
	return richTextPolicy.Sanitize(html)
}

var validate *validator.Validate

func init() {
	validate = validator.New()
	_ = validate.RegisterValidation("password", validatePassword)
	_ = validate.RegisterValidation("phone", validatePhone)
	validate.RegisterTagNameFunc(func(fld reflect.StructField) string {
		name := strings.SplitN(fld.Tag.Get("json"), ",", 2)[0]
		if name == "-" {
			return ""
		}
		return name
	})
}

// Validate validates a struct and returns a map of field errors
func Validate(s interface{}) map[string]string {
	err := validate.Struct(s)
	if err == nil {
		return nil
	}
	errors := make(map[string]string)
	for _, e := range err.(validator.ValidationErrors) {
		field := e.Field()
		errors[field] = translateError(e)
	}
	return errors
}

func translateError(e validator.FieldError) string {
	switch e.Tag() {
	case "required":
		return "هذا الحقل مطلوب"
	case "email":
		return "البريد الإلكتروني غير صحيح"
	case "min":
		return "القيمة أقل من الحد الأدنى المسموح: " + e.Param()
	case "max":
		return "القيمة تتجاوز الحد الأقصى المسموح: " + e.Param()
	case "unique":
		return "هذه القيمة مستخدمة بالفعل"
	case "oneof":
		return "القيمة يجب أن تكون إحدى: " + e.Param()
	case "url":
		return "الرابط غير صحيح"
	case "password":
		return "كلمة المرور يجب أن تحتوي على 8 أحرف على الأقل"
	case "phone":
		return "رقم الهاتف غير صحيح"
	default:
		return "قيمة غير صحيحة"
	}
}

func validatePassword(fl validator.FieldLevel) bool {
	return len(fl.Field().String()) >= 8
}

func validatePhone(fl validator.FieldLevel) bool {
	re := regexp.MustCompile(`^\+?[0-9]{7,15}$`)
	return re.MatchString(fl.Field().String())
}

// SanitizeInput strips HTML tags from user input.
// SQL injection is handled by GORM's parameterized queries; stripping SQL keywords
// here would silently corrupt legitimate user data (e.g. names like "Drop").
func SanitizeInput(input string) string {
	re := regexp.MustCompile(`<[^>]*>`)
	return strings.TrimSpace(re.ReplaceAllString(input, ""))
}

// SanitizeStruct sanitizes all string fields in a struct pointer (recursive).
// Handles: string, *string, []string, nested structs, and slices of structs.
func SanitizeStruct(s interface{}) {
	v := reflect.ValueOf(s)
	if v.Kind() != reflect.Ptr || v.IsNil() {
		return
	}
	v = v.Elem()
	if v.Kind() != reflect.Struct {
		return
	}
	sanitizeValue(v)
}

func sanitizeValue(v reflect.Value) {
	for i := 0; i < v.NumField(); i++ {
		field := v.Field(i)
		if !field.CanSet() {
			continue
		}
		switch field.Kind() {
		case reflect.String:
			field.SetString(SanitizeInput(field.String()))
		case reflect.Ptr:
			if !field.IsNil() && field.Elem().Kind() == reflect.String {
				field.Elem().SetString(SanitizeInput(field.Elem().String()))
			}
		case reflect.Struct:
			sanitizeValue(field)
		case reflect.Slice:
			for j := 0; j < field.Len(); j++ {
				elem := field.Index(j)
				switch elem.Kind() {
				case reflect.String:
					if elem.CanSet() {
						elem.SetString(SanitizeInput(elem.String()))
					}
				case reflect.Struct:
					sanitizeValue(elem)
				case reflect.Ptr:
					if !elem.IsNil() && elem.Elem().Kind() == reflect.Struct {
						sanitizeValue(elem.Elem())
					}
				}
			}
		}
	}
}

func SplitKeywords(keywordsStr string) []string {
	if keywordsStr == "" {
		return nil
	}
	// Normalise Arabic comma (،) and semicolon (؛) to a regular comma before splitting.
	normalised := strings.NewReplacer("،", ",", "؛", ",", ";", ",").Replace(keywordsStr)
	parts := strings.Split(normalised, ",")
	var result []string
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}
