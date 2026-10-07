CREATE SEQUENCE lost_property_ids;

CREATE TABLE lost_property (
    id          BIGINT  PRIMARY KEY DEFAULT nextval('lost_property_ids'),
    item        VARCHAR NOT NULL,
    colour      VARCHAR NOT NULL,
    found_at    VARCHAR NOT NULL,
    days_held   INTEGER NOT NULL,
    claimed     BOOLEAN NOT NULL DEFAULT false,
    owner_phone VARCHAR
);
