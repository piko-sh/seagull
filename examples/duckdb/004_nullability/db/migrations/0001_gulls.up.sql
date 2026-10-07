CREATE SEQUENCE gull_ids;

CREATE SEQUENCE sighting_ids;

CREATE TABLE gulls (
    id       BIGINT  PRIMARY KEY DEFAULT nextval('gull_ids'),
    ring     VARCHAR NOT NULL UNIQUE,
    nickname VARCHAR
);

CREATE TABLE sightings (
    id      BIGINT  PRIMARY KEY DEFAULT nextval('sighting_ids'),
    gull_id BIGINT  NOT NULL REFERENCES gulls (id),
    seen_on DATE    NOT NULL,
    place   VARCHAR NOT NULL
);
