# Review Queue database compatibility fix

Fixed `GET /api/dashboard/content-audit/ai/review-queue` failing with:

`Unknown column 'd.adsense_risk' in 'field list'`

The review queue now builds a schema-aware SELECT. If an older database does not yet have `adsense_risk`, `model`, `prompt_version`, or `processing_time_ms`, the endpoint returns safe default values instead of failing with 500.

Optional SQL migration is available at:

`docs/sql/content_ai_decisions_optional_columns.sql`

Run that SQL later to align the old table with the current model schema.
