CREATE TABLE blobs (id UInt64, payload String) ENGINE = MergeTree() ORDER BY id;
