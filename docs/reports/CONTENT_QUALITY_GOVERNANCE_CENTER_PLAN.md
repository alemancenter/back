# Content & Publishing Quality Governance Center — Implementation Plan

Status: **design only, no code changed**. This document specifies what to build, in what
order, and exactly which existing behaviors must change. It replaces the ad-hoc mix of
`content-audit` sub-pages and blended scores with one governance model, and adds a real
Google Search Console (GSC) integration so "internally ready" can finally be checked
against "actually indexed."

Multi-country note: settings, sitemap and content are already keyed by
`database.CountryID`/`CountryCode` (see `setting_service.go`). Each country code maps to
its own domain, so GSC integration is **per-property, not global** — see §4.

Priority note: §0 below (content quality/SEO at creation and edit time) ships first,
ahead of the rule registry and GSC work — see the rollout order in §7.

---

## 0. Content creation & editing-time SEO/quality gate — top priority

Everything else in this document treats quality/SEO as something an audit engine
discovers *after* content exists. That's the wrong order for meta description and
keywords specifically: they must be handled at the moment an article or post is written
or edited, not surfaced days later in a dashboard queue. Ship this first.

### 0.1 Correction to the earlier claim in this document

This section originally claimed `UpdateArticle` never persisted keyword changes,
citing a stale `// TODO: Handle Keywords using KeywordsRel many-to-many relationship`
comment at the old line 396. On closer reading, that was wrong: `UpdateArticle`
already calls `s.repo.UpdateKeywords(...)` a few lines further down (now
`article_service.go:398-404`), with the same sanitization as `CreateArticle` — update
and create are already in parity, matching `post_service.go`. There was no functional
bug. The only real issue was the leftover TODO comment contradicting the code right
below it, which has been removed. No behavior changed.

### 0.2 Put the SEO/quality signal in the save response, not just the audit scan

`contentquality.Diagnostics` (word count / title length / meta length) is currently only
computed by the background audit engine and the readiness dashboard. Call the same
diagnostics function synchronously inside `CreateArticle` / `UpdateArticle` / the post
service's create+update, and return the result alongside the saved record:

```go
type ContentQualitySignal struct {
    MetaDescription string `json:"meta_description_status"` // missing | too_short | ok
    Keywords        string `json:"keywords_status"`          // missing | ok
    ContentLength   string `json:"content_length_signal"`    // thin | needs_enrichment | adequate
}
```

Informational, not a publish blocker by itself — a draft must still save with gaps. What
changes is *when* the editor sees the signal: at the moment of writing, not in a later
queue they may never open.

### 0.3 Draft-time AI assist, not only post-hoc repair

The existing grounded-fix / `metadata_repair.go` pipeline only runs on content that
already has a saved `ContentAIDecision` — it assumes an audit already happened. Add a
second, lighter endpoint pair that works directly on unsaved draft text, so the "اقترح
وصفًا" / "اقترح كلمات دلالية" buttons can sit in the editor itself, live, exactly as the
original proposal's content-creation path envisioned (step 6, "عرض الإصلاحات المطلوبة
داخل المحرر") but which today only exists after publish:

```
POST /api/dashboard/content-assist/meta-description   { title, content_excerpt } -> suggestion
POST /api/dashboard/content-assist/keywords            { title, content_excerpt } -> suggested tags
```

Both reuse the same grounded-generation model/prompt as `metadata_repair.go`, minus the
requirement for a pre-existing audit row.

### 0.4 Where keywords fit relative to Google — keep the distinction honest

