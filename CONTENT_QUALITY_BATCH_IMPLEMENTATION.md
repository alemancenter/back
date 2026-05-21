# Content Quality Batch Processing - Backend

This build adds preview-first batch processing endpoints for the content audit module.

## Endpoints

- `POST /api/dashboard/content-audit/ai/batch-jobs`
- `GET /api/dashboard/content-audit/ai/batch-jobs`
- `GET /api/dashboard/content-audit/ai/batch-jobs/:id`
- `POST /api/dashboard/content-audit/ai/batch-jobs/:id/cancel`

## Behavior

- Selects targets from AdSense readiness scoring.
- Processes weak/review/ready content in batches.
- Supports articles, posts, or both.
- Supports safe concurrency up to 6 workers.
- Creates AI decisions and optional fix previews.
- Never applies fixes automatically.
- Keeps job state in memory for live monitoring.

## Production note

Current job tracking is intentionally lightweight and in-memory. For very long jobs across service restarts, migrate the job state to database tables such as `content_ai_jobs` and `content_ai_job_items`.
