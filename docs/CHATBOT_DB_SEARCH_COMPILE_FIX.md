تم إصلاح أخطاء البناء بعد تحديث محرك بحث قاعدة البيانات:

1. إصلاح flowDecision struct literal الذي كان يحتوي على field:value مع قيمة بدون اسم حقل.
2. إزالة duplicate case privacy_request داخل contextualAnswer.
3. تنفيذ gofmt على ملفات chatbot.

ملاحظة: تعذر تشغيل go test داخل بيئة ChatGPT بسبب محاولة تحميل Go 1.25 toolchain بدون صلاحية.
