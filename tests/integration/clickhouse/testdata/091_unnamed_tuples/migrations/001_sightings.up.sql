CREATE TABLE sightings (
    id UInt64,
    position Tuple(Float64, Float64),
    legs Array(Tuple(String, UInt16)),
    tallies Map(String, Tuple(UInt8, String))
) ENGINE = MergeTree ORDER BY id;
