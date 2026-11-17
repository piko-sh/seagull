-- sgl.query(LateralAliases, many)
SELECT id + 1 AS next_id, next_id * 2 AS doubled, upper(name) AS loud, concat(loud, '!') AS shout,
    wingspan AS span, span / 2 AS half_span
FROM gulls ORDER BY id;
