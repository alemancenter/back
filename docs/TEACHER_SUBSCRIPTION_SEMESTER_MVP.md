# اشتراك المعلم للفصل الدراسي - MVP

## المنتج
- الاسم: اشتراك المعلم للفصل الدراسي
- الكود الداخلي: teacher_semester
- السعر: 25 دينار أردني
- المدة الافتراضية: 150 يومًا
- الفئة: معلمو الأردن

## ما تم تنفيذه في الباك إند
### Models
- SubscriptionPlan
- TeacherProfile
- TeacherSubscription
- SubscriptionOrder
- TeacherDevice
- TeacherPremiumDownload
- TeacherAIGeneration

### API
Public:
- GET /api/teacher-subscription/plan

Teacher authenticated:
- GET /api/teacher-subscription/me
- POST /api/teacher-subscription/orders
- GET /api/teacher-subscription/devices
- DELETE /api/teacher-subscription/devices/:id

Admin dashboard:
- GET /api/dashboard/teacher-subscriptions/orders
- POST /api/dashboard/teacher-subscriptions/orders/:id/approve
- POST /api/dashboard/teacher-subscriptions/orders/:id/reject

## الصلاحية الإدارية
- manage teacher subscriptions

## ملاحظات مهمة
- الدفع في هذه المرحلة يدوي.
- صورة التحويل الآن يمكن إدخالها كرابط مؤقت payment_proof_url.
- حماية الأجهزة تعتمد على device hash + IP hash + user agent.
- الحد الافتراضي للأجهزة: جهازان.
- تم تجهيز جداول premium downloads و AI generations للمرحلة التالية.
