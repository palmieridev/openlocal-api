DROP TABLE IF EXISTS business_service_areas;

ALTER TABLE businesses
    DROP CONSTRAINT IF EXISTS businesses_coordinate_pair_check,
    DROP COLUMN IF EXISTS location_mode;

-- Downgrading cannot represent nullable broad/mobile locations in the original
-- schema, so restore its historical defaults before reinstating NOT NULL.
UPDATE businesses
SET city = COALESCE(city, 'CDMX'),
    state = COALESCE(state, 'CDMX'),
    country = COALESCE(country, 'MX');

ALTER TABLE businesses
    ALTER COLUMN city SET DEFAULT 'CDMX',
    ALTER COLUMN city SET NOT NULL,
    ALTER COLUMN state SET DEFAULT 'CDMX',
    ALTER COLUMN state SET NOT NULL,
    ALTER COLUMN country SET DEFAULT 'MX',
    ALTER COLUMN country SET NOT NULL;
