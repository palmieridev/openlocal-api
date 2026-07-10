ALTER TABLE businesses
    ADD COLUMN timezone text NOT NULL DEFAULT 'America/Mexico_City'
    CHECK (char_length(timezone) BETWEEN 1 AND 100);

ALTER TABLE business_hours
    ADD CONSTRAINT business_hours_schedule_check CHECK (
        (is_closed AND opens_at IS NULL AND closes_at IS NULL)
        OR
        (NOT is_closed AND opens_at IS NOT NULL AND closes_at IS NOT NULL AND opens_at <> closes_at)
    );
