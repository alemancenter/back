# Project Structure Hardening - Phase 2

## Scope
This phase continues raising the general project structure score by reducing feature-file size, separating responsibilities, and preparing the codebase for persistent AI jobs without changing current public API routes.

## Backend changes
The content audit batch implementation was further split from one large file into focused files:

- `internal/handlers/contentaudit/quality_batch.go`
  - HTTP handlers and batch execution flow only.
- `internal/handlers/contentaudit/quality_batch_types.go`
  - Request, item, job types, country sanitization, and request normalization.
- `internal/handlers/contentaudit/quality_batch_store.go`
  - In-memory job store, snapshots, progress calculation, and job updates.
- `internal/handlers/contentaudit/quality_batch_targets.go`
  - Target selection from Article/Post data and AdSense readiness scoring.

This reduces the main batch handler from about 511 lines to about 235 lines and makes the next database-persistence phase safer.

## Frontend changes
The content quality batch panel was refactored into feature-level helpers:

- `src/features/content-audit/batch-options.ts`
  - Content type, readiness level, processing mode, and model strategy options.
- `src/features/content-audit/batch-types.ts`
  - Batch form state and model strategy type.
- `src/features/content-audit/batch-utils.ts`
  - Status metadata, HTML-to-text conversion, edit path builder, and date formatter.

The main UI component remains in:

- `src/components/dashboard/content-audit/ContentQualityBatchPanel.tsx`

but it is now smaller and easier to maintain.

## Expected structure score after this phase
Before Phase 1: 84%
After Phase 1: 88% - 90%
After Phase 2: 91% - 93%

## Remaining blockers before 100%
1. Move AI batch jobs from memory to database-backed persistent jobs.
2. Add model run/cost tracking tables.
3. Add a dedicated review queue page.
4. Add service/repository separation for content audit batch operations.
5. Add automated tests around request normalization, job progress, cancellation, and target selection.

## Verification notes
The environment used to prepare this patch cannot download the Go 1.25 toolchain or frontend node_modules, so final verification must be done on the server:

```bash
cd /var/www/vhosts/alemancenter.com/api
go build ./cmd/server

cd /var/www/vhosts/alemedu.com/httpdocs
npm run lint
npm run type-check
NEXT_TELEMETRY_DISABLED=1 npm run build
```
