-- sgl.query(TripTuples, many)
SELECT tuple(note, id) AS pair, (note, id).2 AS second, map('note', note) AS notes FROM trips;

-- sgl.query(MovedByUnits, many)
SELECT date_add(DAY, 1, started) AS next_day, date_add(HOUR, 1, started) AS next_hour, date_sub(WEEK, 1, started32) AS week_before, dateAdd(MINUTE, 5, started32) AS later, timestamp_add(MILLISECOND, 1, departed) AS nudged, date_add(MONTH, 1, landed) AS next_month, date_add(DAY, 1, returned) AS back FROM trips;

-- sgl.query(MovedByFunctions, many)
SELECT addDays(started, 1) AS a, addHours(started, 1) AS b, subtractMonths(started32, 1) AS c, addSeconds(started32, 1) AS d, addMicroseconds(departed, 1) AS e, addNanoseconds(landed, 1) AS f FROM trips;

-- sgl.query(MovedByIntervals, many)
SELECT started + INTERVAL 1 DAY AS a, started + INTERVAL 1 HOUR AS b, INTERVAL 1 HOUR + started AS c, started32 - INTERVAL 1 MINUTE AS d, landed + INTERVAL 1 MICROSECOND AS e FROM trips;
