# AGENTS.md

## First rule: secrets

- Never read, print, search, copy, summarize, source, or inspect `.env` or any `.env.*` file.
- This rule applies even when debugging configuration or tests. Ask the user to wire configuration or name the variables they should change.
- It is acceptable to run a user-requested Docker command that passes `.env` via `env_file`; do not open the file or expose its contents.
- Avoid commands whose scope could accidentally include secret files. Exclude `.env` and `.env.*` explicitly from repository-wide searches.

## What this project is

This repository is Yövmiye's Go categorization worker. It consumes SQS events, uses AI plus embeddings to resolve a global category hierarchy, mutates the category domain in PostgreSQL, and atomically assigns the final category to either a listing or a user's offered service/profession.

Yövmiye is a location-aware marketplace for services. Providers are not only individual freelancers: they may be tradespeople, teams, shops, or small businesses. A car wash, computer repair shop, glazier, contractor, lawyer, or private tutor can all register what they offer and be discovered nearby.

The worker owns category-domain behavior and schema design. Core creates listings/users and reads category data. Coordinate schema contracts with Core, but do not weaken the worker's invariants to accommodate stale Core assumptions. Notification/search Lambda consumers must be told when emitted event contracts change.

The application is still in development. The user is willing to recreate the database, so prefer a clean correct model over elaborate compatibility migrations unless compatibility is explicitly requested. Never change unrelated code or user work without reason.

## Current launch scope

- Launch is gig/service-only.
- The static base category slug is `gig`.
- Resolve its UUID from `base_categories` by active slug. Never hard-code a base-category UUID.
- The legacy `for-sale` classifier, prompts, extraction types, validation, and repository write path have been removed. Do not reintroduce multi-family listing behavior unless the user explicitly reopens that feature.
- Exactly one category is assigned per listing.
- Exactly one offered service/profession is assigned per user.
- Assign only the final active, selectable category—not its ancestors.

## Category domain

Category UUID is the durable identity. Names and slugs are metadata, never identity.

Canonical category fields are English:

- `canonical_name`
- `canonical_slug`
- `canonical_description`
- `parent_id`
- `depth`
- `base_category_id`
- `is_active`
- `is_selectable`
- `embedding`
- `generated_by`

Localized presentation lives in `category_localizations`:

- `category_id`
- `locale`
- `name`
- `slug`
- `description`
- `generated_by`
- timestamps
- primary key `(category_id, locale)`

One concept across languages must reuse one UUID:

```text
Avukat (tr)
Lawyer (en)
Rechtsanwalt (de)
    -> one canonical Lawyer category UUID
    -> three category_localizations rows
```

A new language creates a localization, not another category. Localizations are lazy: ensure them for every node in the resolved path in the source locale, reuse existing rows, and do not eagerly generate every language.

Canonical matching uses English concept metadata, parent/sibling context, the existing hierarchy, and embeddings. Localized wording may help an AI understand input but must not define identity. A genuine reusable specialization may be a child (for example `Immigration Lawyer` under `Lawyer`); a translation or wording variant is not a specialization.

Hierarchy is dynamic and AI-generated. Do not introduce manually maintained profession lists or hard-coded taxonomies. Reuse an existing sibling when semantically equivalent; create only genuinely missing reusable concepts. The hierarchy supports arbitrary concepts but prompt output is currently bounded to four meaningful levels to control taxonomy drift.

Sibling uniqueness matters. Roots are unique within a base category; children are unique within `(base_category_id, parent_id)`. Category creation uses a transaction-scoped advisory lock and conflict-safe persistence to handle concurrent messages.

## Provider-neutral taxonomy semantics

Categories describe the offered/requested service, trade, or occupation—not the legal shape of the provider.

- Use a provider-neutral service concept when input names a service or business activity.
- Turkish `Oto Yıkama` must become canonical `Car Wash`, localized `Oto Yıkama`.
- Do not invent `Auto Detailing` unless detailing is explicit.
- Do not turn services into artificial people: avoid `Car Wash Specialist`, `Computer Repair Expert`, `Oto Yıkama Uzmanı`, and similar suffixes.
- Keep established occupational names when the concept itself is conventionally a role: `Lawyer`, `Plumber`, `Graphic Designer`.
- Do not broaden input by adding adjacent services.
- Do not narrow input to an implied specialization.
- Do not classify by generic verbs such as repair, manage, build, or support; classify the actual domain/service.
- Avoid unstable catch-all roots such as “Professional Services” or activity-only roots such as “Maintenance and Repair” when a coherent sector exists.

`is_selectable` is explicit semantic data:

- `true` for a reusable service, trade, or occupation users/listings may reference.
- `false` for grouping-only nodes.
- Selectable is not synonymous with leaf.
- A selectable node may later gain selectable children and must not be demoted.
- Never infer selectability from `parent_id`, `depth`, or child count.

## AI and prompt architecture

Embedded prompts live in `internal/prompter/prompts/`:

- `moderation.txt`: contextual allow/reject decision for adult-only or prohibited marketplace content.
- `category_path.txt`: source locale detection plus canonical English hierarchy and source-locale metadata.
- `profession.txt`: structured gig/service details.

