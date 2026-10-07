CREATE TABLE boats (
    id INTEGER NOT NULL,
    name VARCHAR NOT NULL,
    by_crew MAP(VARCHAR[], INTEGER),
    by_berth MAP(STRUCT(pontoon VARCHAR, number INTEGER), INTEGER),
    by_day MAP(DATE, INTEGER)
);
