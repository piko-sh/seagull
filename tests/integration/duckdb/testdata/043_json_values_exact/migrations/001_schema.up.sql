CREATE TABLE documents (
    id     INTEGER PRIMARY KEY,
    body   JSON NOT NULL,
    note   JSON,
    amount DECIMAL(8, 2) NOT NULL,
    span   INTERVAL NOT NULL,
    bodies JSON[]
);
