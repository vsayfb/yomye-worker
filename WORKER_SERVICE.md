# Categorization Worker Service

## Overview

The **categorization worker** is Yövmiye's asynchronous classification and taxonomy service.

Its primary responsibility is to convert free-form marketplace text into structured, reusable domain data. It consumes events from AWS SQS, asks an AI model to interpret the service/profession described by the user, resolves that interpretation against the existing global category tree, creates genuinely missing categories when necessary, stores the result in PostgreSQL, and publishes downstream events for other services.

The worker is currently **gig/service-only**. It does not classify product/for-sale listings.

At a high level:

```text
Core / API
   |
   | ListingCreated / ProfessionPublished
   v
AWS SQS: categorization queue
   |
   v
Categorization Worker
	|
	+--> AI providers: moderate restricted/adult-only content
	|
	+--> PostgreSQL: load active category taxonomy
   |
   +--> AI providers: understand text and propose category path
   |
   +--> Ollama embeddings: semantic category matching
   |
   +--> PostgreSQL: reuse/create taxonomy and persist classification
   |
   +--> AWS SQS: notification/search events
            |
            +--> listing_approved
            +--> gig_category_matched
			+--> content_rejected
```

The service is designed around three important ideas:

1. **Category UUIDs are durable identity.** Names, slugs, descriptions, and translations are metadata.
2. **The category hierarchy is dynamic.** The AI may propose a missing reusable concept; the worker can create it automatically.
3. **AI output is not written blindly.** It is validated, normalized, compared to existing sibling categories, and persisted transactionally.

---

## Service Responsibilities

The worker owns the following behavior:

- Consume categorization events from SQS.
- Moderate complete listing/profession text before taxonomy work.
- Detect the service/profession represented by arbitrary user text.
- Detect the source language/locale.
- Resolve the active `gig` base category from PostgreSQL.
- Load the current active category hierarchy.
- Ask configured AI providers for a canonical category path.
- Extract gig-specific details for listing events.
- Generate embeddings for proposed category nodes.
- Reuse semantically equivalent existing categories.
- Create genuinely missing category nodes.
- Maintain canonical English category metadata.
- Maintain source-language category localizations.
- Attach one final selectable category to a listing or user profession.
- Persist gig details and transition listings from `PROCESSING` to `OPEN`.
- On an explicit valid moderation rejection, transition a listing from `PROCESSING` to `FAILED` without taxonomy writes.
- Publish downstream notification/search events.
- Provide retry-safe/idempotent behavior for SQS redelivery.
- Emit metrics, traces, and structured logs.

The worker does **not**:

- Create the original listing or user.
- Own the location catalog.
- Maintain a hard-coded profession list.
- Maintain separate category identities per language.
- Publish profession-completed events.
- Perform search itself.
- Guarantee exactly-once event delivery.
- Currently pre-filter the taxonomy using embeddings before the AI call.

---

# Runtime Architecture

## Main Components

```text
cmd/worker
   |
   +-- application/configuration wiring
   +-- health/readiness HTTP server
   +-- graceful shutdown
   |
   v
internal/worker
   |
   +-- SQS polling
   +-- bounded concurrency
   +-- retry/permanent error handling
   +-- trace propagation
   |
   v
internal/processor
   |
   +-- GigProcessor
   +-- ProfessionProcessor
   +-- PostgreSQL repository
   |
   +----------------------+----------------------+
   |                      |                      |
   v                      v                      v
internal/extractor   pkg/embeddings       internal/notification
AI providers         Ollama + MiniLM       SQS publisher
```

Important supporting packages:

- `internal/prompter`: embedded AI prompts and dynamic taxonomy prompt construction.
- `internal/runtimepolicy`: shared timeout/visibility budgets.
- `pkg/metrics`: OpenTelemetry metrics.
- `pkg/tracing`: OpenTelemetry tracing and SQS trace propagation.
- `pkg/database`: PostgreSQL pool creation.
- `migrations`: one complete Core-v18-compatible worker-owned category-domain baseline.
- `cmd/workerctl`: staging maintenance binary; `migrate up` atomically applies pending embedded worker migrations before deployment.

---

# Input Events

The worker currently accepts three SQS message types.

## `ListingCreated`

Current listing event:

```json
{
  "listing_id": "uuid",
  "author_id": "uuid",
  "description": "Musluğumu tamir edecek bir tesisatçı arıyorum",
  "country_id": "optional-uuid",
  "place_id": "optional-uuid"
}
```

Validation rules:

- `listing_id` is required.
- `author_id` is required.
- `description` must not be empty.
- `country_id` is present for local listings and absent for remote listings.
- `place_id` is optional, requires `country_id`, and is absent for remote listings.

The listing is expected to already exist in Core with approximately:

```text
status = PROCESSING
base_category_id = NULL
```

## `GigPublished`

`GigPublished` is currently accepted as a compatibility alias and uses the same `GigProcessor` as `ListingCreated`.

No separate business behavior exists for it.

## `ProfessionPublished`

```json
{
  "request_id": "uuid",
  "author_id": "uuid",
  "description": "Oto Yıkama"
}
```

