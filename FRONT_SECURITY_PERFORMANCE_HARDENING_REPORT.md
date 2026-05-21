# Frontend, Security, and Performance Hardening Report

## Scope
This package focuses on the three requested tracks:

- Frontend stability and UX warnings
- Security hardening around CSP and rich HTML rendering
- Performance pressure reduction around AI polling and visitor tracking

## Frontend changes

- Added missing `name` attributes to lowercase `input`, `select`, and `textarea` elements that lacked both `id` and `name`.
- Verified there are no duplicate props on lowercase form fields after the fix.
- Kept the Cairo font integration self-hosted and removed the mandatory Cairo Medium dependency. Required files are now only:
  - `Cairo-Regular.woff2`
  - `Cairo-SemiBold.woff2`
  - `Cairo-Bold.woff2`
- Kept font preloads disabled to avoid unused preload warnings.
- Improved Content Quality Batch polling: visible tabs poll every 15 seconds, background tabs back off to 45 seconds, and overlapping requests remain blocked.

## Security changes

- Removed production `unsafe-eval` from the global CSP. It remains enabled only in development.
- Rich article and post HTML is routed through `sanitizeRichHtml` before rendering with `dangerouslySetInnerHTML`.
- The sanitizer removes scripts/styles/objects/embeds, inline event handlers, JavaScript/data HTML URLs, untrusted iframes, and applies safe rel/target handling to external links.

## Performance changes

- Dashboard/API/internal polling endpoints are excluded from visitor tracking so they no longer inflate analytics or create unnecessary writes to `visitors_tracking`.
- Static assets, fonts, health checks, auth, notifications, dashboard and backend-api dashboard routes are skipped by visitor tracking.
- AI batch polling is adaptive and visibility-aware.

## Remaining non-code requirement

The Cairo WOFF2 files still need to exist on the production server under:

`public/fonts/cairo/`

Run:

```bash
npm run verify:fonts
```

## Verification notes

- Static JSX scan passed: no lowercase `input`, `select`, or `textarea` without `id` or `name`; no duplicate props detected on those elements.
- `go build` could not be executed in this sandbox because the project requires Go 1.25 and the environment cannot download the toolchain.
- `npm run lint/type-check/build` could not be executed in this sandbox because `node_modules` is not included in the uploaded ZIP.