`internal/prompter/loader.go` embeds the prompt files. `builder.go` injects the current active canonical catalog into the category-path prompt.

Moderation is a separate AI call that runs before taxonomy work. The complete input is sent because Core limits profession text to 200 characters and listing descriptions to 2,000 characters. Only a successfully decoded explicit `reject` decision is a content rejection. Rate limits, quota/payment errors, timeouts, unavailable providers, malformed/empty responses, context cancellation, and exhausted fallback chains are retryable processing failures: they must not change listing status or emit a rejection event.

Category extraction and details extraction are separate AI calls. The category-path response includes `source_locale`, canonical English fields, localized source-language fields, and `is_selectable` for every node. Listing processing runs both calls because it needs gig details; profession processing runs category-path extraction only and must not generate/discard gig details. Details extraction produces the listing title and gig details. Validation must reject malformed paths before persistence.

Configured providers are assembled in `cmd/worker/app.go`. Current fallback order is:

1. Groq
2. OpenRouter
3. Gemini
4. Mistral
5. Nvidia

Only fully configured providers are included. Staging loads Gemini, Groq, OpenRouter, Mistral, and Nvidia configuration from SSM; do not add a provider to the fallback order without also wiring and testing its staging parameter retrieval/mapping. `FallbackExtractor` tries providers sequentially, records provider/model provenance as `generated_by`, stops on success, and returns joined errors when all fail. Taxonomy/details attempts have a 25-second provider deadline and 75-second total deadline. Moderation uses a shorter 10-second provider deadline and 30-second total deadline. Provider-level errors, invalid responses, and per-provider timeouts allow the next configured provider to run; caller cancellation or exhaustion of the total fallback deadline stops the chain immediately. Keep combined budgets below the worker's two-minute message deadline so embeddings, database work, and notification publication retain time to finish. Keep output token limits controlled; oversized `max_tokens` previously caused avoidable credit failures.

Prompts require JSON responses. OpenAI-compatible providers use JSON response format and temperature zero; Gemini requests JSON MIME output. Avoid brittle parsing hacks when prompt/schema validation can solve the issue cleanly.

## Embeddings

The local embedding integration uses Ollama with `all-minilm:l12-v2` (about 67 MB). Keep it unless the user explicitly changes the architecture.

- The Ollama endpoint must include a URL scheme, such as `http://localhost:11434`; a bare `localhost:11434` causes `unsupported protocol scheme`.
- Generate embeddings from canonical English concept data, not just a localized label.
- Current embedding text is canonical breadcrumb, canonical name, and canonical description.
- Embeddings help semantic sibling reuse; parent scope remains mandatory so similarity cannot merge concepts across unrelated branches.

## Operational metrics

Use the existing OpenTelemetry metric pipeline; do not add a parallel metrics stack for worker/domain signals.

The staging-critical metric set is:

- `categorization.worker.messages_processed_total`: successfully processed messages, labeled by message type.
- `categorization.worker.messages_failed_total`: processing failures, labeled by message type.
- `categorization.worker.message_retries_total`: transient failures deliberately left on SQS for redelivery, labeled by message type.
- `categorization.worker.message_processing.duration`: end-to-end worker processing duration by message type.
- `categorization.ai.provider_failures_total`: failed AI provider attempts, labeled by generator and failure reason (`error`, `timeout`, or `canceled`).
- `categorization.ai.fallbacks_total`: transitions from one AI generator to the next.
- `categorization.taxonomy.category_resolutions_total`: category resolution decisions with `action=created|reused` and `workflow=listing|profession`.
- `categorization.embedding.failures_total`: embedding failures labeled by listing/profession workflow.

Metric attributes must stay low-cardinality. Do not add listing IDs, user IDs, request IDs, descriptions, category UUIDs, or arbitrary error strings as metric labels. Those belong in logs/traces.

Tracing sampling is environment-aware. Development samples every trace for local debugging. Staging and production use parent-based 10% root sampling so upstream sampling decisions are honored and steady-state trace volume stays bounded. Do not restore unconditional `AlwaysSample()` in deployed environments without an explicit temporary debugging reason.

Expected trace waterfall:

- `process-message` -> `process-listing` or `process-profession`
- `ai.fallback` -> one or more `ai.provider.attempt` client spans
- `category_path.embed` -> one `ollama.embed` client span per path node
- `listing.persist_transaction` or `profession.persist_transaction` -> `taxonomy.resolve_node` spans
- `listing.publish_notifications` -> one producer span for each outbound SQS event

Trace attributes may include entity IDs and canonical slugs because traces are diagnostic and sampled. Never attach raw descriptions, prompts, AI responses, credentials, or arbitrary full error payloads as attributes. Errors should be recorded through span error/status APIs.

## Listing processing contract

Accepted SQS message types:

- `ListingCreated` (current)
- `GigPublished` (temporary compatibility alias)
- `ProfessionPublished`

`ListingCreated` body:

```json
{
  "listing_id": "uuid",
  "author_id": "uuid",
  "description": "Musluğumu tamir edecek bir tesisatçı arıyorum",
  "country_id": "optional-uuid",
  "place_id": "optional-uuid"
}
```

