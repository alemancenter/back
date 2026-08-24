package contentaudit

import (
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/imanjo/fiber-api/internal/contentquality"
	auditservice "github.com/imanjo/fiber-api/internal/services/contentaudit"
)

const (
	readinessProblemUnaudited        = "unaudited"
	readinessProblemPolicyBlocked    = "policy_blocked"
	readinessProblemAdsNotEligible   = "ads_not_eligible"
	readinessProblemThinContent      = "thin_content"
	readinessProblemNeedsEnrichment  = "needs_enrichment"
	readinessProblemMetaDescription  = "meta_description"
	readinessProblemShortTitle       = "short_title"
	readinessProblemUnpublished      = "unpublished"
)

type readinessProblemDefinition struct {
	Code          string
	Label         string
	Description   string
	Severity      string
	ActionType    string
	Preset        string
	Mode          string
	ModelStrategy string
	Priority      int
}

type readinessItemProblem struct {
	Code       string `json:"code"`
	Label      string `json:"label"`
	Message    string `json:"message"`
	Severity   string `json:"severity"`
	ActionType string `json:"action_type"`
	Preset     string `json:"preset,omitempty"`
}

type readinessProblemSummary struct {
	Code          string `json:"code"`
	Label         string `json:"label"`
	Description   string `json:"description"`
	Severity      string `json:"severity"`
	ActionType    string `json:"action_type"`
	Preset        string `json:"preset,omitempty"`
	Mode          string `json:"mode,omitempty"`
	ModelStrategy string `json:"model_strategy,omitempty"`
	Count         int    `json:"count"`
	Priority      int    `json:"-"`
}

type readinessRepairCenter struct {
	AffectedItems    int                       `json:"affected_items"`
	ActionableItems  int                       `json:"actionable_items"`
	ManualItems      int                       `json:"manual_items"`
	TotalFindings    int                       `json:"total_findings"`
	RecommendedCode string                    `json:"recommended_code,omitempty"`
	BatchSize        int                       `json:"batch_size"`
	Problems         []readinessProblemSummary `json:"problems"`
}

type readinessRepairCollector struct {
	counts          map[string]int
	affectedItems   int
	actionableItems int
	manualItems     int
}

var readinessProblems = map[string]readinessProblemDefinition{
		readinessProblemUnaudited: {
			Code: readinessProblemUnaudited, Label: "محتوى لم يُفحص بعد",
			Description: "تشغيل تدقيق الجودة أولًا؛ تظل الإعلانات متوقفة حتى وجود قرار محفوظ.",
			Severity: "high", ActionType: "analyze", Preset: readinessProblemUnaudited,
			Mode: "analyze_only", ModelStrategy: "balanced", Priority: 90,
		},
		readinessProblemPolicyBlocked: {
			Code: readinessProblemPolicyBlocked, Label: "حظر فهرسة أو سياسة",
			Description: "محتوى يحتاج مراجعة نهائية لأنه مرفوض أو يحمل مخاطرة حرجة.",
			Severity: "critical", ActionType: "full_review", Preset: readinessProblemPolicyBlocked,
			Mode: "full_review", ModelStrategy: "final_review", Priority: 100,
		},
		readinessProblemAdsNotEligible: {
			Code: readinessProblemAdsNotEligible, Label: "مفهرس وغير مؤهل للإعلانات",
			Description: "يوجد قرار تدقيق، لكن المحتوى لم يجتز بوابة أهلية الإعلانات بعد.",
			Severity: "high", ActionType: "ai_preview", Preset: readinessProblemAdsNotEligible,
			Mode: "fix_preview", ModelStrategy: "quality", Priority: 85,
		},
		readinessProblemThinContent: {
			Code: readinessProblemThinContent, Label: "محتوى قصير جدًا",
			Description: "أقل من 120 كلمة ويحتاج إثراءً تحريريًا حقيقيًا ومعاينة بشرية.",
			Severity: "high", ActionType: "ai_preview", Preset: readinessProblemThinContent,
			Mode: "fix_preview", ModelStrategy: "quality", Priority: 80,
		},
		readinessProblemNeedsEnrichment: {
			Code: readinessProblemNeedsEnrichment, Label: "محتوى يحتاج إثراء",
			Description: "بين 120 و299 كلمة؛ يحتاج بنية وشرحًا وقيمة تعليمية أعمق.",
			Severity: "medium", ActionType: "ai_preview", Preset: readinessProblemNeedsEnrichment,
			Mode: "fix_preview", ModelStrategy: "quality", Priority: 65,
		},
		readinessProblemMetaDescription: {
			Code: readinessProblemMetaDescription, Label: "وصف تعريفي ناقص أو قصير",
			Description: "إنشاء وصف دقيق من مضمون الصفحة بدل نص عام أو حشو كلمات مفتاحية.",
			Severity: "medium", ActionType: "ai_preview", Preset: readinessProblemMetaDescription,
			Mode: "fix_preview", ModelStrategy: "balanced", Priority: 60,
		},
		readinessProblemShortTitle: {
			Code: readinessProblemShortTitle, Label: "عنوان يحتاج مراجعة",
			Description: "العنوان أقصر من الحد التحريري الداخلي ولا يوضح غرض الصفحة جيدًا.",
			Severity: "medium", ActionType: "ai_preview", Preset: readinessProblemShortTitle,
			Mode: "fix_preview", ModelStrategy: "balanced", Priority: 55,
		},
		readinessProblemUnpublished: {
			Code: readinessProblemUnpublished, Label: "غير منشور أو غير فعال",
			Description: "راجع حالة النشر يدويًا؛ المحتوى غير المنشور لا يدخل مسار الإعلانات.",
			Severity: "low", ActionType: "manual", Priority: 10,
		},
}

