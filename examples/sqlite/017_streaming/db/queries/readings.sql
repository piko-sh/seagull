-- sgl.query(RecordReading, exec)
INSERT INTO readings (taken_at, level_cm) VALUES (?, ?);

-- sgl.query(StreamReadings, stream)
SELECT taken_at, level_cm FROM readings ORDER BY taken_at;