Location rules:

- Both location fields are absent for a remote listing. A local listing has
  `country_id`; `place_id` remains optional and requires `country_id`.
- Do not use or populate legacy `city_id` or `district_id`.
- Do not reinterpret a place as necessarily a city/district; Core's `places` catalog supports arbitrary administrative levels and localities.
- Core already persists listing location. The worker preserves event values and forwards them where needed; it does not translate them to legacy IDs.

Core creates a listing as:

```text
status = PROCESSING
base_category_id = NULL
```

The worker must:

1. Moderate the complete description.
2. On explicit reject, atomically move `PROCESSING` to the existing terminal database status `FAILED`; do not create taxonomy/details data, then publish `content_rejected` after commit.
3. On moderation error, leave the listing `PROCESSING`, emit nothing, and return a retryable error.
4. On allow, resolve active `gig` base-category UUID by slug.
5. Extract/resolve the canonical hierarchy and source localization.
6. Generate canonical embeddings.
7. Start a transaction and lock the listing row.
8. If already `OPEN`, do not mutate listing/category state again; reload the stored classification and continue to notification publication.
9. While still `PROCESSING`, set `base_category_id` and `description_clean`.
10. Reuse/create every category path node and ensure its source-locale localization.
11. Attach only the final active selectable gig category through `listing_categories`.
12. Upsert `gig_details`.
13. Set the listing to `OPEN` only after all prerequisites exist.
14. Commit, then publish notifications.

Category creation, localization writes, embedding writes, category association, typed details, and the final status change must remain atomic. Never expose an `OPEN` listing without an active gig base category, one selectable category association, and `gig_details`.

`base_category_id_for_slug(...)` no longer exists. Use `SELECT id FROM base_categories WHERE slug = $1 AND is_active = TRUE`. Absence is a configuration/schema error.

## Profession/service registration contract

`ProfessionPublished` body:

```json
{
  "request_id": "uuid",
  "author_id": "uuid",
  "description": "Oto Yıkama"
}
```

Core stores the latest `profession_request_id` on the user. Profession processing performs a cheap stale-event check before AI calls, then locks the user and repeats the check inside the persistence transaction. If a newer request superseded the event, ignore it safely. Preserve this two-stage strategy: it avoids wasting AI quota and prevents an old SQS delivery from overwriting a newer profession.

Profession processing uses `FallbackExtractor.ExtractCategoryPath`, not full listing extraction. It must not call the gig-details prompt because the profession workflow persists only the resolved category association.

Profession content is moderated after the cheap initial stale-request check and before taxonomy work. Explicit rejection leaves the user's existing category unchanged and emits `content_rejected` with `subject_type=profession` and `subject_id=request_id`. Moderation failures are retryable and emit nothing.

The final association must be an active, selectable category in the `gig` base category. A user has exactly one selected offered-service category.

## Emitted events

After a listing commits, and again on an `OPEN` replay, the worker publishes:

- `listing_approved`: recipient, listing UUID, base-category slug, title, timestamp.
- `gig_category_matched`: listing/gig UUID, category UUID, optional `country_id` and `place_id`, timestamp. Both location fields are absent for remote listings.
- `content_rejected`: recipient UUID, `listing` or `profession` subject type, subject UUID, stable reason code, timestamp. Never include the rejected raw content.

Every emitted notification envelope includes `event_id` and `version`. `version` is currently `1`. Event IDs are deterministic for the same logical event so an `OPEN` replay publishes the same ID again: `listing_approved` is keyed by listing UUID and `gig_category_matched` by listing UUID plus category UUID. Downstream consumers should use `event_id` for idempotent handling of the worker's at-least-once publication semantics.

The category UUID is identity. Never attach localized names/slugs as identity in listing, user, notification, or search contracts.

The notification/search Lambda historically consumed integer `city_id`/`district_id`. New events use UUID `country_id`/`place_id`; keep downstream consumers informed and do not silently restore legacy fields.

Notifications happen after the database commit. DB work is atomic, but event publication is not in the same transaction. An `OPEN` replay intentionally publishes the required events again. This gives the current design at-least-once publication semantics: a post-commit publish failure can recover on SQS redelivery, while duplicate downstream events remain possible and consumers must tolerate them. Do not publish from inside the DB transaction. If strict deduplication/exactly-once delivery becomes a requirement, revisit the design explicitly rather than hiding it in processor logic.

## SQS reliability and concurrency

