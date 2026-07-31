BEGIN;

DROP TRIGGER trg_enforce_category_tree ON categories;
DROP FUNCTION enforce_category_tree();

-- A flat schema cannot represent duplicate names or slugs from separate
-- branches. Abort instead of silently losing categories during rollback.
DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM categories
        GROUP BY base_category_id, name
        HAVING count(*) > 1
    ) OR EXISTS (
        SELECT 1
        FROM categories
        GROUP BY base_category_id, slug
        HAVING count(*) > 1
    ) THEN
        RAISE EXCEPTION
            'cannot flatten category tree: duplicate category names or slugs exist';
    END IF;
END
$$;

DROP INDEX categories_active_children_idx;
DROP INDEX categories_child_parent_slug_unique;
DROP INDEX categories_child_parent_name_unique;
DROP INDEX categories_root_base_slug_unique;
DROP INDEX categories_root_base_name_unique;

ALTER TABLE categories
    DROP CONSTRAINT categories_parent_same_base_fk,
    DROP CONSTRAINT categories_not_own_parent,
    DROP CONSTRAINT categories_depth_nonnegative,
    DROP CONSTRAINT categories_description_not_blank,
    DROP COLUMN is_active,
    DROP COLUMN depth,
    DROP COLUMN description,
    DROP COLUMN parent_id;

ALTER TABLE categories
    ADD CONSTRAINT categories_base_name_unique UNIQUE (base_category_id, name),
    ADD CONSTRAINT categories_base_slug_unique UNIQUE (base_category_id, slug);

COMMIT;
