# Teacher AI Subject Lock Fix - BACKEND

Generated: 2026-06-07 17:03:14

## Fix
Teacher AI generation subject is now locked to the teacher subscription/profile subject.

## Backend
- Ignores `req.Subject` from client.
- Loads subject from `TeacherProfile.Subject`.
- Refuses AI generation if no subscription subject exists.

## Frontend
- Removed editable subject input from `/dashboard/teacher/ai-tools`.
- Shows locked subject from `/teacher-subscription/access`.
- Sends AI generation payload without subject.
