# Backend Test Fix Report

This patch targets the failures reported by `go test ./...`.

## Fixed failures

### 1. `internal/handlers/files` test DB dependency

The file upload handler test was failing because `DashboardUpload` called `articleGradeName`, which initialized the global database manager and attempted to connect to `test_db`.

Change:
- Added injectable `articleGradeNameLookup`.
- Production behavior still uses the real DB lookup.
- Tests override the lookup so file upload tests no longer require a real MySQL database.

Files:
- `internal/handlers/files/handler.go`
- `internal/handlers/files/handler_test.go`

### 2. AI service fallback defaults

`TestNewAIServiceUsesOfficialDefaults` expected at least one fallback model, but `defaultAIFallbackModels` was empty.

Change:
- Added safe default fallback models.
- Existing environment overrides still take priority.

File:
- `internal/services/ai_service.go`

### 3. `PostService_Create` mock panic

`PostService.Create` calls `UpdateKeywords` when keywords exist. The test mock embedded the repository interface but did not implement `UpdateKeywords`, causing a nil-pointer panic.

Change:
- Added `UpdateKeywordsFunc` to `MockPostRepository`.
- Implemented `UpdateKeywords` with a safe no-op fallback.

File:
- `internal/services/post_service_test.go`

## Validation note

This environment has Go 1.23.2 and the project requires Go 1.25.0. Because external toolchain download is blocked, `go test` could not be executed here. Please run locally:

```powershell
go test ./...
```

Expected result after this patch:
- `internal/handlers/files` should no longer fail because of missing `test_db`.
- `TestNewAIServiceUsesOfficialDefaults` should pass.
- `TestPostService_Create/SanitizesPostFields` should not panic.