Validation rules:

- `request_id` is required.
- `author_id` is required.
- `description` must not be empty.

`request_id` is important because a user may update their profession multiple times while older SQS messages are still in flight.

---

# SQS Consumption Model

The worker uses AWS SQS long polling.

Current behavior:

```text
ReceiveMessage
  MaxNumberOfMessages = 10
  WaitTimeSeconds      = 20
  VisibilityTimeout    = 180 seconds
```

Processing concurrency is bounded by `WORKER_CONCURRENCY`, with a default of `10`.

Each received message gets its own processing context with a hard deadline:

```text
Message processing timeout = 120 seconds
SQS visibility timeout     = 180 seconds
```

The visibility timeout intentionally exceeds the maximum message processing time so a normally executing message should not become visible to another worker while still being processed.

The worker stops polling when it receives shutdown cancellation, but messages that have already been received continue under a detached context until they finish or hit the 120-second limit.

---

# Error and Retry Semantics

The worker distinguishes **permanent message-contract errors** from **transient/runtime errors**.

## Permanent failures

These are errors where receiving exactly the same SQS record again cannot fix the problem.

Examples:

- Missing message type.
- Unsupported/unknown message type.
- Malformed JSON.
- Missing required event UUIDs.
- Empty required description.
- Invalid message-level location combination.

Permanent messages are logged and deleted from SQS.

## Transient failures

Examples:

- PostgreSQL failure.
- AI provider failure.
- AI timeout.
- Embedding service failure.
- SQS publication failure.
- Network failure.
- Runtime dependency failure.
- Panic during processing.

These messages are **not deleted**. SQS makes them available again according to the queue's redelivery/redrive policy.

The DLQ is an AWS SQS infrastructure concern. The worker does not manually publish failed records to a DLQ.

## Receive failures

Failures while polling SQS use exponential backoff:

```text
1s -> 2s -> 4s -> 8s -> 16s -> 30s -> 30s ...
```

The backoff resets after a successful receive.

## Panic isolation

A panic while handling one message is recovered at the per-message boundary. One malformed or unexpected record cannot terminate the entire worker process.

---

# Listing Processing: End-to-End

A `ListingCreated` event follows this pipeline.

```text
ListingCreated
    |
    v
Validate event contract
	|
	v
AI content moderation
	|
	+--> explicit reject: mark FAILED, publish content_rejected, stop
	+--> provider/decode failure: retry with no mutation/event
	|
	v allow
Resolve active "gig" base category
    |
    v
Load active canonical category catalog
    |
    v
AI category-path extraction
    |
    v
AI gig-details extraction
    |
    v
Validate + normalize AI output
    |
    v
Generate embedding for every proposed path node
    |
    v
BEGIN PostgreSQL transaction
    |
    +--> lock listing
    +--> reuse/create taxonomy nodes
    +--> ensure source-locale localizations
    +--> attach final selectable category
    +--> upsert gig_details
    +--> mark listing OPEN
    |
    v
COMMIT
    |
    +--> publish listing_approved
    +--> publish gig_category_matched
    |
    v
Delete input SQS message
```

Each stage is described below.

---

## 1. Resolve the Base Category

The worker is currently scoped to services/gigs.

It never hard-codes the base-category UUID. Instead it resolves:

```sql
SELECT id
FROM base_categories
WHERE slug = 'gig'
  AND is_active = TRUE;
```

This means the stable business identifier is the slug `gig`, while the database UUID remains deployment/database specific.

If the active base category does not exist, processing fails as a configuration/schema problem.

---

## 2. Load the Current Category Catalog

The worker loads all active categories for the `gig` base category using a recursive query.

The hierarchy is ordered by its canonical path and formatted into text such as:

```text
- Home Services: Services performed in or around residential properties. [selectable=false]
  - Plumbing: Installation and maintenance of water and pipe systems. [selectable=false]
    - Plumber: Plumbing installation and repair services. [selectable=true]
- Automotive Services: Services related to vehicles. [selectable=false]
  - Car Wash: Exterior and interior vehicle washing. [selectable=true]
```

That current catalog is injected directly into the category-path system prompt.

This is intentional: while the taxonomy is still manageable, the AI receives complete visibility of the current canonical hierarchy and can prefer reuse over unnecessary category creation.

### Important current limitation / open scaling decision

Embeddings are **not currently used to pre-filter this catalog before prompting the AI**.

Therefore the current flow is:

```text
full active taxonomy -> AI -> proposed path -> embedding-based DB resolution
```

not:

```text
input embedding -> top-N taxonomy branches -> AI
```

Retrieval-based narrowing may be added later if taxonomy size, token cost, latency, or context-window pressure becomes significant.

---

# AI Extraction Architecture

## Provider Order

Configured providers are tried sequentially in this order:

1. Groq
2. OpenRouter
3. Gemini
4. Mistral
5. Nvidia

A provider is only added to the runtime fallback chain when all configuration required by that provider is present.

For OpenAI-compatible providers, the worker uses a common OpenAI client against each provider's configured base URL.

The generator is recorded as provenance, for example:

