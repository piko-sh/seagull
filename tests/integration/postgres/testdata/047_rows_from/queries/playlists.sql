-- sgl.query(TracksBesideRatings, many)
SELECT p.id, x.track::text AS track, x.rating::integer AS rating, x.position
FROM playlists p, LATERAL ROWS FROM (unnest(p.tracks), unnest(p.ratings)) WITH ORDINALITY AS x(track, rating, position)
WHERE p.id = $1
ORDER BY x.position;

-- sgl.query(SeriesBesideSettings, many)
SELECT x.*
FROM ROWS FROM (generate_series(1, 3), jsonb_each('{"mode": "shuffle"}'::jsonb)) WITH ORDINALITY AS x(n, key, value, position)
ORDER BY x.position;

-- sgl.query(TracksAndRatingsUnnested, many)
SELECT u.*
FROM playlists p, unnest(p.tracks, p.ratings) WITH ORDINALITY AS u
WHERE p.id = $1
ORDER BY u.ordinality;

-- sgl.query(OneFunction, many)
SELECT g FROM ROWS FROM (generate_series(1, 2)) AS g ORDER BY g;
