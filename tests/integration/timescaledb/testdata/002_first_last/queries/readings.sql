-- sgl.query(name: InsertSensorReading, command: exec)
INSERT INTO sensor_readings (ts, sensor_id, value) VALUES ($1, $2, $3);

-- sgl.query(name: FirstLastBySensor, command: many)
SELECT
    sensor_id,
    first(value, ts) AS first_value,
    last(value, ts) AS last_value
FROM sensor_readings
GROUP BY sensor_id
ORDER BY sensor_id;
