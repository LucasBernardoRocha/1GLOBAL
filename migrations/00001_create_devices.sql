-- The service currently has a single aggregate. Keeping the constraints in the
-- database makes the invariants hold even if a future writer bypasses the API.
CREATE TABLE devices (
    id UUID PRIMARY KEY,
    name TEXT NOT NULL CHECK (length(btrim(name)) > 0),
    brand TEXT NOT NULL CHECK (length(btrim(brand)) > 0),
    state TEXT NOT NULL CHECK (state IN ('available', 'in-use', 'inactive')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX devices_brand_idx ON devices (brand);
CREATE INDEX devices_state_idx ON devices (state);
