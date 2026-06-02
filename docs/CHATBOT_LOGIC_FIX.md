# Chatbot logic fix

## Problem
The chatbot handled phrases such as "لا يصلني كود أو رسالة" as a generic question instead of an email verification problem. The frontend also kept rendering suggestion buttons under older messages, which made the conversation look repetitive and illogical.

## Backend changes
- Added Arabic text normalization for intent detection.
- Reordered intent detection so email/verification delivery issues are detected before generic login/register cases.
- Added `password_reset_problem` intent.
- Improved fallback answers for login, password reset, email verification, and download problems.
- Added/upserted default knowledge rows for:
  - login problem
  - password reset
  - email verification
  - code/message not received
  - download problem
- Existing default rows are updated automatically by `seedDefaultKnowledge`, so old typos in seeded answers are repaired after backend restart.

## Frontend changes
- Suggestions are now displayed only below the latest assistant message.
- Old suggestion buttons are cleared when the user sends a new message.
- Suggestion buttons are disabled while a request is loading.

## Test expectation
- "لا يصلني كود أو رسالة" => `email_verification_problem`
- "لا تصلني رسالة التفعيل" => `email_verification_problem`
- "البريد غير مفعل" => `email_verification_problem`
- "نسيت كلمة المرور" => `password_reset_problem`
- Repeated old buttons should not remain under previous messages.
