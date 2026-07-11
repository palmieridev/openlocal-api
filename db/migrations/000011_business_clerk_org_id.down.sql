DROP INDEX IF EXISTS idx_businesses_clerk_org_id;

ALTER TABLE businesses
    DROP COLUMN IF EXISTS clerk_org_id;
