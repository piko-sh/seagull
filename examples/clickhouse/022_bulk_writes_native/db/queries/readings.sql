-- sgl.query(RecordReadings, batch)
INSERT INTO readings (station, minute, height_cm, note)
VALUES ({station:String}, {minute:UInt16}, {height_cm:UInt16}, {note:Nullable(String)});

-- sgl.query(SummariseReadings, many)
SELECT station, count() AS readings, count(note) AS noted
FROM readings
GROUP BY station
ORDER BY station;

-- sgl.query(CountParts, one)
SELECT uniqExact(_part) AS parts FROM readings;
