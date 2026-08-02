BEGIN;

DROP INDEX categories_generated_by_idx;

ALTER TABLE categories
    DROP CONSTRAINT categories_generated_by_not_blank,
    DROP COLUMN generated_by;

COMMIT;
