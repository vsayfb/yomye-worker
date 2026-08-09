# Categorization regression baseline

This directory stores the reviewed golden behavior for the live AI categorization pipeline.
The evaluator is opt-in and is not part of `go test` because it calls real providers and a real category database.

## Establish the first baseline

The existing `internal/extractor/example.txt` corpus is reused as the input set.
Capture the current behavior into an untracked candidate file:

```bash
make capture-ai-baseline
```

Review `testdata/evaluation/candidate.json`. When the paths represent the behavior we want to preserve, promote it deliberately:

```bash
mv testdata/evaluation/candidate.json testdata/evaluation/golden.json
```

Commit `golden.json` only after review. Capturing a new candidate must never silently overwrite the approved baseline.

## Check for regression

```bash
make evaluate-ai
```

The comparison intentionally checks stable business output only:

- source locale
- full canonical slug path
- `is_selectable` on each path node

It intentionally ignores generated descriptions/localized prose and the provider/model that won the fallback chain. Those can vary without changing the categorization decision.

A different parent path, leaf category, selectability flag, locale, or extraction failure is reported. Any non-exact case makes the command exit non-zero. The report also shows leaf-match and locale-match rates so near misses are visible.

Use `-limit` for a small live smoke run, for example:

```bash
go run ./cmd/evaluate -baseline testdata/evaluation/golden.json -limit 10
```

The evaluator reads the same runtime AI configuration and the current `gig` taxonomy from PostgreSQL. It does not publish SQS messages or mutate listings, categories, or localizations.
