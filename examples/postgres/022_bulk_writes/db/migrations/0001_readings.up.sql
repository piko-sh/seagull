CREATE TABLE readings (
    station   TEXT   NOT NULL,
    minute    BIGINT NOT NULL,
    height_cm BIGINT NOT NULL,
    note      TEXT,
    PRIMARY KEY (station, minute)
);
