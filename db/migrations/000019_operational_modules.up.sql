CREATE TABLE business_subscriptions (
    business_id uuid PRIMARY KEY REFERENCES businesses(id) ON DELETE CASCADE,
    tier text NOT NULL DEFAULT 'free' CHECK (tier IN ('free','essential','business')),
    status text NOT NULL DEFAULT 'inactive',
    modules text[] NOT NULL DEFAULT '{}',
    stripe_customer_id text UNIQUE,
    stripe_subscription_id text UNIQUE,
    period_start timestamptz NOT NULL DEFAULT now(),
    period_end timestamptz NOT NULL DEFAULT now(),
    grace_until timestamptz,
    cancel_at_period_end boolean NOT NULL DEFAULT false,
    scheduled_change jsonb,
    updated_at timestamptz NOT NULL DEFAULT now(),
    CHECK (modules <@ ARRAY['appointments','preorders']::text[])
);
CREATE TABLE business_modules (
    business_id uuid NOT NULL REFERENCES businesses(id) ON DELETE CASCADE,
    module text NOT NULL CHECK (module IN ('appointments','preorders')),
    enabled boolean NOT NULL DEFAULT false,
    settings jsonb NOT NULL DEFAULT '{}',
    PRIMARY KEY (business_id,module)
);
CREATE TABLE module_customers (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id uuid NOT NULL REFERENCES businesses(id) ON DELETE CASCADE,
    phone text NOT NULL,
    name text NOT NULL,
    UNIQUE (business_id,phone), UNIQUE(business_id,id)
);
CREATE TABLE services (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id uuid NOT NULL REFERENCES businesses(id) ON DELETE CASCADE,
    name text NOT NULL,
    description text NOT NULL DEFAULT '',
    duration_minutes integer NOT NULL CHECK(duration_minutes BETWEEN 5 AND 720),
    price numeric(12,2) CHECK(price >= 0),
    is_public boolean NOT NULL DEFAULT true,
    active boolean NOT NULL DEFAULT true,
    buffer_minutes integer NOT NULL DEFAULT 0 CHECK(buffer_minutes BETWEEN 0 AND 240),
    UNIQUE(business_id,id)
);
CREATE TABLE appointment_resources (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id uuid NOT NULL REFERENCES businesses(id) ON DELETE CASCADE,
    name text NOT NULL,
    active boolean NOT NULL DEFAULT true,
    service_ids uuid[] NOT NULL DEFAULT '{}',
    hours jsonb NOT NULL DEFAULT '[]',
    closed_dates jsonb NOT NULL DEFAULT '[]',
    UNIQUE(business_id,id)
);
CREATE TABLE appointments (
    id uuid PRIMARY KEY,
    business_id uuid NOT NULL REFERENCES businesses(id) ON DELETE CASCADE,
    customer_id uuid NOT NULL,
    service_id uuid NOT NULL,
    resource_id uuid NOT NULL,
    customer_name text NOT NULL,
    customer_phone text NOT NULL,
    notes text NOT NULL DEFAULT '',
    service_name text NOT NULL,
    duration_minutes integer NOT NULL,
    buffer_minutes integer NOT NULL DEFAULT 0,
    price numeric(12,2),
    starts_at timestamptz NOT NULL,
    ends_at timestamptz NOT NULL,
    timezone text NOT NULL,
    status text NOT NULL DEFAULT 'requested' CHECK(status IN ('requested','confirmed','completed','cancelled','rejected','expired','no_show')),
    version integer NOT NULL DEFAULT 1,
    expires_at timestamptz NOT NULL,
    confirmed_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE(business_id,id), CHECK(ends_at > starts_at),
    FOREIGN KEY(business_id,customer_id) REFERENCES module_customers(business_id,id),
    FOREIGN KEY(business_id,service_id) REFERENCES services(business_id,id),
    FOREIGN KEY(business_id,resource_id) REFERENCES appointment_resources(business_id,id)
);
CREATE INDEX appointments_calendar ON appointments(business_id,resource_id,starts_at,ends_at);
CREATE INDEX appointments_expiration ON appointments(expires_at) WHERE status='requested';
CREATE TABLE preorder_pickup_windows (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id uuid NOT NULL REFERENCES businesses(id) ON DELETE CASCADE,
    starts_at timestamptz NOT NULL,
    ends_at timestamptz NOT NULL,
    UNIQUE(business_id,starts_at,ends_at), UNIQUE(business_id,id),
    CHECK(ends_at > starts_at)
);
CREATE TABLE preorders (
    id uuid PRIMARY KEY,
    business_id uuid NOT NULL REFERENCES businesses(id) ON DELETE CASCADE,
    customer_id uuid NOT NULL,
    pickup_window_id uuid NOT NULL,
    customer_name text NOT NULL,
    customer_phone text NOT NULL,
    notes text NOT NULL DEFAULT '',
    starts_at timestamptz NOT NULL,
    ends_at timestamptz NOT NULL,
    timezone text NOT NULL,
    estimated_total numeric(12,2) NOT NULL CHECK(estimated_total >= 0),
    final_total numeric(12,2) CHECK(final_total >= 0),
    status text NOT NULL DEFAULT 'requested' CHECK(status IN ('requested','confirmed','preparing','ready','collected','uncollected','cancelled','rejected','expired')),
    version integer NOT NULL DEFAULT 1,
    expires_at timestamptz NOT NULL,
    confirmed_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE(business_id,id), CHECK(ends_at > starts_at),
    FOREIGN KEY(business_id,customer_id) REFERENCES module_customers(business_id,id),
    FOREIGN KEY(business_id,pickup_window_id) REFERENCES preorder_pickup_windows(business_id,id)
);
CREATE INDEX preorders_pickup ON preorders(business_id,pickup_window_id,status);
CREATE INDEX preorders_expiration ON preorders(expires_at) WHERE status='requested';
ALTER TABLE product_variants ADD CONSTRAINT variants_business_id_unique UNIQUE(business_id,id);
CREATE TABLE preorder_items (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id uuid NOT NULL,
    preorder_id uuid NOT NULL,
    variant_id uuid NOT NULL,
    name text NOT NULL,
    unit text NOT NULL,
    quantity numeric(12,3) NOT NULL CHECK(quantity > 0),
    unit_price numeric(12,2) NOT NULL CHECK(unit_price >= 0),
    total numeric(12,2) NOT NULL CHECK(total >= 0),
    final_quantity numeric(12,3) CHECK(final_quantity > 0),
    FOREIGN KEY(business_id,preorder_id) REFERENCES preorders(business_id,id) ON DELETE CASCADE,
    FOREIGN KEY(business_id,variant_id) REFERENCES product_variants(business_id,id)
);
CREATE TABLE request_access (
    public_id uuid PRIMARY KEY,
    business_id uuid NOT NULL REFERENCES businesses(id) ON DELETE CASCADE,
    module text NOT NULL CHECK(module IN ('appointments','preorders')),
    token_hash bytea NOT NULL,
    expires_at timestamptz NOT NULL
);
CREATE TABLE request_history (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    business_id uuid NOT NULL REFERENCES businesses(id) ON DELETE CASCADE,
    appointment_id uuid,
    preorder_id uuid,
    from_status text NOT NULL,
    to_status text NOT NULL,
    actor text NOT NULL,
    at timestamptz NOT NULL DEFAULT now(),
    CHECK(num_nonnulls(appointment_id,preorder_id)=1),
    FOREIGN KEY(business_id,appointment_id) REFERENCES appointments(business_id,id) ON DELETE CASCADE,
    FOREIGN KEY(business_id,preorder_id) REFERENCES preorders(business_id,id) ON DELETE CASCADE
);
CREATE TABLE module_usage_events (
    business_id uuid NOT NULL REFERENCES businesses(id) ON DELETE CASCADE,
    module text NOT NULL,
    record_id uuid NOT NULL,
    period_start timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY(business_id,module,record_id)
);
CREATE INDEX module_usage_period ON module_usage_events(business_id,module,period_start);
CREATE TABLE request_idempotency (
    business_id uuid NOT NULL REFERENCES businesses(id) ON DELETE CASCADE,
    scope text NOT NULL,
    key text NOT NULL,
    digest bytea NOT NULL,
    response bytea NOT NULL,
    status_code integer NOT NULL,
    expires_at timestamptz NOT NULL DEFAULT now()+interval '24 hours',
    PRIMARY KEY(business_id,scope,key)
);
CREATE TABLE billing_events (
    id text PRIMARY KEY,
    customer_id text NOT NULL,
    status text NOT NULL DEFAULT 'pending',
    created_at timestamptz NOT NULL DEFAULT now(),
    processed_at timestamptz
);
CREATE TABLE billing_operations (
    business_id uuid NOT NULL REFERENCES businesses(id) ON DELETE CASCADE,
    key text NOT NULL,
    digest bytea NOT NULL,
    response jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY(business_id,key)
);
