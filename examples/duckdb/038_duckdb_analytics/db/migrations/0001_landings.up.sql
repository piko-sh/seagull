CREATE SEQUENCE landing_ids;

CREATE TABLE landings (
    id        BIGINT  PRIMARY KEY DEFAULT nextval('landing_ids'),
    harbour   VARCHAR NOT NULL,
    boat      VARCHAR NOT NULL,
    species   VARCHAR NOT NULL,
    kg        INTEGER NOT NULL,
    landed_on DATE    NOT NULL
);

CREATE MACRO tonnes(kg) AS CAST(kg AS DOUBLE) / 1000;

CREATE MACRO harbour_landings(name) AS TABLE
    SELECT boat, species, kg FROM landings WHERE harbour = name;
