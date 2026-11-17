CREATE TABLE events (
    id UInt64,
    name String,
    doubled UInt64 ALIAS id * 2,
    loud String MATERIALIZED upper(name),
    raw String EPHEMERAL,
    region String DEFAULT 'eu'
) ENGINE = MergeTree ORDER BY id;
