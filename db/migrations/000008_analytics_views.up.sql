CREATE MATERIALIZED VIEW product_sales_summary AS
SELECT
    pv.business_id,
    pv.id AS variant_id,
    p.id AS product_id,
    COALESCE(SUM(sm.quantity) FILTER (WHERE sm.movement_type = 'OUT_SALE'), 0)::numeric(12,3) AS units_sold,
    COALESCE(SUM(sm.quantity * pv.price) FILTER (WHERE sm.movement_type = 'OUT_SALE'), 0)::numeric(12,2) AS sales_revenue
FROM product_variants pv
JOIN products p ON p.id = pv.product_id
LEFT JOIN stock_movements sm ON sm.variant_id = pv.id
GROUP BY pv.business_id, pv.id, p.id;

CREATE UNIQUE INDEX idx_product_sales_summary_variant ON product_sales_summary (variant_id);
CREATE INDEX idx_product_sales_summary_business ON product_sales_summary (business_id);

