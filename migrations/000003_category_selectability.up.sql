BEGIN;

ALTER TABLE categories
    ADD COLUMN IF NOT EXISTS is_selectable BOOLEAN NOT NULL DEFAULT FALSE;

CREATE INDEX IF NOT EXISTS categories_active_selectable_idx
    ON categories(base_category_id, id)
    WHERE is_active = TRUE AND is_selectable = TRUE;

COMMIT;
