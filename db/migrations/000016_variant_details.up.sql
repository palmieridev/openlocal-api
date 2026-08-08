ALTER TABLE product_variants
    ADD COLUMN description text,
    ADD COLUMN price_note text,
    ADD CONSTRAINT product_variants_description_length_check CHECK (
        description IS NULL OR char_length(description) <= 2000
    ),
    ADD CONSTRAINT product_variants_price_note_length_check CHECK (
        price_note IS NULL OR char_length(price_note) <= 500
    );

COMMENT ON COLUMN product_variants.description IS
    'Public details specific to this variant, separate from the parent product description.';

COMMENT ON COLUMN product_variants.price_note IS
    'Public qualification displayed beside the price, such as made-to-measure pricing conditions.';
