CREATE TABLE events (
  id INTEGER PRIMARY KEY,
  happened DATETIME NOT NULL,
  precise TIMESTAMP(3) NOT NULL,
  micro DATETIME(6),
  day DATE(10)
);
