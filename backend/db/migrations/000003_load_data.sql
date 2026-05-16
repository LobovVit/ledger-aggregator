-- Бизнес-справочники, полученные из полных выборок SVAP.

CREATE TABLE IF NOT EXISTS dictionary_items_stage (
    business VARCHAR(10) NOT NULL,
    dictionary_code VARCHAR(50) NOT NULL,
    item_code VARCHAR(100) NOT NULL,
    item_short_name TEXT NOT NULL,
    item_full_name TEXT NOT NULL,
    analytical_attribute_code VARCHAR(50)
);

TRUNCATE TABLE dictionary_items_stage;

\copy dictionary_items_stage (business, dictionary_code, item_code, item_short_name, item_full_name, analytical_attribute_code) FROM 'data/business_dictionary_items.tsv' WITH (FORMAT csv, HEADER true, DELIMITER E'\t');

INSERT INTO dictionaries_cache (
    business,
    dictionary_code,
    item_code,
    item_short_name,
    item_full_name,
    analytical_attribute_code
)
SELECT
    business,
    dictionary_code,
    item_code,
    item_short_name,
    item_full_name,
    analytical_attribute_code
FROM dictionary_items_stage
ON CONFLICT (business, dictionary_code, item_code) DO UPDATE SET
    item_short_name = EXCLUDED.item_short_name,
    item_full_name = EXCLUDED.item_full_name,
    analytical_attribute_code = EXCLUDED.analytical_attribute_code,
    last_updated = CURRENT_TIMESTAMP;

DROP TABLE dictionary_items_stage;
