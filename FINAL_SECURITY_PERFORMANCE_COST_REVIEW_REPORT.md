# Final Security / Performance / Cost Tracking / Review Queue Fixes

## Implemented

- Fixed durable AI jobs remaining in database.
- Added AI model run logging into `content_ai_model_runs`.
- Added estimated token/cost tracking per model attempt.
- Added model fallback run logging for success and failure.
- Added review queue endpoint for pending/applied/rejected AI fix previews.
- Added model cost summary endpoint.
- Fixed `CheckCircle2`/`XCircle` missing frontend imports.
- Added dashboard panels for:
  - Human review queue.
  - AI model cost monitoring.

## New API endpoints

- `GET /api/dashboard/content-audit/ai/review-queue`
- `GET /api/dashboard/content-audit/ai/model-costs`

## Notes

Cost values are estimated using configured/default model prices and reported/estimated tokens. If provider usage tokens are absent, the system estimates tokens conservatively from prompt/completion text.
