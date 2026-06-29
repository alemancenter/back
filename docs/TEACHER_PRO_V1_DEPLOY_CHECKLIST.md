# Teacher Pro v1 Deploy Checklist

## Frontend
```bash
cd /var/www/vhosts/alemancenter.com/httpdocs
rm -rf .next
rm -rf src/app/pricing
npm run build
systemctl restart alemancenter-frontend
```

## Backend
```bash
cd /var/www/vhosts/api.alemancenter.com/httpdocs
go build -o fiber-api ./cmd/server
systemctl restart alemancenter-fiber-api
```

## Cache
```bash
redis-cli FLUSHDB
```

## Required manual tests
1. Admin opens `/dashboard/teacher-subscriptions`.
2. Admin opens subscriptions, devices, downloads, AI generations.
3. Admin opens reports and analytics.
4. Admin opens premium files vault.
5. Teacher opens dashboard, files, downloads, notifications, AI tools.
6. Teacher downloads a Premium file.
7. Admin verifies download log.
8. Admin disables a teacher device.
9. Admin renews/reactivates a subscription.
10. Public pages open: `/pricing/teacher`, `/teacher-subscription/faq`, `/teacher-subscription/policy`.
```
