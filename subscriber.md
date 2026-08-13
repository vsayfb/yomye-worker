# Hierarchical Category Subscriptions

## Goal

Notify providers subscribed to a broader selectable service category when a listing is classified into one of its specializations.

Example:

```text
Technology                   grouping-only
└── Software Engineering     selectable
    └── Backend Developer    selectable and selected listing category
```

A listing classified as `Backend Developer` should be eligible for subscribers of:

- `Backend Developer`
- `Software Engineering`

It should not target `Technology` subscribers because that node is grouping-only and therefore non-selectable.

## Category identity remains unchanged

The worker must continue attaching exactly one category to the listing:

```text
listing.category_id = Backend Developer UUID
```

Do not attach every ancestor through `listing_categories`. The final leaf/specialization remains the listing's exact category identity.

Broader notification eligibility is derived separately from the category hierarchy.

## Proposed event contract

Evolve `gig_category_matched` to include all selectable categories that should match subscriptions:

```json
{
  "type": "gig_category_matched",
  "event_id": "deterministic-uuid",
  "version": 2,
  "payload": {
    "gig_id": "listing-uuid",
    "category_id": "backend-developer-uuid",
    "matched_category_ids": [
      "software-engineering-uuid",
      "backend-developer-uuid"
    ],
    "country_id": "country-uuid",
    "place_id": "optional-place-uuid",
    "occurred_at": "timestamp"
  }
}
```

Fields:

- `category_id` is the exact category assigned to the listing.
- `matched_category_ids` contains the exact category plus its active, selectable ancestors.
- `matched_category_ids` should be ordered from broadest selectable ancestor to the exact selected category.
- `country_id` remains required under Core schema v18.
- `place_id` remains optional.

Because the payload changes, increment the event version rather than silently changing version 1.

## Worker responsibilities

The categorization worker owns taxonomy knowledge, so it should calculate the match set.

After resolving the final category, the worker should:

1. Traverse the category's canonical ancestry within the same `base_category_id`.
2. Include only categories where `is_active = TRUE` and `is_selectable = TRUE`.
3. Always include the final selected category.
4. Exclude grouping-only ancestors.
5. Exclude inactive categories.
6. Deduplicate UUIDs defensively.
7. Emit the resulting UUID list in `gig_category_matched`.

The traversal can use a recursive PostgreSQL query based on the selected category UUID. It must remain scoped to the active `gig` base category.

This derived subscription ancestry does not need another listing association and must not change category identity.

## Lambda/search notification responsibilities

The downstream Lambda should:

1. Find provider subscriptions whose category UUID appears in `matched_category_ids`.
2. Apply country/place, distance, availability, and other eligibility filters.
3. Deduplicate providers who match through more than one category.
4. Deliver at most one notification per provider for the logical event.
5. Deduplicate replayed events using deterministic `event_id`.

Lambda should not independently reconstruct category ancestry. Keeping that logic in the taxonomy-owning worker prevents different consumers from interpreting hierarchy/selectability differently.

## Important semantics

- Selectable and leaf are not equivalent.
- A selectable category can have selectable descendants.
- Adding a child must not make its selectable parent non-selectable.
- A non-selectable grouping node never becomes a subscription target merely because it is an ancestor.
- A subscriber to a specialization should not automatically receive listings assigned only to its broader parent.

Direction is intentionally one-way:

```text
listing in Backend Developer
    -> matches Backend Developer and selectable ancestors

listing in Software Engineering
    -> matches Software Engineering
    -> does not automatically match Backend Developer
```

## Open product decisions

Before implementation, decide:

- Whether users may subscribe only to selectable categories. Recommended: yes.
- Whether matching includes every selectable ancestor or stops after a configured number of levels. Recommended for MVP: every selectable ancestor.
- Whether category subscription matching applies only to listing notifications or also to search defaults/recommendations.
- Whether providers can maintain multiple category subscriptions. If yes, downstream recipient deduplication is mandatory.

## Recommended MVP

- Keep exactly one selected category on each listing.
- Allow subscriptions only to active selectable gig categories.
- Emit the selected category plus all active selectable ancestors.
- Let Lambda handle location/availability filtering and recipient deduplication.
- Use event contract version 2.
