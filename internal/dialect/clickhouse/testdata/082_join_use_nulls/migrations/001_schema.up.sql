CREATE TABLE gulls (id UInt64, name String) ENGINE = MergeTree ORDER BY id;
CREATE TABLE sightings (gull_id UInt64, beach String, note Nullable(String)) ENGINE = MergeTree ORDER BY gull_id;
