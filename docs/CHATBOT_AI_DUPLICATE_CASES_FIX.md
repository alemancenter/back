تم إصلاح duplicate case داخل internal/services/chatbot/ai.go.

الحالات payment_question و open_article و general_question كانت مكررة داخل نفس case line في switch، وتم تنظيفها مع الحفاظ على منع الذكاء الاصطناعي من التدخل في هذه الحالات.

تم تنفيذ gofmt على ملفات chatbot.
