-- sgl.query(BuiltTuples, many)
SELECT
    tuple(1 AS rank, 'gull' AS kind) AS called,
    (id AS key, name AS label) AS parenthesised,
    tuple(id AS key) AS single,
    tuple(wingspan AS span, name) AS partly
FROM gulls ORDER BY id;

-- sgl.query(NestedTuples, many)
SELECT
    [tuple(id AS key, name AS label)] AS listed,
    map(name, tuple(id AS key, 'gull' AS kind)) AS keyed,
    CAST(tuple(id, name) AS Tuple(key UInt64, label String)) AS named
FROM gulls WHERE id = {id:UInt64};

-- sgl.query(AliasedArguments, one)
SELECT plus(id AS key, 1) AS next, (id + 1 AS bumped) * 2 AS doubled, [1 AS first, 2] AS pair FROM gulls WHERE id = {id:UInt64};
