CREATE SEQUENCE landing_ids;

CREATE TABLE landings (
    id          BIGINT    PRIMARY KEY DEFAULT nextval('landing_ids'),
    species     VARCHAR   NOT NULL,
    quay        UTINYINT  NOT NULL,
    boxes       SMALLINT  NOT NULL,
    fish        INTEGER   NOT NULL,
    weight_g    BIGINT    NOT NULL,
    length_cm   DOUBLE    NOT NULL,
    water_temp  REAL      NOT NULL,
    on_ice      BOOLEAN   NOT NULL,
    landed_at   TIMESTAMP NOT NULL,
    landed_on   DATE      NOT NULL,
    catch_id    UUID      NOT NULL,
    auction_lot HUGEINT   NOT NULL,
    tag         BLOB      NOT NULL,
    notes       VARCHAR
);
