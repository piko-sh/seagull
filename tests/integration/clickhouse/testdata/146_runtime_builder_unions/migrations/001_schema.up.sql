CREATE TABLE scores (id Int64, score Nullable(Int64)) ENGINE = MergeTree ORDER BY id;
CREATE TABLE guests (id Int64) ENGINE = MergeTree ORDER BY id;
