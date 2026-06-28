ALTER TABLE stock_movements
    DROP CONSTRAINT stock_movements_created_by_fkey,
    ADD CONSTRAINT stock_movements_created_by_fkey
        FOREIGN KEY (created_by) REFERENCES users (id) ON DELETE RESTRICT;

ALTER TABLE stock_movements
    ALTER COLUMN created_by SET NOT NULL;
