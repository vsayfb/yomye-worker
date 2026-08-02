BEGIN;

ALTER TABLE categories
    ADD COLUMN generated_by VARCHAR(200) NOT NULL DEFAULT 'legacy';

ALTER TABLE categories
    ALTER COLUMN generated_by DROP DEFAULT,
    ADD CONSTRAINT categories_generated_by_not_blank
        CHECK (length(trim(generated_by)) > 0);

CREATE INDEX categories_generated_by_idx
    ON categories(generated_by);

COMMIT;
