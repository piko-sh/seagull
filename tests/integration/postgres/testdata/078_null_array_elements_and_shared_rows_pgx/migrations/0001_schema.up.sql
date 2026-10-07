CREATE TYPE mood AS ENUM ('calm', 'stormy');

CREATE TABLE notes (
  id integer PRIMARY KEY,
  tags text[] NOT NULL,
  scores integer[],
  moods mood[]
);

CREATE TABLE authors (
  id bigint PRIMARY KEY,
  name text NOT NULL,
  bio text
);
