# Internal Notifications Distribution Fix

## Problem
Notifications were not consistently visible in the dashboard bell for non-admin users. Some events created notifications only for a single author/recipient, while other events used broad or legacy broadcast behavior.

## Implemented Fix

### Backend
- Added permission-based recipient discovery through `UserRepository.GetUserIDsByPermissions`.
- Added `NotificationService.NotifyUsersWithPermissions` to notify all active users who own the relevant permission directly or through roles.
- Deduplicated recipients before bulk insert.
- Updated article events to notify users with:
  - `manage articles`
  - `manage notifications`
  - plus the actor/author when applicable.
- Updated post events to notify users with:
  - `manage posts`
  - `manage notifications`
  - plus the actor/author when applicable.
- Updated contact form submissions to notify users with:
  - `manage messages`
  - `manage notifications`.
- Wired notification service into the posts and settings handlers.

### Frontend
- Fixed notification dropdown routing so dashboard action URLs remain dashboard URLs and are no longer converted to public article/post URLs.

## Affected Files

### Backend
- `internal/repositories/user_repository.go`
- `internal/services/notification_service.go`
- `internal/handlers/articles/handler.go`
- `internal/handlers/posts/handler.go`
- `internal/handlers/settings/handler.go`
- `internal/routes/dependencies.go`

### Frontend
- `src/components/layout/NotificationsDropdown.tsx`

## Recommended Database Indexes
Run `docs/sql/internal_notifications_indexes.sql` for faster notification and permission lookups.

## Behavior After Fix
- Creating/editing/deleting/publishing articles creates bell notifications for article managers and notification managers.
- Creating/editing/deleting/toggling posts creates bell notifications for post managers and notification managers.
- Contact form messages create bell notifications for message managers and notification managers.
- Direct internal messages still notify the recipient directly.
