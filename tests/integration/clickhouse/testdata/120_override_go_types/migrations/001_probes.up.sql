CREATE TABLE probes (
    id Int32,
    label String,
    amount Int64,
    ratio Float64,
    flag Bool,
    note String,
    payload String,
    optional_label Nullable(String),
    optional_amount Nullable(Int64),
    tags Array(String),
    host IPv4,
    optional_host Nullable(IPv4),
    address IPv4,
    pair Tuple(name String, rank Int64),
    keyed Map(String, Int64)
) ENGINE = MergeTree ORDER BY id;
