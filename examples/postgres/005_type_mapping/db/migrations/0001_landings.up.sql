CREATE TABLE landings (
    id         BIGINT           GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    catch_id   UUID             NOT NULL,
    species    TEXT             NOT NULL,
    boxes      SMALLINT         NOT NULL,
    fish       INTEGER          NOT NULL,
    weight_g   BIGINT           NOT NULL,
    length_cm  REAL             NOT NULL,
    sea_temp   DOUBLE PRECISION NOT NULL,
    price      NUMERIC(8, 2)    NOT NULL,
    on_ice     BOOLEAN          NOT NULL,
    landed_on  DATE             NOT NULL,
    landed_at  TIMESTAMPTZ      NOT NULL,
    harbour_ip INET             NOT NULL,
    tag        BYTEA            NOT NULL,
    notes      TEXT
);
