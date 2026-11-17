CREATE TABLE switches (
    id UInt64,
    flags Map(Bool, Bool),
    ratios Map(Bool, Nullable(Float64)),
    nested Map(Bool, Map(Bool, Bool))
) ENGINE = MergeTree ORDER BY id;
