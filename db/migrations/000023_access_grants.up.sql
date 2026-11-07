CREATE TABLE business_access_grants (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id uuid NOT NULL REFERENCES businesses(id) ON DELETE CASCADE,
    module text NOT NULL CHECK (module IN ('appointments', 'preorders')),
    tier text NOT NULL CHECK (tier IN ('essential', 'business')),
    starts_at timestamptz NOT NULL,
    expires_at timestamptz,
    internal_reason text NOT NULL CHECK (char_length(btrim(internal_reason)) BETWEEN 1 AND 2000),
    owner_message text CHECK (char_length(owner_message) BETWEEN 1 AND 2000),
    created_by uuid NOT NULL REFERENCES users(id),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    version integer NOT NULL DEFAULT 1 CHECK (version > 0),
    revoked_at timestamptz,
    revoked_by uuid REFERENCES users(id),
    revocation_reason text CHECK (char_length(btrim(revocation_reason)) BETWEEN 1 AND 2000),
    CHECK (expires_at IS NULL OR expires_at > starts_at),
    CHECK (num_nonnulls(revoked_at, revoked_by, revocation_reason) IN (0, 3)),
    UNIQUE(business_id, id)
);
CREATE INDEX access_grants_business ON business_access_grants(business_id, created_at DESC, id DESC);
CREATE INDEX access_grants_resolution ON business_access_grants(business_id, module, starts_at);
CREATE INDEX audit_logs_grant_history ON audit_logs(business_id, entity_id, created_at, id) WHERE entity_type = 'access_grant';
