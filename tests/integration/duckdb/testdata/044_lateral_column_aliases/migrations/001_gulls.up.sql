CREATE TABLE gulls (
    id       INTEGER PRIMARY KEY,
    name     VARCHAR NOT NULL,
    wingspan DOUBLE,
    weight   DECIMAL(6, 2) NOT NULL,
    lifespan INTERVAL NOT NULL,
    notes    JSON
);
