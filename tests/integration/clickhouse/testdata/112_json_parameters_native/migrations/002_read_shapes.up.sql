CREATE TABLE optional_documents (
    id Int32,
    data Nullable(JSON)
) ENGINE = MergeTree ORDER BY id;

CREATE TABLE owners (
    id Int32,
    name String
) ENGINE = MergeTree ORDER BY id;

CREATE TABLE nested_documents (
    id Int32,
    keyed Map(String, JSON),
    optional_keyed Map(String, Nullable(JSON)),
    listed Map(String, Array(JSON)),
    optional_list Array(Nullable(JSON)),
    pairs Map(String, Tuple(doc JSON, n Int64)),
    unnamed Tuple(JSON, Int64),
    listed_unnamed Array(Tuple(JSON, Int64)),
    keyed_unnamed Map(String, Tuple(JSON, Int64))
) ENGINE = MergeTree ORDER BY id;

CREATE TABLE rich_documents (
    id Int32,
    data JSON
) ENGINE = MergeTree ORDER BY id;
