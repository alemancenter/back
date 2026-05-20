# Content AI Decision Center

This backend version connects Content Audit to the existing AI generation pipeline through `services.AIService.RunContentIntelligence`.

## Key behavior
- Uses the same Together AI provider/model/fallback configuration as article generation.
- Falls back to a local deterministic decision engine if the AI provider is unavailable.
- Extracts safe plain text from HTML before scoring, preventing false `words=0` results.
- Normalizes content references such as `alhurani_jo:5` into `country_code=jo` and `content_id=jo:5`.
- Returns `exists:false` instead of a hard 404 when no saved decision exists yet.
- Creates fix previews only; content is changed only after admin approval.

## Fix 2026-05-06

- Normalizes tenant/content references before saving AI decisions and fix previews.
- Converts legacy values like `alhurani_jo:3` + `alhurani_jo` into `content_id=jo:3` and `country_code=jo`.
- Adds startup schema guard for `content_ai_decisions.country_code` and `content_ai_fix_previews.country_code`.
- Adds safe defaults for AI issue severity and suggestion priority.
