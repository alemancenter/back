# Content AI Fix Preview - Update

This update fixes the AI fix-preview flow for weak / thin content.

## What changed

- `CreateFixPreview` now rejects AI responses that return the same weak content.
- The fix-preview flow first tries `RunContentIntelligence(... Task: fix_content ...)`.
- If the returned `fixed_content` is empty, too short, or almost identical to the original content, the backend falls back to the existing `GenerateSEOArticle` pipeline.
- If the AI provider fails or still returns weak output, a local expanded fallback draft is generated so the preview is never identical to the original short content.
- The prompt for `fix_content` was strengthened to require clean HTML, educational value, headings, paragraphs, and minimum word count.

## Important

Old previews already saved in `content_ai_fix_previews` will remain unchanged. Create a new fix preview after replacing the backend to see the improved output.
