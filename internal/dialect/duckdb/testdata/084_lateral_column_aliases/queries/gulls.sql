-- sgl.query(LateralAliases, many)
SELECT id + 1 AS next_id, next_id * 2 AS doubled, upper(name) AS loud, loud || '!' AS shout,
    wingspan AS span, span / 2 AS half_span
FROM gulls ORDER BY id;

-- sgl.query(ColumnBeforeAlias, many)
SELECT name AS id, id + 1 AS next_id FROM gulls ORDER BY next_id;
