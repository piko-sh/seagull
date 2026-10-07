CREATE TABLE notes (
  id Int32,
  tags Array(Nullable(String)),
  scores Array(Nullable(Int32)),
  plain Array(String),
  limits Map(String, Nullable(Int32))
) ENGINE = MergeTree ORDER BY id;

CREATE TABLE authors (id Int64, name String, bio Nullable(String)) ENGINE = MergeTree ORDER BY id;
