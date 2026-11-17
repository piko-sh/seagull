-- sgl.query(RecordGulls, exec)
INSERT INTO gulls (id, name, wingspan, weight) VALUES (1, 'herring', 1.4, 1.25), (2, 'kittiwake', NULL, 0.40);

-- sgl.query(LateralAliases, many)
SELECT id + 1 AS next_id, next_id * 2 AS doubled, upper(name) AS loud, concat(loud, '!') AS shout,
    wingspan AS span, span / 2 AS half_span, weight AS mass, mass * 2 AS double_mass
FROM gulls ORDER BY id;