```text
gemini:gemini-model-name
groq:model-name
open-router:model-name
mistral:model-name
nvidia:model-name
```

That value is persisted as `generated_by` for newly generated category metadata/localizations.

---

## AI Time Budgets

The AI subsystem has nested deadlines:

```text
Per provider attempt = 25 seconds
Entire fallback chain = 75 seconds
Entire SQS message    = 120 seconds
```

A provider-level error or per-provider timeout allows fallback to the next configured provider.

The fallback chain stops immediately when:

- one provider succeeds;
- the caller cancels the operation; or
- the total 75-second AI deadline is exhausted.

This leaves part of the message budget available for embeddings, PostgreSQL persistence, and notification publication.

---

# Category-Path Prompt

The category-path prompt is the central taxonomy prompt.

Its purpose is:

> Given marketplace text in any language and the current global taxonomy, return the most appropriate reusable canonical category path.

The AI is asked to return JSON shaped like:

```json
{
  "source_locale": "tr",
  "category_path": [
    {
      "canonical_name": "Home Services",
      "canonical_slug": "home-services",
      "canonical_description": "...",
      "localized_name": "Ev Hizmetleri",
      "localized_slug": "ev-hizmetleri",
      "localized_description": "...",
      "locale": "tr",
      "is_selectable": false
    },
    {
      "canonical_name": "Plumber",
      "canonical_slug": "plumber",
      "canonical_description": "...",
      "localized_name": "Tesisatçı",
      "localized_slug": "tesisatci",
      "localized_description": "...",
      "locale": "tr",
      "is_selectable": true
    }
  ]
}
```

## Main prompt rules

The prompt instructs the model to:

- Detect `source_locale` as a lowercase BCP-47-style language tag.
- Keep canonical category metadata in English.
- Produce localized presentation metadata in the source language.
- Reuse an existing canonical path whenever it reasonably represents the same concept.
- Create a category only when the concept is genuinely missing among the correct siblings.
- Categorize the reusable service/trade/occupation, not the exact sentence or one-off task.
- Prefer provider-neutral service concepts where appropriate.
- Preserve conventional occupation names where the role itself is the recognized concept.
- Avoid artificial suffixes such as `Expert`, `Professional`, `Specialist`, etc.
- Avoid broadening the input with adjacent services.
- Avoid narrowing the input to an unsupported specialization.
- Avoid generic cross-sector roots such as "Professional Services" when a meaningful sector exists.
- Mark grouping-only nodes as `is_selectable=false`.
- Mark reusable services/trades/occupations as `is_selectable=true`.
- Always make the final category selectable.
- Prefer two or three meaningful levels.
- Never return more than four levels.
- Avoid duplicate or nearly identical adjacent levels.
- Treat translations as the same canonical concept rather than separate categories.

Example identity rule:

```text
Lawyer        (English)
Avukat        (Turkish)
Rechtsanwalt  (German)

             |
             v
      one Category UUID
```

---

# Gig-Details Prompt

`ListingCreated` performs a second AI extraction after category-path extraction.

The gig-details prompt returns:

```json
{
  "title": "Musluk Tamiri İçin Tesisatçı",
  "profession": "Tesisatçı",
  "duration_type": "ONE_TIME",
  "pay_amount": null,
  "pay_currency": "TRY",
  "expires_at": null
}
```

The prompt is intentionally conservative:

- Do not invent missing details.
- Keep the title short and natural in the source language.
- Use a normalized duration enum when explicitly inferable.
- Use numeric pay or `null`.
- Default currency to `TRY` when omitted.
- Do not add adjacent professions or services.
- Preserve a provider-neutral service name where the input describes a business/service activity.

### Profession events do not run this prompt

`ProfessionPublished` uses `ExtractCategoryPath()` only.

It does **not** generate gig details because they are not needed for user profession registration.

---

# AI Response Validation and Normalization

AI output is validated before database persistence.

Important rules include:

- Response must contain a JSON object.
- Source locale must exist.
- Category path must not be empty.
- Path must contain at most four levels.
- Every node must have canonical and localized name/description metadata.
- Every node's locale must equal the extracted source locale.
- Final node must be selectable.
- Adjacent categories may not have the same canonical name.
- Gig extraction must contain a title and profession.
- Negative pay values are rejected.

## Slugs are normalized by the service

Although the AI returns canonical/localized slugs, those values are not blindly trusted.

The processor regenerates slugs from the returned names:

```text
canonical_slug = slug(canonical_name)
localized_slug = slug(localized_name)
```

This keeps slug generation deterministic and consistent across providers.

The gig `profession` field is also normalized to the final canonical category leaf instead of trusting an independently generated profession string.

---

# Category Domain Model

## Canonical identity

A category is globally identified by its UUID.

Canonical fields are English:

```text
categories
  id
  base_category_id
  parent_id
  canonical_name
  canonical_slug
  canonical_description
  depth
  is_active
  is_selectable
  embedding
  generated_by
  timestamps
```

Localized presentation lives separately:

```text
category_localizations
  category_id
  locale
  name
  slug
  description
  generated_by
  timestamps

PRIMARY KEY (category_id, locale)
```

