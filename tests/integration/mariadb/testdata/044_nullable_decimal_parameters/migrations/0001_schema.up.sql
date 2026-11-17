CREATE TABLE prices (
  id INTEGER PRIMARY KEY,
  amount DECIMAL(10, 2) NOT NULL,
  discount DECIMAL(10, 2),
  label TEXT
);
