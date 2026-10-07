CREATE TABLE shipments (
    id integer PRIMARY KEY,
    ordered_at timestamptz NOT NULL,
    shipped_at timestamptz,
    ordered_on date NOT NULL,
    due_on date NOT NULL,
    local_at timestamp NOT NULL,
    opens_at time NOT NULL,
    handling interval NOT NULL
);
