# Auth Security Fix Report

## Completed changes

1. Google/Facebook OAuth callbacks no longer append JWT access tokens to frontend URLs.
   - Old unsafe URL: `/auth/google/callback?token=...`
   - New safe URL: `/auth/google/callback`
   - The backend now sets `token` and `refresh_token` as HttpOnly cookies before redirecting.

2. Login, register, social token login, and refresh now also set HttpOnly cookies.

3. Logout now clears both auth cookies.

4. Refresh token errors now return machine-readable codes:
   - `REFRESH_TOKEN_REQUIRED`
   - `REFRESH_TOKEN_INVALID`

5. Login failures now avoid account enumeration:
   - `INVALID_CREDENTIALS`
   - `ACCOUNT_INACTIVE`

6. Auth rate limiting was expanded to:
   - login
   - register
   - email preflight/check
   - password forgot/reset
   - refresh
   - Google/Facebook redirect/callback/token login

7. Rate limiting now matches route suffixes, so it works whether the app is mounted under `/api` or another prefix.

## Important deployment note

Existing leaked JWTs from old access logs should be invalidated after deployment. Recommended options:

- rotate `JWT_SECRET`, which forces all users to log in again; or
- use token blacklist if your deployment has a controlled revocation process.

After deployment, verify no new logs contain:

```bash
grep -R "auth/google/callback?token=" /var/www/vhosts/system/alemancenter.com/logs/ | tail -20
```

Expected result: no new entries.
