# المرحلة 2: تصميم الباقات والصلاحيات

## المنتج المعتمد
- الاسم: اشتراك المعلم للفصل الدراسي
- الكود الداخلي: teacher_semester
- السعر: 25 دينار أردني
- المدة: 150 يومًا
- عدد الأجهزة: 2
- تحميلات Premium: 300
- عمليات AI: 100
- تصدير Word/PDF: 100

## صلاحيات المعلم داخل الباقة
- teacher.subscription.access
- teacher.files.premium.download
- teacher.files.word_pdf.export
- teacher.ai.exam.generate
- teacher.ai.answer_key.generate
- teacher.ai.worksheet.generate
- teacher.ai.remedial_plan.generate
- teacher.library.access
- teacher.devices.manage
- teacher.usage.view

## صلاحيات الإدارة
- manage teacher subscriptions
- teacher.subscription.orders.review
- teacher.subscription.plans.view

## API تمت إضافتها/تثبيتها
- GET /api/teacher-subscription/design
- GET /api/teacher-subscription/access

## ملاحظات
تمت إضافة permissions_json و limits_json إلى subscription_plans حتى يكون تصميم الباقة قابلًا للتوسعة لاحقًا بدون تغيير جذري في قاعدة البيانات.
