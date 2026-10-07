CREATE SEQUENCE voyage_ids;

CREATE TABLE voyages (
    id        BIGINT    PRIMARY KEY DEFAULT nextval('voyage_ids'),
    boat      VARCHAR   NOT NULL,
    ports     VARCHAR[] NOT NULL,
    catch_kg  INTEGER[] NOT NULL,
    sightings VARCHAR[]
);
