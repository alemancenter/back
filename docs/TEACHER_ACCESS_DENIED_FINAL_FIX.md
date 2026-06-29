# Teacher Access Denied Final Fix

## المشكلة
عند دخول مستخدم غير مصرح له إلى /dashboard/teacher كانت الصفحة تطلب API ثم يحدث unhandledRejection في المتصفح.

## الإصلاح
- الباك إند يرجع 403 بكود TEACHER_SUBSCRIPTION_INACTIVE بدل 500 عند عدم وجود اشتراك نشط.
- الفرونت إند يلتقط الخطأ ويعرض صفحة منع وصول واضحة.
- تم تطبيق المعالجة على:
  - /dashboard/teacher
  - /dashboard/teacher/files
  - /dashboard/teacher/exams
  - /dashboard/teacher/plans
  - /dashboard/teacher/worksheets
  - /dashboard/teacher/library
  - /dashboard/teacher/downloads

## النتيجة
لا تظهر unhandledRejection. تظهر صفحة مغلقة مع تنبيه وروابط للرجوع أو طلب الاشتراك.
