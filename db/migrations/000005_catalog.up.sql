CREATE TABLE categories (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id uuid NOT NULL REFERENCES businesses (id) ON DELETE CASCADE,
    name text NOT NULL CHECK (char_length(name) BETWEEN 2 AND 80),
    slug text NOT NULL CHECK (slug ~ '^[a-z0-9]+(?:-[a-z0-9]+)*$'),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (business_id, slug)
);

CREATE TABLE products (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id uuid NOT NULL REFERENCES businesses (id) ON DELETE CASCADE,
    category_id uuid REFERENCES categories (id) ON DELETE SET NULL,
    name text NOT NULL CHECK (char_length(name) BETWEEN 2 AND 180),
    slug text NOT NULL CHECK (slug ~ '^[a-z0-9]+(?:-[a-z0-9]+)*$'),
    description text NOT NULL DEFAULT '',
    brand text,
    unit text NOT NULL DEFAULT 'piece',
    product_type text NOT NULL DEFAULT 'stocked_product' CHECK (product_type IN ('stocked_product', 'made_to_order_product', 'unique_item')),
    is_handmade boolean NOT NULL DEFAULT false,
    is_public boolean NOT NULL DEFAULT false,
    status text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'active', 'archived')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (business_id, slug)
);

CREATE INDEX idx_products_business_id ON products (business_id);
CREATE INDEX idx_products_public_status ON products (is_public, status);

CREATE TABLE product_variants (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    product_id uuid NOT NULL REFERENCES products (id) ON DELETE CASCADE,
    business_id uuid NOT NULL REFERENCES businesses (id) ON DELETE CASCADE,
    sku text NOT NULL CHECK (char_length(sku) BETWEEN 2 AND 80),
    barcode text,
    internal_code text NOT NULL CHECK (char_length(internal_code) BETWEEN 2 AND 80),
    name text NOT NULL DEFAULT '',
    attributes jsonb NOT NULL DEFAULT '{}'::jsonb,
    price numeric(12,2) NOT NULL CHECK (price >= 0),
    cost numeric(12,2) CHECK (cost IS NULL OR cost >= 0),
    currency text NOT NULL DEFAULT 'MXN' CHECK (currency ~ '^[A-Z]{3}$'),
    track_inventory boolean NOT NULL DEFAULT true,
    public_stock_status text NOT NULL DEFAULT 'unknown' CHECK (public_stock_status IN ('available', 'low_stock', 'out_of_stock', 'made_to_order', 'unknown')),
    reorder_point numeric(12,3) NOT NULL DEFAULT 0 CHECK (reorder_point >= 0),
    lead_time_days int NOT NULL DEFAULT 0 CHECK (lead_time_days >= 0),
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'archived')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (business_id, sku),
    UNIQUE (business_id, internal_code),
    UNIQUE (business_id, barcode)
);

CREATE INDEX idx_variants_product_id ON product_variants (product_id);
CREATE INDEX idx_variants_barcode ON product_variants (business_id, barcode);
CREATE INDEX idx_variants_sku ON product_variants (business_id, sku);

CREATE TABLE product_images (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    product_id uuid NOT NULL REFERENCES products (id) ON DELETE CASCADE,
    url text NOT NULL,
    alt_text text NOT NULL DEFAULT '',
    position int NOT NULL DEFAULT 0 CHECK (position >= 0),
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE tags (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id uuid NOT NULL REFERENCES businesses (id) ON DELETE CASCADE,
    name text NOT NULL CHECK (char_length(name) BETWEEN 2 AND 60),
    slug text NOT NULL CHECK (slug ~ '^[a-z0-9]+(?:-[a-z0-9]+)*$'),
    UNIQUE (business_id, slug)
);

CREATE TABLE product_tags (
    product_id uuid NOT NULL REFERENCES products (id) ON DELETE CASCADE,
    tag_id uuid NOT NULL REFERENCES tags (id) ON DELETE CASCADE,
    PRIMARY KEY (product_id, tag_id)
);

