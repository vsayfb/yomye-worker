DROP TRIGGER IF EXISTS trg_touch_category_localization_updated_at
    ON category_localizations;
DROP FUNCTION IF EXISTS touch_category_localization_updated_at();

DROP TABLE IF EXISTS category_localizations;

DROP TRIGGER IF EXISTS trg_touch_category_updated_at ON categories;
DROP FUNCTION IF EXISTS touch_category_updated_at();

DROP TRIGGER IF EXISTS trg_propagate_category_depth ON categories;
DROP FUNCTION IF EXISTS propagate_category_depth();

DROP TRIGGER IF EXISTS trg_enforce_category_tree ON categories;
DROP FUNCTION IF EXISTS enforce_category_tree();

DROP TABLE IF EXISTS categories;
