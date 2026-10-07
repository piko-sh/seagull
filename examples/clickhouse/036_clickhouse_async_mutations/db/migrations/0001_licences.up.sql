CREATE TABLE licences (
    id       UInt64,
    boat     String,
    expires  Date,
    renewals UInt8 DEFAULT 0
) ENGINE = MergeTree() ORDER BY id;
