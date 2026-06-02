# Chatbot MySQL key length fix

## Problem
MySQL failed to create `chat_knowledge_base` with:

`Error 1071 (42000): Specified key was too long; max key length is 3072 bytes`

The cause was a GORM index on `keywords varchar(1000)`. With `utf8mb4`, this can exceed the maximum allowed index length.

## Fix
- Changed `ChatKnowledgeBase.Keywords` from `varchar(1000);index` to `text` without an index.
- Changed `ChatKnowledgeBase.Question` to `text` because it is searched with `LIKE`, not by exact indexed lookup.
- Kept short/safe indexes only on fields used for filtering/order:
  - `category`
  - `country_code`
  - `is_active`
  - `priority`
  - `created_by`
  - `updated_by`
- Existing repository search still works with `LIKE` against `question`, `title`, `keywords`, and `category`.

## Result
AutoMigrate can now create the chatbot tables on MySQL/MariaDB using utf8mb4 without exceeding key length limits.
