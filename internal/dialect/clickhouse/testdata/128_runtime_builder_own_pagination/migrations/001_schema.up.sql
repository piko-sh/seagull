CREATE TABLE items (
  id Int64,
  grp Int64,
  score Int64
) ENGINE = MergeTree ORDER BY id;
