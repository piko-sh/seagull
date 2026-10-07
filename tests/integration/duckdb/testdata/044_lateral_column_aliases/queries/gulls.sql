-- sgl.query(name: LateralAliases, command: many)
SELECT id + 1 AS next_id, next_id * 2 AS doubled, upper(name) AS loud, loud || '!' AS shout,
    wingspan AS span, span / 2 AS half_span,
    weight AS mass, mass * 2 AS double_mass,
    lifespan AS life, life > INTERVAL 1 YEAR AS long_lived,
    notes AS document, document->>'colour' AS colour
FROM gulls ORDER BY id;

-- sgl.query(name: ColumnBeforeAlias, command: many)
SELECT name AS id, id + 1 AS next_id FROM gulls ORDER BY next_id;
