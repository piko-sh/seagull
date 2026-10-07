CREATE TABLE gulls (
    id       UInt64,
    ring     String,
    nickname Nullable(String)
) ENGINE = MergeTree() ORDER BY id;

CREATE TABLE sightings (
    id      UInt64,
    gull_id UInt64,
    seen_on Date,
    place   String
) ENGINE = MergeTree() ORDER BY id;
