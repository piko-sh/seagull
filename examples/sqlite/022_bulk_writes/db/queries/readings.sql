-- sgl.query(RecordReadings, batch)
INSERT INTO readings (station, minute, height_cm, note) VALUES (?, ?, ?, ?);

-- sgl.query(SummariseReadings, many)
SELECT station, COUNT(*) AS readings, COUNT(note) AS noted
FROM readings
GROUP BY station
ORDER BY station;