- The worker long-polls SQS in batches of up to 10.
- Processing concurrency is bounded by configuration; the DB pool is sized with headroom.
- A single message has a two-minute processing deadline.
- The receive visibility timeout is three minutes and must remain strictly longer than the processing deadline.
- Cross-package runtime budgets are centralized in `internal/runtimepolicy/timeouts.go`; change the shared policy there rather than independently editing worker, extractor, or shutdown constants.
- Shutdown stops receiving and drains in-flight work; in-flight work is detached from the shutdown context but remains timeout-bound. The application then gets up to 10 seconds to stop HTTP/telemetry/resources.
- The worker is deployed as a `systemd` service. Its unit must allow the process to drain in-flight work before force termination; the repository provides `deploy/systemd/10-graceful-stop.conf` with `TimeoutStopSec=135s` and `KillSignal=SIGTERM`.
- Panics are recovered per message so one bad record cannot crash the process.
- Delete an SQS message after successful processing, or when it is explicitly classified as a permanent message-contract failure that retrying cannot fix.
- Permanent failures are intentionally narrow: missing/unknown message type, malformed message JSON, or invalid required payload fields. Do not classify database, AI/provider, embedding, network, timeout, panic, or other runtime/dependency failures as permanent.
- Transient/runtime failures remain undeleted for normal SQS redelivery. DLQ behavior belongs to the AWS queue redrive policy; do not add application-side DLQ publishing unless that architecture is explicitly changed.
- If deleting a permanently invalid message fails, leave it retryable because the worker has not successfully acknowledged/discarded it.
- Missing LocalStack queues or receive failures use exponential backoff: 1s, 2s, 4s, 8s, 16s, then 30s maximum. Reset after a successful receive.
- Keep database processing idempotent because SQS is at-least-once. Notification publication is also at-least-once and may duplicate on replay.

## Database ownership and migrations

The worker currently targets Core schema version 18.

The migration directory contains one clean staging baseline:

- `000001_category_domain`: creates the complete worker-owned `categories` and `category_localizations` domain, including hierarchy/depth enforcement, sibling uniqueness, selectability, provenance, embeddings, localization constraints, indexes, and timestamp triggers.

`cmd/workerctl migrate up` embeds and applies pending worker migrations under a transaction-scoped advisory lock, recording completed versions in `categorization_schema_migrations`. The staging workflow runs this command before replacing the worker binary and checks `/ready` after deployment.

Core v18 uses PostgreSQL `INTEGER` for `categories.depth` (Go `int`), not `SMALLINT`/`int16`.

The shared database also contains Core-owned tables such as `base_categories`, `listings`, `listing_categories`, `gig_details`, `users`, `user_categories`, `countries`, `places`, and `place_names`. Inspect current schema/migrations supplied by Core before changing SQL contracts. Do not assume old compatibility views or helper functions exist.

The user has stated that this worker owns category-related schema design, while the Core team/service may need to apply corresponding shared-database changes. When category schema changes, produce a precise migration contract for Core/Lambda as part of the handoff.

## Important trade-offs

- Dynamic taxonomy over manual lists: broad coverage and multilingual growth, at the cost of AI drift. Control drift with strict prompts, canonical sibling context, embeddings, validation, and regression examples—not handcrafted profession conditionals.
- English canonical pivot over per-language trees: stable cross-language identity, at the cost of translation/model dependence. Never let localized wording create duplicate UUIDs.
- Semantic reuse over exact strings: catches translation/synonym equivalence, but can over-merge. Always scope by parent/base category and preserve genuinely reusable specializations.
- Provider-neutral services over person-only professions: supports businesses and individuals, but requires the prompt to distinguish service names (`Car Wash`) from conventional roles (`Lawyer`).
- Explicit selectability over leaf inference: categories can evolve without breaking existing assignments.
- Sequential AI fallback over parallel fan-out: limits quota/cost and uses deterministic provenance, but increases worst-case latency. Keep per-message timeout and concise prompts in mind.
- Post-commit notifications preserve DB correctness. Re-publishing on `OPEN` replay closes the silent-loss window but accepts possible duplicate events. Introduce an outbox only if future requirements justify stronger delivery/deduplication guarantees.

## Recent implementation decisions

### 2026-09-21 — Remote listings have no location

- Previous situation: every listing event required `country_id`, even when the listing represented work that could be completed remotely.
- Current situation: a remote listing event omits both `country_id` and `place_id`. Local listings keep the existing country and optional place contract. Worker classification does not require location and forwards the optional values to Notification unchanged.
- Why: remote work is not country-scoped. Requiring a country forced the Client to invent location context that the listing does not have.

### 2026-09-21 — Groq-first AI fallback order

- Previous situation: configured AI calls tried Gemini first, followed by Groq, OpenRouter, Mistral, and Nvidia.
- Current situation: runtime processing, the live evaluator, and the opt-in AI benchmark now try Groq, OpenRouter, Gemini, Mistral, and Nvidia. Providers that are not fully configured remain skipped.
- Why: the selected Groq and OpenRouter models should receive normal traffic before Gemini while preserving the existing sequential fallback, validation, provenance, and timeout behavior.

### 2026-09-14 — Persistent GCP production VM

- Previous situation: production targeted a managed instance group whose proactive update policy could replace the Worker VM whenever its instance template changed, discarding the systemd deployment installed after boot.
- Current situation: production targets one fixed Terraform-managed Compute Engine instance by name. Terraform protects it with `prevent_destroy`, so a replacement-requiring infrastructure change fails instead of rotating the VM automatically.
- Why: Worker is deployed as a persistent systemd service, including host-local Ollama state. Its infrastructure lifecycle must match that deployment model; replacing it is an explicit maintenance operation, never a routine Terraform rollout.

### 2026-08-10 — Explicit AI content moderation for MVP

