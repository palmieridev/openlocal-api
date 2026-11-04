CREATE TABLE saved_businesses (
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    business_id uuid NOT NULL REFERENCES businesses(id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, business_id)
);
CREATE INDEX saved_businesses_recent ON saved_businesses(user_id, created_at DESC, business_id DESC);
ALTER TABLE appointments ADD COLUMN customer_user_id uuid REFERENCES users(id) ON DELETE SET NULL;
ALTER TABLE preorders ADD COLUMN customer_user_id uuid REFERENCES users(id) ON DELETE SET NULL;
CREATE INDEX appointments_personal ON appointments(customer_user_id, created_at DESC, id DESC) WHERE customer_user_id IS NOT NULL;
CREATE INDEX preorders_personal ON preorders(customer_user_id, created_at DESC, id DESC) WHERE customer_user_id IS NOT NULL;
