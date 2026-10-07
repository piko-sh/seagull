CREATE TABLE sightings (
    id UInt64,
    legs Nested(colony String, minutes Nullable(UInt16)),
    tags Array(String),
    counts Array(Nullable(Int32)),
    nests Array(Tuple(colony String, eggs UInt8)),
    pairs Array(Tuple(UInt8, String))
) ENGINE = MergeTree ORDER BY id;
