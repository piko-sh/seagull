-- sgl.query(TracksInOrder, many)
SELECT p.id, t.track, t.position
FROM playlists p, unnest(p.tracks) WITH ORDINALITY AS t(track, position)
WHERE p.id = $1
ORDER BY t.position;

-- sgl.query(TracksNamedAfterTheAlias, many)
SELECT track, ordinality FROM playlists, unnest(tracks) WITH ORDINALITY AS track;

-- sgl.query(PartlyNamedColumns, many)
SELECT r.rating, r.ordinality FROM playlists, unnest(ratings) WITH ORDINALITY AS r(rating);

-- sgl.query(SettingsInOrder, many)
SELECT s.key, s.value, s.ordinality FROM playlists, jsonb_each(settings) WITH ORDINALITY AS s;

-- sgl.query(OptionalRatings, many)
SELECT p.id, r.rating, r.position
FROM playlists p LEFT JOIN LATERAL unnest(p.ratings) WITH ORDINALITY r(rating, position) ON true;

-- sgl.query(NumberedSeries, many)
SELECT value, ordinality FROM generate_series(1, 3) WITH ORDINALITY AS value;

-- sgl.query(SeriesWithoutAs, many)
SELECT v, p FROM generate_series(1, 3) WITH ORDINALITY g(v, p);

-- sgl.query(NumberedRows, many)
SELECT t FROM playlists, unnest(tracks) WITH ORDINALITY AS t(track, position);
