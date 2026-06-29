# مرحلة 5: إدارة ملفات Premium للمعلمين

## ما تم تنفيذه

### Backend API
- GET /api/dashboard/teacher-subscriptions/premium-files
- POST /api/dashboard/teacher-subscriptions/premium-files/:id
- POST /api/dashboard/teacher-subscriptions/premium-files/:id/disable

### Frontend
- /dashboard/teacher-subscriptions/premium-files

## طريقة العمل
1. ارفع الملف كمرفق عادي داخل مقال أو منشور من النظام الحالي.
2. افتح لوحة الإدارة:
   /dashboard/teacher-subscriptions/premium-files
3. ابحث عن الملف.
4. اضغط إعدادات.
5. اكتب المادة، مثل:
   اللغة العربية
6. اختر فئة الملف:
   exam, answer_key, plan, content_analysis, worksheet, remedial_plan, question_bank, final_review
7. اضغط حفظ كملف Premium.

## النتيجة
الملف يصبح:
- is_premium = true
- premium_audience = teacher
- premium_requires_subscription = true
- premium_subject = المادة
- premium_category = التصنيف

ويظهر للمعلم المشترك في نفس المادة داخل منطقة المعلم.
