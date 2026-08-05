ALTER TABLE businesses
    ADD COLUMN location_mode text NOT NULL DEFAULT 'fixed'
    CHECK (location_mode IN ('fixed', 'mobile', 'hybrid'));

-- A lone coordinate cannot identify a usable fixed location. Existing API
-- versions allowed one side to be supplied independently, so clean those rows
-- before enforcing the pair invariant.
UPDATE businesses
SET latitude = NULL,
    longitude = NULL
WHERE (latitude IS NULL) <> (longitude IS NULL);

ALTER TABLE businesses
    ALTER COLUMN city DROP NOT NULL,
    ALTER COLUMN city DROP DEFAULT,
    ALTER COLUMN state DROP NOT NULL,
    ALTER COLUMN state DROP DEFAULT,
    ALTER COLUMN country DROP NOT NULL,
    ALTER COLUMN country DROP DEFAULT,
    ADD CONSTRAINT businesses_coordinate_pair_check CHECK (
        (latitude IS NULL AND longitude IS NULL)
        OR (latitude IS NOT NULL AND longitude IS NOT NULL)
    );

CREATE TABLE business_service_areas (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id uuid NOT NULL REFERENCES businesses (id) ON DELETE CASCADE,
    name text NOT NULL CHECK (char_length(name) BETWEEN 2 AND 120),
    country text NOT NULL DEFAULT 'MX'
        CHECK (country ~ '^[A-Z]{2}$'),
    state text NOT NULL CHECK (char_length(state) BETWEEN 1 AND 120),
    municipality text CHECK (municipality IS NULL OR char_length(municipality) BETWEEN 1 AND 120),
    city text CHECK (city IS NULL OR char_length(city) BETWEEN 1 AND 120),
    neighborhood text CHECK (neighborhood IS NULL OR char_length(neighborhood) BETWEEN 1 AND 120),
    postal_code text CHECK (postal_code IS NULL OR char_length(postal_code) BETWEEN 1 AND 20),
    country_key text NOT NULL,
    state_key text NOT NULL,
    municipality_key text,
    city_key text,
    neighborhood_key text,
    postal_code_key text,
    normalized_key text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (business_id, normalized_key)
);

CREATE INDEX idx_business_service_areas_business
    ON business_service_areas (business_id);

CREATE INDEX idx_business_service_areas_reach
    ON business_service_areas (
        country_key,
        state_key,
        municipality_key,
        city_key,
        neighborhood_key,
        postal_code_key
    );

COMMENT ON TABLE business_service_areas IS
    'Structured public coverage for mobile and hybrid businesses; unrelated to business_categories taxonomy.';