This allows one canonical concept to have many localizations without creating multiple category identities.

---

## Hierarchy

Categories form a tree under a `base_category`.

Example:

```text
Home Services                      selectable=false
└── Plumbing                       selectable=false
    └── Plumber                    selectable=true
        └── Emergency Plumber      selectable=true   (possible future specialization)
```

`is_selectable` is explicit semantic information.

It does **not** mean "leaf".

A category can be selectable and later receive more specific children.

If an existing category is currently non-selectable but the AI later identifies it as a valid directly selectable service, the worker may promote it to selectable. It never demotes a selectable category during normal processing.

---

# Embeddings

## Why embeddings exist

The AI can propose semantically equivalent categories using different wording.

For example:

```text
Plumber
Plumbing Professional
Plumbing Specialist
```

Exact string comparison alone would risk creating duplicate categories.

Instead of calling another LLM to decide whether every proposed node matches an existing node, the worker generates a compact vector representation and lets PostgreSQL/pgvector perform a fast semantic sibling lookup.

This is where embeddings improve the pipeline:

- Fast semantic comparison in PostgreSQL.
- Less dependence on exact wording.
- Reduced duplicate-category creation.
- No extra LLM request for category equivalence.

They do **not currently reduce the size of the taxonomy sent to the AI**.

---

## Embedding implementation

The worker calls a local Ollama instance:

```text
POST /api/embeddings
```

Default model:

```text
all-minilm:l12-v2
```

Default endpoint:

```text
http://localhost:11434
```

The local embedding HTTP client has a short timeout of approximately two seconds.

The model produces a vector (`[]float32`) that is stored on the canonical category row using pgvector.

---

## What text is embedded

For every category-path node, the worker builds text from:

1. The canonical English breadcrumb up to that node.
2. The canonical category name.
3. The canonical category description.

Example:

```text
Home Services > Plumbing > Plumber
Plumber
Provides plumbing installation and repair services for water and pipe systems.
```

Including the breadcrumb gives the embedding more semantic context than embedding a single label such as `Plumber`.

Canonical English is used so equivalent concepts coming from different source languages are projected through the same semantic representation.

---

# Category Resolution Algorithm

For each proposed node, from root to leaf, the worker resolves the category inside the **current parent scope**.

Conceptually:

```text
AI proposes node
     |
     v
Generate canonical embedding
     |
     v
Search active siblings under same parent
     |
     +--> exact canonical slug match? ---- yes --> reuse
     |
     +--> exact canonical name match? ---- yes --> reuse
     |
     +--> embedding distance < 0.15? ----- yes --> reuse
     |
     v
No equivalent sibling
     |
     v
Create category
```

The database lookup is approximately:

```sql
WHERE base_category_id = ?
  AND parent_id IS NOT DISTINCT FROM ?
  AND is_active = TRUE
  AND (
       canonical_slug = ?
       OR lower(canonical_name) = lower(?)
       OR embedding_distance < 0.15
  )
```

Exact slug/name matches are preferred over semantic similarity.

## Why parent scope matters

Similarity is never applied globally across the entire taxonomy.

The proposed node is compared only with siblings under the same base category and parent.

That prevents an embedding similarity from accidentally merging concepts that happen to have similar words but belong in different domains.

---

## Concurrency protection during category creation

Multiple worker messages may propose the same missing category concurrently.

Before sibling resolution, the repository acquires a transaction-scoped PostgreSQL advisory lock based on:

```text
base category + parent category
```

This serializes competing category creation within that sibling scope.

Database uniqueness constraints and conflict-safe inserts provide an additional layer of protection.

The result is that concurrent messages should converge on one canonical category rather than creating duplicate siblings.

---

# Creating a New Category

When no equivalent sibling exists, the worker creates a canonical category with:

- new UUID;
- active `gig` base category;
- resolved parent UUID;
- canonical English name;
- canonical slug;
- canonical English description;
- generated embedding;
- `is_selectable` from validated AI semantics;
- generator/model provenance;
- depth determined by the hierarchy.

The category hierarchy is therefore **AI-assisted and dynamic**, not manually enumerated in application code.

A category should represent a reusable marketplace concept, not a one-off task.

Good:

```text
Car Wash
Plumber
Graphic Designer
Immigration Lawyer
Computer Repair
```

Bad:

```text
Fix My Kitchen Sink Tomorrow
Very Good Professional
Someone Who Can Help
Car Wash and Auto Detailing Expert
```

---

# Localizations

For every resolved path node, the worker ensures a localization for the detected source locale.

Example:

```text
Canonical category UUID: 7d...f1
canonical_name: Lawyer

category_localizations:
  en -> Lawyer
  tr -> Avukat
  de -> Rechtsanwalt
```

The current runtime behavior is first-write-wins:

```sql
ON CONFLICT (category_id, locale) DO NOTHING
```

Normal categorization does not continuously rewrite existing localizations.

The long-term localization maintenance/correction strategy remains an open design decision; current categorization processing preserves existing localization rows.

---

# Listing Persistence Transaction

After AI extraction and embeddings are complete, the worker opens a PostgreSQL transaction.

