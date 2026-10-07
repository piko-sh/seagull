-- sgl.query(RecordSighting, exec)
INSERT INTO sightings (species, place) VALUES ($1, $2);

-- sgl.query(ListSightings, many)
SELECT id, species, place FROM sightings ORDER BY id;
