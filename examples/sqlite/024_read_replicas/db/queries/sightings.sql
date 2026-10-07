-- sgl.query(RecordSighting, one)
INSERT INTO sightings (species, place) VALUES (?, ?) RETURNING id;

-- sgl.query(ListSightings, many)
SELECT id, species, place FROM sightings ORDER BY id;

-- sgl.query(CountSightings, one, readonly: false)
SELECT COUNT(*) AS total FROM sightings;
