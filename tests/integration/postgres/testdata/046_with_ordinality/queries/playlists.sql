-- sgl.query(TracksInOrder, many)
SELECT p.id, t.track::text AS track, t.position
FROM playlists p, unnest(p.tracks) WITH ORDINALITY AS t(track, position)
WHERE p.id = $1
ORDER BY t.position;

-- sgl.query(OptionalRatings, many)
SELECT p.id, r.rating::integer AS rating, r.ordinality
FROM playlists p LEFT JOIN LATERAL unnest(p.ratings) WITH ORDINALITY r(rating) ON true
ORDER BY p.id, r.ordinality;

-- sgl.query(NumberedSeries, many)
SELECT v, p FROM generate_series(10, 12) WITH ORDINALITY g(v, p) ORDER BY p;
