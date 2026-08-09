-- Test-only minimal platform schema for processor E2E tests.
-- This intentionally does not replace or mirror production migrations. It
-- defines only the final tables/constraints that the categorization worker
-- reads or writes during its E2E suite.

CREATE EXTENSION IF NOT EXISTS vector;

CREATE TABLE base_categories (
    id UUID PRIMARY KEY,
    slug TEXT NOT NULL UNIQUE,
    is_active BOOLEAN NOT NULL DEFAULT TRUE
);

CREATE TABLE categories (
    id UUID PRIMARY KEY,
    base_category_id UUID NOT NULL REFERENCES base_categories(id) ON DELETE RESTRICT,
    parent_id UUID,
    canonical_name TEXT NOT NULL,
    canonical_slug TEXT NOT NULL,
    canonical_description TEXT NOT NULL,
    generated_by VARCHAR(200) NOT NULL,
    is_selectable BOOLEAN NOT NULL DEFAULT FALSE,
    depth SMALLINT NOT NULL DEFAULT 0 CHECK (depth >= 0),
    embedding vector,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (id, base_category_id),
    CONSTRAINT categories_not_own_parent CHECK (parent_id IS NULL OR parent_id <> id),
    CONSTRAINT categories_parent_same_base_fk
        FOREIGN KEY (parent_id, base_category_id)
        REFERENCES categories(id, base_category_id)
        ON DELETE RESTRICT
);

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

CREATE TABLE category_localizations (
    category_id UUID NOT NULL REFERENCES categories(id) ON DELETE CASCADE,
    locale VARCHAR(35) NOT NULL,
    name TEXT NOT NULL,
    slug TEXT NOT NULL,
    description TEXT NOT NULL,
    generated_by VARCHAR(200) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (category_id, locale)
);

CREATE TABLE listings (
    id UUID PRIMARY KEY,
    author_id UUID NOT NULL,
    description TEXT NOT NULL,
    description_clean TEXT,
    status TEXT NOT NULL,
    base_category_id UUID REFERENCES base_categories(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE listing_categories (
    listing_id UUID NOT NULL REFERENCES listings(id) ON DELETE CASCADE,
    category_id UUID NOT NULL REFERENCES categories(id) ON DELETE RESTRICT,
    base_category_id UUID NOT NULL REFERENCES base_categories(id) ON DELETE RESTRICT,
    PRIMARY KEY (listing_id, category_id)
);

CREATE TABLE gig_details (
    gig_id UUID PRIMARY KEY REFERENCES listings(id) ON DELETE CASCADE,
    title TEXT NOT NULL,
    duration_type TEXT,
    pay_amount NUMERIC,
    pay_currency TEXT NOT NULL DEFAULT '',
    expires_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE users (
    id UUID PRIMARY KEY,
    profession_request_id UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE user_categories (
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    category_id UUID NOT NULL REFERENCES categories(id) ON DELETE RESTRICT,
    PRIMARY KEY (user_id, category_id),
    UNIQUE (user_id)
);
