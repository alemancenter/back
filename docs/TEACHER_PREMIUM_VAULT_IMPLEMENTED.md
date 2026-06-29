# Teacher Premium Vault

تم إصلاح بنية ملفات اشتراك المعلمين بشكل جذري.

## القرار
ملفات اشتراك المعلمين لم تعد تعتمد على مرفقات المقالات العامة.
تم إنشاء خزنة مستقلة:
- جدول teacher_premium_files
- تخزين خاص storage/private/teacher-premium
- تحميل محمي عبر API
- لا يوجد رابط عام مباشر للملف

## API
Teacher:
- GET /api/teacher-subscription/files
- GET /api/teacher-subscription/premium-files/:id/download

Admin:
- GET /api/dashboard/teacher-subscriptions/premium-files
- POST /api/dashboard/teacher-subscriptions/premium-files/upload
- POST /api/dashboard/teacher-subscriptions/premium-files/:id
- POST /api/dashboard/teacher-subscriptions/premium-files/:id/disable

## الحماية
التحميل يفحص:
- تسجيل الدخول
- تفعيل البريد
- اشتراك Teacher Pro نشط
- مطابقة مادة المعلم مع مادة الملف
- حد التحميل
- تسجيل التحميل في teacher_premium_downloads
