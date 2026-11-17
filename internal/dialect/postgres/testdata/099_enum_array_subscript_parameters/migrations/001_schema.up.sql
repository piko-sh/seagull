CREATE TYPE mood AS ENUM ('calm', 'choppy', 'stormy');

CREATE TABLE crossings (
    id bigint PRIMARY KEY,
    moods mood[] NOT NULL
);
