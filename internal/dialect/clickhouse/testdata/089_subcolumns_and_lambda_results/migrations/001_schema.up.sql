CREATE TABLE gulls (
    id UInt64,
    tags Array(String),
    sightings Array(UInt8),
    nicknames Array(Nullable(String)),
    ringings Map(String, UInt32),
    grid Array(Array(UInt8)),
    note Nullable(String),
    flights Nested(colony String, minutes UInt16)
) ENGINE = MergeTree ORDER BY id;
