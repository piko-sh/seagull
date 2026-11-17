-- sgl.query(RecordReadings, exec)
INSERT INTO readings (id, name, attributes, flags) VALUES (1, 'gannet', {'wing': 180, 'beak': 10}, {true: 1, false: 0}), (2, 'puffin', {}, {true: 2});

-- sgl.query(Entries, many)
SELECT id, entry FROM readings ARRAY JOIN attributes AS entry ORDER BY id, entry.keys;

-- sgl.query(EntriesInPlace, many)
SELECT id, attributes, attributes.keys AS label, attributes.values AS amount FROM readings LEFT ARRAY JOIN attributes ORDER BY id, label;

-- sgl.query(ShadowingAliases, many)
SELECT id + 1 AS name, name AS again, readings.name AS original FROM readings WHERE name > 1 ORDER BY name;

-- sgl.query(Flags, many)
SELECT id, flags FROM readings ORDER BY id;
