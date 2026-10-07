-- sgl.query(RingGull, exec)
INSERT INTO gulls (id, ring, nickname) VALUES ({id:UInt64}, {ring:String}, {nickname:Nullable(String)});

-- sgl.query(RecordSighting, exec)
INSERT INTO sightings (id, gull_id, seen_on, place) VALUES ({id:UInt64}, {gull_id:UInt64}, {seen_on:Date}, {place:String});

-- sgl.query(ListSightings, many)
SELECT gulls.ring, gulls.nickname, sightings.place
FROM gulls
LEFT JOIN sightings ON sightings.gull_id = gulls.id
ORDER BY gulls.id, sightings.seen_on;

-- sgl.query(SummariseGulls, many)
SELECT
    ring,
    coalesce(nickname, concat('ring ', ring)) AS known_as,
    count(seen_on) AS times_seen,
    max(seen_on) AS last_seen
FROM gulls
LEFT JOIN sightings ON sightings.gull_id = gulls.id
GROUP BY gulls.id, ring, nickname
ORDER BY gulls.id
SETTINGS join_use_nulls = 1;
