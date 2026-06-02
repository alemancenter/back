-- مرجع فقط: قاعدة المعرفة الافتراضية لمساعد المنصة أصبحت تُزرع تلقائيًا من الكود.
-- لا تحتاج لتنفيذ هذا الملف يدويًا في الوضع الطبيعي.
-- الملف الفعلي المسؤول عن التهيئة:
-- internal/repositories/chatbot/repository.go -> seedDefaultKnowledge

SELECT category, COUNT(*) AS total
FROM chat_knowledge_base
GROUP BY category
ORDER BY category;
