CREATE SEQUENCE harbour_ids;

CREATE SEQUENCE skipper_ids;

CREATE SEQUENCE boat_ids;

CREATE TABLE harbours (
    id     BIGINT  PRIMARY KEY DEFAULT nextval('harbour_ids'),
    name   VARCHAR NOT NULL,
    county VARCHAR NOT NULL
);

CREATE TABLE skippers (
    id      BIGINT  PRIMARY KEY DEFAULT nextval('skipper_ids'),
    name    VARCHAR NOT NULL,
    licence VARCHAR NOT NULL
);

CREATE TABLE boats (
    id         BIGINT  PRIMARY KEY DEFAULT nextval('boat_ids'),
    name       VARCHAR NOT NULL,
    harbour_id BIGINT  NOT NULL REFERENCES harbours (id),
    skipper_id BIGINT  REFERENCES skippers (id)
);
