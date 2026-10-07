CREATE TABLE readings (
    id INTEGER PRIMARY KEY,
    station VARCHAR NOT NULL,
    taken_at TIMESTAMP NOT NULL,
    taken_on DATE NOT NULL,
    reading DOUBLE NOT NULL,
    price DECIMAL(10, 2) NOT NULL,
    tally INTEGER NOT NULL,
    valid BOOLEAN NOT NULL,
    label VARCHAR NOT NULL,
    day_text VARCHAR NOT NULL,
    scores INTEGER[] NOT NULL,
    payload VARCHAR NOT NULL
);
