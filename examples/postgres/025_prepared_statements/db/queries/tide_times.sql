-- sgl.query(AddTideTime, exec)
INSERT INTO tide_times (port, day, high_at) VALUES ($1, $2, $3);

-- sgl.query(HighTide, one)
SELECT high_at FROM tide_times WHERE port = $1 AND day = $2;
