package services

import "testing"

// Regression test for the exact generation the user reported: a model reliably falls back to
// generic filler at the opening ("يُعد الاختبار... محطة مهمة لقياس...") and closing ("لا يقل دور
// الأسرة عن دور المدرسة... يخفف من التوتر... انعكاسًا صادقًا لجهد سنة كاملة") of a draft despite
// the prompt banning it outright — this deterministic scan is the backstop for when the prompt
// instruction alone isn't followed.
func TestDetectGenericFillerPhrases_CatchesReportedSample(t *testing.T) {
	sample := `يُعد الاختبار النهائي لمادة اللغة الإنجليزية في الصف السادس الابتدائي للفصل الدراسي الثاني محطة مهمة لقياس مدى تقدم التلميذ في المهارات اللغوية الأساسية التي تراكمت لديه طوال العام.

ولا يقل دور الأسرة عن دور المدرسة في هذه المرحلة؛ فتشجيع الطالب على المراجعة اليومية بانتظام بدلًا من الحفظ المكثف في ليلة الاختبار يخفف من التوتر ويرفع التركيز. ومع ثقة التلميذ بنفسه، يصبح الاختبار النهائي فرصة لإظهار ما تعلمه لا مصدر قلق، وتكون نتيجته انعكاسًا صادقًا لجهد سنة كاملة من التعلم.`

	found := detectGenericFillerPhrases(sample)
	if len(found) == 0 {
		t.Fatal("expected at least one generic-filler phrase to be detected in the reported sample")
	}
}

func TestDetectGenericFillerPhrases_ClearOnConcreteContent(t *testing.T) {
	sample := `تتكون الأزمنة الأساسية في اللغة الإنجليزية للصف السادس من المضارع البسيط والماضي البسيط والمستقبل بـ will. يُستخدم المضارع البسيط للحقائق والعادات، مثل: She reads books every day. أما الماضي البسيط فيضاف له -ed في الأفعال المنتظمة، مثل: She played football yesterday.

من الأخطاء الشائعة عند الطلاب في هذا المستوى الخلط بين he/she/it التي تحتاج -s في المضارع البسيط، وكتابة she read بدون -s بدلًا من she reads.`

	found := detectGenericFillerPhrases(sample)
	if len(found) != 0 {
		t.Fatalf("expected no generic-filler phrases in concrete subject-matter content, got %v", found)
	}
}
