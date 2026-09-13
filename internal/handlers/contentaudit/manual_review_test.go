package contentaudit

import (
	"github.com/imanjo/fiber-api/internal/database"
	"github.com/imanjo/fiber-api/internal/models"
	"testing"
)

func TestEditorialApplyRequiresContentPermissionAndMatchingCountry(t *testing.T) {
	preview := &models.ContentAIFixPreview{ContentType: "article", CountryCode: "jo"}
	auditor := &models.User{ID: 7, Permissions: []models.Permission{{Name: "manage content audit"}}}
	if canApplyEditorialPreview(auditor, database.CountryJordan, preview) {
		t.Fatal("audit access must not allow article writes")
	}
	editor := &models.User{ID: 8, Permissions: []models.Permission{{Name: "manage articles"}}}
	if !canApplyEditorialPreview(editor, database.CountryJordan, preview) {
		t.Fatal("article editor should be able to approve article")
	}
	if canApplyEditorialPreview(editor, database.CountrySaudi, preview) {
		t.Fatal("cross-country approval must fail")
	}
	preview.ContentType = "post"
	if canApplyEditorialPreview(editor, database.CountryJordan, preview) {
		t.Fatal("article permission must not grant post writes")
	}
	if canApplyEditorialPreview(nil, database.CountryJordan, preview) {
		t.Fatal("anonymous approval must fail")
	}
}
