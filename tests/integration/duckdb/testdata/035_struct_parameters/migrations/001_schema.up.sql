CREATE TABLE boats (
    id     INTEGER PRIMARY KEY,
    berth  STRUCT(pontoon VARCHAR, number INTEGER) NOT NULL,
    route  STRUCT(lat DOUBLE, lon DOUBLE)[] NOT NULL,
    extra  STRUCT(note VARCHAR, fees MAP(VARCHAR, INTEGER))
);
