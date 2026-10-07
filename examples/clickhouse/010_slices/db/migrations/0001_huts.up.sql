CREATE TABLE huts (
    number UInt64,
    beach  String,
    colour String,
    booked Bool DEFAULT false
) ENGINE = MergeTree() ORDER BY number;
