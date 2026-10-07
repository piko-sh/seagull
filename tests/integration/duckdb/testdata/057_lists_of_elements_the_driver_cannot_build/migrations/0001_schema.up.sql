CREATE TYPE mood AS ENUM ('calm', 'stormy');

CREATE TABLE voyages (
  id INTEGER PRIMARY KEY,
  moods mood[] NOT NULL,
  fares DECIMAL(10, 2)[] NOT NULL,
  photos BLOB[] NOT NULL,
  crew UUID[] NOT NULL,
  legs INTERVAL[] NOT NULL,
  gusts UTINYINT[] NOT NULL,
  watches mood[][] NOT NULL,
  pair mood[2] NOT NULL,
  forecast mood[],
  price STRUCT(amount DECIMAL(10, 2), currency VARCHAR),
  rates MAP(VARCHAR, DECIMAL(10, 2))
);