- Previous situation: listing and profession content went directly into taxonomy extraction, with no adult/restricted-content decision.
- Current situation: the worker moderates the complete bounded input first. A validated explicit rejection uses one of `sexual_content`, `alcohol`, `tobacco_nicotine`, or `controlled_substances`. Listings move from `PROCESSING` to the existing internal `FAILED` status and emit `content_rejected`; rejected profession requests leave the current user category unchanged and emit the same event keyed by request ID. Raw content is never emitted.
- Why: rejection is sufficient for MVP, but dependency failures must never be mistaken for policy decisions. Rate limits, provider outages, malformed responses, timeouts, and fallback exhaustion remain retryable and produce neither status mutation nor rejection events. Moderation has a separate 10-second-per-provider/30-second-total budget so the subsequent 75-second taxonomy fallback still fits inside the two-minute message deadline.

### 2026-08-09 — Dependency and workflow trace spans

- Previous situation: traces showed SQS receipt and the top-level processor only, so AI fallback delays, Ollama failures, database transaction time, taxonomy decisions, and outbound notification latency could not be distinguished.
- Current situation: spans cover the fallback chain and each provider attempt, category-path embedding and each Ollama request, listing/profession persistence transactions, each taxonomy node resolution, and each notification publish. Provider and external-call spans use client/producer span kinds, errors are recorded on the failing child, and prompts/descriptions are not captured.
- Why: traces should identify the slow or failing boundary without relying on log correlation or adding high-cardinality operational metrics.

### 2026-08-09 — SQS visibility and permanent message failures

#### Visibility timeout

- Previous situation: SQS message visibility was 60 seconds while one processing attempt could run for up to two minutes. The same message could therefore become visible to another worker before the first valid attempt finished.
- Current situation: receive visibility is three minutes while the processing deadline remains two minutes.
- Why: keep the visibility window strictly longer than a valid processing attempt and avoid avoidable concurrent duplicate AI/embedding work.

#### Permanent message failures

- Previous situation: every processing error left the message undeleted, including malformed JSON, missing required fields, and unsupported message types that can never succeed on retry.
- Current situation: message-contract failures are explicitly marked permanent, logged, and deleted; database, AI/provider, embedding, network, timeout, panic, and other runtime/dependency failures remain retryable. DLQ/redrive remains an AWS SQS queue policy concern.
- Why: stop poison messages from retrying forever without weakening recovery for failures that can actually become healthy later.

### 2026-08-09 — Post-commit notification recovery

- Previous situation: after the DB transaction committed, notification publication could fail. SQS would redeliver the input, but an already-`OPEN` listing returned successfully without publishing again, so a required downstream event could be permanently lost.
- Current situation: an already-`OPEN` replay leaves database state untouched, reloads the stored category/title, and publishes `listing_approved` and `gig_category_matched` again. A publication error still returns an error so the input message remains retryable.
- Why: close the commit/publish loss window with the simplest current architecture. The resulting contract is at-least-once event publication, so duplicate events are acceptable on replay and downstream consumers must be idempotent.

### 2026-08-09 — AWS staging credential lifecycle

- Previous situation: staging loaded the AWS default credential provider once, immediately retrieved credentials, copied only access key/secret into application config, and later rebuilt a static credential provider. This discarded the session token and prevented IAM/STS credentials from refreshing.
- Current situation: staging AWS clients use the AWS SDK default credential chain directly, preserving temporary session tokens and automatic refresh. Development still uses the explicitly configured static credentials needed for LocalStack. An SQS endpoint override is applied only when one is actually configured.
- Why: staging should rely on IAM/default-chain credential lifecycle rather than snapshotting temporary credentials at process startup, while local development should remain simple and deterministic.

### 2026-08-09 — Bounded AI fallback execution

- Previous situation: every provider inherited only the worker's outer two-minute message deadline. A slow or hung provider could consume most of that budget before fallback, leaving little time for later providers, embeddings, database persistence, and notification publication.
- Current situation: each AI provider attempt is bounded to 25 seconds and the entire sequential provider chain is bounded to 75 seconds. A provider error, invalid response, or per-provider timeout falls through to the next configured provider. Caller cancellation or the 75-second total deadline stops fallback immediately.
- Why: external AI calls are the highest-variance part of the pipeline. Explicit nested budgets make worst-case latency predictable while preserving enough of the worker's two-minute deadline for the non-AI steps required to finish the message safely.

### 2026-08-09 — Taxonomy mutation safeguards deliberately unchanged

- Previous situation: new canonical categories are created automatically when the AI proposes a genuinely missing concept and sibling matching does not find an equivalent existing category.
- Current situation: that behavior remains unchanged; no proposal/review queue, confidence gate, or manual approval layer was added.
- Why: the current dynamic-taxonomy behavior is intentional for this stage and the user explicitly decided additional taxonomy-creation safeguards are unnecessary. Control quality through prompts, semantic sibling reuse, validation, regression examples, and the new created-vs-reused metrics rather than adding workflow complexity.

### 2026-08-09 — Staging operational metrics

