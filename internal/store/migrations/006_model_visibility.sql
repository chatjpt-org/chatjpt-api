CREATE TABLE model_visibility (
    model_id TEXT PRIMARY KEY,
    is_public BOOLEAN NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);