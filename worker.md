# Listing and Profession Processing

This document explains how the categorization worker processes listing and profession events from receipt to completion. The worker is currently gig/service-only. It owns category resolution and category mutations, while Core owns listings, users, locations, and the initial event publication.

## Shared message lifecycle

Both workflows begin in `internal/worker/worker.go`.

1. The worker long-polls the categorization SQS queue for up to 10 messages at a time.
2. Processing runs through a bounded goroutine pool using the configured worker concurrency.
3. Each message gets a two-minute processing deadline. SQS visibility is three minutes, so a normal attempt should finish before the message becomes visible again.
4. The SQS `Type` message attribute selects the processor:
   - `ListingCreated` → listing processor
   - `GigPublished` → listing processor as a temporary compatibility alias
   - `ProfessionPublished` → profession processor
5. Missing types, unknown types, malformed JSON, and invalid required payload fields are permanent message-contract failures. They are deleted because retrying cannot repair them.
6. AI, database, Ollama, SQS publication, timeout, network, and panic failures are transient. The input message is not deleted and becomes available for SQS redelivery.
7. A successful processor result is followed by deletion of the input message.
8. Panics are recovered per message so one bad record does not terminate the worker process.

Receive failures use exponential backoff: 1, 2, 4, 8, and 16 seconds, then a maximum of 30 seconds. Graceful shutdown stops receiving new work and waits for in-flight messages to finish.

## Shared AI behavior

The configured provider order is:

1. Gemini
2. Groq
3. OpenRouter
4. Mistral
5. Nvidia

Only fully configured providers participate. Providers are attempted sequentially. A provider error, timeout, or invalid response moves processing to the next provider. The successful provider/model is stored as `generated_by` for provenance.

Taxonomy and listing-detail calls have a 25-second limit per provider and a 75-second total fallback limit. Moderation uses a 10-second limit per provider and a 30-second total limit. Exhausting the fallback chain is a transient processing failure, not a content rejection.

The prompts are embedded from `internal/prompter/prompts/`:

- `moderation.txt` decides whether marketplace content is allowed.
- `category_path.txt` detects the source locale and produces an English canonical hierarchy plus source-language localizations.
- `profession.txt` extracts listing title and gig-specific details. Despite its filename, this prompt is used for listing details; profession registration does not call it.

The active canonical category catalog is inserted into the category-path prompt. This gives the AI existing English concepts and hierarchy context before it proposes a path.

## Shared category resolution

Both workflows resolve categories using the same principles:

1. Resolve the active base category whose slug is `gig`. Its UUID is read from `base_categories`; it is never hard-coded.
2. Load the current active canonical English category tree for that base category.
3. Ask the AI for a path of at most four nodes. Each node contains:
   - English canonical name, slug, and description
   - localized name, slug, and description
   - source locale
   - explicit `is_selectable`
4. Normalize locales and regenerate slugs from names. Validate that every node is complete, every node uses the detected source locale, adjacent nodes are not duplicates, and the final node is selectable.
5. Generate a 384-dimensional Ollama embedding for every path node using:

   ```text
   canonical breadcrumb
   canonical name
   canonical description
   ```

6. Resolve the path from root to final node inside the persistence transaction. Matching is always scoped to the active `gig` base category and the current parent.
7. For each sibling scope, the repository takes a transaction-scoped PostgreSQL advisory lock. It then tries to reuse a category by canonical slug, case-insensitive canonical name, or close embedding similarity.
8. If no sibling represents the same concept, create a new category with canonical metadata, embedding, provenance, depth, parent, and AI-selected selectability.
9. If an existing category is reused and the new result says it is selectable, it may be promoted to selectable. It is never demoted merely because it has or gains children.
10. Ensure a localization exists for the source locale using `ON CONFLICT DO NOTHING`. Existing localizations are preserved rather than overwritten.

This is how Turkish `Avukat`, English `Lawyer`, and German `Rechtsanwalt` can resolve to one canonical `Lawyer` UUID with separate localization rows.

