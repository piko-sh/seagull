CREATE TABLE boats (
    id INTEGER NOT NULL,
    name VARCHAR NOT NULL,
    berth STRUCT(pontoon VARCHAR, number INTEGER)
);

CREATE TABLE crews (
    boat_id INTEGER NOT NULL,
    skipper VARCHAR
);
