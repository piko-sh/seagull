CREATE TABLE shows (
    id Int64,
    title String,
    seasons Int32
) ENGINE = MergeTree ORDER BY id;

CREATE TABLE documents (
    id Int64,
    data JSON
) ENGINE = MergeTree ORDER BY id;
