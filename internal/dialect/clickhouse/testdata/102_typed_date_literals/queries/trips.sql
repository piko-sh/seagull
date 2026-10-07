-- sgl.query(name: TripsSince, command: many)
SELECT
    id,
    DATE '2024-01-01' AS epoch_day,
    TIMESTAMP '2024-01-01 00:00:00' AS epoch_time,
    dateDiff('day', started, DATE '2024-12-31') AS days_left
FROM trips
WHERE started >= date '2024-01-01'
  AND departed < timestamp '2025-01-01 00:00:00';
