CREATE TABLE readings (
    id BIGINT PRIMARY KEY,
    label VARCHAR(40) NOT NULL,
    tally INT NOT NULL,
    token BINARY(16) NOT NULL,
    document JSON NOT NULL
);
