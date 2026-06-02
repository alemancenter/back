# Chatbot V4 AI Logic Fix

## Problem fixed
The first V4 AI integration was too eager:

- It used AI for deterministic support questions such as download location and contact support.
- It triggered site content search for support phrases containing generic words like "أريد", "ملف", or "تحميل".
- It displayed unrelated content results next to support answers.
- The AI sometimes invented a generic "support/help" page instead of using the real `/contact-us` action.

## Backend changes

### 1. AI is no longer used for deterministic support intents
AI is disabled for these stable support flows:

- login/register/password reset
- email verification
- download problem/download location
- permission/file not found
- contact support/report/profile/site errors
- privacy/account lookup

Rules and knowledge-base answers are more accurate and cheaper for these cases.

### 2. Content search only runs for educational/search intents
The bot will not search articles/posts/files for support messages like:

- أريد التواصل مع الدعم
- عملت تحميل ولا أعرف أين أجد الملف
- زيارة صفحة الدعم في الموقع

Search results now appear only when the user is clearly asking for educational content, such as exams, summaries, worksheets, lessons, grade, subject, or semester.

### 3. Contact support intent priority improved
Phrases such as "أريد التواصل مع الدعم" and "زيارة صفحة الدعم" now resolve to `contact_support`, not `search_content`.

### 4. AI prompt hardened
The AI prompt now explicitly forbids inventing a Help/Support page and instructs the model to use only backend-provided actions such as `/contact-us`, `/search`, and `/classes`.

## Expected behavior

### Download location
User: عملت تحميل ولا أعرف أين أجد الملف

Expected:
- deterministic answer with Downloads / browser download history steps
- no AI badge
- no unrelated content results

### Contact support
User: أريد التواصل مع الدعم

Expected:
- deterministic contact instructions
- `/contact-us` button
- no AI badge
- no unrelated content results

### Educational search
User: نماذج امتحان حاسوب صف ثامن فصل ثاني

Expected:
- content search runs
- results are shown if available
- AI may improve the answer only when the intent is educational/search-oriented