Inside the transaction:

```text
1. SELECT listing ... FOR UPDATE
2. Check listing state
3. Set base_category_id + description_clean
4. Resolve/reuse/create every category-path node
5. Ensure source-locale localization for each node
6. Attach only the final selectable category
7. Upsert gig_details
8. Transition listing to OPEN
9. COMMIT
```

## Listing state rules

### `PROCESSING`

This is the expected state for first processing.

The worker performs full persistence.

### `OPEN`

This indicates that database classification already succeeded on an earlier attempt.

The worker:

- does not rebuild/mutate persisted classification;
- reloads the stored category and title;
- continues to publish required downstream events.

This behavior is important for recovery after a database commit followed by an SQS publication failure.

### `FAILED`

For this worker, `FAILED` is the existing internal terminal status used for an explicit validated content-moderation rejection. Moderation dependency failures never set it. A rejected listing does not create categories, associations, embeddings, or `gig_details`.

### Other states

A state other than `PROCESSING`, `OPEN`, or the moderation-rejection `FAILED` state is non-processable.

---

# Listing Database Invariants

A listing is not allowed to become `OPEN` until all required data exists.

The final `UPDATE listings SET status='OPEN'` verifies that:

- `base_category_id` exists;
- an active selectable `gig` category is attached;
- the category association matches the listing base category;
- `gig_details` exists.

Therefore normal processing should never expose:

```text
OPEN listing
+ missing category
```

or:

```text
OPEN listing
+ missing gig details
```

The category writes, localization writes, listing association, gig details, and status transition are all part of the same transaction.

---

# Gig Details

The worker upserts:

```text
gig_details
  gig_id
  title
  duration_type
  pay_amount
  pay_currency
  expires_at
```

If the AI returns no explicit expiration time, the worker currently defaults expiration to approximately one month from processing time.

Currency defaults to `TRY` when omitted.

The final profession concept is tied to the resolved category leaf rather than allowing a second AI field to diverge from the taxonomy decision.

---

# Outbound Events

After a successful classification transaction commits, listing processing emits two approval/match events. An explicit listing or profession moderation rejection emits `content_rejected` instead.

## `listing_approved`

```json
{
  "type": "listing_approved",
  "event_id": "deterministic-uuid",
  "version": 1,
  "payload": {
    "recipient_id": "uuid",
    "listing_id": "uuid",
    "base_category_slug": "gig",
    "title": "Musluk Tamiri İçin Tesisatçı",
    "occurred_at": "2026-08-09T00:00:00Z"
  }
}
```

## `gig_category_matched`

```json
{
  "type": "gig_category_matched",
  "event_id": "deterministic-uuid",
  "version": 1,
  "payload": {
    "gig_id": "uuid",
    "category_id": "uuid",
    "country_id": "uuid",
    "place_id": "optional-uuid",
    "occurred_at": "2026-08-09T00:00:00Z"
  }
}
```

## `content_rejected`

```json
{
  "type": "content_rejected",
  "event_id": "deterministic-uuid",
  "version": 1,
  "payload": {
    "recipient_id": "uuid",
    "subject_type": "listing",
    "subject_id": "uuid",
    "reason": "alcohol",
    "occurred_at": "2026-08-10T00:00:00Z"
  }
}
```

`subject_type` is `listing` or `profession`. Reasons are `sexual_content`, `alcohol`, `tobacco_nicotine`, or `controlled_substances`. Raw rejected content is never emitted. A deterministic `event_id` makes rejection-event replay safe for idempotent consumers.

---

# Event IDs and At-Least-Once Publication

The worker deliberately uses **at-least-once** event publication.

Database persistence commits before notification publication.

Possible failure:

```text
DB commit succeeds
     |
     v
listing becomes OPEN
     |
     v
notification SQS publish fails
```

The input SQS record remains retryable.

When the input is delivered again, the worker sees the listing is already `OPEN`, reloads the persisted classification, and publishes the notifications again.

This closes the "DB committed but event disappeared forever" failure window without introducing a transactional outbox.

The trade-off is possible duplicate events.

## Deterministic `event_id`

To make duplicates manageable, the same logical event receives the same deterministic UUID on every publication.

Conceptually:

```text
listing_approved
    ID = hash(namespace + event type + listing ID)

gig_category_matched
    ID = hash(namespace + event type + listing ID + category ID)
```

Downstream consumers should deduplicate using `event_id` rather than the SQS message ID.

`version` is currently `1` and is intended for future event-schema evolution.

---

# Profession Processing

`ProfessionPublished` uses the same taxonomy system but has a shorter pipeline because it does not need gig details or outbound listing notifications.

```text
ProfessionPublished
    |
    v
Validate event
    |
    v
Cheap stale request_id check
	|
	v
AI content moderation
	|
	+--> explicit reject: keep existing user category, publish content_rejected, stop
	+--> provider/decode failure: retry with no mutation/event
	|
	v allow
Resolve gig base category
    |
    v
Load active taxonomy
    |
    v
AI category-path extraction ONLY
    |
    v
Validate category path
    |
    v
Generate embeddings
    |
    v
BEGIN transaction
    |
    +--> lock user's current profession request
    +--> authoritative stale request_id check
    +--> resolve/reuse/create category path
    +--> ensure localizations
    +--> attach final selectable category to user
    |
    v
COMMIT
```

