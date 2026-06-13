CREATE INDEX idx_marketplace_businesses_public ON businesses (status, slug) WHERE status = 'active';
CREATE INDEX idx_marketplace_products_public ON products (business_id, is_public, status) WHERE is_public = true AND status = 'active';
CREATE INDEX idx_marketplace_product_search ON products USING gin (to_tsvector('simple', name || ' ' || description));

