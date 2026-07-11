ALTER TABLE businesses
    ADD COLUMN clerk_org_id text;

UPDATE businesses b
SET clerk_org_id = bm.clerk_org_id
FROM (
    SELECT DISTINCT ON (business_id) business_id, clerk_org_id
    FROM business_members
    ORDER BY business_id, created_at ASC
) bm
WHERE bm.business_id = b.id
  AND b.clerk_org_id IS NULL;

CREATE UNIQUE INDEX idx_businesses_clerk_org_id
    ON businesses (clerk_org_id)
    WHERE clerk_org_id IS NOT NULL;
