-- sgl.query(SeriesBesideLetters, many)
SELECT n, s FROM ROWS FROM (generate_series(1, 3), unnest(ARRAY['a', 'b'])) AS x(n, s);

-- sgl.query(TracksBesideSettings, many)
SELECT x.*
FROM playlists p, LATERAL ROWS FROM (unnest(p.tracks), jsonb_each(p.settings)) WITH ORDINALITY AS x(track, key, value, position)
WHERE p.id = $1;

-- sgl.query(AliasNamesNoColumn, many)
SELECT z.* FROM ROWS FROM (jsonb_each('{"a": 1}'), generate_series(1, 2)) AS z;

-- sgl.query(OneFunction, many)
SELECT g, ordinality FROM ROWS FROM (generate_series(1, 3)) WITH ORDINALITY AS g;

-- sgl.query(RowsAsValues, many)
SELECT r FROM ROWS FROM (generate_series(1, 2), unnest(ARRAY['a'])) AS r;
