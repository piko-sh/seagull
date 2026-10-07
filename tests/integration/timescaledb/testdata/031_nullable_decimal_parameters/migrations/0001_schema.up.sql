CREATE TABLE prices (
  id INTEGER PRIMARY KEY,
  amount NUMERIC(10, 2) NOT NULL,
  discount NUMERIC(10, 2),
  label TEXT
);
