# imanjo.com deployment drafts

Drafted during the `alemancenter.com` → `imanjo.com` migration audit. These are **starting
templates**, not verified-working configs — every file has `REPLACE_*` placeholders or an
explicit comment flagging an assumption that needs confirming against the real server before
use. Nothing here has been deployed or tested against a live imanjo.com/api.imanjo.com.

## Open questions to resolve before deploying (in priority order)

1. **Plesk Additional Directives vs standalone `nginx.conf`?** This session confirmed the live
   server sends `X-Powered-By: PleskLin`, meaning Plesk is the real hosting panel — Plesk
   usually manages the main server block itself and takes extra rules through its own UI
   field, not a manually-symlinked `sites-available` file. The old `back/nginx.conf` was also
   caught drifting from what's actually live (its tracked `/storage/` location didn't match a
   live test during this session). **Use `nginx-api-additional-directives.conf` +
   `nginx-frontend-additional-directives.conf` if Plesk is confirmed** (pair with whatever
   Plesk auto-generates for the main vhost/SSL); fall back to the standalone `nginx.conf` only
   for a non-Plesk deployment.

2. **Which `WorkingDirectory` convention is real?** `back/` has two different
   `alemancenter-api.service` files with different paths (`/var/www/vhosts/api.alemancenter.com/httpdocs`
   vs `/var/www/vhosts/alemancenter.com/fiber/`) and it wasn't possible to confirm from the
   repo alone which one is actually installed on the server. `imanjo-api.service` here uses the
   first convention (matches the actively-referenced `install-backend.sh`) — verify against the
   real server before trusting it.

3. **Same server or a different one?** `api.env.example`'s `TRUSTED_PROXIES` had the current
   production server's real IP hardcoded — left as `REPLACE_WITH_SERVER_IP` here since it's
   unknown whether imanjo.com deploys to the same box.

4. **`ADSENSE_CLIENT`** — the old `deploy/alemancenter/api.env.example` and the actually-
   deployed `02220` mirror disagree on this value. Resolve which publisher ID is correct, and
   confirm whether AdSense needs an entirely new/separate publisher ID for the new domain
   (it ties publisher IDs to verified domains) — this is an AdSense-account decision, not
   something resolvable from the codebase.

## What's new here vs. the alemancenter deploy set

- **`imanjo-web.service`** has no prior equivalent — the old frontend ran under PM2 as Next.js
  (`back/deploy-alemancenter.sh`, `ooole/front/`), not systemd. This migration replaces that
  entire stack with Astro's `@astrojs/node` standalone adapter, so this is a from-scratch
  systemd unit, not an adaptation of an existing one.
- **`nginx.conf`'s frontend server block** no longer proxies to a Next.js `upstream` on :3000
  with ISR page-caching — it proxies to Astro on :8080. Astro does its own response caching
  in-process (`Astro/astro.config.mjs`'s `routeRules` + `memoryCache()`), so there's no
  nginx-level cache layer to replicate for the frontend anymore.
- **`location ^~ /storage/private/ { deny all; return 404; }`** is carried into both nginx
  drafts — a real security fix made to `back/nginx.conf` during this same audit (protected/
  premium files were reachable only by directory-layout accident, not a deliberate rule).

## Files

| File | Purpose |
|---|---|
| `api.env.example` | Backend `.env` template — all 4 country DB connections (the old example only had `_JO`), new domain values. |
| `imanjo-api.service` | systemd unit for the Go backend. |
| `imanjo-web.service` | systemd unit for the Astro frontend (new — see above). |
| `nginx.conf` | Standalone sites-available style, both domains, Next.js removed. |
| `nginx-api-additional-directives.conf` | Plesk "Additional Directives" for api.imanjo.com. |
| `nginx-frontend-additional-directives.conf` | Plesk "Additional Directives" for imanjo.com (new — no prior equivalent). |
| `nginx-rate-limit-zones.conf` | `http{}`-context rate-limit zones — install to `/etc/nginx/conf.d/`, not inside Plesk's directives field. |
| `install-backend.sh` | Adapted install script — paths/service names updated to match this directory's files. |

## Not covered here — separate manual steps

- Issuing TLS certs for `imanjo.com`, `www.imanjo.com`, `api.imanjo.com`.
- Updating OAuth authorized redirect URIs in the Google Cloud Console / Facebook App dashboard.
- Adding + verifying `imanjo.com` in the Google AdSense account; there's no `ads.txt` anywhere
  in this codebase currently — a pre-existing gap, not something this migration introduces.
- Migrating actual file contents from the old storage path to the new one.
- Updating the DB `settings` table (`site_url`, `canonical_url`, `contact_email`) — see
  `back/docs/sql/fix_adsense_and_canonical_url.sql` for the pattern used last time.
- Confirming `ooole/front`'s PM2 process (the old Next.js frontend) is fully stopped, not just
  unlinked from nginx.