Only the final active and selectable category UUID is associated with a listing or user. Ancestors exist for navigation and broader discovery but are not attached as additional selected categories.

## Listing processing

### Input

```json
{
  "listing_id": "uuid",
  "author_id": "uuid",
  "description": "Musluğumu tamir edecek bir tesisatçı arıyorum",
  "country_id": "uuid",
  "place_id": "optional-uuid"
}
```

Core creates the listing beforehand with `status = PROCESSING` and `base_category_id = NULL`. `country_id` is required; `place_id` is optional. The worker does not translate these UUIDs into legacy city or district identifiers.

### Step 1: Decode and validate the event

The worker requires a valid listing UUID, author UUID, nonblank description, and country UUID. Contract failures are permanent and the message is discarded after successful SQS deletion.

### Step 2: Moderate the full description

The complete trimmed description is sent to the moderation prompt. A valid result is either:

- `allow` with no reason, or
- `reject` with `sexual_content`, `alcohol`, `tobacco_nicotine`, or `controlled_substances`.

Only a successfully decoded explicit rejection is treated as rejected content. Provider failures, malformed responses, quotas, rate limits, and timeouts leave the listing in `PROCESSING`, emit no rejection event, and cause SQS retry.

For an explicit rejection, the worker locks the listing in a transaction:

- `PROCESSING` becomes `FAILED`.
- An already `FAILED` listing is treated as a rejection replay.
- An already `OPEN` listing is never retroactively rejected; its stored classification is loaded and normal approval events are published again.

After the rejection transaction commits, the worker emits `content_rejected`. The event contains recipient, subject type `listing`, listing UUID, stable reason code, and timestamp. Raw content is never included.

### Step 3: Prepare the classification

For allowed content, the worker:

1. Resolves the active `gig` base-category UUID.
2. Loads the active canonical catalog.
3. Calls category-path extraction and listing-details extraction through the provider fallback chain.
4. Validates the English canonical path, source localizations, provider provenance, and gig details.
5. Normalizes the title, sets the profession value to the final canonical category name, uppercases currency, and defaults an empty currency to `TRY`.
6. Rejects negative pay and currencies that are not exactly three characters.
7. Generates an embedding for every canonical path node.

An empty category path is currently treated as “category not detected”: the processor logs it and returns successfully without opening the listing. Other invalid AI output is a transient processing error and is retried.

### Step 4: Persist atomically

All listing database changes happen in one transaction under the `listing.persist_transaction` span.

1. Lock the listing row with `FOR UPDATE`.
2. If it is already `OPEN`, load its stored base category, final category, and title. Do not run category or listing mutations again.
3. Otherwise require `PROCESSING`; any other status fails processing.
4. Set `listings.base_category_id` and `description_clean` while the listing remains `PROCESSING`.
5. Resolve or create every category node and ensure its source-locale localization.
6. Insert the single final category into `listing_categories`. The SQL requires an active, selectable category in the active `gig` family and matching base-category IDs.
7. Upsert `gig_details` with title, optional duration type, optional pay amount, three-character currency, and expiry. If AI supplies no expiry, the worker defaults it to one month from processing time.
8. Transition the listing to `OPEN` only if the database can verify all prerequisites:
   - non-null base category
   - matching active `gig` category association
   - active and selectable final category
   - existing `gig_details` row
9. Commit everything together.

If any step fails, category creation, localization creation, association, details, and status changes all roll back. An `OPEN` listing therefore cannot be exposed with only part of its classification.

### Step 5: Publish listing events

Events are published only after the database commit:

1. `listing_approved` includes recipient UUID, listing UUID, base-category slug, title, and timestamp.
2. `gig_category_matched` includes listing/gig UUID, final category UUID, country UUID, optional place UUID, and timestamp.

Each envelope has schema version `1` and a deterministic `event_id`. If event publication fails after commit, the original SQS message remains undeleted. On redelivery, the processor recognizes the `OPEN` listing, reloads its classification, and publishes both events again without changing database state. Consumers must deduplicate by `event_id` because publication is intentionally at least once.