---

## Why there are two stale checks

Suppose a user changes their profession rapidly:

```text
Request A: Plumber
Request B: Electrician
```

SQS delivery order is not guaranteed to represent the newest business state.

The worker uses `profession_request_id` stored on the user.

### First check: before AI work

Before spending AI quota and embedding work, the worker asks:

```text
Does this event.request_id still equal users.profession_request_id?
```

If not, the event is safely ignored.

### Second check: inside the transaction

The worker locks the user's current profession request row and checks again.

This closes the race where a newer request arrives after the first check but before persistence.

Only the current request is allowed to update `user_categories`.

A user has one active offered-service category association.

---

# Observability

The service uses OpenTelemetry for metrics and tracing and structured `slog` logging.

## Worker metrics

```text
categorization.worker.messages_processed_total
categorization.worker.messages_failed_total
categorization.worker.message_retries_total
categorization.worker.message_processing.duration
```

These are labeled by low-cardinality message type.

## AI metrics

```text
categorization.ai.provider_failures_total
categorization.ai.fallbacks_total
```

Provider failure reason is normalized to values such as:

```text
error
timeout
canceled
```

## Taxonomy metrics

```text
categorization.taxonomy.category_resolutions_total
```

Useful dimensions:

```text
workflow = listing | profession
action   = created | reused
```

This is especially useful for detecting unexpected taxonomy growth.

## Embedding metrics

```text
categorization.embedding.failures_total
```

Dimension:

```text
workflow = listing | profession
```

## Cardinality policy

Metrics intentionally do not include:

- listing UUID;
- user UUID;
- request UUID;
- category UUID;
- arbitrary descriptions;
- arbitrary error strings.

Those values belong in logs/traces rather than metric labels.

---

# Tracing

Tracing is environment-aware.

```text
development -> 100% root sampling
staging     -> parent-based 10% root sampling
production  -> parent-based 10% root sampling
```

Parent-based sampling means an upstream distributed trace decision is respected.

SQS message attributes are used to propagate trace context between services.

Telemetry initialization retries in the background rather than preventing the worker from starting solely because the collector is temporarily unavailable.

---

# Health and Readiness

The service exposes a small HTTP server on:

```text
:8082
```

## `GET /health`

Simple liveness check.

Returns success when the process and HTTP server are alive.

It does not call dependencies.

## `GET /ready`

Currently checks PostgreSQL connectivity using a short timeout.

Readiness is intentionally separate from liveness so an external dependency problem does not imply that the process itself is dead.

---

# Configuration

## Development

Development loads configuration from environment variables.

Development AWS clients use explicit static credentials, primarily for LocalStack/local testing.

Typical local dependencies:

```text
PostgreSQL / pgvector
LocalStack SQS
Ollama
AI provider API(s)
OpenTelemetry collector (optional/retryable)
```

## Staging

Staging is the current deployment target.

With:

```text
APP_ENV=staging
```

configuration is loaded from AWS using:

- AWS SDK default configuration/credential chain;
- SSM Parameter Store;
- AWS Secrets Manager for the RDS username/password secret.

The worker does **not** copy IAM/STS credentials into static application credentials. AWS SDK clients retain normal temporary credential/session-token refresh behavior.

Staging SSM configuration includes:

- database host/port/name;
- RDS secret ARN;
- categorization SQS queue URL;
- notification SQS queue URL;
- Gemini configuration;
- Groq configuration;
- OpenRouter configuration;
- Mistral configuration;
- Nvidia configuration.

## Production

`APP_ENV=production` loads configuration through Google Application Default Credentials. It renders the existing parameter IDs from Google Cloud Parameter Manager using `GOOGLE_CLOUD_PROJECT` and `GCP_PARAMETER_LOCATION` (`global` by default), then resolves PostgreSQL credentials from Google Secret Manager. The legacy SQS-named messaging parameters contain fully-qualified Pub/Sub topic names in production.

The message consumer and notification publisher are still SQS adapters. Worker application startup therefore stops at an explicit, compile-safe Pub/Sub transport boundary in production instead of accidentally sending GCP topic names to AWS. The production configuration itself remains usable by maintenance commands such as `workerctl`; full worker startup requires the separate Pub/Sub adapters.

This prevents accidental startup with an invalid nil configuration.

---

# Database Connection Pool

The PostgreSQL connection pool is sized with worker concurrency in mind.

Conceptually:

```text
MaxConns = worker concurrency + small operational headroom
```

This avoids configuring 10 concurrent worker goroutines while allowing only a tiny database pool that serializes all database work.

---

# Graceful Shutdown and systemd

The worker is intended to run as a `systemd` service.

On `SIGTERM`:

```text
1. Stop receiving new SQS messages.
2. Allow already received messages to finish.
3. Each in-flight message remains bounded by 120 seconds.
4. Shut down health HTTP server, telemetry, publisher, and DB resources.
5. Application cleanup gets up to 10 additional seconds.
```

