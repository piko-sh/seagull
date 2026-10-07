CREATE TABLE harbours (
    id     BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name   TEXT   NOT NULL,
    county TEXT   NOT NULL
);

CREATE TABLE skippers (
    id      BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name    TEXT   NOT NULL,
    licence TEXT   NOT NULL
);

CREATE TABLE boats (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name       TEXT   NOT NULL,
    harbour_id BIGINT NOT NULL REFERENCES harbours (id),
    skipper_id BIGINT REFERENCES skippers (id)
);