- Previous situation: telemetry exposed HTTP metrics plus a generic worker count/duration. Failed and retryable messages were not cleanly separated, AI fallback/provider failures were invisible as metrics, and there were no domain signals for category creation/reuse or embedding failures.
- Current situation: OpenTelemetry now records successful messages, failed messages, transient retry decisions, processing duration, AI provider failures, AI fallback transitions, category created/reused decisions for listing/profession flows, and embedding failures. Labels are intentionally low-cardinality.
- Why: staging needs enough signal to answer whether the worker is healthy, why work is retrying, which AI providers are degrading, and whether taxonomy behavior is changing unexpectedly without relying on manual log inspection.

### 2026-08-09 — Event timestamp completeness

- Previous situation: `listing_approved` populated `occurred_at`, but `gig_category_matched` left it unset and serialized Go's zero time (`0001-01-01T00:00:00Z`). The existing JSON contract test encoded that broken value.
- Current situation: `gig_category_matched` now sets `occurred_at` to the UTC publication time, and contract/processor tests require a real non-zero timestamp.
- Why: downstream event timestamps must represent an actual occurrence/publication rather than an accidental zero-value implementation detail.

### 2026-08-09 — Versioned, deduplicatable notification events

- Previous situation: notification envelopes contained only `type` and `payload`. Replays could intentionally publish duplicates, but consumers had no stable event identity for deduplication and no explicit schema version for contract evolution.
- Current situation: all notification envelope types carry `event_id` and `version`; the current schema version is `1`. IDs are deterministic for the logical event, so retries/replays produce the same ID instead of a fresh UUID. `listing_approved` uses event type + listing UUID; `gig_category_matched` uses event type + listing UUID + category UUID.
- Why: the worker deliberately provides at-least-once publication. Stable IDs let consumers implement practical idempotency, while explicit versions let event contracts evolve without inferring schema shape from payload fields.

### 2026-08-09 — Profession extraction is category-only

- Previous situation: `ProfessionPublished` called the same full extraction path as listings. Providers generated both the category path and gig details even though profession processing discarded the gig-details result.
- Current situation: extractors expose a category-path-only operation and `ProfessionProcessor` uses it through the same sequential fallback/time-budget policy. Listing processing still performs category path plus gig-details extraction.
- Why: profession registration only persists a category association. Avoiding the second prompt removes unnecessary AI latency, quota usage, and cost without changing taxonomy behavior.

### 2026-08-09 — Graceful shutdown budget aligned with systemd

- Previous situation: the application already stopped receiving on SIGTERM and allowed in-flight messages up to their two-minute processing deadline before running up to 10 seconds of HTTP/telemetry/resource cleanup, but the repository did not define the corresponding `systemd` stop budget. A unit with a shorter `TimeoutStopSec` could therefore force-kill the process before that graceful drain completed.
- Current situation: the two-minute message processing deadline is preserved, application cleanup has an explicit 10-second budget, and `deploy/systemd/10-graceful-stop.conf` sets `KillSignal=SIGTERM` and `TimeoutStopSec=135s`. The drop-in is intended to be installed for the actual worker unit used in staging.
- Why: 135 seconds covers the worst-case 120 seconds of remaining in-flight work plus 10 seconds of application cleanup with a 5-second margin. This keeps the service-manager timeout aligned with the application's existing graceful-shutdown behavior without shortening valid message processing.

### 2026-08-24 — Google Cloud production configuration

- Previous situation: production configuration failed explicitly because Google Cloud parameter loading had not been implemented.
- Current situation: `APP_ENV=production` uses Application Default Credentials to render the worker's existing parameter IDs from Google Cloud Parameter Manager, reads `GOOGLE_CLOUD_PROJECT`, defaults `GCP_PARAMETER_LOCATION` to `global`, and resolves PostgreSQL credentials through Google Secret Manager. The legacy SQS-named messaging parameters contain fully-qualified Pub/Sub topic names in production.
- Transport boundary: production uses a dedicated Pub/Sub subscription consumer and Pub/Sub notification publisher. Retryable processing failures are nacked, successful and permanently invalid events are acked, and AWS SQS remains staging-only.
- Why: production configuration should be testable and provider-correct without ever passing Pub/Sub resource names into AWS SDK clients or pretending that the transport migration is complete.

### 2026-08-09 — Environment-aware trace sampling

- Previous situation: OpenTelemetry used `AlwaysSample()` unconditionally, so every deployed message trace was exported regardless of environment or upstream sampling decisions.
- Current situation: development still samples 100% of traces; staging and production use parent-based 10% root sampling and honor parent sampling decisions. The active telemetry initializer and the standalone tracer helper use the same sampler policy.
- Why: retain full local debugging visibility while bounding collector/storage volume in deployed environments. Parent-based sampling also keeps distributed traces coherent when an upstream service has already made the sampling decision.

### 2026-08-09 — Legacy for-sale path removed

- Previous situation: launch behavior was already gig-only, but the repository still carried an unused base-family classifier, `for-sale`/sale prompts, sale extraction structures and validation, a `sale_details` repository writer, sale-aware open-listing reload logic, and tests for that inactive path.
- Current situation: the worker contains only the gig/service extraction path. Base-family classification and sale-specific prompts/types/validation/persistence code are removed; reloading an `OPEN` listing reads its title from `gig_details` only. Tests now cover the active gig-only contract rather than preserving dead sale behavior.
- Why: dead multi-family code made the supported domain look broader than it is, increased maintenance surface, and could accidentally be revived without a deliberate product decision. If product-sale support returns, it should be designed against current requirements rather than inherited from stale partial implementation.

