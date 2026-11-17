CREATE TABLE shipments (
    id INTEGER PRIMARY KEY,
    ordered_at TIMESTAMP NOT NULL,
    shipped_at TIMESTAMP,
    ordered_on DATE NOT NULL,
    due_on DATE NOT NULL,
    handling INTERVAL NOT NULL
);
