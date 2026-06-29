# Premium Download Protection

## تم تنفيذ
- تسجيل تفاصيل التحميل داخل teacher_premium_downloads:
  - premium_file_id
  - country
  - file_title
  - original_filename
  - subject_name
  - category
  - file_size
  - mime_type
  - download_code
  - ip_hash
  - user_agent_hash
- تطبيق حد التحميل قبل إرسال الملف.
- إرجاع كود TEACHER_DOWNLOAD_LIMIT_REACHED عند تجاوز الحد.
- زيادة download_count داخل teacher_premium_files بعد التسجيل.
- إرسال X-Teacher-Download-Code في استجابة التحميل.

## Endpoint
GET /api/teacher-subscription/premium-files/:id/download
