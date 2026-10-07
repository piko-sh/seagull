CREATE TYPE mood AS ENUM ('calm', 'stormy');

CREATE TABLE voyages (
    id        INTEGER PRIMARY KEY,
    stops     TIMESTAMP[] NOT NULL,
    days      DATE[],
    times     TIME[],
    instants  TIMESTAMPTZ[],
    fine      TIMESTAMP_NS[],
    berth     STRUCT(arrived TIMESTAMP, day DATE),
    by_day    MAP(DATE, INTEGER),
    by_weight MAP(DOUBLE, VARCHAR),
    by_flag   MAP(BOOLEAN, INTEGER),
    legs      STRUCT(seen TIMESTAMP[], schedule MAP(DATE, TIMESTAMP))[],
    fees      DECIMAL(8, 2)[],
    spans     INTERVAL[],
    counts    BIGINT[],
    huge      HUGEINT[],
    blobs     BLOB[],
    moods     mood[],
    readings  DOUBLE[]
);

CREATE TABLE arrivals (
    id  INTEGER PRIMARY KEY,
    arrived TIMESTAMP NOT NULL,
    day DATE NOT NULL
);
