CREATE TYPE colour AS ENUM ('red', 'green', 'blue');

CREATE TABLE things (
    id bigint PRIMARY KEY,
    colours colour[] NOT NULL,
    favourite colour
);
