# Teacher Bootstrap Safe Fix

Fixed:
- Removed models.File from TeacherSubscription AutoMigrate to avoid altering unrelated tables such as posts.country.
- File premium columns are now added with raw ALTER TABLE files ADD COLUMN only when missing.
- Role/permission bootstrap now runs only when roles/permissions/model_has_roles/role_has_permissions tables exist.
- Country databases without auth tables are skipped safely instead of throwing roles table missing errors.
