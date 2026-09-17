package contentquality

import "testing"

// Regression test for the exact generation the user reported: a model reliably falls back to
// generic filler at the opening ("يُعد الاختبار... محطة مهمة لقياس...") and closing ("لا يقل دور
// الأسرة عن دور المدرسة... يخفف من التوتر... انعكاسًا صادقًا لجهد سنة كاملة") of a draft despite
// the prompt banning it outright — this deterministic scan is the backstop for when the prompt
// instruction alone isn't followed, whether that draft is a brand-new AI generation or already
// published content the adsense-policy scan is checking.
func TestDetectGenericFillerPhrases_CatchesReportedSample(t *testing.T) {
	sample := `يُعد الاختبار النهائي لمادة اللغة الإنجليزية في الصف السادس الابتدائي للفصل الدراسي الثاني محطة مهمة لقياس مدى تقدم التلميذ في المهارات اللغوية الأساسية التي تراكمت لديه طوال العام.

ولا يقل دور الأسرة عن دور المدرسة في هذه المرحلة؛ فتشجيع الطالب على المراجعة اليومية بانتظام بدلًا من الحفظ المكثف في ليلة الاختبار يخفف من التوتر ويرفع التركيز. ومع ثقة التلميذ بنفسه، يصبح الاختبار النهائي فرصة لإظهار ما تعلمه لا مصدر قلق، وتكون نتيجته انعكاسًا صادقًا لجهد سنة كاملة من التعلم.`

	found := DetectGenericFillerPhrases(sample)
	if len(found) == 0 {
		t.Fatal("expected at least one generic-filler phrase to be detected in the reported sample")
	}
}

// Regression test for a real miss found while investigating the "AI fix does nothing" report:
// the actual article text read "وتكمن أهميته في تحويل التقويم..." — the possessive-suffixed form
// ("أهميته"/"أهميتها"), not the bare noun "أهمية" the phrase list used to require verbatim.
// NormalizeForSimilarity does not strip possessive suffixes, so the phrase must be a short enough
// stem ("تكمن اهمي") to match both forms.
func TestDetectGenericFillerPhrases_CatchesPossessiveSuffixedForm(t *testing.T) {
	sample := "وتكمن أهميته في تحويل التقويم من مجرد رصد درجات إلى أداة تطوير مستمر لأداء الطالب."
	found := DetectGenericFillerPhrases(sample)
	if len(found) == 0 {
		t.Fatal("expected the possessive-suffixed form \"تكمن أهميته\" to be caught by the filler-phrase stem")
	}
}

func TestDetectGenericFillerPhrases_ClearOnConcreteContent(t *testing.T) {
	sample := `تتكون الأزمنة الأساسية في اللغة الإنجليزية للصف السادس من المضارع البسيط والماضي البسيط والمستقبل بـ will. يُستخدم المضارع البسيط للحقائق والعادات، مثل: She reads books every day. أما الماضي البسيط فيضاف له -ed في الأفعال المنتظمة، مثل: She played football yesterday.

من الأخطاء الشائعة عند الطلاب في هذا المستوى الخلط بين he/she/it التي تحتاج -s في المضارع البسيط، وكتابة she read بدون -s بدلًا من she reads.`

	found := DetectGenericFillerPhrases(sample)
	if len(found) != 0 {
		t.Fatalf("expected no generic-filler phrases in concrete subject-matter content, got %v", found)
	}
}