`KeywordsRel`/`Keyword` (`article.go:28`, many-to-many via `article_keyword`) is
legitimate first-party taxonomy — internal search, related-content, structured data —
**not** the deprecated HTML `<meta name="keywords">` tag, which Google Search ignores for
ranking (§2 point 6 already confirmed nothing in the scoring code treats it otherwise).
Label the editor field as internal taxonomy ("كلمات دلالية داخلية لتحسين الربط والبحث
الداخلي في الموقع"), not as an SEO ranking lever — this gets editors the real workflow
benefit without reintroducing the false belief the original proposal correctly flagged.

---

## 1. Corrected data model — single source of truth

Today three unrelated status vocabularies coexist and none of them is the "governance"
state the proposal wants:

| Field | Values | Where |
|---|---|---|
| `ContentAIDecision.Decision` | `approved / needs_fix / restricted_ads / rejected` | `models/content_audit.go:176-179` |
| Unified readiness `Level` | `ready / review / weak` | `readiness_unified.go:184-189` |
| Editorial `Decision` | `unclassified / keep / improve / noindex / merge_301` | content_editorial_decisions model |

**Do not add a fourth parallel column.** Add one pure function that derives the
governance state from data that already exists (`contentquality.Gate` + matched rules +
GSC status), computed at read time in the API layer:

```go
type ReadinessState string

const (
    StateCriticalBlocker      ReadinessState = "critical_blocker"       // مانع حرج
    StateNeedsImprovement     ReadinessState = "needs_improvement"      // يحتاج تحسين جوهري
    StateHumanDecisionPending ReadinessState = "human_decision_pending" // قرار بشري مطلوب
    StateInternallyReady      ReadinessState = "internally_ready"       // جاهز داخليًا
)

func DeriveReadinessState(gate contentquality.Gate, matches []RuleMatch) ReadinessState
```

Mapping: any matched rule with `RuleType = official_google_requirement` (or
`!gate.Indexable`) → `critical_blocker`. Else any `site_editorial_standard` match whose
content type it applies to → `needs_improvement`. Else any rule flagged
`requires_human_decision` (title rewrite, canonical change, noindex toggle, merge,
rights) with no saved human decision yet → `human_decision_pending`. Else →
`internally_ready`. `AdsEligible` stays a separate boolean next to this state, exactly as
today — never collapse the two into one traffic light.

Google's own index status becomes a **separate badge**, sourced only from GSC (§4), never
influencing `ReadinessState`:

```
indexed | not_indexed | crawled_not_indexed | discovered_not_crawled | unknown_not_synced
```

---

## 2. Fix the flawed heuristics (concrete, file-level)

These are real, not hypothetical — the earlier audit confirmed each one in the running
code. Fix before anything else in this doc; they're what makes "internally ready" mean
something.

1. **`ai_service.go:712-723`** — the legacy `fix_content` prompt instructs the model to
   "expand to at least 300 words, target 450." This directly contradicts the newer
   grounded pipeline's own rule ("لا تطارد عدد كلمات ولا تضف حشو SEO",
   `grounded_fix.go:676`) and is the exact padding behavior that got the site rejected
   for thin content once already. **Delete this instruction**; word count is never a
   generation target.
2. **`ad_readiness.go:20-21`** — `ApplyAdReadinessRequirements` revokes ad eligibility for
   any content under 300 words, uniformly, regardless of content type. Change from a
   fixed universal threshold to a **content-type-aware signal**: for `file` pages (§3) a
   250-word *description* is not thin if the page also carries structured file metadata;
   for `article`/`post` keep an explicit, documented internal threshold but stop treating
   it as a silent revoke — surface it as a `needs_improvement` rule match with the
   `site_editorial_standard` type, not a hidden gate side-effect.
3. **`ai_decision.go:583,720,802-824`** (`enforceCurrentSourceRequirements`,
   `isMeaningfulFix`) — caps score at 89 and requires `fixedWords >= originalWords + 40`
   before a fix counts as "meaningful." Replace the word-delta check with a real
   value check (new information added: source cited, question answered, structure
   added) — word count can be a *symptom* logged for review, never the pass/fail
   condition.
4. **`engine.go:441-483`** baseline scorer — keep it (it's a legacy no-AI signal
   deliberately excluded from `contentquality.Evaluate`, per the comment in
   `models/content_audit.go:52-58`), but stop surfacing its blended score anywhere in the
   new UI; it's diagnostic-only input to `ReadinessState`, never a displayed "quality %".
