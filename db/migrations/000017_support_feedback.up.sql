CREATE TABLE support_feedback (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    doc_id text NOT NULL,
    locale text NOT NULL,
    verdict text NOT NULL,
    comment text,
    path text,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT support_feedback_locale_check CHECK (locale IN ('es', 'en')),
    CONSTRAINT support_feedback_verdict_check CHECK (verdict IN ('up', 'down'))
);

CREATE INDEX support_feedback_doc_id_created_at_idx
    ON support_feedback (doc_id, created_at DESC);
