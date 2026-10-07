-- sgl.query(RecordSighting, exec)
INSERT INTO sightings (species, place) VALUES (?, ?);

-- sgl.query(ListSightings, many)
SELECT id, species, place FROM sightings ORDER BY id;
