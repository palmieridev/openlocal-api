ALTER TABLE stock_movements
    DROP CONSTRAINT stock_movements_created_by_fkey,
    ALTER COLUMN created_by DROP NOT NULL,
    ADD CONSTRAINT stock_movements_created_by_fkey
        FOREIGN KEY (created_by) REFERENCES users (id) ON DELETE SET NULL;

DELETE FROM users
WHERE email IS NULL
  AND first_name IS NULL
  AND last_name IS NULL
  AND image_url IS NULL;
