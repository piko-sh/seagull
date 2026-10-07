-- sgl.query(RecordSightings, exec)
INSERT INTO sightings (id, position, legs, tallies) VALUES
    (1, (51.5, -0.12), [('north', 3), ('south', 5)], map('cod', (2, 'many'))),
    (2, (55.9, -3.2), [], map());

-- sgl.query(ListSightings, many)
SELECT id, position, legs, tallies FROM sightings ORDER BY id;

-- sgl.query(BuiltTuples, many)
SELECT id, tuple(id, 'gull') AS tagged, (id + 1, position.1) AS shifted FROM sightings ORDER BY id;
