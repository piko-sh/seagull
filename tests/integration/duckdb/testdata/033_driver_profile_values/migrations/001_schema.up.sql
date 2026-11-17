CREATE TABLE readings (
    id     INTEGER PRIMARY KEY,
    body   JSON NOT NULL,
    amount DECIMAL(8, 2) NOT NULL,
    span   INTERVAL NOT NULL,
    note   JSON,
    attrs  MAP(VARCHAR, INTEGER),
    point  STRUCT(x INTEGER, y INTEGER)
);
