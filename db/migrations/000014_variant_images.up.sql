ALTER TABLE product_images
    ADD COLUMN variant_id uuid REFERENCES product_variants (id) ON DELETE CASCADE;

UPDATE product_images AS pi
SET variant_id = (
    SELECT pv.id
    FROM product_variants AS pv
    WHERE pv.product_id = pi.product_id
    ORDER BY pv.created_at ASC, pv.id ASC
    LIMIT 1
);

ALTER TABLE product_images
    ALTER COLUMN variant_id SET NOT NULL,
    DROP COLUMN product_id;

CREATE INDEX idx_product_images_variant_id ON product_images (variant_id);
