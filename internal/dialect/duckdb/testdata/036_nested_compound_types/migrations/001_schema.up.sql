CREATE TABLE routes (
    id INTEGER PRIMARY KEY,
    name VARCHAR NOT NULL,
    waypoints STRUCT(x DOUBLE, y DOUBLE)[] NOT NULL,
    metadata MAP(VARCHAR, INTEGER[])
);
