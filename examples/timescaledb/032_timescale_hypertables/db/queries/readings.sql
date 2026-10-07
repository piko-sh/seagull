-- sgl.query(RecordReading, exec)
INSERT INTO readings (ts, buoy, temperature) VALUES ($1, $2, $3);

-- sgl.query(ListChunks, many)
SELECT show_chunks('readings') AS chunk;

-- sgl.query(HourlyAverages, many)
SELECT time_bucket(INTERVAL '1 hour', ts) AS bucket, avg(temperature) AS mean
FROM readings
WHERE buoy = $1
GROUP BY bucket
ORDER BY bucket;

-- sgl.query(FirstAndLast, many)
SELECT
    buoy,
    first(temperature, ts)::double precision AS first_temperature,
    last(temperature, ts)::double precision AS last_temperature
FROM readings
GROUP BY buoy
ORDER BY buoy;

-- sgl.query(GapfilledHourly, many)
-- $2 as sgl.param(start)
-- $3 as sgl.param(finish)
SELECT
    time_bucket_gapfill(INTERVAL '1 hour', ts, $2::timestamptz, $3::timestamptz) AS bucket,
    avg(temperature) AS mean,
    locf(avg(temperature))::double precision AS carried
FROM readings
WHERE buoy = $1 AND ts >= $2 AND ts < $3
GROUP BY bucket
ORDER BY bucket;
