CREATE TYPE mood AS ENUM ('calm', 'stormy');

CREATE TABLE notes (
  id INT4 PRIMARY KEY,
  tags text[] NOT NULL,
  scores INT4[],
  moods mood[]
);

CREATE TABLE authors (
  id bigint PRIMARY KEY,
  name text NOT NULL,
  bio text
);
