ALTER TABLE business_hours
    DROP CONSTRAINT IF EXISTS business_hours_schedule_check;

ALTER TABLE businesses
    DROP COLUMN IF EXISTS timezone;
