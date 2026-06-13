CREATE TABLE business_members (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id uuid NOT NULL REFERENCES businesses (id) ON DELETE CASCADE,
    user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    clerk_org_id text NOT NULL,
    role text NOT NULL CHECK (role IN ('owner', 'manager', 'staff')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (business_id, user_id),
    UNIQUE (business_id, clerk_org_id, user_id)
);

CREATE INDEX idx_business_members_user_id ON business_members (user_id);
CREATE INDEX idx_business_members_clerk_org_id ON business_members (clerk_org_id);

