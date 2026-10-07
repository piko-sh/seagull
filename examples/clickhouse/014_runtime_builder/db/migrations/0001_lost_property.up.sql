CREATE TABLE lost_property (
    id          UInt64,
    item        String,
    colour      String,
    found_at    String,
    days_held   UInt32,
    claimed     Bool DEFAULT false,
    owner_phone Nullable(String)
) ENGINE = MergeTree() ORDER BY id;
