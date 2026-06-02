# Chatbot Core Implementation Report

Implemented a safe first version of the Alemancenter support chatbot.

## Backend

- Added database models:
  - `ChatSession`
  - `ChatMessage`
  - `ChatKnowledgeBase`
  - `ChatFeedback`
- Added chatbot repository, service, and Fiber handler.
- Added public endpoints:
  - `GET /api/chatbot/suggestions`
  - `POST /api/chatbot/message`
  - `POST /api/chatbot/feedback`
- Added dashboard endpoints protected with `manage settings`:
  - `GET /api/dashboard/chatbot/sessions`
  - `GET /api/dashboard/chatbot/knowledge`
  - `POST /api/dashboard/chatbot/knowledge`
  - `PUT /api/dashboard/chatbot/knowledge/:id`
  - `DELETE /api/dashboard/chatbot/knowledge/:id`
- Added Redis prefix rate limits for chatbot message and feedback endpoints.
- Added AutoMigrate registration for the new chatbot tables.
- Added manual SQL reference: `docs/sql/chatbot_tables.sql`.

## Frontend

- Added chatbot API service.
- Added floating public chatbot widget in the main site layout.
- Added dashboard page: `/dashboard/chatbot`.
- Added dashboard sidebar item under `manage settings`.
- Added session persistence for guest conversations through localStorage.

## Current behavior

The chatbot uses deterministic rules + knowledge base + content search. It does not call external AI yet, which keeps cost low and prevents hallucinated answers in the first release.

## Validation note

Go tests could not run in the sandbox because this project requires Go 1.25 and the sandbox has Go 1.23.2 with no internet access to download the Go 1.25 toolchain. Frontend lint could not run because `eslint` is not installed in `node_modules` inside the uploaded archive.
