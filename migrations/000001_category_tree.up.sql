BEGIN;

ALTER TABLE categories
    ADD COLUMN parent_id UUID,
    ADD COLUMN description TEXT,
    ADD COLUMN depth INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN is_active BOOLEAN NOT NULL DEFAULT TRUE;

UPDATE categories
SET description = name,
    is_active = FALSE
WHERE description IS NULL;

ALTER TABLE categories
    ALTER COLUMN description SET NOT NULL,
    ADD CONSTRAINT categories_description_not_blank
        CHECK (length(trim(description)) > 0),
    ADD CONSTRAINT categories_depth_nonnegative
        CHECK (depth >= 0),
    ADD CONSTRAINT categories_not_own_parent
        CHECK (parent_id IS NULL OR parent_id <> id),
    ADD CONSTRAINT categories_parent_same_base_fk
        FOREIGN KEY (parent_id, base_category_id)
        REFERENCES categories(id, base_category_id)
        ON DELETE RESTRICT;

ALTER TABLE categories
    DROP CONSTRAINT categories_base_name_unique,
    DROP CONSTRAINT categories_base_slug_unique;

CREATE UNIQUE INDEX categories_root_base_name_unique
    ON categories(base_category_id, name)
    WHERE parent_id IS NULL;

CREATE UNIQUE INDEX categories_root_base_slug_unique
    ON categories(base_category_id, slug)
    WHERE parent_id IS NULL;

CREATE UNIQUE INDEX categories_child_parent_name_unique
    ON categories(base_category_id, parent_id, name)
    WHERE parent_id IS NOT NULL;

CREATE UNIQUE INDEX categories_child_parent_slug_unique
    ON categories(base_category_id, parent_id, slug)
    WHERE parent_id IS NOT NULL;

CREATE INDEX categories_active_children_idx
    ON categories(base_category_id, parent_id)
    WHERE is_active = TRUE;

CREATE FUNCTION enforce_category_tree()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    parent_depth INTEGER;
BEGIN
    IF NEW.parent_id IS NULL THEN
        NEW.depth := 0;
        RETURN NEW;
    END IF;

    SELECT depth
    INTO parent_depth
    FROM categories
    WHERE id = NEW.parent_id
      AND base_category_id = NEW.base_category_id;

    IF parent_depth IS NULL THEN
        RAISE EXCEPTION 'category parent does not exist in the same base category';
    END IF;

    IF EXISTS (
        WITH RECURSIVE ancestors AS (
            SELECT id, parent_id
            FROM categories
            WHERE id = NEW.parent_id

            UNION ALL

            SELECT c.id, c.parent_id
            FROM categories c
            JOIN ancestors a ON c.id = a.parent_id
        )
        SELECT 1
        FROM ancestors
        WHERE id = NEW.id
    ) THEN
        RAISE EXCEPTION 'category tree cycle detected';
    END IF;

    NEW.depth := parent_depth + 1;
    RETURN NEW;
END
$$;

CREATE TRIGGER trg_enforce_category_tree
BEFORE INSERT OR UPDATE OF parent_id, base_category_id
ON categories
FOR EACH ROW
EXECUTE FUNCTION enforce_category_tree();

COMMIT;
