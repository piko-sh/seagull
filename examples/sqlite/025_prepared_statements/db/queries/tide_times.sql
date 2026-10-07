-- sgl.query(AddTideTime, exec)
INSERT INTO tide_times (port, day, high_at) VALUES (?, ?, ?);

-- sgl.query(HighTide, one)
SELECT high_at FROM tide_times WHERE port = ? AND day = ?;
