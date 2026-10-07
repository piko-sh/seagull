CREATE TABLE landings (
    id         INTEGER  PRIMARY KEY,
    species    TEXT     NOT NULL,
    boxes      SMALLINT NOT NULL,
    fish       INTEGER  NOT NULL,
    weight_g   BIGINT   NOT NULL,
    length_cm  REAL     NOT NULL,
    on_ice     BOOLEAN  NOT NULL,
    landed_at  DATETIME NOT NULL,
    tag        BLOB     NOT NULL,
    notes      TEXT
);
