CREATE TABLE stations (
    id INTEGER PRIMARY KEY,
    name VARCHAR NOT NULL,
    region VARCHAR,
    reading DOUBLE NOT NULL,
    names VARCHAR[] NOT NULL,
    readings INTEGER[] NOT NULL
);
