CREATE TABLE things (
  id INTEGER PRIMARY KEY,
  external_id UUID NOT NULL,
  other_id UUID,
  starts_at TIME,
  zoned TIMESTAMPTZ,
  span INTERVAL,
  raw BYTEA,
  price MONEY NOT NULL,
  label TEXT
);
