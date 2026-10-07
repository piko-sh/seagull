-- sgl.query(AddLog, exec)
-- $2 as sgl.param(mood_names)
-- $3 as sgl.param(mood_values)
-- $4 as sgl.param(count_moods)
-- $5 as sgl.param(count_values)
-- $6 as sgl.param(note)
-- $7 as sgl.param(feeling)
-- $8 as sgl.param(feelings)
INSERT INTO logs (id, moods, counts, entry, history, entries)
VALUES (
  $1,
  map($2::VARCHAR[], $3::mood[]),
  map($4::mood[], $5::INTEGER[]),
  {'note': $6::VARCHAR, 'feeling': $7::mood},
  {'feelings': $8::mood[]},
  [{'feeling': $7::mood}, {'feeling': 'calm'::mood}]
);

-- sgl.query(AddEmptyLog, exec)
INSERT INTO logs (id) VALUES ($1);

-- sgl.query(GetLog, one)
SELECT id, moods, counts, entry, history, entries FROM logs WHERE id = $1;

-- sgl.query(LogFeelings, one)
SELECT entry.feeling AS feeling, history.feelings AS feelings, moods['morning'] AS morning, map_values(moods) AS mood_values
FROM logs WHERE id = $1;

-- sgl.query(LogsFeeling, many)
SELECT id FROM logs WHERE entry.feeling = $1 ORDER BY id;

-- sgl.query(LogsFeelingByKey, many)
SELECT id FROM logs WHERE entry['feeling'] = $1 ORDER BY id;

-- sgl.query(LogsFeelingExtracted, many)
SELECT id FROM logs WHERE struct_extract(entry, 'feeling') = $1 ORDER BY id;

-- sgl.query(LogsWithMorning, many)
SELECT id FROM logs WHERE moods['morning'] = $1 ORDER BY id;
