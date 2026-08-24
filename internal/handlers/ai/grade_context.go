package ai

import (
	"strconv"
	"strings"

	"github.com/imanjo/fiber-api/internal/database"
	"github.com/imanjo/fiber-api/internal/models"
	"github.com/imanjo/fiber-api/internal/services"
)

// schoolClassNameLookup is injected in tests so the semantic-grade rules can be
// verified without opening a database connection.
type schoolClassNameLookup func(countryCode string, schoolClassID uint) string

// buildGenerationContext converts dashboard taxonomy values into AI-safe semantic
// context. In the ImanJo schema grade_level on subjects/semesters/articles is an
// internal school_classes identifier/order value, not the human academic grade.
// Therefore a numeric value such as "12" must never be exposed to the model as
// "grade 12". We resolve it to school_classes.grade_name first; if resolution
// fails we omit the grade rather than inventing a semantic meaning for the ID.
func buildGenerationContext(req GenerateRequest, lookup schoolClassNameLookup) services.SEOGenerationContext {
	countryCode := normalizeAIGradeCountry(firstNonEmpty(req.CountryCode, req.Country))
	gradeName := semanticGradeName(req.GradeName, req.GradeLevel, countryCode, lookup)

	return services.SEOGenerationContext{
		CountryCode:       countryCode,
		// GradeLevel intentionally stays empty on the AI boundary. Numeric values in
		// this project are internal school-class IDs/order positions, not grade numbers.
		GradeLevel:        "",
		GradeName:         gradeName,
		SubjectID:         strings.TrimSpace(req.SubjectID),
		SubjectName:       strings.TrimSpace(req.SubjectName),
		SemesterID:        strings.TrimSpace(req.SemesterID),
		SemesterName:      strings.TrimSpace(req.SemesterName),
		CategoryID:        strings.TrimSpace(req.CategoryID),
		CategoryName:      strings.TrimSpace(req.CategoryName),
		CurriculumContext: strings.TrimSpace(req.CurriculumContext),
	}
}

func semanticGradeName(explicitName, rawGradeLevel, countryCode string, lookup schoolClassNameLookup) string {
	explicitName = strings.TrimSpace(explicitName)
	if explicitName != "" && !isInternalNumericGradeToken(explicitName) {
		return explicitName
	}

	rawGradeLevel = strings.TrimSpace(rawGradeLevel)
	if rawGradeLevel == "" {
		return ""
	}

	id, err := strconv.ParseUint(rawGradeLevel, 10, 64)
	if err != nil || id == 0 {
		// Backward compatibility for legacy callers that used grade_level for a real
		// textual label such as "الصف السابع". Only numeric tokens are considered IDs.
		return rawGradeLevel
	}

	if lookup == nil {
		return ""
	}
	name := strings.TrimSpace(lookup(normalizeAIGradeCountry(countryCode), uint(id)))
	if name == "" || isInternalNumericGradeToken(name) {
		return ""
	}
	return name
}

func isInternalNumericGradeToken(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	_, err := strconv.ParseUint(value, 10, 64)
	return err == nil
}

func normalizeAIGradeCountry(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "jo", "", "1":
		return "jo"
	case "sa", "2":
		return "sa"
	case "eg", "3":
		return "eg"
	case "ps", "4":
		return "ps"
	default:
		return "jo"
	}
}

func lookupSchoolClassGradeName(countryCode string, schoolClassID uint) string {
	if schoolClassID == 0 {
		return ""
	}
	db := database.GetManager().GetByCode(normalizeAIGradeCountry(countryCode))
	if db == nil {
		return ""
	}
	var schoolClass models.SchoolClass
	if err := db.Select("id", "grade_name").First(&schoolClass, schoolClassID).Error; err != nil {
		return ""
	}
	return strings.TrimSpace(schoolClass.GradeName)
}
