CREATE TABLE landings (
    id        UInt64,
    catch_id  UUID,
    species   String,
    boxes     UInt16,
    fish      UInt32,
    weight_g  Int64,
    length_cm Float32,
    price     Decimal(9, 2),
    on_ice    Bool,
    landed_at DateTime('UTC'),
    tag       FixedString(4),
    notes     Nullable(String)
) ENGINE = MergeTree() ORDER BY id;