5. **`gate.go:11-13`** `ApprovedMinScore = 90` — keep the constant (it already has a
   comment disclaiming it's not a Google requirement) but the rule registry entry that
   documents it (§3) must carry `rule_type = site_editorial_standard` and a visible
   "internal standard, not a Google/AdSense rule" label in the UI, so it can never again
   be read as an official requirement.
6. Meta keywords: confirmed **not** used as a ranking/eligibility signal anywhere in the
   scoring code — no fix needed, only stop collecting/displaying it as if it mattered for
   SEO in any dashboard copy.

---

## 3. Rule registry

Extend `readinessProblems` (`readiness_problems.go:76-124`, currently a Go map with
Code/Label/Severity/ActionType/Preset/Mode/Priority) into a registry with the fields the
proposal requires. Verification *logic* stays in Go (it has to — these are code paths,
not data); only the documentation/governance metadata moves to a table so it can carry a
version and review date without a deploy.

```sql
CREATE TABLE content_quality_rules (
  code                    VARCHAR(60) PRIMARY KEY,
  scope                   VARCHAR(10) NOT NULL,   -- 'page' | 'site'
  content_types           TEXT NOT NULL,          -- json array: ["article","post","file","category"]
  severity                VARCHAR(20) NOT NULL,
  rule_type               VARCHAR(30) NOT NULL,   -- official_google_requirement | site_editorial_standard | optimization_recommendation
  blocks_readiness        BOOLEAN NOT NULL DEFAULT FALSE,
  requires_human_decision BOOLEAN NOT NULL DEFAULT FALSE,
  source_url              TEXT,                   -- link to the official Google policy page, if any
  verification_method     TEXT NOT NULL,          -- human-readable pointer to the Go check, e.g. "contentquality.ApplyAdReadinessRequirements"
  fix_method               VARCHAR(20) NOT NULL,  -- manual | ai_preview | auto_apply
  auto_apply_allowed       BOOLEAN NOT NULL DEFAULT FALSE,
  version                 INT NOT NULL DEFAULT 1,
  last_reviewed_at        DATE NOT NULL,
  updated_at               TIMESTAMP NOT NULL
);
```

A startup check (in `contentaudit.NewService` init) asserts every row's `code` has a
registered Go evaluator and vice versa — this is what stops "internal opinion" and
"Google rule" from silently blurring again, and stops a rule existing in code with no
registry entry (undocumented) or vice versa (dead config).

Only `official_google_requirement` rows may set `blocks_readiness = true`. This is
enforced at write time (admin UI can't save a `site_editorial_standard` row as blocking),
not just by convention.

---

## 4. Google Search Console integration

### 4.1 Auth: service account, not per-user OAuth

The repo already depends on `golang.org/x/oauth2` (see `go.mod:23`) but not the heavy
`google.golang.org/api` client. Keep it lean: use
`golang.org/x/oauth2/google.JWTConfigFromJSON` to mint bearer tokens from a **service
account key**, and call the three GSC REST APIs directly with `net/http` — no new heavy
dependency, consistent with the project's current footprint.

One-time manual step (outside code, done once per property): add the service account's
email as a user (Full or Restricted) on every property in Search Console — this avoids
building and storing per-user OAuth refresh tokens entirely, which is both simpler and
the architecturally correct choice for a single backend service acting on its own sites.

Config additions, following the existing `GoogleConfig` pattern in `config.go:174`:

```go
type SearchConsoleConfig struct {
    ServiceAccountJSON string // GSC_SERVICE_ACCOUNT_JSON — raw JSON, not a checked-in file
    Enabled            bool   // GSC_ENABLED
}
```

Per-country property mapping goes in a table, not env vars, since it grows with every
new country/domain:

```sql
CREATE TABLE gsc_properties (
  id           SERIAL PRIMARY KEY,
  country_code VARCHAR(10) NOT NULL UNIQUE,
  site_url     VARCHAR(255) NOT NULL,   -- e.g. "sc-domain:alemancenter.com" or "https://imanjo.com/"
  verified_at  TIMESTAMP,
  active       BOOLEAN NOT NULL DEFAULT TRUE
);
```

### 4.2 New tables

```sql
CREATE TABLE gsc_url_status (
  id                 SERIAL PRIMARY KEY,
  content_type       VARCHAR(30) NOT NULL,
  content_id         BIGINT NOT NULL,
  country_code       VARCHAR(10) NOT NULL,
  url                VARCHAR(1000) NOT NULL,
  index_status       VARCHAR(30) NOT NULL,   -- indexed | not_indexed | crawled_not_indexed | discovered_not_crawled | unknown_not_synced
  coverage_verdict   VARCHAR(20),             -- raw URL Inspection verdict: PASS/FAIL/NEUTRAL/PARTIAL
  google_canonical   VARCHAR(1000),
  user_canonical     VARCHAR(1000),
  robots_txt_state   VARCHAR(30),
  last_crawl_time    TIMESTAMP,
  raw_response       JSONB,
  checked_at         TIMESTAMP NOT NULL,
  UNIQUE (content_type, content_id, country_code)
);

CREATE TABLE gsc_search_analytics_daily (
  id           SERIAL PRIMARY KEY,
  country_code VARCHAR(10) NOT NULL,
  url          VARCHAR(1000) NOT NULL,
  date         DATE NOT NULL,
  clicks       INT NOT NULL DEFAULT 0,
  impressions  INT NOT NULL DEFAULT 0,
  ctr          NUMERIC(6,4),
  position     NUMERIC(6,2),
  UNIQUE (country_code, url, date)
);

CREATE TABLE gsc_sync_runs (
  id             SERIAL PRIMARY KEY,
  kind           VARCHAR(20) NOT NULL,  -- url_inspection | search_analytics | sitemap_ping
  status         VARCHAR(20) NOT NULL,  -- running | completed | failed
  triggered_by   VARCHAR(20) NOT NULL,  -- manual | scheduled
  urls_checked   INT NOT NULL DEFAULT 0,
  error_message  TEXT,
  started_at     TIMESTAMP NOT NULL,
  finished_at    TIMESTAMP
);
```

`gsc_url_status` is deliberately never read by `contentquality.Evaluate`/`Gate`, mirroring
the existing rule for `ContentPolicyReadiness` (`models/content_audit.go:56-58`) — Google's
index status enriches the dashboard badge, it never feeds back into `Indexable`/
`AdsEligible`.

### 4.3 Sync job

Reuse the existing pattern in `contentaudit/service.go` (mutex-guarded `Start`/`execute`,
`PolicyAuditRun`-style status row) instead of inventing a new job abstraction:

- **URL Inspection API** (`POST https://searchconsole.googleapis.com/v1/urlInspection/index:inspect`):
  quota is ~2,000 requests/day and ~600/min *per property*. Sync incrementally and
  prioritized: newly published pages first, then pages whose internal `ReadinessState`
  just became `internally_ready` (these are exactly the ones worth checking against
  reality), then a slow round-robin refresh of the rest (e.g. re-check anything not
  checked in 14+ days). Never full-scan on every run.
- **Search Analytics API** (`POST .../searchAnalytics/query`): cheap, high quota, but data
  lags 2-3 days and only covers 16 months — pull daily per property, incrementally.
- **Sitemaps API**: ping only after a real publish event, reusing the existing
  `sitemap_service.go` publish hook — this is the one place Google-facing notification on
  publish *is* correct (unlike the Indexing API, which stays unused for ordinary pages,
  confirmed nowhere in the codebase today — keep it that way).
- Trigger: same external cron/systemd mechanism already used for
  `PolicyAuditTriggerScheduled` runs — no new scheduler infrastructure needed.
- All calls go through one rate-limited client with exponential backoff on 429/403 quota
  errors, logged via the existing `pkg/logger` (zap) and recorded on `gsc_sync_runs`, not
  silently swallowed.

### 4.4 New endpoints

```
GET  /api/dashboard/gsc/properties                     admin: list/configure per-country properties
POST /api/dashboard/gsc/properties/{country_code}      admin: set site_url
POST /api/dashboard/gsc/sync                            trigger a sync run (manual)
GET  /api/dashboard/gsc/status/{content_type}/{id}      cached single-URL status for the badge
GET  /api/dashboard/gsc/analytics?country_code=&url=    clicks/impressions/position series
```

Credential value itself (`GSC_SERVICE_ACCOUNT_JSON`) stays in env/secrets, never in the
`settings` table — it fails every marker in `privateSettingMarkers`
(`setting_service.go:64-75`) by design; don't special-case it into that table.

---

## 5. Content-type-specific rule sets

The engine today only recognizes `article`/`post` (`normalizeContentType`,
`ai_decision.go:923-932`). Add `file` and `category` as first-class content types with
their own rule rows in the registry (§3), not bolted onto the article rule set:

**`file` (download pages — the highest-risk content type for this site today):**
required fields checked before `internally_ready`: file description present and
non-generic, subject/grade/semester/year tags set, file type/size/version/update date
populated, a short usage explanation exists, source/rights field is not empty. The
ad-button-spacing requirement is a template/layout concern, not per-page content data —
enforce it once in the Astro download-page template and add a single **site-scope**
rule (`ad_button_spacing_ok`) that's checked structurally (DOM distance) rather than
per-item, so it can't regress silently.

**`category`/`tag` pages**: rule set checks for a real intro/selection value and flags
near-duplicate category pages for `noindex` or canonical merge (reuses the existing
`similarity.go` duplicate detector) instead of applying the article word-count rules to
them at all — today nothing distinguishes them.

---

## 6. Unified `/dashboard/quality` UI

Replace the 10-route sprawl under `Astro/src/pages/dashboard/content-audit/`
(`index`, `ai-operations/index`, `ai-operations/batch-jobs/[id]`,
`ai-operations/decisions/[id]`, `readiness/index`, `scan/index`, `corruption/index`,
`similarity/index`, `inventory/index`, `inventory/[type]/[id]`) with:

- `/dashboard/quality` — **Site status tab**: AdSense-critical blocker count, GSC
  property health, privacy/consent/ads.txt checklist, duplicate/thin-page counts, human
  decisions pending. All backed by existing checks (corruption, similarity, inventory)
  called as library functions from one aggregating handler — don't rewrite their logic,
  just stop giving each one its own top-level page.
- `/dashboard/quality` — **Content work tab**: table of items with a non-`internally_ready`
  state, filterable by content type and state, replacing `readiness/index` +
  `ai-operations/index` as the primary surface.
- Clicking a row opens a **side drawer** (not a route navigation) reusing
  `ai-operations/decisions/[id].astro`'s existing preview/accept/reject logic as a
  component, plus the new GSC badge and the rule's registry metadata (why it fired,
  official/editorial/recommendation label, fix method, before/after).
- Keep `ai-operations/batch-jobs/[id].astro`-style progress as a small background
  indicator inside the work tab, not a separate page.
- Ship behind a `quality_dashboard_v2` public setting flag so the old pages keep working
  until the new one is verified, then redirect old routes to the new one.

---

## 7. Rollout order

0. **Ship first, per explicit priority:** the creation/edit-time SEO+quality signal and
   draft-time assist endpoints (§0.2, §0.3). No schema changes, touches only the
   article/post save path, and is the part editors feel on every single piece of
   content — deliberately sequenced ahead of the registry and GSC work below, which are
   backend/governance plumbing by comparison.
1. Delete the word-padding prompt instruction and reclassify the 300-word / 90-score
   gates as documented `site_editorial_standard` rules instead of silent blockers (§2) —
   cheapest change, directly addresses real AdSense risk, no schema changes.
2. Ship the rule registry table + startup consistency check (§3), backfilling it from
   today's `readinessProblems` map.
3. Add `file`/`category` as real content types with their own rule rows (§5) — the
   single highest-value gap for this site's actual content mix.
4. Build the GSC integration (§4): properties table → URL Inspection sync → Search
   Analytics sync → badge in the API response. This has to land before step 5, or the
   new "Site status" tab has nothing but internal heuristics to show, which is the exact
   complaint the original proposal raises.
5. Ship `DeriveReadinessState` (§1) and the consolidated `/dashboard/quality` UI (§6)
   behind the feature flag; migrate, then deprecate the old routes.

## 8. Explicitly out of scope

- Google Indexing API for ordinary articles/files/categories — confirmed unused today,
  stays unused; only JobPosting/BroadcastEvent pages qualify, and this site has neither.
- Per-user OAuth login flow for GSC — service account only (§4.1).
- Any "fix all" batch action that applies a generated rewrite to more than the set of
  items sharing one identical rule code and fix method — current `quality_batch.go:226-230`
  restriction to the `meta_description` auto-apply preset stays as the ceiling for
  automatic application; broader batches remain preview-then-individual-accept.
