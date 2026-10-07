CREATE TABLE trips (
    id UInt64,
    note Nullable(String),
    started Date,
    started32 Date32,
    departed DateTime,
    landed DateTime64(3),
    returned Nullable(Date)
) ENGINE = MergeTree ORDER BY id;