Repository drop-in:

```text
deploy/systemd/10-graceful-stop.conf
```

contains approximately:

```ini
[Service]
KillSignal=SIGTERM
TimeoutStopSec=135s
```

The budget is intentionally:

```text
120s in-flight message
+ 10s application cleanup
+ 5s margin
= 135s systemd stop timeout
```

---

# Testing

## Unit / regression tests

The repository includes coverage for areas such as:

- permanent vs transient message errors;
- SQS delete/retry behavior;
- visibility/processing timeout relationships;
- bounded worker concurrency and panic recovery;
- AI provider fallback and timeout semantics;
- category validation;
- deterministic event IDs;
- event timestamps/versioning;
- notification replay after an `OPEN` listing;
- profession stale-event races;
- category selectability;
- AWS credential behavior;
- staging provider configuration;
- trace sampling;
- runtime timeout policy.

Standard test command:

```bash
go test ./...
```

---

# Isolated E2E Tests

The E2E suite is intentionally isolated from staging/shared infrastructure by default.

```bash
make test-e2e
```

The command starts disposable infrastructure:

- pgvector-enabled PostgreSQL;
- LocalStack SQS.

The test database gets a minimal test-only platform schema containing only the tables required by the worker.

This prevents a developer `.env` value such as an internal deployment DNS name (`postgres-svc`) from accidentally making host-side E2E tests depend on the staging network.

The E2E suite still exercises the real categorization path, including configured AI providers and embeddings.

An explicit external-environment target remains available when needed:

```bash
make test-e2e-external
```

---

# AI Categorization Regression Evaluation

AI behavior is inherently less deterministic than ordinary application code, so the repository also contains an opt-in golden evaluation harness.

It reuses the existing corpus of roughly 273 categorization examples.

## Capture current behavior

```bash
make capture-ai-baseline
```

This writes a candidate baseline for review.

After manually reviewing the result, it can be promoted to the approved golden baseline.

## Detect future regression

```bash
make evaluate-ai
```

The evaluator compares stable business decisions:

- detected source locale;
- full canonical category-slug path;
- `is_selectable` values.

It intentionally ignores noisy/non-business-equivalent differences such as:

- reworded category descriptions;
- reworded localized prose;
- which fallback provider produced the same final category decision.

The evaluation tool is opt-in and does not run automatically as part of ordinary `go test`, because it calls real AI providers and depends on the current taxonomy database.

It is read-only with respect to listing/category persistence.

---

# Example: Turkish Plumbing Listing

Input:

```json
{
  "listing_id": "...",
  "author_id": "...",
  "description": "Mutfaktaki musluk su kaçırıyor, tesisatçı arıyorum"
}
```

Possible AI category result:

```text
source_locale = tr

Home Services
  canonical: Home Services
  localized: Ev Hizmetleri
  selectable: false

Plumbing
  canonical: Plumbing
  localized: Tesisat
  selectable: false

Plumber
  canonical: Plumber
  localized: Tesisatçı
  selectable: true
```

The worker generates embeddings such as:

```text
Home Services
Home Services
<canonical description>
```

then:

```text
Home Services > Plumbing
Plumbing
<canonical description>
```

then:

```text
Home Services > Plumbing > Plumber
Plumber
<canonical description>
```

For every level it searches only the appropriate sibling scope.

If `Plumber` already exists semantically, its existing UUID is reused.

If the Turkish localization does not yet exist:

```text
category_localizations(category_id=<Plumber UUID>, locale=tr)
```

is inserted.

Only the final selectable `Plumber` UUID is attached to the listing.

The listing receives its extracted gig details and becomes `OPEN`.

Finally:

```text
listing_approved
```

and:

```text
gig_category_matched
```

are published with deterministic event IDs.

---

# Example: Profession Registration

Input:

```json
{
  "request_id": "...",
  "author_id": "...",
  "description": "Oto Yıkama"
}
```

Possible resolution:

```text
Automotive Services
└── Car Wash
```

Canonical identity:

```text
Car Wash
```

Turkish presentation:

```text
Oto Yıkama
```

The worker does not invent `Auto Detailing` unless the user's description actually describes detailing.

It also does not create something artificial like:

```text
Car Wash Specialist
Oto Yıkama Uzmanı
```

unless that is genuinely the recognized domain concept, which it normally is not.

The final `Car Wash` category UUID is written to the user's profession/category association.

No gig-details AI call occurs and no listing notification event is emitted.

---

# Important Design Trade-offs

## Dynamic taxonomy vs fixed profession list

The system deliberately uses a dynamic taxonomy.

Advantages:

- New marketplace services can appear without application releases.
- Multilingual input maps into one canonical domain.
- The hierarchy can evolve with actual marketplace usage.

Risks:

- Poor model behavior can create bad categories.
- Similarity thresholds and prompts affect taxonomy quality.
- Taxonomy growth must be observed over time.

Current controls include:

- detailed prompt rules;
- maximum four-level paths;
- parent-scoped matching;
- exact name/slug reuse;
- embeddings;
- database sibling uniqueness;
- advisory locking;
- validation;
- created-vs-reused metrics;
- regression evaluation corpus.

