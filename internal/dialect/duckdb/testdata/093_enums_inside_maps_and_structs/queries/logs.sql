-- sgl.query(name: GetLog, command: one)
SELECT id, moods, counts, entry, history, entries, tagged FROM logs WHERE id = $1;

-- sgl.query(name: LogFeelings, command: one)
SELECT entry.feeling AS feeling, entry['feeling'] AS keyed_feeling, history.feelings AS feelings,
    moods['morning'] AS morning, map_values(moods) AS mood_values
FROM logs WHERE id = $1;

-- sgl.query(name: LogsFeeling, command: many)
SELECT id FROM logs
WHERE entry.feeling = $1 OR entry['feeling'] = $2 OR struct_extract(entry, 'feeling') = $3 OR moods['morning'] = $4;

-- sgl.query(name: AddLog, command: exec)
INSERT INTO logs (id, moods, entry)
VALUES ($1, map($2::VARCHAR[], $3::mood[]), {'note': $4::VARCHAR, 'feeling': $5::mood});

-- sgl.query(name: SetMoods, command: exec)
UPDATE logs SET moods = $1 WHERE id = $2;

-- sgl.query(name: SetCounts, command: exec)
UPDATE logs SET counts = $1 WHERE id = $2;

-- sgl.query(name: SetEntry, command: exec)
UPDATE logs SET entry = $1 WHERE id = $2;

-- sgl.query(name: SetHistory, command: exec)
UPDATE logs SET history = $1 WHERE id = $2;
