# Backend AdSense Readiness Changes

- Added dashboard endpoint: `GET /api/dashboard/content-audit/adsense-readiness`.
- Calculates lightweight readiness score for articles and posts.
- Helps prioritize weak/thin pages without manually opening 2000+ content items.
- Uses the selected country database and supports filters by type, level, and search query.
