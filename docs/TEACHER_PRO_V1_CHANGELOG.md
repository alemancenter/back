# Teacher Pro v1 - BACKEND Changelog

Generated: 2026-06-07 16:52:25

## Consolidated scope
- Teacher subscription semester plan.
- Teacher Pro role and permissions.
- Premium private vault.
- Protected downloads and download logs.
- Teacher dashboard, files, downloads, devices, notifications, AI tools.
- Admin subscriptions, teachers, devices, downloads, AI generations, reports, analytics.
- Payment proof flow and payment settings scaffold.
- FAQ, policy, and teacher pricing public pages.
- Fixes for duplicate routes, item scope, notification table migration, bootstrap safety.

## Production notes
- Replace the project files with this package cleanly, not by extracting over old files only.
- Delete stale files that were intentionally removed, especially `src/app/pricing` in the frontend.
- Clear `.next` before rebuilding.
- Backend requires Go toolchain compatible with `go.mod` toolchain settings.
