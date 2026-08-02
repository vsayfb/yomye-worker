BEGIN;

DROP INDEX IF EXISTS categories_active_selectable_idx;

ALTER TABLE categories
    DROP COLUMN IF EXISTS is_selectable;

COMMIT;