func readinessProblemCatalog() map[string]readinessProblemDefinition { return readinessProblems }

func newReadinessRepairCollector() *readinessRepairCollector {
	return &readinessRepairCollector{counts: make(map[string]int)}
}

func classifyReadinessProblems(title, meta string, diagnostics contentquality.Diagnostics, published bool, gate auditservice.ContentQualityGate) []readinessItemProblem {
	catalog := readinessProblemCatalog()
	codes := make([]string, 0, 7)
	messages := make(map[string]string, 7)
	add := func(code, message string) {
		for _, existing := range codes {
			if existing == code {
				return
			}
		}
		codes = append(codes, code)
		messages[code] = message
	}

	if !gate.Audited {
		add(readinessProblemUnaudited, "لم يخضع المحتوى لتدقيق الجودة؛ ابدأ بالتحليل قبل إنشاء أي إصلاح.")
	} else if !gate.Indexable {
		add(readinessProblemPolicyBlocked, "بوابة الجودة تمنع الفهرسة والإعلانات حتى معالجة سبب الرفض أو المخاطرة الحرجة.")
	} else if !gate.AdsEligible {
		add(readinessProblemAdsNotEligible, "المحتوى مفهرس لكنه لم يستوفِ شروط الاعتماد الداخلية لعرض الإعلانات.")
	}

	if diagnostics.WordCount < contentquality.DiagnosticReviewMinWords {
		add(readinessProblemThinContent, "المحتوى يحتوي على أقل من 120 كلمة ويحتاج مراجعة وإثراءً حقيقيًا.")
	} else if diagnostics.WordCount < contentquality.DiagnosticStrongMinWords {
		add(readinessProblemNeedsEnrichment, "عمق المحتوى متوسط؛ راجع الشرح والبنية والفائدة التعليمية قبل الاعتماد.")
	}
	if utf8.RuneCountInString(strings.TrimSpace(meta)) < contentquality.DiagnosticMetaMinChars {
		add(readinessProblemMetaDescription, "الوصف التعريفي مفقود أو أقصر من 80 حرفًا.")
	}
	if utf8.RuneCountInString(strings.TrimSpace(title)) < contentquality.DiagnosticTitleMinChars {
		add(readinessProblemShortTitle, "العنوان قصير ولا يوضح موضوع الصفحة بالقدر الكافي.")
	}
	if !published {
		add(readinessProblemUnpublished, "العنصر غير منشور أو غير فعال، لذلك لا يمكن فهرسته أو عرض الإعلانات عليه.")
	}

	problems := make([]readinessItemProblem, 0, len(codes))
	for _, code := range codes {
		definition := catalog[code]
		problems = append(problems, readinessItemProblem{
			Code: code, Label: definition.Label, Message: messages[code], Severity: definition.Severity,
			ActionType: definition.ActionType, Preset: definition.Preset,
		})
	}
	sort.SliceStable(problems, func(i, j int) bool {
		return catalog[problems[i].Code].Priority > catalog[problems[j].Code].Priority
	})
	return problems
}

func (c *readinessRepairCollector) Add(item unifiedReadinessItem) {
	if c == nil || len(item.Problems) == 0 {
		return
	}
	c.affectedItems++
	actionable := false
	manual := false
	for _, problem := range item.Problems {
		c.counts[problem.Code]++
		if problem.ActionType == "manual" {
			manual = true
		} else {
			actionable = true
		}
	}
	if actionable {
		c.actionableItems++
	}
	if manual {
		c.manualItems++
	}
}

func (c *readinessRepairCollector) Build() readinessRepairCenter {
	result := readinessRepairCenter{BatchSize: 20, Problems: make([]readinessProblemSummary, 0)}
	if c == nil {
		return result
	}
	result.AffectedItems = c.affectedItems
	result.ActionableItems = c.actionableItems
	result.ManualItems = c.manualItems
	catalog := readinessProblemCatalog()
	for code, count := range c.counts {
		definition, ok := catalog[code]
		if !ok || count <= 0 {
			continue
		}
		result.TotalFindings += count
		result.Problems = append(result.Problems, readinessProblemSummary{
			Code: definition.Code, Label: definition.Label, Description: definition.Description,
			Severity: definition.Severity, ActionType: definition.ActionType, Preset: definition.Preset,
			Mode: definition.Mode, ModelStrategy: definition.ModelStrategy, Count: count, Priority: definition.Priority,
		})
	}
	sort.SliceStable(result.Problems, func(i, j int) bool {
		if result.Problems[i].Priority == result.Problems[j].Priority {
			return result.Problems[i].Count > result.Problems[j].Count
		}
		return result.Problems[i].Priority > result.Problems[j].Priority
	})
	for _, problem := range result.Problems {
		if problem.ActionType != "manual" {
			result.RecommendedCode = problem.Code
			break
		}
	}
	return result
}

func hasReadinessProblem(item unifiedReadinessItem, code string) bool {
	if strings.TrimSpace(code) == "" {
		return true
	}
	for _, problem := range item.Problems {
		if problem.Code == code {
			return true
		}
	}
	return false
}
