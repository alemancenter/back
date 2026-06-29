# المرحلة 4: منطقة المعلم وقوائم الداشبورد

## قرار نطاق الاشتراك
اشتراك 25 دينار يكون لمادة واحدة يحددها المعلم عند طلب الاشتراك، وليس لكل مواد المنصة.
السبب: الحفاظ على القيمة التجارية ومنع مشاركة اشتراك واحد للوصول إلى كامل مواد المنصة.

## ما تم تنفيذه
- إضافة حقول Premium على جدول files:
  - is_premium
  - premium_audience
  - premium_category
  - premium_requires_subscription
  - premium_subject
  - premium_download_count
- إضافة جدول:
  - teacher_library_items
- إضافة API:
  - GET /api/teacher-subscription/workspace
  - GET /api/teacher-subscription/files
  - GET /api/teacher-subscription/library
  - POST /api/teacher-subscription/library
  - GET /api/teacher-subscription/downloads
  - GET /api/teacher-subscription/ai-generations
- إضافة صفحات منطقة المعلم في الفرونت:
  - /dashboard/teacher
  - /dashboard/teacher/files
  - /dashboard/teacher/exams
  - /dashboard/teacher/plans
  - /dashboard/teacher/worksheets
  - /dashboard/teacher/ai-tools
  - /dashboard/teacher/library
  - /dashboard/teacher/downloads
- إضافة قائمة منطقة المعلم في Sidebar.
- فرض إدخال المادة عند طلب الاشتراك.

## تصنيفات ملفات المعلم
- exam
- answer_key
- plan
- content_analysis
- worksheet
- remedial_plan
- question_bank
- final_review

## ملاحظة
زر التحميل داخل بطاقات الملفات هو واجهة مبدئية. مرحلة التحميل الفعلي مع تسجيل التحميل وWatermark تأتي في المرحلة التالية.
