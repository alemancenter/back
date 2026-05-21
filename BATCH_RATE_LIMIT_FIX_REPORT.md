# Batch Jobs Rate Limit Fix

## Problem
The content quality dashboard polls `/api/dashboard/content-audit/ai/batch-jobs` while AI jobs are running. The previous prefix limiter applied the same `60 requests / 5 minutes` limit to both expensive AI mutations and lightweight polling reads. Polling every few seconds generated `429 Too Many Requests` during normal operation.

## Backend fix
- Added optional `Methods []string` support to `middleware.RateLimitRule`.
- Added method-aware Redis keys so GET and POST limits are isolated.
- Added dedicated rules for batch-job polling:
  - `GET /api/dashboard/content-audit/ai/batch-jobs*`: `900 / 5 minutes`
  - `POST /api/dashboard/content-audit/ai/batch-jobs`: `20 / 5 minutes`
  - `POST /api/dashboard/content-audit/ai/batch-jobs/:id/cancel`: `120 / 5 minutes`
- Kept the general AI endpoint limiter at `60 / 5 minutes`.

## Expected result
Normal dashboard progress polling should no longer be blocked by 429 while long AI jobs are running, while expensive job creation remains protected.
