-- sgl.query(RingGull, exec)
INSERT INTO gulls (ring, nickname) VALUES ($1, $2);

-- sgl.query(RecordSighting, exec)
INSERT INTO sightings (gull_id, seen_on, place) VALUES ($1, $2, $3);

-- sgl.query(ListSightings, many)
SELECT gulls.ring, gulls.nickname, sightings.place
FROM gulls
LEFT JOIN sightings ON sightings.gull_id = gulls.id
ORDER BY gulls.id, sightings.seen_on;

-- sgl.query(SummariseGulls, many)
SELECT
    gulls.ring,
    COALESCE(gulls.nickname, 'ring ' || gulls.ring) AS known_as,
    COUNT(sightings.id) AS times_seen,
    MAX(sightings.seen_on) AS last_seen
FROM gulls
LEFT JOIN sightings ON sightings.gull_id = gulls.id
GROUP BY gulls.id
ORDER BY gulls.id;
