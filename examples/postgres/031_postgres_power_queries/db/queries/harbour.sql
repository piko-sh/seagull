-- sgl.query(AddBoat, one)
INSERT INTO boats (name, gear) VALUES ($1, $2) RETURNING id;

-- sgl.query(RecordLanding, exec)
INSERT INTO landings (boat_id, species, kilos, landed_on, notes) VALUES ($1, $2, $3, $4, $5);

-- sgl.query(LatestLandings, many)
SELECT DISTINCT ON (landings.boat_id) boats.name, landings.species, landings.landed_on
FROM landings
JOIN boats ON boats.id = landings.boat_id
ORDER BY landings.boat_id, landings.landed_on DESC, landings.id DESC;

-- sgl.query(BiggestLandings, many)
SELECT boats.name, biggest.species, biggest.kilos
FROM boats
LEFT JOIN LATERAL (
    SELECT species, kilos
    FROM landings
    WHERE landings.boat_id = boats.id
    ORDER BY kilos DESC
    LIMIT 2
) AS biggest ON true
ORDER BY boats.name, biggest.kilos DESC;

-- sgl.query(SearchNotes, many)
SELECT landings.id, landings.notes, ts_rank(landings.search, websearch_to_tsquery('english', :terms)) AS rank
FROM landings
WHERE landings.search @@ websearch_to_tsquery('english', :terms)
ORDER BY rank DESC, landings.id;

-- sgl.query(BoatGear, many)
SELECT boats.name, gear.key, gear.value
FROM boats
CROSS JOIN LATERAL jsonb_each(boats.gear) AS gear
ORDER BY boats.name, gear.key;

-- sgl.query(ArchiveLandings, many)
WITH archived AS (
    DELETE FROM landings
    WHERE landed_on < :before
    RETURNING id, boat_id, species, kilos, landed_on
)
INSERT INTO landing_archive (id, boat_id, species, kilos, landed_on)
SELECT id, boat_id, species, kilos, landed_on FROM archived
RETURNING id, species, landed_on;
