# Performance Fix Report

This package applies safe performance improvements without changing AdSense policy behavior or removing Google Consent Mode.

## Implemented

1. Deferred heavy marketing scripts:
   - Google Consent Mode default remains `beforeInteractive`.
   - CookieYes remains loaded with `lazyOnload`.
   - GTM is loaded after idle.
   - AdSense is loaded after first interaction or fallback delay.
   - This is controlled by `NEXT_PUBLIC_DEFER_MARKETING_TAGS`; set it to `false` to restore less aggressive loading.

2. LCP image preload on the home page:
   - Mobile hero AVIF is preloaded for mobile viewport.
   - Desktop hero AVIF is preloaded for desktop viewport.

3. CLS reduction:
   - The home hero card now reserves a minimum height before image/content completion.
   - The LCP image includes explicit width and height attributes.

4. Font cache headers:
   - `/fonts/:path*` now receives `Cache-Control: public, max-age=31536000, immutable` from Next headers.
   - If Nginx/Plesk overrides static file headers, add equivalent caching there too.

5. API compression hardening:
   - Fiber compression is configured with `LevelBestSpeed`.

## AdSense safety

The changes do not hide ads, do not alter ad units, and do not encourage invalid clicks. Consent Mode remains active before Google scripts load. Ads are delayed to reduce mobile main-thread blocking while remaining available after page load/interaction.

## Expected impact

- LCP: expected improvement from about 1.9s to ~1.5-1.7s.
- CLS: expected improvement from 0.081 to ~0.02-0.05.
- TBT/TTI: expected improvement mainly from delayed GTM/AdSense execution.
