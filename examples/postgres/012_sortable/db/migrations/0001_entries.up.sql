CREATE TABLE entries (
    id             BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    boat           TEXT   NOT NULL,
    crew           BIGINT NOT NULL,
    finish_seconds BIGINT NOT NULL
);
