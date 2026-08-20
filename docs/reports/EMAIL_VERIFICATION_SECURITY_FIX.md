# Email Verification Security Fix

## What changed

- Email verification links now use secure random tokens instead of `sha256(email)`.
- Verification tokens are stored hashed in Redis under `email_verify:{user_id}:{token_hash}`.
- Tokens expire automatically after 60 minutes.
- Resending verification invalidates older verification links for the same user.
- Successful verification sets `email_verified_at`, invalidates user cache, and deletes the verification token.
- Verification emails now point to the frontend route: `FRONTEND_URL/verify-email/{id}/{token}`.
- Protected APIs now require verified email through `RequireVerifiedEmail()`.
- Unverified users may still access `/auth/user`, `/auth/logout`, `/auth/email/resend`, and `/auth/email/verify`.

## Required environment

Make sure `.env` contains:

```env
FRONTEND_URL=http://localhost:3000
APP_URL=http://127.0.0.1:8082
MAIL_HOST=...
MAIL_PORT=587
MAIL_USERNAME=...
MAIL_PASSWORD=...
MAIL_FROM_ADDRESS=...
MAIL_FROM_NAME="Aleman Center"
```

## Verification flow

1. User registers.
2. Backend creates user.
3. Backend creates a random verification token.
4. Backend stores the token hash in Redis for 60 minutes.
5. Email link opens the frontend verification page.
6. Frontend calls `/api/auth/email/verify/{id}/{token}`.
7. Backend validates Redis token and updates `email_verified_at`.

## Security notes

Old email links based on `sha256(email)` are no longer accepted.
This is intentional because those links never expired.