No manual category approval queue currently exists by design.

---

## Full taxonomy prompting

Current approach:

```text
send full active taxonomy to AI
```

This is simpler and gives the model complete context while the taxonomy is reasonably sized.

Potential future approach:

```text
embed input
   -> retrieve likely category branches
   -> send only relevant subtree to AI
```

This is intentionally an **open decision**, not a current requirement.

Revisit when there is actual evidence such as:

- large taxonomy size;
- high prompt token usage;
- increased LLM latency;
- increased AI cost;
- context-window pressure.

---

## Localization lifecycle

Current runtime behavior keeps existing `(category_id, locale)` values once written.

A future explicit maintenance/correction strategy has not been finalized.

This remains an **open decision** rather than allowing ordinary classification requests to silently rewrite existing localizations.

---

# Repository Map

```text
cmd/worker/
  main.go               process lifecycle, health server, shutdown
  app.go                dependency wiring, providers, DB, telemetry

cmd/evaluate/
  main.go               opt-in AI regression evaluator

internal/worker/
  worker.go             SQS polling, concurrency, retry/delete semantics

internal/processor/
  gig.go                ListingCreated/GigPublished workflow
  profession.go         ProfessionPublished workflow
  repository.go         PostgreSQL category/listing/user persistence

internal/extractor/
  extractor.go          AI extraction domain structures
  fallback.go           provider fallback/time limits
  gemini.go             Gemini implementation
  open_ai.go            OpenAI-compatible provider implementation

internal/prompter/
  builder.go            inserts active catalog into prompt
  loader.go             embeds prompt files
  prompts/
    category_path.txt    taxonomy prompt
    profession.txt       gig-details prompt

internal/notification/
  types.go              outbound event contracts + deterministic IDs
  sqs.go                SQS event publisher

internal/runtimepolicy/
  timeouts.go           shared processing/AI/visibility/shutdown budgets

pkg/embeddings/
  huggingface.go         Ollama embedding client

pkg/metrics/
  metrics.go            worker/AI/taxonomy metrics

pkg/tracing/
  ...                   tracing, SQS propagation, sampler

migrations/
  ...                   category hierarchy/localization schema evolution

deploy/systemd/
  10-graceful-stop.conf systemd shutdown budget

testdata/
  evaluation/            AI golden regression data/docs
  postgres/              isolated E2E test schema
```

---

# Operational Summary

A healthy listing flow should look like:

```text
SQS message received
  -> AI path extracted
  -> AI details extracted
  -> category nodes reused/created
  -> listing transaction committed
  -> listing OPEN
  -> listing_approved published
  -> gig_category_matched published
  -> input message deleted
```

A healthy profession flow should look like:

```text
SQS message received
  -> request still current
  -> AI category path extracted
  -> category nodes reused/created
  -> user category committed
  -> input message deleted
```

A transient failure should look like:

```text
processing fails
  -> error/metric/trace recorded
  -> input message NOT deleted
  -> SQS retries it later
  -> queue redrive policy eventually handles repeated failures
```

A permanent malformed event should look like:

```text
contract validation fails
  -> warning/error recorded
  -> message deleted
  -> no useless retry loop
```

---

# Key Runtime Constants

| Policy | Current value |
|---|---:|
| Worker concurrency | 10 by default |
| SQS receive batch | up to 10 |
| SQS long poll | 20 seconds |
| Message processing timeout | 120 seconds |
| SQS visibility timeout | 180 seconds |
| AI provider attempt timeout | 25 seconds |
| AI fallback total timeout | 75 seconds |
| Ollama HTTP timeout | 2 seconds |
| Application shutdown cleanup | 10 seconds |
| systemd stop timeout | 135 seconds |
| Deployed root trace sampling | 10% parent-based |
| Maximum category path depth | 4 |
| Semantic sibling distance threshold | `< 0.15` cosine distance |
| Default embedding model | `all-minilm:l12-v2` |
| Current outbound event version | `1` |

The shared worker/AI/visibility/shutdown timing budgets are centralized in `internal/runtimepolicy/timeouts.go` and should not be independently changed in unrelated packages.

---

# Final Mental Model

The easiest way to reason about the service is:

```text
              Natural-language marketplace input
                            |
                            v
                 "What concept is this?"
                            |
                            v
                           AI
                            |
                canonical proposed hierarchy
                            |
                            v
          "Do we already know these concepts?"
                            |
                            v
             PostgreSQL + vector embeddings
                    /               \
                 reuse             create
                    \               /
                     canonical taxonomy
                            |
            +---------------+---------------+
            |                               |
         listing                         profession
            |                               |
   category + gig details            user category
            |
          OPEN
            |
     downstream events
```

The AI performs **language understanding and taxonomy proposal**.

Embeddings perform **fast semantic equivalence matching against existing siblings**.

PostgreSQL performs **identity, hierarchy, concurrency control, and atomic persistence**.

SQS provides **asynchronous at-least-once delivery**, while deterministic outbound event IDs allow downstream consumers to make side effects idempotent.
