CREATE TABLE readings (
    id INTEGER PRIMARY KEY,
    delta INTEGER NOT NULL,
    count UINTEGER NOT NULL,
    total UBIGINT NOT NULL,
    offset_value BIGINT NOT NULL,
    label VARCHAR NOT NULL
);
INSERT INTO readings VALUES (1, -5, 7, 18446744073709551615, -9, 'gull'), (2, 3, 4000000000, 5, 9, 'tern');
