# Chatbot V3 - Real User Questions Patch

This patch improves the chatbot using real questions commonly sent by members and visitors.

## Added handling

- Mixed request: user wants an educational file but access is blocked by email verification.
- Download location: user downloaded a file but does not know where it was saved.
- Broken attachment/download link: "عدم الوصول", "المرفق غير موجود", "الرابط لا يفتح", "موراضي".
- Conversational content requests such as:
  - نماذج امتحان حاسوب نهائي صف ثامن فصل ثاني
  - خطة نمو مهني تربية فنية
  - امتحان ثقافة مالية الصف الثامن الفصل الثاني
- Expanded entity extraction:
  - صف ثامن / ثامن
  - دين / التربية الإسلامية
  - ثقافة مالية
  - تربية فنية
  - خطة نمو مهني
  - نماذج / نهائي

## Files changed

- internal/services/chatbot/service.go
- internal/repositories/chatbot/repository.go
- src/components/chatbot/ChatbotWidget.tsx in frontend package

## Notes

The backend still relies on AutoMigrate and default knowledge seeding. Existing knowledge base entries are updated automatically by question and country_code.
