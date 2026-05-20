# Backend AdSense Readiness Pagination Fix

## المشكلة
الـ endpoint كان يحسب `summary.total` من عدد العناصر الراجعة في الصفحة الحالية فقط، لذلك كان يظهر 30 عنصرًا تقريبًا عند اختيار الكل.

## الإصلاح
- فحص كل المقالات والمنشورات المطابقة للفلاتر.
- حساب summary عالمي لكل النتائج المطابقة للنوع والبحث.
- تطبيق فلتر الحالة ثم pagination على النتائج النهائية.
- إرجاع meta يحتوي على: current_page, per_page, total, last_page, from, to, filtered_total.
- استخدام counts للملفات من جدول `files` بدل `Preload("Files")` لكل عنصر لتحسين الأداء.
