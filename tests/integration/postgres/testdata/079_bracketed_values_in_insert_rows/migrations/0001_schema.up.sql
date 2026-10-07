CREATE TABLE readings (
  id integer PRIMARY KEY,
  scores integer[] NOT NULL,
  label text NOT NULL,
  flagged boolean NOT NULL,
  tags text[] NOT NULL
);
