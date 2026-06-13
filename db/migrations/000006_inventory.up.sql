CREATE TABLE inventory_locations (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id uuid NOT NULL REFERENCES businesses (id) ON DELETE CASCADE,
    name text NOT NULL CHECK (char_length(name) BETWEEN 2 AND 120),
    is_default boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (business_id, name)
);

CREATE UNIQUE INDEX idx_inventory_locations_default ON inventory_locations (business_id) WHERE is_default;

CREATE TABLE stock_movements (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id uuid NOT NULL REFERENCES businesses (id) ON DELETE CASCADE,
    variant_id uuid NOT NULL REFERENCES product_variants (id) ON DELETE RESTRICT,
    location_id uuid NOT NULL REFERENCES inventory_locations (id) ON DELETE RESTRICT,
    movement_type text NOT NULL CHECK (movement_type IN ('IN_PURCHASE', 'IN_PRODUCTION', 'OUT_SALE', 'OUT_ADJUSTMENT', 'IN_ADJUSTMENT', 'OUT_LOSS')),
    quantity numeric(12,3) NOT NULL CHECK (quantity > 0),
    unit_cost numeric(12,2) CHECK (unit_cost IS NULL OR unit_cost >= 0),
    reference_type text,
    reference_id text,
    notes text NOT NULL DEFAULT '',
    created_by uuid NOT NULL REFERENCES users (id) ON DELETE RESTRICT,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX idx_stock_movements_business_created_at ON stock_movements (business_id, created_at DESC);
CREATE INDEX idx_stock_movements_variant_id ON stock_movements (variant_id);

CREATE TABLE stock_levels (
    business_id uuid NOT NULL REFERENCES businesses (id) ON DELETE CASCADE,
    variant_id uuid NOT NULL REFERENCES product_variants (id) ON DELETE CASCADE,
    location_id uuid NOT NULL REFERENCES inventory_locations (id) ON DELETE CASCADE,
    quantity_on_hand numeric(12,3) NOT NULL DEFAULT 0,
    quantity_reserved numeric(12,3) NOT NULL DEFAULT 0 CHECK (quantity_reserved >= 0),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (business_id, variant_id, location_id)
);

CREATE INDEX idx_stock_levels_business_variant ON stock_levels (business_id, variant_id);

