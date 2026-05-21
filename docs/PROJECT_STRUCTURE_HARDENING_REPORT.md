# Project Structure Hardening Report

## Scope
This package applies the first stabilization phase focused on project structure, separation of responsibilities, and maintainability without changing public routes or database schema.

## Backend changes
- Split the oversized `internal/handlers/contentaudit/handler.go` file.
- Moved content-quality batch job request/types/store/execution logic to:
  - `backend/internal/handlers/contentaudit/quality_batch.go`
- Kept the existing endpoint method names unchanged:
  - `StartQualityBatch`
  - `ListQualityBatches`
  - `ShowQualityBatch`
  - `CancelQualityBatch`
- Preserved current behavior: batch jobs still create analysis/fix previews only and do not auto-apply AI text.

## Frontend changes
- Added a feature layer for Content Audit helper logic:
  - `frontend/src/features/content-audit/constants.ts`
  - `frontend/src/features/content-audit/utils.ts`
  - `frontend/src/features/content-audit/components/PaginationControls.tsx`
- Reduced responsibility inside:
  - `frontend/src/app/dashboard/content-audit/page.tsx`
- Kept the existing route unchanged:
  - `/dashboard/content-audit`

## Structural effect
Before this phase, critical Content Audit logic was concentrated inside large files. After this phase:
- Backend handler file is smaller and focused on request/response handlers.
- Batch processing logic has its own file inside the same package.
- Frontend page delegates constants, utility functions, and pagination UI to a dedicated feature folder.
- The system is easier to continue refactoring toward persistent jobs, cost tracking, and review queues.

## Remaining structural hardening
To reach the next level:
1. Move batch jobs from memory to database tables.
2. Add a backend repository/service layer for content quality jobs.
3. Move frontend Content Audit state/actions into a hook such as `useContentAuditPage`.
4. Add a dedicated `/dashboard/content-audit/review-queue` page.
5. Add API contract types for batch jobs and model router responses.
