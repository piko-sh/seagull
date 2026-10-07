-- sgl.query(RecordSighting, one)
INSERT INTO sightings (species, place) VALUES ($1, $2) RETURNING id;

-- sgl.query(LogSighting, one)
SELECT log_sighting(:species, :place) AS id;

-- sgl.query(ListSightings, many)
SELECT id, sighting_label(species, place) AS label FROM sightings ORDER BY id;

-- sgl.query(CountSightings, one, readonly: false)
SELECT COUNT(*) AS total FROM sightings;

-- sgl.query(ClaimSighting, one)
SELECT id, species FROM sightings WHERE id = $1 FOR UPDATE;

-- sgl.query(ForgetPlace, many)
WITH forgotten AS (DELETE FROM sightings WHERE place = $1 RETURNING id) SELECT id FROM forgotten ORDER BY id;
