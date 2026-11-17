-- sgl.query(SeriesBesideLetters, many)
SELECT n, s FROM ROWS FROM (generate_series(1, 3), unnest(ARRAY['a', 'b'])) AS x(n, s);

-- sgl.query(TracksBesideSettings, many)
SELECT x.*
FROM playlists p, LATERAL ROWS FROM (unnest(p.tracks), jsonb_each(p.settings)) WITH ORDINALITY AS x(track, key, value, position)
WHERE p.id = $1;

-- sgl.query(UnaliasedSeries, many)
SELECT * FROM ROWS FROM (generate_series(1, 3), generate_series(10, 11)) WITH ORDINALITY;

-- sgl.query(RecordBesideSeries, many)
SELECT a, b, generate_series
FROM ROWS FROM (json_to_record('{"a": 1, "b": "x"}') AS (a integer, b text), generate_series(1, 2)) AS r;

-- sgl.query(OneFunction, many)
SELECT g, ordinality FROM ROWS FROM (generate_series(1, 3)) WITH ORDINALITY AS g;

-- sgl.query(RowsAsValues, many)
SELECT r FROM ROWS FROM (generate_series(1, 2), unnest(ARRAY['a'])) AS r;

-- sgl.query(TracksBesideRatings, many)
SELECT p.id, u.track, u.rating
FROM playlists p, unnest(p.tracks, p.ratings) AS u(track, rating);

-- sgl.query(TracksBesideRatingsInOrder, many)
SELECT u.* FROM playlists p, unnest(p.tracks, p.ratings) WITH ORDINALITY AS u;
