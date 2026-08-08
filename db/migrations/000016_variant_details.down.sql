ALTER TABLE product_variants
    DROP CONSTRAINT IF EXISTS product_variants_price_note_length_check,
    DROP CONSTRAINT IF EXISTS product_variants_description_length_check,
    DROP COLUMN IF EXISTS price_note,
    DROP COLUMN IF EXISTS description;
