BEGIN;

ALTER TABLE categories
    RENAME COLUMN name TO canonical_name;

ALTER TABLE categories
    RENAME COLUMN slug TO canonical_slug;

ALTER TABLE categories
    RENAME COLUMN description TO canonical_description;

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

COMMIT;
