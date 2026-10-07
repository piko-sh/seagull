CREATE SEQUENCE boat_ids;

CREATE TABLE boats (
    id      BIGINT                                    PRIMARY KEY DEFAULT nextval('boat_ids'),
    name    VARCHAR                                   NOT NULL UNIQUE,
    berth   STRUCT(pontoon VARCHAR, number INTEGER)   NOT NULL,
    crew    VARCHAR[]                                 NOT NULL,
    route   STRUCT(lat DOUBLE, lon DOUBLE)[]          NOT NULL,
    fees    MAP(VARCHAR, INTEGER)                     NOT NULL,
    contact UNION(phone VARCHAR, vhf_channel INTEGER)
);
