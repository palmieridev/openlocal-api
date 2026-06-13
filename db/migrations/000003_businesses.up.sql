CREATE TABLE businesses (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name text NOT NULL CHECK (char_length(name) BETWEEN 2 AND 160),
    slug text NOT NULL UNIQUE CHECK (slug ~ '^[a-z0-9]+(?:-[a-z0-9]+)*$'),
    description text NOT NULL DEFAULT '',
    business_type text NOT NULL DEFAULT 'retail',
    phone text,
    whatsapp text,
    email text,
    website text,
    logo_url text,
    cover_image_url text,
    status text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'active', 'suspended', 'archived')),
    address text,
    neighborhood text,
    city text NOT NULL DEFAULT 'CDMX',
    state text NOT NULL DEFAULT 'CDMX',
    country text NOT NULL DEFAULT 'MX',
    postal_code text,
    latitude numeric(9,6),
    longitude numeric(9,6),
    pickup_available boolean NOT NULL DEFAULT true,
    delivery_available boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CHECK (latitude IS NULL OR (latitude >= -90 AND latitude <= 90)),
    CHECK (longitude IS NULL OR (longitude >= -180 AND longitude <= 180))
);

CREATE INDEX idx_businesses_status ON businesses (status);
CREATE INDEX idx_businesses_location ON businesses (latitude, longitude);

CREATE TABLE business_hours (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id uuid NOT NULL REFERENCES businesses (id) ON DELETE CASCADE,
    day_of_week int NOT NULL CHECK (day_of_week BETWEEN 0 AND 6),
    opens_at time,
    closes_at time,
    is_closed boolean NOT NULL DEFAULT false,
    UNIQUE (business_id, day_of_week)
);

CREATE TABLE business_categories (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id uuid NOT NULL REFERENCES businesses (id) ON DELETE CASCADE,
    name text NOT NULL CHECK (char_length(name) BETWEEN 2 AND 80),
    slug text NOT NULL CHECK (slug ~ '^[a-z0-9]+(?:-[a-z0-9]+)*$'),
    UNIQUE (business_id, slug)
);

