package ai

import "testing"

func TestBuildGenerationContextResolvesInternalSchoolClassID(t *testing.T) {
	req := GenerateRequest{
		CountryCode: "jo",
		GradeLevel:  "12",
		SubjectName: "التربية الإسلامية",
	}
	ctx := buildGenerationContext(req, func(countryCode string, schoolClassID uint) string {
		if countryCode != "jo" {
			t.Fatalf("unexpected country code: %s", countryCode)
		}
		if schoolClassID != 12 {
			t.Fatalf("unexpected school class id: %d", schoolClassID)
		}
		return "الصف الأول الثانوي"
	})

	if ctx.GradeName != "الصف الأول الثانوي" {
		t.Fatalf("expected semantic grade name, got %q", ctx.GradeName)
	}
	if ctx.GradeLevel != "" {
		t.Fatalf("internal numeric grade id must never cross AI boundary: %q", ctx.GradeLevel)
	}
}

func TestBuildGenerationContextPrefersExplicitSemanticGradeName(t *testing.T) {
	called := false
	ctx := buildGenerationContext(GenerateRequest{
		CountryCode: "jo",
		GradeLevel:  "12",
		GradeName:   "الصف الأول الثانوي",
	}, func(string, uint) string {
		called = true
		return "wrong"
	})

	if called {
		t.Fatal("lookup must not run when an explicit semantic grade name is available")
	}
	if ctx.GradeName != "الصف الأول الثانوي" || ctx.GradeLevel != "" {
		t.Fatalf("unexpected context: %+v", ctx)
	}
}

func TestBuildGenerationContextTreatsNumericGradeNameAsInternal(t *testing.T) {
	ctx := buildGenerationContext(GenerateRequest{
		Country:    "1",
		GradeName:  "12",
		GradeLevel: "12",
	}, func(countryCode string, schoolClassID uint) string {
		if countryCode != "jo" || schoolClassID != 12 {
			t.Fatalf("unexpected lookup: country=%s id=%d", countryCode, schoolClassID)
		}
		return "الصف الأول الثانوي"
	})

	if ctx.GradeName != "الصف الأول الثانوي" {
		t.Fatalf("numeric grade_name must be resolved as an internal id, got %q", ctx.GradeName)
	}
}

func TestBuildGenerationContextFailsSafeWhenInternalIDCannotResolve(t *testing.T) {
	ctx := buildGenerationContext(GenerateRequest{
		CountryCode: "jo",
		GradeLevel:  "12",
	}, func(string, uint) string { return "" })

	if ctx.GradeName != "" || ctx.GradeLevel != "" {
		t.Fatalf("unresolved internal id must be omitted, not interpreted as grade 12: %+v", ctx)
	}
}

func TestBuildGenerationContextKeepsLegacyTextualGradeLabel(t *testing.T) {
	ctx := buildGenerationContext(GenerateRequest{
		CountryCode: "jo",
		GradeLevel:  "الصف السابع",
	}, nil)

	if ctx.GradeName != "الصف السابع" {
		t.Fatalf("legacy textual grade label should remain semantic, got %q", ctx.GradeName)
	}
	if ctx.GradeLevel != "" {
		t.Fatalf("grade_level must remain empty at AI boundary, got %q", ctx.GradeLevel)
	}
}
