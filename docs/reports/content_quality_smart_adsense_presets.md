# Smart AdSense-based Content Quality Batches

تم تحديث Batch Jobs في الباك اند لدعم اختيار العناصر من تقرير جاهزية AdSense عبر:

- `source=adsense_readiness`
- `preset=weak_first | indexed_weak | short_file_pages | custom_filter`

## الأولويات

يقوم النظام بترتيب العناصر حسب:

- انخفاض score.
- كون الصفحة قابلة للفهرسة.
- وجود ملفات مع محتوى قصير.
- عدد الكلمات.
- مستوى الضعف.

## ملاحظات قاعدة البيانات

تم إضافة `source` و `preset` إلى `content_ai_jobs` عبر AutoMigrate. إن كان AutoMigrate معطلًا، أضف الأعمدة يدويًا:

```sql
ALTER TABLE content_ai_jobs ADD COLUMN source VARCHAR(40) NOT NULL DEFAULT 'adsense_readiness';
ALTER TABLE content_ai_jobs ADD COLUMN preset VARCHAR(60) NOT NULL DEFAULT 'weak_first';
CREATE INDEX idx_content_ai_jobs_source ON content_ai_jobs(source);
CREATE INDEX idx_content_ai_jobs_preset ON content_ai_jobs(preset);
```
