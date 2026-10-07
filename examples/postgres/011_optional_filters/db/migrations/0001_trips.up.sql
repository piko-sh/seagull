CREATE TABLE trips (
    id           BIGINT  GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name         TEXT    NOT NULL,
    destination  TEXT    NOT NULL,
    price_pence  BIGINT  NOT NULL,
    dogs_welcome BOOLEAN NOT NULL
);
