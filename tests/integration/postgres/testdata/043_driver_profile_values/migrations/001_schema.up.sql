CREATE TABLE shifts (
    id SERIAL PRIMARY KEY,
    starts_at TIME NOT NULL,
    length INTERVAL NOT NULL,
    ends_at TIME
);
