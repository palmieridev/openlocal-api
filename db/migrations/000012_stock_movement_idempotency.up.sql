ALTER TABLE stock_movements
ADD COLUMN idempotency_key text
CHECK (idempotency_key IS NULL OR (
    char_length(idempotency_key) BETWEEN 8 AND 128
    AND idempotency_key ~ '^[A-Za-z0-9._:-]+$'
));

CREATE UNIQUE INDEX idx_stock_movements_business_idempotency
ON stock_movements (business_id, idempotency_key)
WHERE idempotency_key IS NOT NULL;
