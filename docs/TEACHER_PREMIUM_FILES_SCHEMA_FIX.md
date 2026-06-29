# Teacher Premium Files Schema Fix

## المشكلة
ظهور الخطأ:
Unknown column 'is_premium' in 'where clause'

## السبب
كود منطقة المعلم يبحث في جدول files باستخدام أعمدة Premium:
- is_premium
- premium_audience
- premium_category
- premium_requires_subscription
- premium_subject
- premium_download_count

لكن قاعدة بيانات الدولة التي تعمل عليها لم تكن تحتوي هذه الأعمدة بعد.

## الإصلاح
- تمت إضافة models.File إلى AutoMigrate داخل EnsureTeacherSubscriptionDatabase.
- تمت إضافة ensureTeacherPremiumFileColumns داخل bootstrap.
- تمت إضافة self-healing داخل repository قبل أي استعلام Premium على files.
- عند أول تشغيل أو أول طلب لملفات المعلم، يتم إنشاء الأعمدة الناقصة تلقائيًا.

## ملاحظة
هذا إصلاح مرحلي مهم قبل الانتقال إلى إدارة ملفات Premium وWatermark.