## Profession processing

### Input

```json
{
  "request_id": "uuid",
  "author_id": "uuid",
  "description": "Oto Yıkama"
}
```

Core stores the user's latest `profession_request_id` before publishing the event. This request UUID prevents an older SQS delivery from overwriting a newer profession choice.

### Step 1: Decode and validate the event

The worker requires a valid request UUID, author UUID, and nonblank description. Invalid payloads are permanent message-contract failures.

### Step 2: Perform the cheap stale-event check

Before spending AI quota, the worker reads the user's current `profession_request_id`.

- If the user no longer exists, the event is ignored successfully.
- If the current request UUID differs from the event request UUID, the event is stale and is ignored successfully.
- Only the latest request proceeds.

### Step 3: Moderate the profession text

The full trimmed profession text is moderated using the same explicit-decision rules as listing content.

An explicit rejection:

- leaves the user's existing category unchanged,
- emits `content_rejected` with subject type `profession`,
- uses the request UUID as the subject UUID.

Unlike listing rejection, there is no user status transition. A moderation dependency failure emits nothing and leaves the message retryable.

### Step 4: Extract only the category path

The worker resolves the active `gig` base category, loads the active canonical catalog, and calls `ExtractCategoryPath`.

It deliberately does not run the listing-details prompt. Profession registration needs only a service/category association, so generating a title, pay, duration, or expiry would waste latency and provider quota.

The extracted path undergoes the same canonical English, localization, selectability, maximum-depth, provenance, and locale validation used by listing taxonomy. Every node then receives a canonical English embedding.

### Step 5: Persist atomically with an authoritative stale check

All profession persistence occurs in one transaction under `profession.persist_transaction`.

1. Lock the user row and read `profession_request_id` again.
2. If the request changed while AI work was running, stop and commit no category or association changes. This second check closes the race with a newer profession request.
3. Resolve or create every canonical path node from root to final node.
4. Ensure the source-locale localization exists for every node.
5. Upsert exactly one row in `user_categories` for the user, replacing the previous selected profession if necessary.
6. The SQL accepts only an active, selectable category belonging to the active `gig` family.
7. Commit category mutations, localizations, and final user association together.

The profession workflow currently emits no success event. It only logs and traces the resolved category. Rejected profession content emits `content_rejected` as described above.

## Retry and idempotency summary

| Situation | Database result | Event result | SQS result |
|---|---|---|---|
| Valid new listing | Atomically becomes `OPEN` | Two approval/match events | Input deleted after publication |
| Explicitly rejected listing | Atomically becomes `FAILED` | `content_rejected` | Input deleted after publication |
| Valid profession request | One final user category committed | No success event | Input deleted |
| Explicitly rejected profession | Existing profession unchanged | `content_rejected` | Input deleted after publication |
| Stale profession request | No mutation | No event | Input deleted |
| AI/embedding/database failure | Transaction rolls back or no mutation | No rejection event | Input retained for retry |
| Post-commit listing event failure | Listing remains `OPEN` | Re-published on replay | Input retained for retry |
| Malformed/unsupported message | No mutation | No event | Permanently discarded |
| Panic | Current transaction rolls back | No guaranteed event | Input retained for retry |

## Observability

The normal trace hierarchy is:

```text
process-message
├── process-listing
│   ├── ai.fallback / ai.provider.attempt
│   ├── category_path.embed / ollama.embed
│   ├── listing.persist_transaction / taxonomy.resolve_node
│   └── listing.publish_notifications / sqs.publish ...
└── process-profession
    ├── ai.fallback / ai.provider.attempt
    ├── category_path.embed / ollama.embed
    └── profession.persist_transaction / taxonomy.resolve_node
```

Metrics distinguish successful messages, failed messages, retry decisions, AI provider failures and fallback transitions, embedding failures, and category creation versus reuse. IDs and descriptions are kept out of metric labels to avoid high cardinality; sampled traces and structured logs carry diagnostic identifiers instead. Raw descriptions, prompts, AI responses, and credentials are not attached to traces.
