CREATE TABLE sightings (
    id UInt64,
    legs Nested(colony String, minutes Nullable(UInt16)),
    tags Array(String),
    started Date,
    departed DateTime,
    note Nullable(String)
) ENGINE = MergeTree ORDER BY id;
