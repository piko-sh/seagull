CREATE TABLE blobs (id Int64, tags Array(String), nums Array(Int64), maybe Nullable(String)) ENGINE = MergeTree ORDER BY id;
