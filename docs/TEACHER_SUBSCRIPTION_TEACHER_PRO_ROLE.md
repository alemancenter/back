# Teacher Pro Role Automation

تمت إضافة دور تلقائي باسم:

- Teacher Pro

عند موافقة الإدارة على طلب اشتراك المعلم:
1. يتم إنشاء/تحديث دور Teacher Pro إن لم يكن موجودًا.
2. يتم إنشاء صلاحيات المعلم وربطها بالدور.
3. يتم إضافة الدور للمستخدم عبر model_has_roles دون حذف دور User العادي.
4. يتم مسح كاش المستخدم من Redis حتى تظهر الصلاحيات مباشرة.

الصلاحيات المرتبطة بالدور:
- teacher.subscription.access
- teacher.files.premium.download
- teacher.files.word_pdf.export
- teacher.ai.exam.generate
- teacher.ai.answer_key.generate
- teacher.ai.worksheet.generate
- teacher.ai.remedial_plan.generate
- teacher.library.access
- teacher.devices.manage
- teacher.usage.view
