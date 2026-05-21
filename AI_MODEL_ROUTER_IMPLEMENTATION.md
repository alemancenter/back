# AI Model Router Implementation

تمت إضافة Model Router لمعالجة المحتوى في `/dashboard/content-audit` بهدف توزيع الضغط على أكثر من موديل، تقليل التكلفة، وتفعيل fallback تلقائي عند فشل أي موديل.

## الاستراتيجيات المتاحة

- `economy`: للدفعات الكبيرة والتحليل الرخيص.
- `balanced`: الوضع الافتراضي المناسب لمعظم المعالجات.
- `quality`: للصفحات المهمة أو المحتوى الضعيف الذي يحتاج جودة أعلى.
- `final_review`: لأهم 50-100 صفحة قبل الاعتماد النهائي أو إعادة تقديم AdSense.

## متغيرات البيئة الاختيارية

يمكنك ضبط أسماء الموديلات حسب الأسماء الفعلية الظاهرة في حساب Together AI لديك:

```env
AI_MODELS_AUDIT_ECONOMY=togethercomputer/LFM2-24B-A2B,openai/gpt-oss-20b,Qwen/Qwen3.5-9B
AI_MODELS_AUDIT_BALANCED=openai/gpt-oss-20b,Qwen/Qwen3.5-9B,google/gemma-3n-E4B-it
AI_MODELS_AUDIT_QUALITY=Qwen/Qwen3-235B-A22B-Instruct-2507-FP8-Throughput,openai/gpt-oss-120b,Qwen/Qwen3.5-9B
AI_MODELS_AUDIT_FINAL=openai/gpt-oss-120b,Kimi/Kimi-K2.5,Qwen/Qwen3.5-9B

AI_MODELS_FIX_ECONOMY=Qwen/Qwen3.5-9B,openai/gpt-oss-20b
AI_MODELS_FIX_BALANCED=Qwen/Qwen3.5-9B,google/gemma-4-31b-it-fp8,openai/gpt-oss-120b
AI_MODELS_FIX_QUALITY=Qwen/Qwen3-235B-A22B-Instruct-2507-FP8-Throughput,openai/gpt-oss-120b,Qwen/Qwen3.5-9B
AI_MODELS_FIX_FINAL=openai/gpt-oss-120b,Kimi/Kimi-K2.5,Qwen/Qwen3.5-9B
```

> مهم: إن كان اسم الموديل في Together AI مختلفًا عن المثال، ضع الاسم المطابق من لوحة Together AI. النظام سيستخدم fallback تلقائيًا حتى لا تتوقف المهمة عند أول فشل.

## حدود التوازي حسب الاستراتيجية

- economy: حتى 6 عناصر بالتوازي.
- balanced: حتى 4 عناصر بالتوازي.
- quality: حتى 3 عناصر بالتوازي.
- final_review: حتى عنصرين بالتوازي.

## طريقة الاختبار المقترحة

ابدأ من `/dashboard/content-audit` بهذه القيم:

```txt
الاستراتيجية: economy
طريقة المعالجة: تحليل فقط
العدد: 20
التوازي: 2
```

ثم جرّب:

```txt
الاستراتيجية: balanced
طريقة المعالجة: تحليل + معاينة تحسين
العدد: 20
التوازي: 2
```

راقب logs وستظهر أسطر مثل:

```txt
Content AI | strategy=balanced | role=primary | model=... | task=audit_content
Content AI | strategy=balanced | role=fallback_1 | model=... | task=fix_content
```
