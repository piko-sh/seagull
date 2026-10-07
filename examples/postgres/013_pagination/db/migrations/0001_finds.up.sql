CREATE TABLE finds (
    id       BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    creature TEXT   NOT NULL,
    pool     TEXT   NOT NULL,
    size_mm  BIGINT NOT NULL
);