### 2026-08-09 — OPEN DECISION: category localization lifecycle

- Previous situation: category localizations were effectively first-write-wins through `ON CONFLICT (category_id, locale) DO NOTHING`, and this was briefly treated as a settled domain policy.
- Current situation: keep the existing first-write-wins runtime behavior unchanged for now, but treat the long-term localization lifecycle as an open decision. Normal categorization still must not overwrite an existing `(category_id, locale)` row.
- Why: the current behavior is stable and safe for staging, so there is no reason to introduce automatic updates now. We have not yet decided whether future corrections should remain manual/admin-only or use a separate controlled localization-maintenance path. Do not change this behavior incidentally; revisit it only as an explicit product/domain decision.

### 2026-08-09 — OPEN DECISION: taxonomy retrieval scaling

- Previous situation: every categorization request builds prompt context from the full active taxonomy hierarchy. This is simple and gives the model complete visibility, but prompt size, latency, and token cost will grow with the taxonomy.
- Current situation: keep full-taxonomy prompting unchanged for now. Do not add embedding pre-retrieval, top-N candidate filtering, subtree selection, or other narrowing logic yet.
- Why: taxonomy size is not yet demonstrated to be a practical bottleneck, and premature retrieval logic would add complexity while potentially hiding relevant categories from the model. Revisit this decision when measured taxonomy size, prompt tokens, latency, model context pressure, or AI cost show that the full hierarchy is no longer efficient. The likely future direction is retrieval of relevant categories/branches (potentially using existing embeddings/pgvector) followed by LLM reasoning over that reduced context, but the exact strategy remains undecided.

### 2026-08-09 — Staging Mistral/Nvidia provider configuration completed

- Previous situation: the staging fallback order included Mistral and Nvidia, but AWS configuration was incomplete in two different ways. Mistral parameters were fetched from SSM but never assigned into `cfg.AI.Mistral`; Nvidia fields were assigned from the parameter map but its SSM parameters were never requested, leaving them empty. Both providers could therefore be silently omitted from the active staging fallback chain despite appearing in `cmd/worker/app.go`.
- Current situation: staging requests and maps API key, endpoint/base URL, and model parameters for both Mistral and Nvidia. Tests cover both the staging SSM parameter list and the final AI configuration mapping for all fallback providers.
- Why: provider fallback is only real if every declared provider is fully wired in the deployment configuration. Silent partial wiring weakens resilience and makes the configured provider order misleading.

### 2026-08-09 — Focused runtime cleanup without business-behavior changes

- Previous situation: related timeout budgets were duplicated across worker, extractor, and application packages; full extraction and category-only extraction duplicated the same fallback loop; notification SQS publication duplicated marshal/send logic; and unused `gig_rejected` notification types/publisher code remained after the legacy sale path was removed.
- Current situation: shared runtime budgets live in `internal/runtimepolicy/timeouts.go`; both extraction modes use one fallback implementation while preserving their existing behavior/error contract; notification publishing uses one internal marshal/send helper; and the unused `gig_rejected` event/publisher surface is removed. Health/readiness comments also no longer assume a container orchestrator because staging runs under `systemd`.
- Why: reduce accidental maintenance surface and prevent policy drift without changing active listing/profession processing semantics. The cleanup keeps domain behavior intact while making future operational changes less likely to become inconsistent across packages.

### 2026-08-09 — Categorization golden regression evaluator added

- Previous situation: the repository already had a large opt-in live AI benchmark corpus and the current prompt/provider/model combination had been selected after substantial manual experimentation, but there was no reviewed machine-readable baseline that could say whether a future prompt/model/taxonomy change altered the categorization behavior we currently consider good.
- Current situation: keep the worker runtime, prompts, provider order, and extraction behavior unchanged. A separate opt-in `cmd/evaluate` command reuses `internal/extractor/example.txt`, reads the current `gig` taxonomy from PostgreSQL, and runs the existing category-path fallback logic. `make capture-ai-baseline` writes the current behavior to the intentionally untracked `testdata/evaluation/candidate.json`; that file must be reviewed and deliberately promoted to `testdata/evaluation/golden.json`. `make evaluate-ai` then compares live results with that approved baseline and exits non-zero on drift. The comparison intentionally checks source locale, the full canonical-slug path, and every node's `is_selectable` flag while ignoring generated prose and which provider/model won the fallback chain. The evaluator performs read-only taxonomy access and does not publish SQS messages or mutate marketplace data.
- Why: the current AI behavior is an asset we want to protect, not casually retune. A reviewed golden baseline turns the user's existing model/prompt experimentation into a regression contract without making nondeterministic, quota-consuming live AI calls part of ordinary `go test`. Ignoring prose/provider identity avoids false regressions when the business categorization decision is unchanged, while exact canonical path/selectability/locale comparison still catches changes that matter to the marketplace. Never refresh or overwrite the golden baseline automatically after a failure; baseline changes require deliberate review because otherwise the evaluator would simply bless regressions.


