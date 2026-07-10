DROP INDEX IF EXISTS idx_stock_movements_business_idempotency;

ALTER TABLE stock_movements
DROP COLUMN IF EXISTS idempotency_key;
