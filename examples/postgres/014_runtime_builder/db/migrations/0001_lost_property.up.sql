CREATE TABLE lost_property (
    id          BIGINT  GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    item        TEXT    NOT NULL,
    colour      TEXT    NOT NULL,
    found_at    TEXT    NOT NULL,
    days_held   BIGINT  NOT NULL,
    claimed     BOOLEAN NOT NULL DEFAULT false,
    owner_phone TEXT
);
