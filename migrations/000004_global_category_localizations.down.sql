BEGIN;

DROP TABLE category_localizations;

ALTER TABLE categories
    RENAME COLUMN canonical_description TO description;

ALTER TABLE categories
    RENAME COLUMN canonical_slug TO slug;

ALTER TABLE categories
    RENAME COLUMN canonical_name TO name;

COMMIT;
