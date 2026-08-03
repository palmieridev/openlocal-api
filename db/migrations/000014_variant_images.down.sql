ALTER TABLE product_images
    ADD COLUMN product_id uuid REFERENCES products (id) ON DELETE CASCADE;

UPDATE product_images AS pi
SET product_id = pv.product_id
FROM product_variants AS pv
WHERE pv.id = pi.variant_id;

ALTER TABLE product_images
    ALTER COLUMN product_id SET NOT NULL;

DROP INDEX idx_product_images_variant_id;

ALTER TABLE product_images
    DROP COLUMN variant_id;
