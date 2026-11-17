CREATE TYPE mood AS ENUM ('calm', 'stormy');

CREATE TABLE notes (
  id INTEGER PRIMARY KEY,
  tags VARCHAR[] NOT NULL,
  scores INTEGER[],
  moods mood[],
  limits MAP(VARCHAR, INTEGER)
);

CREATE TABLE authors (
  id BIGINT PRIMARY KEY,
  name VARCHAR NOT NULL,
  bio VARCHAR
);
