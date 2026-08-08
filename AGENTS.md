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
- `for-sale` types and some legacy code may remain, but do not add or process for-sale categories unless the user explicitly reopens that feature.
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

- `base_category.txt`: legacy base-family classification prompt.
- `category_path.txt`: source locale detection plus canonical English hierarchy and source-locale metadata.
- `profession.txt`: structured gig/service details.
- `sale.txt`: retained legacy sale extraction prompt; not launch scope.

`internal/prompter/loader.go` embeds the prompt files. `builder.go` injects the current active canonical catalog into the category-path prompt.

Category extraction and details extraction are separate AI calls. The category-path response includes `source_locale`, canonical English fields, localized source-language fields, and `is_selectable` for every node. Details extraction produces the listing title and gig details. Validation must reject malformed paths before persistence.

Configured providers are assembled in `cmd/worker/app.go`. Current fallback order is:

1. Gemini
2. Groq
3. OpenRouter
4. Mistral
5. Nvidia

Only fully configured providers are included. `FallbackExtractor` tries providers sequentially, records provider/model provenance as `generated_by`, stops on success, and returns joined errors when all fail. Each provider attempt has a 25-second deadline and the whole fallback chain has a 75-second deadline. Provider-level errors, invalid responses, and per-provider timeouts allow the next configured provider to run; caller cancellation or exhaustion of the total fallback deadline stops the chain immediately. Keep these budgets below the worker's two-minute message deadline so embeddings, database work, and notification publication retain time to finish. Keep output token limits controlled; oversized `max_tokens` previously caused avoidable credit failures.

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

- `country_id` and `place_id` are optional UUIDs.
- `place_id` requires `country_id`.
- Do not use or populate legacy `city_id` or `district_id`.
- Do not reinterpret a place as necessarily a city/district; Core's `places` catalog supports arbitrary administrative levels and localities.
- Core already persists listing location. The worker preserves event values and forwards them where needed; it does not translate them to legacy IDs.

Core creates a listing as:

```text
status = PROCESSING
base_category_id = NULL
```

The worker must:

1. Resolve active `gig` base-category UUID by slug.
2. Extract/resolve the canonical hierarchy and source localization.
3. Generate canonical embeddings.
4. Start a transaction and lock the listing row.
5. If already `OPEN`, do not mutate listing/category state again; reload the stored classification and continue to notification publication.
6. While still `PROCESSING`, set `base_category_id` and `description_clean`.
7. Reuse/create every category path node and ensure its source-locale localization.
8. Attach only the final active selectable gig category through `listing_categories`.
9. Upsert `gig_details`.
10. Set the listing to `OPEN` only after all prerequisites exist.
11. Commit, then publish notifications.

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

The final association must be an active, selectable category in the `gig` base category. A user has exactly one selected offered-service category.

## Emitted events

After a listing commits, and again on an `OPEN` replay, the worker publishes:

- `listing_approved`: recipient, listing UUID, base-category slug, title, timestamp.
- `gig_category_matched`: listing/gig UUID, category UUID, optional `country_id`, optional `place_id`, timestamp.

The category UUID is identity. Never attach localized names/slugs as identity in listing, user, notification, or search contracts.

The notification/search Lambda historically consumed integer `city_id`/`district_id`. New events use UUID `country_id`/`place_id`; keep downstream consumers informed and do not silently restore legacy fields.

Notifications happen after the database commit. DB work is atomic, but event publication is not in the same transaction. An `OPEN` replay intentionally publishes the required events again. This gives the current design at-least-once publication semantics: a post-commit publish failure can recover on SQS redelivery, while duplicate downstream events remain possible and consumers must tolerate them. Do not publish from inside the DB transaction. If strict deduplication/exactly-once delivery becomes a requirement, revisit the design explicitly rather than hiding it in processor logic.

## SQS reliability and concurrency

- The worker long-polls SQS in batches of up to 10.
- Processing concurrency is bounded by configuration; the DB pool is sized with headroom.
- A single message has a two-minute processing deadline.
- The receive visibility timeout is three minutes and must remain strictly longer than the processing deadline. The extra minute prevents SQS from redelivering a message while a valid in-flight attempt may still be running.
- Shutdown stops receiving and drains in-flight work; in-flight work is detached from the shutdown context but remains timeout-bound.
- Panics are recovered per message so one bad record cannot crash the process.
- Delete an SQS message after successful processing, or when it is explicitly classified as a permanent message-contract failure that retrying cannot fix.
- Permanent failures are intentionally narrow: missing/unknown message type, malformed message JSON, or invalid required payload fields. Do not classify database, AI/provider, embedding, network, timeout, panic, or other runtime/dependency failures as permanent.
- Transient/runtime failures remain undeleted for normal SQS redelivery. DLQ behavior belongs to the AWS queue redrive policy; do not add application-side DLQ publishing unless that architecture is explicitly changed.
- If deleting a permanently invalid message fails, leave it retryable because the worker has not successfully acknowledged/discarded it.
- Missing LocalStack queues or receive failures use exponential backoff: 1s, 2s, 4s, 8s, 16s, then 30s maximum. Reset after a successful receive.
- Keep database processing idempotent because SQS is at-least-once. Notification publication is also at-least-once and may duplicate on replay.

## Database ownership and migrations

Migrations in `migrations/` define the worker-owned category domain:

- `000001`: hierarchy, depth enforcement, sibling uniqueness.
- `000002`: `generated_by` provenance.
- `000003`: explicit `is_selectable`.
- `000004`: canonical English columns and `category_localizations`.

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

## Repository map

- `cmd/worker/`: dependency wiring and message-type dispatch.
- `internal/worker/`: SQS polling, bounded concurrency, retry/backoff, deletion, shutdown.
- `internal/processor/gig.go`: listing pipeline, validation, taxonomy resolution, atomic persistence, notifications.
- `internal/processor/profession.go`: user service/profession pipeline and stale-request protection.
- `internal/processor/repository.go`: PostgreSQL contracts and invariant-enforcing SQL.
- `internal/extractor/`: provider clients, response decoding, provider fallback, AI benchmark.
- `internal/prompter/`: embedded prompts and builders.
- `internal/notification/`: emitted event types and SQS publisher.
- `pkg/embeddings/`: local Ollama embedding client.
- `pkg/dbtx/`: transaction context propagation.
- `migrations/`: category-domain schema.

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
- Before handing off implementation changes, normally run:

```bash
GOCACHE=/tmp/yevmiye-worker-go-cache /usr/local/go/bin/go test ./...
GOCACHE=/tmp/yevmiye-worker-go-cache /usr/local/go/bin/go vet ./...
git diff --check
```

- Run `go test -race` for concurrency/worker changes when practical.
- Live AI/database benchmark tests are intentionally opt-in and may take many minutes or consume provider quota. Do not run them casually. The benchmark is designed to process all examples and persist results for inspection when explicitly requested.
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
