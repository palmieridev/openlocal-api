CREATE TABLE audit_logs (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id uuid REFERENCES businesses (id) ON DELETE SET NULL,
    actor_user_id uuid REFERENCES users (id) ON DELETE SET NULL,
    action text NOT NULL CHECK (char_length(action) BETWEEN 3 AND 120),
    entity_type text NOT NULL CHECK (char_length(entity_type) BETWEEN 2 AND 80),
    entity_id uuid,
    metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX idx_audit_logs_business_created_at ON audit_logs (business_id, created_at DESC);
CREATE INDEX idx_audit_logs_actor_user_id ON audit_logs (actor_user_id);

