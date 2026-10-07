CREATE TABLE sailings (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    origin      TEXT   NOT NULL,
    destination TEXT   NOT NULL,
    departs     TEXT   NOT NULL,
    seats_free  BIGINT NOT NULL
);
