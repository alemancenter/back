# AdSense Readiness Repair Center

The readiness endpoint now exposes structured, reviewer-facing problem codes and a repair-center summary while preserving the canonical content quality gate as the only authority for indexing and ad eligibility.

## API additions

`GET /api/dashboard/content-audit/adsense-readiness`

- Accepts an optional `problem` filter.
- Each item includes `problems` and `primary_problem`.
- The response includes `repair_center` with counts, priority, action type, batch preset, mode, and model strategy.

The diagnostic problem codes never grant indexing or ad eligibility. They only route editorial work into the existing preview-first workflow.

## Repair presets

- `unaudited`
- `policy_blocked`
- `ads_not_eligible`
- `thin_content`
- `needs_enrichment`
- `meta_description`
- `short_title`
- `selected_items`

`POST /api/dashboard/content-audit/ai/batch-jobs` also accepts up to 100 explicit targets:

```json
{
  "preset": "selected_items",
  "mode": "fix_preview",
  "targets": [
    { "content_type": "article", "content_id": 123 },
    { "content_type": "post", "content_id": 456 }
  ]
}
```

Targets are validated, de-duplicated, and processed through the existing human-approval pipeline. Generated text is never applied automatically.

