-- Core schema v18 owns base_categories. This migration is the complete
-- staging baseline for the category domain owned by this worker.
CREATE TABLE categories (
    id UUID PRIMARY KEY,
    base_category_id UUID NOT NULL
        REFERENCES base_categories(id)
        ON DELETE RESTRICT,
    parent_id UUID,
    canonical_name VARCHAR(100) NOT NULL,
    canonical_slug VARCHAR(100) NOT NULL,
    canonical_description TEXT NOT NULL,
    embedding VECTOR(384),
    depth INTEGER NOT NULL DEFAULT 0,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    is_selectable BOOLEAN NOT NULL DEFAULT FALSE,
    generated_by VARCHAR(200) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT categories_id_base_category_unique
        UNIQUE (id, base_category_id),
    CONSTRAINT categories_parent_same_base_fk
        FOREIGN KEY (parent_id, base_category_id)
        REFERENCES categories(id, base_category_id)
        ON DELETE RESTRICT,
    CONSTRAINT categories_canonical_name_not_blank
        CHECK (length(trim(canonical_name)) > 0),
    CONSTRAINT categories_canonical_slug_not_blank
        CHECK (length(trim(canonical_slug)) > 0),
    CONSTRAINT categories_canonical_description_not_blank
        CHECK (length(trim(canonical_description)) > 0),
    CONSTRAINT categories_generated_by_not_blank
        CHECK (length(trim(generated_by)) > 0),
    CONSTRAINT categories_depth_nonnegative
        CHECK (depth >= 0),
    CONSTRAINT categories_not_own_parent
        CHECK (parent_id IS NULL OR parent_id <> id)
);

-- PostgreSQL treats NULL values as distinct in ordinary UNIQUE constraints,
-- so roots and children need separate indexes for sibling uniqueness.
CREATE UNIQUE INDEX categories_root_base_name_unique
    ON categories(base_category_id, canonical_name)
    WHERE parent_id IS NULL;

CREATE UNIQUE INDEX categories_root_base_slug_unique
    ON categories(base_category_id, canonical_slug)
    WHERE parent_id IS NULL;

CREATE UNIQUE INDEX categories_child_parent_name_unique
    ON categories(base_category_id, parent_id, canonical_name)
    WHERE parent_id IS NOT NULL;

CREATE UNIQUE INDEX categories_child_parent_slug_unique
    ON categories(base_category_id, parent_id, canonical_slug)
    WHERE parent_id IS NOT NULL;

CREATE INDEX categories_active_children_idx
    ON categories(base_category_id, parent_id)
    WHERE is_active = TRUE;

CREATE INDEX categories_active_selectable_idx
    ON categories(base_category_id, id)
    WHERE is_active = TRUE AND is_selectable = TRUE;

CREATE INDEX categories_generated_by_idx
    ON categories(generated_by);

CREATE INDEX categories_embedding_hnsw_idx
    ON categories
    USING hnsw (embedding vector_cosine_ops)
    WHERE embedding IS NOT NULL;

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
        RAISE EXCEPTION
            'category parent does not exist in the same base category';
    END IF;

    IF EXISTS (
        WITH RECURSIVE ancestors AS (
            SELECT id, parent_id
            FROM categories
            WHERE id = NEW.parent_id
              AND base_category_id = NEW.base_category_id

            UNION ALL

            SELECT c.id, c.parent_id
            FROM categories c
            JOIN ancestors a ON c.id = a.parent_id
            WHERE c.base_category_id = NEW.base_category_id
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
BEFORE INSERT OR UPDATE OF parent_id, base_category_id, depth
ON categories
FOR EACH ROW
EXECUTE FUNCTION enforce_category_tree();

CREATE FUNCTION propagate_category_depth()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    UPDATE categories
    SET depth = NEW.depth + 1
    WHERE parent_id = NEW.id
      AND base_category_id = NEW.base_category_id
      AND depth IS DISTINCT FROM NEW.depth + 1;

    RETURN NULL;
END
$$;

-- Updating a direct child fires this trigger again, so depth changes cascade
-- through the whole subtree while each row remains parent.depth + 1.
CREATE TRIGGER trg_propagate_category_depth
AFTER UPDATE ON categories
FOR EACH ROW
WHEN (OLD.depth IS DISTINCT FROM NEW.depth)
EXECUTE FUNCTION propagate_category_depth();

CREATE FUNCTION touch_category_updated_at()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    NEW.updated_at := NOW();
    RETURN NEW;
END
$$;

CREATE TRIGGER trg_touch_category_updated_at
BEFORE UPDATE ON categories
FOR EACH ROW
EXECUTE FUNCTION touch_category_updated_at();

CREATE TABLE category_localizations (
    category_id UUID NOT NULL
        REFERENCES categories(id)
        ON DELETE CASCADE,
    locale VARCHAR(35) NOT NULL,
    name TEXT NOT NULL,
    slug TEXT NOT NULL,
    description TEXT NOT NULL,
    generated_by VARCHAR(200) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    PRIMARY KEY (category_id, locale),
    CONSTRAINT category_localizations_locale_not_blank
        CHECK (length(trim(locale)) > 0),
    CONSTRAINT category_localizations_name_not_blank
        CHECK (length(trim(name)) > 0),
    CONSTRAINT category_localizations_slug_not_blank
        CHECK (length(trim(slug)) > 0),
    CONSTRAINT category_localizations_description_not_blank
        CHECK (length(trim(description)) > 0),
    CONSTRAINT category_localizations_generated_by_not_blank
        CHECK (length(trim(generated_by)) > 0)
);

CREATE INDEX category_localizations_locale_slug_idx
    ON category_localizations(locale, slug);

CREATE FUNCTION touch_category_localization_updated_at()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    NEW.updated_at := NOW();
    RETURN NEW;
END
$$;

CREATE TRIGGER trg_touch_category_localization_updated_at
BEFORE UPDATE ON category_localizations
FOR EACH ROW
EXECUTE FUNCTION touch_category_localization_updated_at();
