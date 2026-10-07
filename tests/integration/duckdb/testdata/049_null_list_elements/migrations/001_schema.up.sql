CREATE TABLE readings (
    id INTEGER PRIMARY KEY,
    label VARCHAR NOT NULL,
    note VARCHAR,
    scores INTEGER[] NOT NULL,
    limits MAP(VARCHAR, INTEGER) NOT NULL,
    stages STRUCT(step INTEGER)[] NOT NULL
);
