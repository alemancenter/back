# Persistent AI Jobs Implementation

This phase upgrades `/dashboard/content-audit` batch processing from an in-memory runtime store to database-backed job tracking.

## What changed

### New persistent tables

The backend now defines and migrates:

- `content_ai_jobs`
- `content_ai_job_items`
- `content_ai_model_runs`

`content_ai_jobs` stores the batch job header, filters, status, progress, cancellation flag, timestamps, and owner.

`content_ai_job_items` stores every article/post item in the batch with its own status, score before/after, AI decision ID, fix preview ID, and error/message.

`content_ai_model_runs` is prepared for model-cost and reliability tracking. The table exists now so the router can log model calls in the next phase without another schema redesign.

### Runtime behavior

- Jobs survive API restarts.
- Job list and job details are loaded from the database.
- Cancelling a job updates the database state.
- Progress updates are written to the database after every item.
- A server restart marks unfinished jobs as failed/interrupted to avoid misleading “running forever” states.
- Public API response shape remains compatible with the existing frontend.

## Manual SQL

AutoMigrate creates the missing tables on first use. A controlled SQL fallback is available at:

```txt
docs/sql/content_ai_persistent_jobs.sql
```

## Remaining work for true 100% backend maturity

- Add detailed cost logging from the AI model router into `content_ai_model_runs`.
- Add a review queue endpoint that aggregates pending fix previews across jobs.
- Add retry/resume for interrupted jobs if desired.
- Add tests once the CI/server has Go 1.25 available.
