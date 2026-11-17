CREATE TABLE events (id UInt64, kind String, tags Array(String), version UInt32) ENGINE = ReplacingMergeTree(version) ORDER BY (id, intHash32(id)) SAMPLE BY intHash32(id);
