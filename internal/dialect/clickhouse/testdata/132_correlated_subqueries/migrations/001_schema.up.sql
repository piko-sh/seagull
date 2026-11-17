CREATE TABLE crew (id Int64, boat String) ENGINE = MergeTree ORDER BY id;
CREATE TABLE boats (id Int64, name String) ENGINE = MergeTree ORDER BY id;
