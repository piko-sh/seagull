CREATE TABLE items (id Int64, category Int64, score Nullable(Int64)) ENGINE = MergeTree ORDER BY id;