### 2026-08-09 — Processor E2E tests use isolated disposable infrastructure

- Previous situation: `make test-e2e` sourced `.env` wholesale and expected its database/SQS hostnames to be reachable from the host running `go test`. Docker-network or staging-only names such as `postgres-svc` therefore failed DNS resolution locally, and pointing the suite at a shared platform database risked mutating shared state. This repository also does not own the platform's base schema, so its production migrations cannot bootstrap a blank database by themselves.
- Current situation: `make test-e2e` starts disposable `pgvector/pgvector:pg16` and LocalStack SQS containers from `docker-compose.test.yml`, waits for them to become healthy, overrides all DB/SQS settings with loopback test endpoints, runs the processor E2E suite, then tears down the containers and database volume via a shell trap. `testdata/postgres/init.sql` is a test-only minimal final schema containing only tables/constraints the worker reads or writes; it is not a production migration. The E2E suite still uses a configured real AI provider and local Ollama embeddings. `make test-e2e-external` remains available only when an intentionally configured shared environment is desired.
- Why: E2E tests should be reproducible and must not depend on deployment-network DNS or mutate shared staging data merely to validate processor behavior. Keeping the minimal schema under `testdata` also respects database ownership: production schema remains owned by the appropriate platform services while this repository gets an isolated fixture for testing its SQL contract.

## Repository map

- `cmd/worker/`: dependency wiring and message-type dispatch.
- `internal/worker/`: SQS polling, bounded concurrency, retry/backoff, deletion, shutdown.
- `internal/processor/gig.go`: listing pipeline, validation, taxonomy resolution, atomic persistence, notifications.
- `internal/processor/profession.go`: user service/profession pipeline and stale-request protection.
- `internal/processor/repository.go`: PostgreSQL contracts and invariant-enforcing SQL.
- `internal/extractor/`: provider clients, response decoding, provider fallback, AI benchmark.
- `internal/evaluation/`: golden categorization baseline schema and regression comparison.
- `cmd/evaluate/`: opt-in live categorization regression runner; never part of worker startup.
- `testdata/evaluation/`: reviewed golden baseline documentation/data.
- `internal/prompter/`: embedded prompts and builders.
- `internal/notification/`: emitted event types and SQS publisher.
- `internal/runtimepolicy/`: shared processing, visibility, AI fallback, and shutdown time budgets.
- `pkg/embeddings/`: local Ollama embedding client.
- `pkg/dbtx/`: transaction context propagation.
- `migrations/`: category-domain schema.
- `deploy/systemd/`: systemd drop-in settings required for graceful worker shutdown.

## Working practices

- Treat the user as a technical collaborator. Be direct, informal when they are informal, and lead with the actual outcome or diagnosis.
- The user prefers autonomous implementation after giving direction. Inspect the code and make safe in-scope decisions instead of repeatedly asking for confirmation.
- Explain important trade-offs briefly and concretely. When another service must change, provide a copy-pasteable contract/prompt for that service.
- The user wires configuration and credentials. Do not redesign or edit configuration unless explicitly asked; state which variable shape is required without inspecting secret values.
- Use the project's local Go installation when available: `/usr/local/go/bin/go` and `/usr/local/go/bin/gofmt`.
- In restricted environments, set `GOCACHE` to a task-specific directory under `/tmp`; do not repurpose `$HOME`.
- Prefer `rg`/`rg --files` for inspection and explicitly exclude `.env` files.
- Use `apply_patch` for manual file edits.
- Preserve unrelated dirty-worktree changes. Inspect `git status` before editing and before committing.
- Do not commit or push unless the user asks. If asked to commit only, do not push.
- After each implemented fix, update this file's decision history with `Previous situation`, `Current situation`, and `Why`, and provide the user a commit message for that fix/change set.
- Before handing off implementation changes, normally run:

```bash
GOCACHE=/tmp/yevmiye-worker-go-cache /usr/local/go/bin/go test ./...
GOCACHE=/tmp/yevmiye-worker-go-cache /usr/local/go/bin/go vet ./...
git diff --check
```

- Run `go test -race` for concurrency/worker changes when practical.
- Live AI/database benchmark tests are intentionally opt-in and may take many minutes or consume provider quota. Do not run them casually. The benchmark is designed to process all examples and persist results for inspection when explicitly requested.
- Live categorization evaluation is also opt-in. Treat `testdata/evaluation/golden.json` as an approved behavior contract: capture candidates separately, review them, and never auto-update the golden baseline merely to make a regression pass.
- Never claim a production integration is verified merely because unit tests pass; clearly distinguish unit tests, local Docker/LocalStack checks, live provider tests, and real shared-database tests.

## How to reason about future changes

Before editing, trace the full contract:

```text
Core event
  -> SQS envelope/type dispatch
  -> decoder and validation
  -> AI prompts and response schema
  -> canonical matching/embedding
  -> transactional repository writes
  -> Core database constraints
  -> notification/search event consumers
```

A change is incomplete if only one layer is updated. Add tests at the narrowest useful boundaries, preserve UUID identity and atomicity, and explicitly tell dependent services what changed.
