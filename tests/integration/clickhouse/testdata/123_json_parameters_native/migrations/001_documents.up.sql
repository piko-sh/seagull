CREATE TABLE documents (
    id Int32,
    data JSON
) ENGINE = MergeTree ORDER BY id;

CREATE TABLE composites (
    id Int32,
    documents Array(JSON),
    pair Tuple(doc JSON, n Int64)
) ENGINE = MergeTree ORDER BY id;
